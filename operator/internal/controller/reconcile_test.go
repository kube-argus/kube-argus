/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	ctrl "sigs.k8s.io/controller-runtime"

	gwoidciov1 "github.com/kube-argus/kube-argus/operator/api/v1"
)

// These tests use the controller-runtime fake client: pure in-memory unit tests
// with no envtest binaries required (unlike the Ginkgo suite in suite_test.go).

const (
	crUID = "test-uid"
	crNS  = "default"
)

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(gwoidciov1.AddToScheme(s))
	return s
}

// newReconciler returns a reconciler backed by a fake client seeded with objs.
func newReconciler(t *testing.T, objs ...client.Object) (*UserAuthenticationBindReconciler, client.Client) {
	t.Helper()
	s := testScheme(t)
	c := fake.NewClientBuilder().
		WithScheme(s).
		WithStatusSubresource(&gwoidciov1.UserAuthenticationBind{}).
		WithObjects(objs...).
		Build()
	return &UserAuthenticationBindReconciler{Client: c, Scheme: s}, c
}

// bindFixture builds a CR pre-seeded with finalizer + pending status so a single
// Reconcile proceeds straight into the bind flow.
func bindFixture(memberships ...string) *gwoidciov1.UserAuthenticationBind {
	var ms []gwoidciov1.Membership
	for _, g := range memberships {
		ms = append(ms, gwoidciov1.Membership{Gid: g, Name: "n", Domain: "d"})
	}
	return &gwoidciov1.UserAuthenticationBind{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "bind",
			Namespace:         crNS,
			UID:               crUID,
			Finalizers:        []string{finalizer},
			CreationTimestamp: metav1.Now(),
		},
		Spec: gwoidciov1.UserAuthenticationBindSpec{
			TTL: "12h", Domain: "kargus.io", User: "admin", Memberships: ms,
		},
		Status: gwoidciov1.UserAuthenticationBindStatus{
			Sv: gwoidciov1.ServiceBind{Status: gwoidciov1.BindPending},
		},
	}
}

func reconcileOnce(t *testing.T, r *UserAuthenticationBindReconciler) ctrl.Result {
	t.Helper()
	res, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "bind", Namespace: crNS},
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	return res
}

func getBind(t *testing.T, c client.Client) *gwoidciov1.UserAuthenticationBind {
	t.Helper()
	var b gwoidciov1.UserAuthenticationBind
	if err := c.Get(context.Background(), types.NamespacedName{Name: "bind", Namespace: crNS}, &b); err != nil {
		t.Fatalf("get CR: %v", err)
	}
	return &b
}

func clusterRole(name, group string) *rbacv1.ClusterRole {
	cr := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: name}}
	if group != "" {
		cr.Annotations = map[string]string{groupAnnotation: group}
	}
	return cr
}

func role(name, ns, group string) *rbacv1.Role {
	r := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
	if group != "" {
		r.Annotations = map[string]string{groupAnnotation: group}
	}
	return r
}

func TestReconcile_AddsFinalizer(t *testing.T) {
	cr := &gwoidciov1.UserAuthenticationBind{
		ObjectMeta: metav1.ObjectMeta{Name: "bind", Namespace: crNS, UID: crUID, CreationTimestamp: metav1.Now()},
		Spec:       gwoidciov1.UserAuthenticationBindSpec{TTL: "12h", Domain: "d", User: "admin"},
	}
	r, c := newReconciler(t, cr)

	reconcileOnce(t, r)

	if !hasFinalizer(getBind(t, c)) {
		t.Fatal("expected finalizer to be added")
	}
}

func TestReconcile_PendingThenBinds(t *testing.T) {
	// Plain CR (no pre-seeded status) drives the full create path over reconciles.
	cr := &gwoidciov1.UserAuthenticationBind{
		ObjectMeta: metav1.ObjectMeta{Name: "bind", Namespace: crNS, UID: crUID, CreationTimestamp: metav1.Now()},
		Spec:       gwoidciov1.UserAuthenticationBindSpec{TTL: "12h", Domain: "d", User: "admin"},
	}
	r, c := newReconciler(t, cr, clusterRole("admin", ""))

	// 1: finalizer, 2: pending, 3: bind.
	reconcileOnce(t, r) // adds finalizer
	res := reconcileOnce(t, r)
	if !res.Requeue {
		t.Fatal("expected requeue after pending")
	}
	if got := getBind(t, c).Status.Sv.Status; got != gwoidciov1.BindPending {
		t.Fatalf("phase = %q, want pending", got)
	}
	reconcileOnce(t, r) // bind
	if got := getBind(t, c).Status.Sv.Status; got != gwoidciov1.BindBinded {
		t.Fatalf("phase = %q, want binded", got)
	}
}

