// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package tools

import (
	"path/filepath"
	"testing"
)

// TestSafeDiffPath verifies Finding 5 lexical containment and extension rules.
func TestSafeDiffPath(t *testing.T) {
	base := t.TempDir()

	cases := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{"valid relative", "previous.tfstate", false},
		{"valid nested", "snapshots/previous.tfstate", false},
		{"traversal escape", "../../etc/passwd.tfstate", true},
		{"absolute outside", "/etc/passwd.tfstate", true},
		{"wrong extension", "previous.json", true},
		{"sibling dotdot prefix not escape", "..config/previous.tfstate", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rel, absBase, err := safeDiffPath(tc.raw, base)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got rel=%q", tc.raw, rel)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.raw, err)
			}
			if absBase == "" {
				t.Errorf("expected non-empty absBase")
			}
			// Resolved path must stay within base.
			full := filepath.Join(absBase, rel)
			if r, _ := filepath.Rel(absBase, full); r == ".." || filepath.IsAbs(rel) {
				t.Errorf("path %q escaped base: rel=%q", tc.raw, rel)
			}
		})
	}
}

// TestSafeDiffPathAbsoluteInside verifies an absolute path inside the base dir is accepted.
func TestSafeDiffPathAbsoluteInside(t *testing.T) {
	base := t.TempDir()
	abs := filepath.Join(base, "previous.tfstate")
	rel, absBase, err := safeDiffPath(abs, base)
	if err != nil {
		t.Fatalf("absolute-inside path rejected: %v", err)
	}
	if filepath.Join(absBase, rel) != filepath.Clean(abs) {
		t.Errorf("resolved path mismatch: got %q want %q", filepath.Join(absBase, rel), abs)
	}
}
