package oidc

import "testing"

func TestParseGroups(t *testing.T) {
	got := ParseGroups([]any{"group/g1", "engineering", "", 42}, "kargus.io")
	if len(got) != 2 {
		t.Fatalf("expected 2 groups (skip empty + non-string), got %d: %+v", len(got), got)
	}
	if got[0].Gid != "group/g1" || got[0].Domain != "kargus.io" {
		t.Fatalf("unexpected first group: %+v", got[0])
	}
	if ParseGroups("not-an-array", "d") != nil {
		t.Fatal("expected nil for non-array claim")
	}
	if ParseGroups(nil, "d") != nil {
		t.Fatal("expected nil for missing claim")
	}
}

func TestDomainOf(t *testing.T) {
	if DomainOf("a@b.com") != "b.com" {
		t.Fatal("want b.com")
	}
	if DomainOf("no-at") != "" {
		t.Fatal("want empty for no @")
	}
}
