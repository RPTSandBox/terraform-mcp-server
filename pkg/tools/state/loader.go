// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/terraform-mcp-server/pkg/client"
	log "github.com/sirupsen/logrus"
)

const (
	defaultCacheTTL      = 5 * time.Minute
	defaultCacheMaxSize  = 500
	defaultStateMaxBytes = int64(50 * 1024 * 1024) // 50 MB
	subprocessTimeout    = 30 * time.Second
)

// --- cache ---

type cacheEntry struct {
	state    *TerraformState
	loadedAt time.Time
}

type stateCache struct {
	mu      sync.RWMutex
	entries map[string]*cacheEntry
	maxSize int
	ttl     time.Duration
}

func newStateCache(maxSize int, ttl time.Duration) *stateCache {
	return &stateCache{
		entries: make(map[string]*cacheEntry),
		maxSize: maxSize,
		ttl:     ttl,
	}
}

func (c *stateCache) get(key string) *TerraformState {
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok {
		return nil
	}
	if time.Since(entry.loadedAt) >= c.ttl {
		c.mu.Lock()
		delete(c.entries, key)
		c.mu.Unlock()
		return nil
	}
	return entry.state
}

func (c *stateCache) put(key string, state *TerraformState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= c.maxSize {
		now := time.Now()
		for k, e := range c.entries {
			if now.Sub(e.loadedAt) >= c.ttl {
				delete(c.entries, k)
			}
		}
		if len(c.entries) >= c.maxSize {
			for k := range c.entries {
				delete(c.entries, k)
				break
			}
		}
	}
	c.entries[key] = &cacheEntry{state: state, loadedAt: time.Now()}
}

func (c *stateCache) invalidate(key string) {
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
}

// --- StateLoader ---

// StateLoader loads Terraform state from a configured backend with per-workspace TTL caching.
type StateLoader struct {
	backend          string
	statePath        string
	bucket           string
	key              string
	prefix           string
	diffBaseDir      string
	maxBytes         int64
	sensitivePattern *regexp.Regexp
	cache            *stateCache
}

var (
	loaderOnce   sync.Once
	globalLoader *StateLoader
)

// GetLoader returns the package-level StateLoader singleton, initialized from env vars.
func GetLoader() *StateLoader {
	loaderOnce.Do(func() {
		globalLoader = newStateLoader()
	})
	return globalLoader
}

func newStateLoader() *StateLoader {
	backend := strings.ToLower(os.Getenv("TF_STATE_BACKEND"))
	if backend == "" {
		backend = "local"
	}

	maxBytes := defaultStateMaxBytes
	if s := os.Getenv("TF_STATE_MAX_SIZE_MB"); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 {
			maxBytes = n * 1024 * 1024
		}
	}

	maxCacheSize := defaultCacheMaxSize
	if s := os.Getenv("TF_STATE_CACHE_MAX_WORKSPACES"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			maxCacheSize = n
		}
	}

	var sensitivePattern *regexp.Regexp
	if p := os.Getenv("TF_SENSITIVE_ATTR_PATTERN"); p != "" {
		if re, err := regexp.Compile("(?i)" + p); err == nil {
			sensitivePattern = re
		}
	}

	statePath := os.Getenv("TF_STATE_PATH")
	if statePath == "" {
		statePath = "terraform.tfstate"
	}

	diffBaseDir := os.Getenv("TF_STATE_DIFF_BASE_DIR")
	if diffBaseDir == "" {
		diffBaseDir = "."
	}

	return &StateLoader{
		backend:          backend,
		statePath:        statePath,
		bucket:           os.Getenv("TF_STATE_BUCKET"),
		key:              os.Getenv("TF_STATE_KEY"),
		prefix:           os.Getenv("TF_STATE_PREFIX"),
		diffBaseDir:      diffBaseDir,
		maxBytes:         maxBytes,
		sensitivePattern: sensitivePattern,
		cache:            newStateCache(maxCacheSize, defaultCacheTTL),
	}
}

// Backend returns the configured backend type (local, gcs, s3, tfc).
func (l *StateLoader) Backend() string { return l.backend }

// DiffBaseDir returns the directory within which other_state_path comparisons are confined.
func (l *StateLoader) DiffBaseDir() string { return l.diffBaseDir }

// SensitivePattern returns the operator-configured sensitive attribute redaction pattern.
func (l *StateLoader) SensitivePattern() *regexp.Regexp { return l.sensitivePattern }

func (l *StateLoader) cacheKey(org, workspace string) string {
	if l.backend == "tfc" {
		return fmt.Sprintf("tfc:%s/%s", org, workspace)
	}
	return fmt.Sprintf("%s:default", l.backend)
}

