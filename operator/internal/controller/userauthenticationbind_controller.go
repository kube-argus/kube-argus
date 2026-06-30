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
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	gwoidciov1 "github.com/kube-argus/kube-argus/operator/api/v1"
)

const (
	// finalizer ensures owned RBAC bindings are removed before the CR is gone.
	finalizer = "kargus.io/finalizer"
	// groupAnnotation tags a (Cluster)Role with the group it grants.
	groupAnnotation = "rbac.kargus.io/group"
	// renewedAtAnnotation is stamped by the broker on each login; the TTL is
	// anchored to it (sliding window) so a re-login renews an expired bind.
	renewedAtAnnotation = "kargus.io/renewed-at"
	// ownerLabel marks bindings created for a given CR (by UID) for prune/cleanup.
	ownerLabel = "kargus.io/owned-by"
	// conditionReady is the status condition type reported on the CR.
	conditionReady = "Ready"

	rbacAPIGroup = "rbac.authorization.k8s.io"
)

// UserAuthenticationBindReconciler reconciles a UserAuthenticationBind object
type UserAuthenticationBindReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=kargus.io,resources=userauthenticationbinds,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kargus.io,resources=userauthenticationbinds/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kargus.io,resources=userauthenticationbinds/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;roles,verbs=get;list;watch;bind
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings;rolebindings,verbs=get;list;watch;create;delete

// Reconcile drives a UserAuthenticationBind to its desired state: a per-user
// ServiceAccount plus (Cluster)RoleBindings for every (Cluster)Role annotated
// with a group that is in spec.memberships. Runs on create and update.
func (r *UserAuthenticationBindReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var bind gwoidciov1.UserAuthenticationBind
	if err := r.Get(ctx, req.NamespacedName, &bind); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !bind.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &bind)
	}

	if controllerutil.AddFinalizer(&bind, finalizer) {
		if err := r.Update(ctx, &bind); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
		return ctrl.Result{}, nil
	}

	// Baseline for status merge-patches, captured before any status mutation: a
	// reconcile that changes nothing then yields an empty patch (no event).
	orig := bind.DeepCopy()

	// pending: first observation only, so the phase is visible before sync.
	if bind.Status.Sv.Status == "" {
		if err := r.updatePhase(ctx, orig, &bind, gwoidciov1.BindPending,
			metav1.ConditionUnknown, "Accepted", "Bind accepted, pending sync"); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	ttl, err := time.ParseDuration(bind.Spec.TTL)
	if err != nil {
		return ctrl.Result{}, r.updatePhase(ctx, orig, &bind, gwoidciov1.BindFailed,
			metav1.ConditionFalse, "InvalidTTL", err.Error())
	}
	// Anchor the TTL to the broker's renewal stamp so a re-login (which refreshes
	// the annotation) renews the bind; fall back to creation time if absent.
	anchor := bind.CreationTimestamp.Time
	if v := bind.Annotations[renewedAtAnnotation]; v != "" {
		if t, perr := time.Parse(time.RFC3339, v); perr == nil {
			anchor = t
		}
	}
	// Truncate to seconds: status.sv.expiresAt is stored at second precision, so a
	// sub-second expiry would diff against the stored value on every reconcile.
	expiry := anchor.Add(ttl).Truncate(time.Second)

	// expired: revoke access by deleting owned bindings, mark unbound.
	if !time.Now().Before(expiry) {
		if bind.Status.Sv.Status != gwoidciov1.BindUnbound {
			if err := r.deleteOwned(ctx, string(bind.UID)); err != nil {
				return ctrl.Result{}, err
			}
			log.Info("Unbound expired UserAuthenticationBind", "user", bind.Spec.User)
			return ctrl.Result{}, r.updatePhase(ctx, orig, &bind, gwoidciov1.BindUnbound,
				metav1.ConditionFalse, "Expired", "TTL elapsed")
		}
		return ctrl.Result{}, nil
	}

	prevStatus := bind.Status.Sv.Status

	// Ensure the per-user ServiceAccount (GC'd via owner reference on delete).
	sa, err := r.ensureServiceAccount(ctx, &bind)
	if err != nil {
		return ctrl.Result{}, r.updatePhase(ctx, orig, &bind, gwoidciov1.BindFailed,
			metav1.ConditionFalse, "ServiceAccountFailed", err.Error())
	}

	// binding: transient phase, ONLY when (re)binding from a non-binded state.
	// Writing it on an already-binded CR flips binded->binding->binded, and each
	// status write re-triggers the watch -> infinite reconcile loop.
	if prevStatus != gwoidciov1.BindBinded {
		if prevStatus == gwoidciov1.BindUnbound {
			log.Info("Renewing UserAuthenticationBind", "user", bind.Spec.User, "expiry", expiry)
		}
		bind.Status.Sv.Ref = string(sa.UID)
		if err := r.updatePhase(ctx, orig, &bind, gwoidciov1.BindBinding,
			metav1.ConditionUnknown, "Syncing", "Syncing RBAC bindings"); err != nil {
			return ctrl.Result{}, err
		}
	}

	if err := r.syncBindings(ctx, &bind, sa); err != nil {
		return ctrl.Result{}, r.updatePhase(ctx, orig, &bind, gwoidciov1.BindFailed,
			metav1.ConditionFalse, "BindFailed", err.Error())
	}

	// Converge to binded. If it is already established with the same ref/expiry,
	// skip the status write entirely so the resource settles (no event, no loop).
	ref := string(sa.UID)
	established := prevStatus == gwoidciov1.BindBinded &&
		bind.Status.Sv.Ref == ref &&
		bind.Status.Sv.ExpiresAt != nil && bind.Status.Sv.ExpiresAt.Time.Equal(expiry)
	if !established {
		bind.Status.Sv.Ref = ref
		bind.Status.Sv.ExpiresAt = &metav1.Time{Time: expiry}
		if err := r.updatePhase(ctx, orig, &bind, gwoidciov1.BindBinded, metav1.ConditionTrue, "Bound",
			fmt.Sprintf("User %s bound for %d membership(s)", bind.Spec.User, len(bind.Spec.Memberships))); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{RequeueAfter: time.Until(expiry)}, nil
}

// ensureServiceAccount creates/updates the SA named after spec.user in the CR
// namespace, owned by the CR for garbage collection.
//
// ponytail: rename leaks old SA; add old-SA cleanup if spec.user mutation is a real case.
func (r *UserAuthenticationBindReconciler) ensureServiceAccount(
	ctx context.Context, bind *gwoidciov1.UserAuthenticationBind,
) (*corev1.ServiceAccount, error) {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: bind.Spec.User, Namespace: bind.Namespace},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, sa, func() error {
		return controllerutil.SetControllerReference(bind, sa, r.Scheme)
	})
	return sa, err
}

