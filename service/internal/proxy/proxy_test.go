package proxy

import (
	"context"
	"testing"
	"time"

	"github.com/kube-argos/kargos/service/internal/model"
)

type minterFunc func(ctx context.Context, sa string) (model.Token, error)

func (f minterFunc) Mint(ctx context.Context, sa string) (model.Token, error) { return f(ctx, sa) }

func TestTokenCache_ReusesUntilTTL(t *testing.T) {
	calls := 0
	m := minterFunc(func(_ context.Context, _ string) (model.Token, error) {
		calls++
		return model.Token{AccessToken: "tok"}, nil
	})

	c := newTokenCache(time.Hour)
	if _, err := c.token(context.Background(), "lucas", m); err != nil {
		t.Fatal(err)
	}
	if _, err := c.token(context.Background(), "lucas", m); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 mint (cached), got %d", calls)
	}
	// Different SA mints separately.
	_, _ = c.token(context.Background(), "other", m)
	if calls != 2 {
		t.Fatalf("expected 2 mints for distinct SAs, got %d", calls)
	}
}

func TestTokenCache_RemintsAfterExpiry(t *testing.T) {
	calls := 0
	m := minterFunc(func(_ context.Context, _ string) (model.Token, error) {
		calls++
		return model.Token{AccessToken: "tok"}, nil
	})
	c := newTokenCache(-time.Second) // already expired
	_, _ = c.token(context.Background(), "lucas", m)
	_, _ = c.token(context.Background(), "lucas", m)
	if calls != 2 {
		t.Fatalf("expected re-mint on expiry, got %d calls", calls)
	}
}
