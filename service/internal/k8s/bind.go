// Package k8s adapts the broker to the cluster: it upserts UserAuthenticationBind
// custom resources and mints ServiceAccount tokens.
package k8s

import (
	"context"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	gwoidciov1 "github.com/kube-argus/kube-argus/operator/api/v1"
	"github.com/kube-argus/kube-argus/service/internal/model"
)

// renewedAtAnnotation is stamped on each login; the operator anchors the bind
// TTL to it so a re-login renews an expired bind.
const renewedAtAnnotation = "kargus.io/renewed-at"

// Binder upserts UserAuthenticationBind CRs and waits for them to bind.
type Binder struct {
	c            client.Client
	namespace    string
	ttl          string
	pollInterval time.Duration
	timeout      time.Duration
}

// NewBinder builds a Binder. timeout bounds WaitBinded.
func NewBinder(c client.Client, namespace, ttl string, timeout time.Duration) *Binder {
	return &Binder{
		c:            c,
		namespace:    namespace,
		ttl:          ttl,
		pollInterval: 500 * time.Millisecond,
		timeout:      timeout,
	}
}

// Upsert creates or updates the CR for an identity and returns its name (which
// is also the ServiceAccount name the operator provisions).
func (b *Binder) Upsert(ctx context.Context, id model.Identity) (string, error) {
	name := SanitizeName(id.Email)
	cr := &gwoidciov1.UserAuthenticationBind{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: b.namespace},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, b.c, cr, func() error {
		// Stamp the renewal time on every login so the operator renews the TTL
		// (sliding window) and re-binds an expired/unbound CR.
		if cr.Annotations == nil {
			cr.Annotations = map[string]string{}
		}
		cr.Annotations[renewedAtAnnotation] = time.Now().UTC().Format(time.RFC3339Nano)
		cr.Spec.TTL = b.ttl
		cr.Spec.Domain = id.Domain
		cr.Spec.User = name
		cr.Spec.Memberships = toCRMemberships(id.Groups)
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("upsert UserAuthenticationBind %s: %w", name, err)
	}
	return name, nil
}

// WaitBinded polls the CR until status.sv.status is binded, returning an error on
// a failed phase or timeout.
//
// ponytail: poll over watch — a few cheap Gets beats wiring an informer for a
// one-shot wait. Switch to a watch if login latency becomes a problem.
func (b *Binder) WaitBinded(ctx context.Context, name string) error {
	ctx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()
	ticker := time.NewTicker(b.pollInterval)
	defer ticker.Stop()
	for {
		var cr gwoidciov1.UserAuthenticationBind
		if err := b.c.Get(ctx, types.NamespacedName{Name: name, Namespace: b.namespace}, &cr); err != nil {
			return fmt.Errorf("get UserAuthenticationBind %s: %w", name, err)
		}
		switch cr.Status.Sv.Status {
		case gwoidciov1.BindBinded:
			return nil
		case gwoidciov1.BindFailed:
			return fmt.Errorf("UserAuthenticationBind %s failed to bind", name)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for %s to bind: %w", name, ctx.Err())
		case <-ticker.C:
		}
	}
}

func toCRMemberships(in []model.Membership) []gwoidciov1.Membership {
	out := make([]gwoidciov1.Membership, 0, len(in))
	for _, m := range in {
		out = append(out, gwoidciov1.Membership{Gid: m.Gid, Name: m.Name, Domain: m.Domain})
	}
	return out
}

// SanitizeName turns an email into a DNS-1123 subdomain usable as a resource name.
func SanitizeName(email string) string {
	s := strings.ToLower(email)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 253 {
		out = strings.Trim(out[:253], "-")
	}
	if out == "" {
		out = "user"
	}
	return out
}
