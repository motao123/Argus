//go:build !windows

package task

import "testing"

func TestEqualSHA256(t *testing.T) {
	if !equalSHA256(" AABB ", "aabb") {
		t.Fatal("expected case-insensitive trimmed match")
	}
	if equalSHA256("aa", "bb") {
		t.Fatal("unexpected match")
	}
}

func TestShellQuote(t *testing.T) {
	got := shellQuote("a'b")
	if got != `'a'\''b'` {
		t.Fatalf("shellQuote = %q", got)
	}
}
