package k8s

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gwoidciov1 "github.com/kube-argos/kargos/operator/api/v1"
	"github.com/kube-argos/kargos/service/internal/model"
)

func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"lucas@golinux.network": "lucas-golinux-network",
		"a.b+c@x.io":            "a-b-c-x-io",
		"@@@":                   "user",
	}
	for in, want := range cases {
		if got := SanitizeName(in); got != want {
			t.Errorf("SanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func scheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = gwoidciov1.AddToScheme(s)
	return s
}

func TestBinder_UpsertCreatesCR(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(scheme(t)).Build()
	b := NewBinder(c, "ns", "12h", time.Second)

	id := model.Identity{
		Email:  "lucas@golinux.network",
		Domain: "golinux.network",
		Groups: []model.Membership{{Gid: "group/g1", Name: "eng", Domain: "golinux.network"}},
	}
	name, err := b.Upsert(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}

	var cr gwoidciov1.UserAuthenticationBind
	if err := c.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "ns"}, &cr); err != nil {
		t.Fatalf("CR not created: %v", err)
	}
	if cr.Spec.User != name || cr.Spec.Domain != "golinux.network" || len(cr.Spec.Memberships) != 1 {
		t.Fatalf("unexpected spec: %+v", cr.Spec)
	}
}

func TestBinder_WaitBindedReturnsOnBinded(t *testing.T) {
	cr := &gwoidciov1.UserAuthenticationBind{
		ObjectMeta: metav1.ObjectMeta{Name: "lucas", Namespace: "ns"},
		Status:     gwoidciov1.UserAuthenticationBindStatus{Sv: gwoidciov1.ServiceBind{Status: gwoidciov1.BindBinded}},
	}
	c := fake.NewClientBuilder().WithScheme(scheme(t)).
		WithStatusSubresource(&gwoidciov1.UserAuthenticationBind{}).
		WithObjects(cr).Build()
	b := NewBinder(c, "ns", "12h", time.Second)

	if err := b.WaitBinded(context.Background(), "lucas"); err != nil {
		t.Fatalf("expected nil (binded), got %v", err)
	}
}

func TestBinder_WaitBindedFailsOnFailed(t *testing.T) {
	cr := &gwoidciov1.UserAuthenticationBind{
		ObjectMeta: metav1.ObjectMeta{Name: "lucas", Namespace: "ns"},
		Status:     gwoidciov1.UserAuthenticationBindStatus{Sv: gwoidciov1.ServiceBind{Status: gwoidciov1.BindFailed}},
	}
	c := fake.NewClientBuilder().WithScheme(scheme(t)).
		WithStatusSubresource(&gwoidciov1.UserAuthenticationBind{}).
		WithObjects(cr).Build()
	b := NewBinder(c, "ns", "12h", time.Second)

	if err := b.WaitBinded(context.Background(), "lucas"); err == nil {
		t.Fatal("expected error on failed phase")
	}
}