func TestReconcile_FullBind(t *testing.T) {
	cr := bindFixture("group/g123")
	r, c := newReconciler(t, cr,
		clusterRole("editor", "group/g123"), // match -> ClusterRoleBinding
		clusterRole("viewer", "group/other"), // no match
		clusterRole("admin", ""),             // no annotation
		role("dev", "team", "group/g123"),    // match -> RoleBinding in team
		role("ops", "team", ""),              // no annotation
	)

	reconcileOnce(t, r)

	// ServiceAccount created in CR namespace.
	var sa corev1.ServiceAccount
	if err := c.Get(context.Background(), types.NamespacedName{Name: "admin", Namespace: crNS}, &sa); err != nil {
		t.Fatalf("expected ServiceAccount admin: %v", err)
	}

	// Exactly one ClusterRoleBinding, for the matching role.
	var crbs rbacv1.ClusterRoleBindingList
	if err := c.List(context.Background(), &crbs, client.MatchingLabels{ownerLabel: crUID}); err != nil {
		t.Fatal(err)
	}
	if len(crbs.Items) != 1 {
		t.Fatalf("ClusterRoleBindings = %d, want 1", len(crbs.Items))
	}
	crb := crbs.Items[0]
	if crb.RoleRef.Name != "editor" || crb.RoleRef.Kind != "ClusterRole" {
		t.Fatalf("roleRef = %+v", crb.RoleRef)
	}
	if len(crb.Subjects) != 1 || crb.Subjects[0].Name != "admin" || crb.Subjects[0].Kind != "ServiceAccount" {
		t.Fatalf("subjects = %+v", crb.Subjects)
	}

	// Exactly one RoleBinding, in team namespace.
	var rbs rbacv1.RoleBindingList
	if err := c.List(context.Background(), &rbs, client.MatchingLabels{ownerLabel: crUID}); err != nil {
		t.Fatal(err)
	}
	if len(rbs.Items) != 1 {
		t.Fatalf("RoleBindings = %d, want 1", len(rbs.Items))
	}
	if rbs.Items[0].Namespace != "team" || rbs.Items[0].RoleRef.Name != "dev" {
		t.Fatalf("rolebinding = %+v", rbs.Items[0])
	}

	b := getBind(t, c)
	if b.Status.Sv.Status != gwoidciov1.BindBinded {
		t.Fatalf("phase = %q, want binded", b.Status.Sv.Status)
	}
	if !readyTrue(b) {
		t.Fatal("expected Ready=True")
	}
}

func TestReconcile_BindedSettles(t *testing.T) {
	// Reaching binded then reconciling again must NOT write status (no flip to
	// binding, no resourceVersion bump) — otherwise it loops forever.
	cr := bindFixture("group/g123")
	r, c := newReconciler(t, cr, clusterRole("editor", "group/g123"))

	reconcileOnce(t, r) // -> binded
	b1 := getBind(t, c)
	if b1.Status.Sv.Status != gwoidciov1.BindBinded {
		t.Fatalf("phase = %q, want binded", b1.Status.Sv.Status)
	}
	rv := b1.ResourceVersion

	reconcileOnce(t, r) // steady state
	b2 := getBind(t, c)
	if b2.Status.Sv.Status != gwoidciov1.BindBinded {
		t.Fatalf("phase flipped to %q on steady reconcile", b2.Status.Sv.Status)
	}
	if b2.ResourceVersion != rv {
		t.Fatalf("steady reconcile wrote status (loop): resourceVersion %s -> %s", rv, b2.ResourceVersion)
	}
}

func TestReconcile_PrunesStaleBindings(t *testing.T) {
	// Membership empty -> previously-owned binding must be pruned.
	stale := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "kargus-test-uid-old", Labels: map[string]string{ownerLabel: crUID}},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacAPIGroup, Kind: "ClusterRole", Name: "old"},
	}
	r, c := newReconciler(t, bindFixture(), clusterRole("editor", "group/g123"), stale)

	reconcileOnce(t, r)

	var crbs rbacv1.ClusterRoleBindingList
	if err := c.List(context.Background(), &crbs, client.MatchingLabels{ownerLabel: crUID}); err != nil {
		t.Fatal(err)
	}
	if len(crbs.Items) != 0 {
		t.Fatalf("ClusterRoleBindings = %d, want 0 (pruned)", len(crbs.Items))
	}
}

