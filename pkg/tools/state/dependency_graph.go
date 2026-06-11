// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	log "github.com/sirupsen/logrus"
)

// TFDependencyGraph returns a tool that renders a resource dependency tree from state.
func TFDependencyGraph(logger *log.Logger) server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("tf_dependency_graph",
			mcp.WithDescription("Generate a dependency graph of Terraform resources showing which resources depend on which others. "+
				"Output is an ASCII tree diagram suitable for understanding resource ordering and blast radius."),
			mcp.WithTitleAnnotation("Generate Terraform Dependency Graph"),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithOpenWorldHintAnnotation(true),
			mcp.WithString("organization",
				mcp.Description("TFC/TFE organization name (required when TF_STATE_BACKEND=tfc; falls back to TF_CLOUD_ORG env var)"),
			),
			mcp.WithString("workspace",
				mcp.Description("TFC/TFE workspace name (required when TF_STATE_BACKEND=tfc)"),
			),
			mcp.WithString("resource_type",
				mcp.Description("Only include resources of this type in the graph (e.g. 'aws_instance')"),
			),
		),
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return tfDependencyGraphHandler(ctx, req, logger)
		},
	}
}

func tfDependencyGraphHandler(ctx context.Context, req mcp.CallToolRequest, logger *log.Logger) (*mcp.CallToolResult, error) {
	org, _ := req.GetArguments()["organization"].(string)
	workspace, _ := req.GetArguments()["workspace"].(string)
	resourceType, _ := req.GetArguments()["resource_type"].(string)

	loader := GetLoader()
	state, err := loader.Load(ctx, org, workspace, false, logger)
	if err != nil {
		return ToolError(logger, "loading Terraform state", err)
	}

	resources := ExtractResources(state, loader.SensitivePattern())

	var filtered []ExtractedResource
	for _, r := range resources {
		if resourceType != "" && !strings.EqualFold(r.Type, resourceType) {
			continue
		}
		filtered = append(filtered, r)
	}

	// Sort by address for deterministic output
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Address < filtered[j].Address
	})

	var lines []string
	lines = append(lines, "# Terraform Dependency Graph")
	if workspace != "" {
		lines = append(lines, fmt.Sprintf("# Workspace: %s/%s", org, workspace))
	}
	lines = append(lines, fmt.Sprintf("# Resources: %d", len(filtered)))
	lines = append(lines, "")

	if len(filtered) == 0 {
		lines = append(lines, "(no resources matched the filter)")
		return mcp.NewToolResultText(strings.Join(lines, "\n")), nil
	}

	for _, r := range filtered {
		lines = append(lines, r.Address)
		deps := r.Dependencies
		for i, dep := range deps {
			if i == len(deps)-1 {
				lines = append(lines, "  └─ "+dep)
			} else {
				lines = append(lines, "  ├─ "+dep)
			}
		}
		if len(deps) == 0 {
			lines = append(lines, "  (no dependencies)")
		}
		lines = append(lines, "")
	}

	return mcp.NewToolResultText(strings.Join(lines, "\n")), nil
}