// syncBindings reconciles owned (Cluster)RoleBindings to the desired set: bind
// every annotated (Cluster)Role whose group is in spec.memberships, prune the rest.
func (r *UserAuthenticationBindReconciler) syncBindings(
	ctx context.Context, bind *gwoidciov1.UserAuthenticationBind, sa *corev1.ServiceAccount,
) error {
	groups := make(map[string]bool, len(bind.Spec.Memberships))
	for _, m := range bind.Spec.Memberships {
		groups[m.Gid] = true
	}
	cruid := string(bind.UID)
	subject := rbacv1.Subject{Kind: "ServiceAccount", Name: sa.Name, Namespace: sa.Namespace}

	// ClusterRoles -> ClusterRoleBindings (cluster-scoped, can't owner-ref a namespaced CR).
	var clusterRoles rbacv1.ClusterRoleList
	if err := r.List(ctx, &clusterRoles); err != nil {
		return fmt.Errorf("listing ClusterRoles: %w", err)
	}
	desiredCRB := map[string]bool{}
	for _, role := range clusterRoles.Items {
		if !groupMatches(groups, role.Annotations) {
			continue
		}
		name := bindingName(cruid, role.Name)
		desiredCRB[name] = true
		crb := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{ownerLabel: cruid}},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacAPIGroup, Kind: "ClusterRole", Name: role.Name},
			Subjects:   []rbacv1.Subject{subject},
		}
		if err := r.Create(ctx, crb); err != nil && !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("creating ClusterRoleBinding %s: %w", name, err)
		}
	}
	var existingCRB rbacv1.ClusterRoleBindingList
	if err := r.List(ctx, &existingCRB, client.MatchingLabels{ownerLabel: cruid}); err != nil {
		return fmt.Errorf("listing owned ClusterRoleBindings: %w", err)
	}
	for i := range existingCRB.Items {
		if !desiredCRB[existingCRB.Items[i].Name] {
			if err := r.Delete(ctx, &existingCRB.Items[i]); err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("pruning ClusterRoleBinding: %w", err)
			}
		}
	}

	// Roles (all namespaces) -> RoleBindings in the role's namespace.
	var roles rbacv1.RoleList
	if err := r.List(ctx, &roles); err != nil {
		return fmt.Errorf("listing Roles: %w", err)
	}
	desiredRB := map[string]bool{}
	for _, role := range roles.Items {
		if !groupMatches(groups, role.Annotations) {
			continue
		}
		name := bindingName(cruid, role.Name)
		desiredRB[role.Namespace+"/"+name] = true
		rb := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: role.Namespace, Labels: map[string]string{ownerLabel: cruid}},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacAPIGroup, Kind: "Role", Name: role.Name},
			Subjects:   []rbacv1.Subject{subject},
		}
		if err := r.Create(ctx, rb); err != nil && !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("creating RoleBinding %s/%s: %w", role.Namespace, name, err)
		}
	}
	var existingRB rbacv1.RoleBindingList
	if err := r.List(ctx, &existingRB, client.MatchingLabels{ownerLabel: cruid}); err != nil {
		return fmt.Errorf("listing owned RoleBindings: %w", err)
	}
	for i := range existingRB.Items {
		b := &existingRB.Items[i]
		if !desiredRB[b.Namespace+"/"+b.Name] {
			if err := r.Delete(ctx, b); err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("pruning RoleBinding: %w", err)
			}
		}
	}
	return nil
}