func TestReconcile_InvalidTTL(t *testing.T) {
	cr := bindFixture("group/g123")
	cr.Spec.TTL = "notaduration"
	r, c := newReconciler(t, cr)

	reconcileOnce(t, r)

	b := getBind(t, c)
	if b.Status.Sv.Status != gwoidciov1.BindFailed {
		t.Fatalf("phase = %q, want failed", b.Status.Sv.Status)
	}
}

func TestReconcile_ExpiredUnbinds(t *testing.T) {
	cr := bindFixture("group/g123")
	cr.Spec.TTL = "1s"
	cr.CreationTimestamp = metav1.NewTime(time.Now().Add(-time.Hour))
	owned := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "kargus-test-uid-editor", Labels: map[string]string{ownerLabel: crUID}},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacAPIGroup, Kind: "ClusterRole", Name: "editor"},
	}
	r, c := newReconciler(t, cr, owned)

	reconcileOnce(t, r)

	b := getBind(t, c)
	if b.Status.Sv.Status != gwoidciov1.BindUnbound {
		t.Fatalf("phase = %q, want unbound", b.Status.Sv.Status)
	}
	var crbs rbacv1.ClusterRoleBindingList
	_ = c.List(context.Background(), &crbs, client.MatchingLabels{ownerLabel: crUID})
	if len(crbs.Items) != 0 {
		t.Fatalf("expected owned bindings revoked, got %d", len(crbs.Items))
	}
}

func TestReconcile_RenewedAnnotationRebindsExpired(t *testing.T) {
	// Old creation time (would be expired) but a fresh renewed-at stamp from a
	// re-login: the bind must renew, not stay unbound.
	cr := bindFixture("group/g123")
	cr.Spec.TTL = "12h"
	cr.CreationTimestamp = metav1.NewTime(time.Now().Add(-48 * time.Hour))
	cr.Annotations = map[string]string{renewedAtAnnotation: time.Now().UTC().Format(time.RFC3339)}
	cr.Status.Sv.Status = gwoidciov1.BindUnbound
	r, c := newReconciler(t, cr, clusterRole("editor", "group/g123"))

	reconcileOnce(t, r)

	if got := getBind(t, c).Status.Sv.Status; got != gwoidciov1.BindBinded {
		t.Fatalf("phase = %q, want binded (renewed)", got)
	}
}

func TestReconcile_DeleteCleansBindings(t *testing.T) {
	now := metav1.Now()
	cr := bindFixture("group/g123")
	cr.DeletionTimestamp = &now
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "kargus-test-uid-editor", Labels: map[string]string{ownerLabel: crUID}},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacAPIGroup, Kind: "ClusterRole", Name: "editor"},
	}
	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "kargus-test-uid-dev", Namespace: "team", Labels: map[string]string{ownerLabel: crUID}},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacAPIGroup, Kind: "Role", Name: "dev"},
	}
	r, c := newReconciler(t, cr, crb, rb)

	reconcileOnce(t, r)

	var crbs rbacv1.ClusterRoleBindingList
	_ = c.List(context.Background(), &crbs, client.MatchingLabels{ownerLabel: crUID})
	var rbs rbacv1.RoleBindingList
	_ = c.List(context.Background(), &rbs, client.MatchingLabels{ownerLabel: crUID})
	if len(crbs.Items) != 0 || len(rbs.Items) != 0 {
		t.Fatalf("expected all bindings deleted, got crb=%d rb=%d", len(crbs.Items), len(rbs.Items))
	}
}

func TestBindingName(t *testing.T) {
	if got := bindingName("uid", "admin"); got != "kargus-uid-admin" {
		t.Fatalf("bindingName = %q", got)
	}
}

func hasFinalizer(b *gwoidciov1.UserAuthenticationBind) bool {
	for _, f := range b.Finalizers {
		if f == finalizer {
			return true
		}
	}
	return false
}

func readyTrue(b *gwoidciov1.UserAuthenticationBind) bool {
	c := apimeta.FindStatusCondition(b.Status.Conditions, conditionReady)
	return c != nil && c.Status == metav1.ConditionTrue
}

func TestReconcile_NotFoundIsNoOp(t *testing.T) {
	r, _ := newReconciler(t)
	if _, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "missing", Namespace: crNS},
	}); err != nil {
		t.Fatalf("expected nil error for missing CR, got %v", err)
	}
}
