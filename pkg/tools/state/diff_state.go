// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	log "github.com/sirupsen/logrus"
)

// TFDiffState returns a tool that compares current state against another .tfstate file.
func TFDiffState(logger *log.Logger) server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("tf_diff_state",
			mcp.WithDescription("Compare the current Terraform state against another .tfstate file and report "+
				"added, removed, and changed resources. "+
				"The comparison file must be within the directory configured by TF_STATE_DIFF_BASE_DIR (default: current working directory)."),
			mcp.WithTitleAnnotation("Diff Terraform State Files"),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithOpenWorldHintAnnotation(false),
			mcp.WithString("other_state_path",
				mcp.Required(),
				mcp.Description("Absolute or relative path to a .tfstate file to compare against the current state"),
			),
			mcp.WithString("organization",
				mcp.Description("TFC/TFE organization name (required when TF_STATE_BACKEND=tfc; falls back to TF_CLOUD_ORG env var)"),
			),
			mcp.WithString("workspace",
				mcp.Description("TFC/TFE workspace name (required when TF_STATE_BACKEND=tfc)"),
			),
		),
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return tfDiffStateHandler(ctx, req, logger)
		},
	}
}

func tfDiffStateHandler(ctx context.Context, req mcp.CallToolRequest, logger *log.Logger) (*mcp.CallToolResult, error) {
	rawPath, ok := req.GetArguments()["other_state_path"].(string)
	if !ok || strings.TrimSpace(rawPath) == "" {
		return ToolErrorf(logger, "other_state_path parameter is required")
	}

	org, _ := req.GetArguments()["organization"].(string)
	workspace, _ := req.GetArguments()["workspace"].(string)

	loader := GetLoader()

	// Validate and canonicalize the comparison path
	safePath, err := safeDiffPath(strings.TrimSpace(rawPath), loader.DiffBaseDir())
	if err != nil {
		return ToolError(logger, "invalid other_state_path", err)
	}

	// Load both states
	currentState, err := loader.Load(ctx, org, workspace, false, logger)
	if err != nil {
		return ToolError(logger, "loading current Terraform state", err)
	}
	otherState, err := LoadStateFile(safePath, loader.maxBytes)
	if err != nil {
		return ToolError(logger, "loading comparison state file", err)
	}

	sensitivePattern := loader.SensitivePattern()
	currentResources := resourceIndex(ExtractResources(currentState, sensitivePattern))
	otherResources := resourceIndex(ExtractResources(otherState, sensitivePattern))

	type attrDiff struct {
		Before interface{} `json:"before"`
		After  interface{} `json:"after"`
	}
	type changedResource struct {
		Address string               `json:"address"`
		Diff    map[string]attrDiff  `json:"attribute_changes"`
	}

	var added, removed []string
	var changed []changedResource

	for addr := range otherResources {
		if _, exists := currentResources[addr]; !exists {
			added = append(added, addr)
		}
	}
	for addr := range currentResources {
		if _, exists := otherResources[addr]; !exists {
			removed = append(removed, addr)
		}
	}
	for addr, cur := range currentResources {
		other, exists := otherResources[addr]
		if !exists {
			continue
		}
		diff := diffAttrs(cur.Attributes, other.Attributes)
		if len(diff) > 0 {
			attrChanges := make(map[string]attrDiff, len(diff))
			for k, pair := range diff {
				attrChanges[k] = attrDiff{Before: pair[0], After: pair[1]}
			}
			changed = append(changed, changedResource{Address: addr, Diff: attrChanges})
		}
	}

	if added == nil {
		added = []string{}
	}
	if removed == nil {
		removed = []string{}
	}
	if changed == nil {
		changed = []changedResource{}
	}

	result := map[string]interface{}{
		"current_state":      fmt.Sprintf("%s (serial %d)", loader.backend, currentState.Serial),
		"other_state_path":   safePath,
		"other_state_serial": otherState.Serial,
		"summary": map[string]int{
			"added":   len(added),
			"removed": len(removed),
			"changed": len(changed),
		},
		"added":   added,
		"removed": removed,
		"changed": changed,
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return ToolError(logger, "marshaling diff", err)
	}
	return mcp.NewToolResultText(string(data)), nil
}

func resourceIndex(resources []ExtractedResource) map[string]ExtractedResource {
	idx := make(map[string]ExtractedResource, len(resources))
	for _, r := range resources {
		idx[r.Address] = r
	}
	return idx
}

// diffAttrs returns a map of changed attribute keys to [before, after] pairs.
func diffAttrs(before, after map[string]interface{}) map[string][2]interface{} {
	changes := make(map[string][2]interface{})
	allKeys := make(map[string]bool)
	for k := range before {
		allKeys[k] = true
	}
	for k := range after {
		allKeys[k] = true
	}
	for k := range allKeys {
		bVal := before[k]
		aVal := after[k]
		if !reflect.DeepEqual(bVal, aVal) {
			changes[k] = [2]interface{}{bVal, aVal}
		}
	}
	return changes
}

// safeDiffPath validates and canonicalizes a path for state diff operations.
// It rejects symlinks, enforces .tfstate extension, and ensures the path
// is within baseDir to prevent directory traversal.
func safeDiffPath(rawPath, baseDir string) (string, error) {
	// Resolve baseDir to absolute first.
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return "", fmt.Errorf("resolving base directory: %w", err)
	}

	// For relative paths, root them under baseDir rather than the binary's
	// working directory.  This makes "previous.tfstate" resolve naturally
	// inside the allowed directory and prevents CWD from influencing the
	// containment check.
	var cleaned string
	if filepath.IsAbs(rawPath) {
		cleaned = filepath.Clean(rawPath)
	} else {
		cleaned = filepath.Clean(filepath.Join(absBase, rawPath))
	}

	// Enforce containment within baseDir before any filesystem access.
	// Use this check first so traversal attempts (../../etc/passwd) are
	// caught with a clear "outside allowed directory" error rather than an
	// extension error.
	rel, err := filepath.Rel(absBase, cleaned)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("other_state_path is outside the allowed directory (%s) — configure TF_STATE_DIFF_BASE_DIR to change this", absBase)
	}

	// Enforce .tfstate extension before any filesystem access.
	if !strings.EqualFold(filepath.Ext(cleaned), ".tfstate") {
		return "", errors.New("other_state_path must have a .tfstate extension")
	}

	// Now check existence and reject symlinks (TOCTOU guard).
	// Use cleaned (absolute) path, not rawPath, to avoid re-introducing traversal.
	linfo, err := os.Lstat(cleaned)
	if err != nil {
		return "", errors.New("other_state_path does not exist or is not readable")
	}
	if linfo.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("other_state_path must not be a symbolic link")
	}

	return cleaned, nil
}
