// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package tools

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestCacheKeyIdentityScoping verifies Finding 3: the tfc cache key is partitioned by identity,
// while non-tfc backends ignore identity/org/workspace.
func TestCacheKeyIdentityScoping(t *testing.T) {
	tfc := &StateLoader{backend: "tfc"}
	a := tfc.cacheKey("sessionA", "org", "ws")
	b := tfc.cacheKey("sessionB", "org", "ws")
	if a == b {
		t.Errorf("tfc cache key must differ by identity: %q == %q", a, b)
	}

	local := &StateLoader{backend: "local"}
	if k1, k2 := local.cacheKey("sessionA", "org", "ws"), local.cacheKey("sessionB", "x", "y"); k1 != k2 {
		t.Errorf("non-tfc cache key should not depend on identity/org/workspace: %q vs %q", k1, k2)
	}
}

// TestCacheEvictsOldest verifies Finding 7: when full, the oldest entry by load time is evicted.
func TestCacheEvictsOldest(t *testing.T) {
	c := newStateCache(2, time.Hour)
	base := time.Now()
	c.entries["old"] = &cacheEntry{state: &TerraformState{}, loadedAt: base.Add(-2 * time.Minute)}
	c.entries["mid"] = &cacheEntry{state: &TerraformState{}, loadedAt: base.Add(-1 * time.Minute)}

	// At capacity (2); inserting a third should evict "old".
	c.put("new", &TerraformState{})

	if _, ok := c.entries["old"]; ok {
		t.Errorf("oldest entry was not evicted")
	}
	if _, ok := c.entries["mid"]; !ok {
		t.Errorf("non-oldest entry was wrongly evicted")
	}
	if _, ok := c.entries["new"]; !ok {
		t.Errorf("new entry missing after put")
	}
}

func writeState(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// TestLoadStateFileInRootValid verifies a legitimate in-base file loads.
func TestLoadStateFileInRootValid(t *testing.T) {
	base := t.TempDir()
	writeState(t, filepath.Join(base, "previous.tfstate"), `{"version":4,"serial":7}`)

	state, err := LoadStateFileInRoot(base, "previous.tfstate", 50*1024*1024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.Serial != 7 {
		t.Errorf("wrong serial: %d", state.Serial)
	}
}

// TestLoadStateFileInRootSizeLimit verifies Finding 4: oversized files are rejected.
func TestLoadStateFileInRootSizeLimit(t *testing.T) {
	base := t.TempDir()
	big := make([]byte, 2048)
	for i := range big {
		big[i] = 'x'
	}
	writeState(t, filepath.Join(base, "big.tfstate"), string(big))

	if _, err := LoadStateFileInRoot(base, "big.tfstate", 1024); err == nil {
		t.Errorf("expected size-limit rejection, got nil")
	}
}

// TestLoadStateFileInRootSymlinkEscape verifies Finding 5: a symlink that resolves outside the
// base directory is refused by os.Root, even though the path is lexically contained.
func TestLoadStateFileInRootSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	base := t.TempDir()
	outside := t.TempDir()
	writeState(t, filepath.Join(outside, "secret.tfstate"), `{"version":4,"serial":99}`)

	// base/link -> outside ; lexically "link/secret.tfstate" stays within base.
	if err := os.Symlink(outside, filepath.Join(base, "link")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	rel, absBase, err := safeDiffPath("link/secret.tfstate", base)
	if err != nil {
		t.Fatalf("safeDiffPath rejected a lexically-valid path: %v", err)
	}
	if _, err := LoadStateFileInRoot(absBase, rel, 50*1024*1024); err == nil {
		t.Errorf("expected os.Root to refuse the symlink escape, got nil error")
	}
}
