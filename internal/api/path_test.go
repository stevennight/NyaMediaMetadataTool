package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestContainsParentTraversal(t *testing.T) {
	for _, path := range []string{`..\outside`, `child\..\outside`, "../outside", "child/../outside"} {
		if !containsParentTraversal(path) {
			t.Fatalf("expected parent traversal to be rejected: %q", path)
		}
	}
	if containsParentTraversal(filepath.Join("child", "season")) {
		t.Fatal("normal child path should not contain parent traversal")
	}
}

func TestPathWithinRoot(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "show", "season")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()

	if !pathWithinRoot(child, root) {
		t.Fatal("expected child path inside root")
	}
	if pathWithinRoot(outside, root) {
		t.Fatal("expected outside path to be rejected")
	}
}
