// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	log "github.com/sirupsen/logrus"
)

// TFListResources returns a tool that lists all resources in the current Terraform state.
func TFListResources(logger *log.Logger) server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("tf_list_resources",
			mcp.WithDescription("List all resources in the current Terraform state with their addresses, types, modules, and dependency counts. "+
				"Supports optional filtering by resource type or module path."),
			mcp.WithTitleAnnotation("List Terraform State Resources"),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithOpenWorldHintAnnotation(true),
			mcp.WithString("organization",
				mcp.Description("TFC/TFE organization name (required when TF_STATE_BACKEND=tfc; falls back to TF_CLOUD_ORG env var)"),
			),
			mcp.WithString("workspace",
				mcp.Description("TFC/TFE workspace name (required when TF_STATE_BACKEND=tfc)"),
			),
			mcp.WithString("type_filter",
				mcp.Description("Filter resources whose type contains this substring (case-insensitive), e.g. 'aws_instance'"),
			),
			mcp.WithString("module_filter",
				mcp.Description("Filter resources in a specific module path, e.g. 'module.vpc'"),
			),
			mcp.WithString("response_format",
				mcp.Description("Output format: 'json' (default) or 'text'"),
				mcp.Enum("json", "text"),
			),
		),
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return tfListResourcesHandler(ctx, req, logger)
		},
	}
}

func tfListResourcesHandler(ctx context.Context, req mcp.CallToolRequest, logger *log.Logger) (*mcp.CallToolResult, error) {
	org, _ := req.GetArguments()["organization"].(string)
	workspace, _ := req.GetArguments()["workspace"].(string)
	typeFilter, _ := req.GetArguments()["type_filter"].(string)
	moduleFilter, _ := req.GetArguments()["module_filter"].(string)
	format, _ := req.GetArguments()["response_format"].(string)
	if format == "" {
		format = "text"
	}

	loader := GetLoader()
	state, err := loader.Load(ctx, org, workspace, false, logger)
	if err != nil {
		return ToolError(logger, "loading Terraform state", err)
	}

	resources := ExtractResources(state, loader.SensitivePattern())

	// Apply filters
	var filtered []ExtractedResource
	for _, r := range resources {
		if typeFilter != "" && !strings.Contains(strings.ToLower(r.Type), strings.ToLower(typeFilter)) {
			continue
		}
		if moduleFilter != "" && !strings.Contains(r.Module, moduleFilter) {
			continue
		}
		filtered = append(filtered, r)
	}
	if filtered == nil {
		filtered = []ExtractedResource{}
	}

	if format == "text" {
		var lines []string
		lines = append(lines, fmt.Sprintf("# Terraform Resources (%d total)", len(filtered)))
		lines = append(lines, "")
		for _, r := range filtered {
			lines = append(lines, fmt.Sprintf("- %s", r.Address))
			lines = append(lines, fmt.Sprintf("  type:     %s", r.Type))
			lines = append(lines, fmt.Sprintf("  module:   %s", r.Module))
			lines = append(lines, fmt.Sprintf("  mode:     %s", r.Mode))
			if len(r.Dependencies) > 0 {
				lines = append(lines, fmt.Sprintf("  depends:  %d resources", len(r.Dependencies)))
			}
		}
		return mcp.NewToolResultText(strings.Join(lines, "\n")), nil
	}

	// Emit a slimmed-down list (no full attributes) for token efficiency
	type resourceSummary struct {
		Address      string   `json:"address"`
		Type         string   `json:"type"`
		Name         string   `json:"name"`
		Module       string   `json:"module"`
		Mode         string   `json:"mode"`
		Provider     string   `json:"provider"`
		Dependencies []string `json:"dependencies"`
	}
	summaries := make([]resourceSummary, 0, len(filtered))
	for _, r := range filtered {
		summaries = append(summaries, resourceSummary{
			Address:      r.Address,
			Type:         r.Type,
			Name:         r.Name,
			Module:       r.Module,
			Mode:         r.Mode,
			Provider:     r.Provider,
			Dependencies: r.Dependencies,
		})
	}

	data, err := json.MarshalIndent(summaries, "", "  ")
	if err != nil {
		return ToolError(logger, "marshaling response", err)
	}
	return mcp.NewToolResultText(string(data)), nil
}
