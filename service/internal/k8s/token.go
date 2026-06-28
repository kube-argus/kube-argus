package k8s

import (
	"context"
	"fmt"

	authnv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/kube-argos/kargos/service/internal/model"
)

// Minter mints short-lived ServiceAccount tokens via the TokenRequest API.
type Minter struct {
	clientset      kubernetes.Interface
	namespace      string
	audiences      []string
	expirationSecs int64
}

// NewMinter builds a Minter for SAs in namespace with the given token audiences.
func NewMinter(cs kubernetes.Interface, namespace string, audiences []string, expirationSecs int64) *Minter {
	return &Minter{clientset: cs, namespace: namespace, audiences: audiences, expirationSecs: expirationSecs}
}

// Mint requests a token for the named ServiceAccount.
func (m *Minter) Mint(ctx context.Context, saName string) (model.Token, error) {
	tr := &authnv1.TokenRequest{
		Spec: authnv1.TokenRequestSpec{
			Audiences:         m.audiences,
			ExpirationSeconds: &m.expirationSecs,
		},
	}
	res, err := m.clientset.CoreV1().ServiceAccounts(m.namespace).
		CreateToken(ctx, saName, tr, metav1.CreateOptions{})
	if err != nil {
		return model.Token{}, fmt.Errorf("tokenrequest for %s: %w", saName, err)
	}
	return model.Token{
		AccessToken: res.Status.Token,
		ExpiresIn:   int(m.expirationSecs),
	}, nil
}
