package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemory_AuthRequestRoundTripAndOneShot(t *testing.T) {
	m := newMemory()
	defer func() { _ = m.Close() }()
	ctx := context.Background()

	want := AuthRequest{ClientID: "c", State: "s", CodeChallenge: "ch"}
	if err := m.SaveAuthRequest(ctx, "k", want, time.Minute); err != nil {
		t.Fatal(err)
	}
	got, err := m.TakeAuthRequest(ctx, "k")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
	// Second take must fail: one-shot.
	if _, err := m.TakeAuthRequest(ctx, "k"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound on second take, got %v", err)
	}
}

func TestMemory_CodeOneShot(t *testing.T) {
	m := newMemory()
	defer func() { _ = m.Close() }()
	ctx := context.Background()

	if err := m.SaveCode(ctx, "code", CodeGrant{AccessToken: "tok"}, time.Minute); err != nil {
		t.Fatal(err)
	}
	g, err := m.TakeCode(ctx, "code")
	if err != nil || g.AccessToken != "tok" {
		t.Fatalf("take = %+v, %v", g, err)
	}
	if _, err := m.TakeCode(ctx, "code"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound (replay), got %v", err)
	}
}

func TestMemory_Expiry(t *testing.T) {
	m := newMemory()
	defer func() { _ = m.Close() }()
	ctx := context.Background()

	_ = m.SaveCode(ctx, "code", CodeGrant{}, -time.Second) // already expired
	if _, err := m.TakeCode(ctx, "code"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected expired entry to be ErrNotFound, got %v", err)
	}
}
