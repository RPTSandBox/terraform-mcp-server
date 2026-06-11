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

// TFSummary returns a tool that generates a human-readable summary of the Terraform state.
func TFSummary(logger *log.Logger) server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("tf_summary",
			mcp.WithDescription("Generate a high-level summary of the Terraform state including resource counts by type and module, "+
				"output names, Terraform version, and state serial number. "+
				"Useful as a first pass before diving into specific resources."),
			mcp.WithTitleAnnotation("Summarize Terraform State"),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithOpenWorldHintAnnotation(true),
			mcp.WithString("organization",
				mcp.Description("TFC/TFE organization name (required when TF_STATE_BACKEND=tfc; falls back to TF_CLOUD_ORG env var)"),
			),
			mcp.WithString("workspace",
				mcp.Description("TFC/TFE workspace name (required when TF_STATE_BACKEND=tfc)"),
			),
		),
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return tfSummaryHandler(ctx, req, logger)
		},
	}
}

func tfSummaryHandler(ctx context.Context, req mcp.CallToolRequest, logger *log.Logger) (*mcp.CallToolResult, error) {
	org, _ := req.GetArguments()["organization"].(string)
	workspace, _ := req.GetArguments()["workspace"].(string)

	loader := GetLoader()
	state, err := loader.Load(ctx, org, workspace, false, logger)
	if err != nil {
		return ToolError(logger, "loading Terraform state", err)
	}

	resources := ExtractResources(state, loader.SensitivePattern())

	// Count by type
	typeCounts := make(map[string]int)
	// Count by module
	moduleCounts := make(map[string]int)

	for _, r := range resources {
		typeCounts[r.Type]++
		moduleCounts[r.Module]++
	}

	// Sort type names for deterministic output
	types := make([]string, 0, len(typeCounts))
	for t := range typeCounts {
		types = append(types, t)
	}
	sort.Strings(types)

	modules := make([]string, 0, len(moduleCounts))
	for m := range moduleCounts {
		modules = append(modules, m)
	}
	sort.Strings(modules)

	var lines []string
	lines = append(lines, "# Terraform State Summary")
	lines = append(lines, "")

	if workspace != "" {
		lines = append(lines, fmt.Sprintf("Workspace:         %s / %s", org, workspace))
	}
	lines = append(lines, fmt.Sprintf("Backend:           %s", loader.Backend()))
	lines = append(lines, fmt.Sprintf("Terraform version: %s", state.TerraformVersion))
	lines = append(lines, fmt.Sprintf("State serial:      %d", state.Serial))
	lines = append(lines, fmt.Sprintf("State lineage:     %s", state.Lineage))
	lines = append(lines, fmt.Sprintf("Total resources:   %d", len(resources)))
	lines = append(lines, fmt.Sprintf("Total outputs:     %d", len(state.Outputs)))
	lines = append(lines, "")

	if len(types) > 0 {
		lines = append(lines, "## Resource Types")
		for _, t := range types {
			lines = append(lines, fmt.Sprintf("  %-50s %d", t, typeCounts[t]))
		}
		lines = append(lines, "")
	}

	if len(modules) > 1 || (len(modules) == 1 && modules[0] != "(root)") {
		lines = append(lines, "## Modules")
		for _, m := range modules {
			lines = append(lines, fmt.Sprintf("  %-50s %d resources", m, moduleCounts[m]))
		}
		lines = append(lines, "")
	}

	if len(state.Outputs) > 0 {
		lines = append(lines, "## Outputs")
		outputNames := make([]string, 0, len(state.Outputs))
		for name := range state.Outputs {
			outputNames = append(outputNames, name)
		}
		sort.Strings(outputNames)
		for _, name := range outputNames {
			out := state.Outputs[name]
			suffix := ""
			if out.Sensitive {
				suffix = " (sensitive)"
			}
			typeStr := ""
			if out.Type != nil {
				typeStr = fmt.Sprintf(" [%v]", out.Type)
			}
			lines = append(lines, fmt.Sprintf("  %s%s%s", name, typeStr, suffix))
		}
		lines = append(lines, "")
	}

	return mcp.NewToolResultText(strings.Join(lines, "\n")), nil
}