// reconcileDelete removes owned bindings (no GC for cluster/cross-ns) then the finalizer.
func (r *UserAuthenticationBindReconciler) reconcileDelete(
	ctx context.Context, bind *gwoidciov1.UserAuthenticationBind,
) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(bind, finalizer) {
		return ctrl.Result{}, nil
	}
	if err := r.deleteOwned(ctx, string(bind.UID)); err != nil {
		return ctrl.Result{}, err
	}
	controllerutil.RemoveFinalizer(bind, finalizer)
	if err := r.Update(ctx, bind); err != nil {
		return ctrl.Result{}, fmt.Errorf("removing finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

// deleteOwned deletes every ClusterRoleBinding/RoleBinding labelled for this CR.
func (r *UserAuthenticationBindReconciler) deleteOwned(ctx context.Context, cruid string) error {
	var crbs rbacv1.ClusterRoleBindingList
	if err := r.List(ctx, &crbs, client.MatchingLabels{ownerLabel: cruid}); err != nil {
		return fmt.Errorf("listing owned ClusterRoleBindings: %w", err)
	}
	for i := range crbs.Items {
		if err := r.Delete(ctx, &crbs.Items[i]); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("deleting ClusterRoleBinding: %w", err)
		}
	}
	var rbs rbacv1.RoleBindingList
	if err := r.List(ctx, &rbs, client.MatchingLabels{ownerLabel: cruid}); err != nil {
		return fmt.Errorf("listing owned RoleBindings: %w", err)
	}
	for i := range rbs.Items {
		if err := r.Delete(ctx, &rbs.Items[i]); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("deleting RoleBinding: %w", err)
		}
	}
	return nil
}

// updatePhase sets status.sv.status + the Ready condition and persists status as
// a merge-patch against orig (the object as fetched at the start of the reconcile).
//
// Patching against the reconcile-start baseline — rather than re-snapshotting on
// every call — means (a) all status fields mutated this reconcile (Ref, ExpiresAt)
// are included in the patch, and (b) a reconcile that changes nothing produces an
// EMPTY patch: no resourceVersion bump, no watch event, so the CR settles instead
// of looping. Merge-patch (not Update) also avoids the optimistic-concurrency race
// with the broker rewriting spec.
func (r *UserAuthenticationBindReconciler) updatePhase(
	ctx context.Context, orig, bind *gwoidciov1.UserAuthenticationBind,
	phase gwoidciov1.BindPhase, condStatus metav1.ConditionStatus, reason, msg string,
) error {
	bind.Status.Sv.Status = phase
	apimeta.SetStatusCondition(&bind.Status.Conditions, metav1.Condition{
		Type: conditionReady, Status: condStatus, Reason: reason, Message: msg,
	})
	if err := r.Status().Patch(ctx, bind, client.MergeFrom(orig)); err != nil {
		return fmt.Errorf("updating status: %w", err)
	}
	return nil
}

// groupMatches reports whether annotations carry a group that is in the set.
func groupMatches(groups map[string]bool, annotations map[string]string) bool {
	g, ok := annotations[groupAnnotation]
	return ok && groups[g]
}

// bindingName is the deterministic, idempotent name for a CR's binding to a role.
func bindingName(cruid, role string) string {
	return fmt.Sprintf("kargus-%s-%s", cruid, role)
}

// SetupWithManager sets up the controller with the Manager.
func (r *UserAuthenticationBindReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gwoidciov1.UserAuthenticationBind{}).
		Named("userauthenticationbind").
		Complete(r)
}