// Load returns the Terraform state for the given workspace, using the cache when fresh.
// For tfc backend org and workspace are required (or resolved from TF_CLOUD_ORG env).
// For all other backends org and workspace are ignored; pass empty strings.
func (l *StateLoader) Load(ctx context.Context, org, workspace string, forceRefresh bool, logger *log.Logger) (*TerraformState, error) {
	if l.backend == "tfc" {
		if org == "" {
			org = os.Getenv("TF_CLOUD_ORG")
		}
		if org == "" {
			org = os.Getenv("TFE_ORG")
		}
		if org == "" {
			return nil, fmt.Errorf("organization is required for tfc backend — pass it as a parameter or set TF_CLOUD_ORG")
		}
		if workspace == "" {
			return nil, fmt.Errorf("workspace is required for tfc backend")
		}
	}

	key := l.cacheKey(org, workspace)
	if !forceRefresh {
		if cached := l.cache.get(key); cached != nil {
			return cached, nil
		}
	}

	state, err := l.fetchState(ctx, org, workspace, logger)
	if err != nil {
		return nil, err
	}
	l.cache.put(key, state)
	return state, nil
}

// Invalidate removes a workspace's cached state so the next Load fetches fresh data.
func (l *StateLoader) Invalidate(org, workspace string) {
	l.cache.invalidate(l.cacheKey(org, workspace))
}

func (l *StateLoader) fetchState(ctx context.Context, org, workspace string, logger *log.Logger) (*TerraformState, error) {
	var data []byte
	var err error

	switch l.backend {
	case "local":
		data, err = os.ReadFile(l.statePath)
		if err != nil {
			return nil, fmt.Errorf("reading state file %q — verify TF_STATE_PATH points to a readable .tfstate file: %w", l.statePath, err)
		}

	case "gcs":
		if l.bucket == "" {
			return nil, fmt.Errorf("TF_STATE_BUCKET is required for gcs backend")
		}
		objPath := "default.tfstate"
		if l.prefix != "" {
			objPath = l.prefix + "/default.tfstate"
		}
		data, err = runSubprocess(ctx, "gsutil", "cat", fmt.Sprintf("gs://%s/%s", l.bucket, objPath))
		if err != nil {
			return nil, fmt.Errorf("reading GCS state gs://%s/%s: %w", l.bucket, objPath, err)
		}

	case "s3":
		if l.bucket == "" {
			return nil, fmt.Errorf("TF_STATE_BUCKET is required for s3 backend")
		}
		if l.key == "" {
			return nil, fmt.Errorf("TF_STATE_KEY is required for s3 backend")
		}
		data, err = runSubprocess(ctx, "aws", "s3", "cp", fmt.Sprintf("s3://%s/%s", l.bucket, l.key), "-")
		if err != nil {
			return nil, fmt.Errorf("reading S3 state s3://%s/%s: %w", l.bucket, l.key, err)
		}

	case "tfc":
		data, err = l.fetchTFCState(ctx, org, workspace, logger)
		if err != nil {
			return nil, err
		}

	default:
		return nil, fmt.Errorf("unsupported backend %q — set TF_STATE_BACKEND to one of: local, gcs, s3, tfc", l.backend)
	}

	if int64(len(data)) > l.maxBytes {
		return nil, fmt.Errorf("state is %.1f MB which exceeds the %.0f MB limit — increase TF_STATE_MAX_SIZE_MB to override",
			float64(len(data))/(1024*1024), float64(l.maxBytes)/(1024*1024))
	}

	var state TerraformState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parsing state: file may be encrypted, corrupted, or empty")
	}
	return &state, nil
}

func (l *StateLoader) fetchTFCState(ctx context.Context, org, workspace string, logger *log.Logger) ([]byte, error) {
	tfeClient, err := client.GetTfeClientFromContext(ctx, logger)
	if err != nil {
		return nil, fmt.Errorf("getting TFE client — ensure TFE_TOKEN and TFE_ADDRESS are configured: %w", err)
	}

	ws, err := tfeClient.Workspaces.Read(ctx, org, workspace)
	if err != nil {
		return nil, fmt.Errorf("workspace %q not found in org %q: %w", workspace, org, err)
	}

	sv, err := tfeClient.StateVersions.ReadCurrent(ctx, ws.ID)
	if err != nil {
		return nil, fmt.Errorf("reading current state version for workspace %q: %w", workspace, err)
	}

	data, err := tfeClient.StateVersions.Download(ctx, sv.DownloadURL)
	if err != nil {
		return nil, fmt.Errorf("downloading state for workspace %q: %w", workspace, err)
	}

	return data, nil
}

// LoadStateFile reads and parses a local .tfstate file without using the cache.
// Used by tf_diff_state to load the comparison state.
func LoadStateFile(path string, maxBytes int64) (*TerraformState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading state file %q: %w", path, err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("state file %.1f MB exceeds the %.0f MB limit",
			float64(len(data))/(1024*1024), float64(maxBytes)/(1024*1024))
	}
	var state TerraformState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parsing state file %q: file may be corrupted or encrypted", path)
	}
	return &state, nil
}

func runSubprocess(parentCtx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parentCtx, subprocessTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("command %q timed out after %v", name, subprocessTimeout)
		}
		return nil, fmt.Errorf("command %q failed: %w", name, err)
	}
	return out, nil
}
