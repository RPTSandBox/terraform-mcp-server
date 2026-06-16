// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
			// Evict the oldest entry by load time (deterministic), rather than an
			// arbitrary map-iteration-order entry.
			var oldestKey string
			var oldest time.Time
			for k, e := range c.entries {
				if oldestKey == "" || e.loadedAt.Before(oldest) {
					oldestKey, oldest = k, e.loadedAt
				}
			}
			if oldestKey != "" {
				delete(c.entries, oldestKey)
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

func (l *StateLoader) cacheKey(identity, org, workspace string) string {
	if l.backend == "tfc" {
		// Partition by session identity so one session cannot read state another session
		// loaded for the same org/workspace.
		return fmt.Sprintf("tfc:%s:%s/%s", identity, org, workspace)
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

	identity := ""
	if l.backend == "tfc" {
		identity = client.SessionIdentityFromContext(ctx)
		if identity == "" {
			// No session identity to scope the cache to — never serve a shared cache entry.
			forceRefresh = true
		}
	}

	key := l.cacheKey(identity, org, workspace)
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
// For the tfc backend the entry is scoped to the calling session's identity.
func (l *StateLoader) Invalidate(ctx context.Context, org, workspace string) {
	identity := ""
	if l.backend == "tfc" {
		identity = client.SessionIdentityFromContext(ctx)
	}
	l.cache.invalidate(l.cacheKey(identity, org, workspace))
}

func (l *StateLoader) fetchState(ctx context.Context, org, workspace string, logger *log.Logger) (*TerraformState, error) {
	var data []byte
	var err error

	switch l.backend {
	case "local":
		if info, statErr := os.Stat(l.statePath); statErr == nil && info.Size() > l.maxBytes {
			return nil, fmt.Errorf("state is %.1f MB which exceeds the %.0f MB limit — increase TF_STATE_MAX_SIZE_MB to override",
				float64(info.Size())/(1024*1024), float64(l.maxBytes)/(1024*1024))
		}
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
		data, err = runSubprocess(ctx, l.maxBytes, "gsutil", "cat", fmt.Sprintf("gs://%s/%s", l.bucket, objPath))
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
		data, err = runSubprocess(ctx, l.maxBytes, "aws", "s3", "cp", fmt.Sprintf("s3://%s/%s", l.bucket, l.key), "-")
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

// LoadStateFileInRoot reads and parses a .tfstate file located at relPath within baseDir,
// without using the cache. It opens via os.Root so symlinks that escape baseDir are refused
// (closing the intermediate-symlink and TOCTOU traversal gaps), enforces the size limit via
// fstat before reading, and rejects non-regular files. Used by tf_diff_state.
func LoadStateFileInRoot(baseDir, relPath string, maxBytes int64) (*TerraformState, error) {
	root, err := os.OpenRoot(baseDir)
	if err != nil {
		return nil, fmt.Errorf("opening base directory %q: %w", baseDir, err)
	}
	defer root.Close()

	f, err := root.Open(relPath)
	if err != nil {
		return nil, fmt.Errorf("opening state file: file may not exist, be unreadable, or resolve outside the allowed directory")
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stating state file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("other_state_path must be a regular file")
	}
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("state file %.1f MB exceeds the %.0f MB limit",
			float64(info.Size())/(1024*1024), float64(maxBytes)/(1024*1024))
	}

	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading state file: %w", err)
	}
	var state TerraformState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parsing state file: file may be corrupted or encrypted")
	}
	return &state, nil
}

// runSubprocess runs name with args, capturing at most maxBytes of stdout. If the command
// produces more, it is killed and an error is returned, so an oversized object can never be
// fully buffered into memory.
func runSubprocess(parentCtx context.Context, maxBytes int64, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parentCtx, subprocessTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("command %q: %w", name, err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting %q: %w", name, err)
	}

	data, readErr := io.ReadAll(io.LimitReader(stdout, maxBytes+1))
	if int64(len(data)) > maxBytes {
		cancel() // kill the child before Wait so it cannot block on a full pipe
		_ = cmd.Wait()
		return nil, fmt.Errorf("state exceeds the %.0f MB limit — increase TF_STATE_MAX_SIZE_MB to override",
			float64(maxBytes)/(1024*1024))
	}

	waitErr := cmd.Wait()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("command %q timed out after %v", name, subprocessTimeout)
	}
	if waitErr != nil {
		return nil, fmt.Errorf("command %q failed: %w", name, waitErr)
	}
	if readErr != nil {
		return nil, fmt.Errorf("reading %q output: %w", name, readErr)
	}
	return data, nil
}
