// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/go-tfe"
	"github.com/hashicorp/terraform-mcp-server/pkg/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	log "github.com/sirupsen/logrus"
)

func TFListWorkspaces(logger *log.Logger) server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("tf_list_workspaces",
			mcp.WithDescription("List TFC/TFE workspaces in an organization with state-relevant metadata: "+
				"resource count, lock status, Terraform version, and last-updated timestamp. "+
				"Supports name filtering and pagination. "+
				"Requires TFE_TOKEN to be configured."),
			mcp.WithTitleAnnotation("List TFC/TFE Workspaces (State Metadata)"),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithOpenWorldHintAnnotation(true),
			mcp.WithString("organization",
				mcp.Required(),
				mcp.Description("The TFC/TFE organization name to list workspaces for"),
			),
			mcp.WithString("name_filter",
				mcp.Description("Optional substring to filter workspaces by name (case-insensitive)"),
			),
			mcp.WithNumber("page",
				mcp.Description("Page number for pagination (default 1)"),
				mcp.Min(1),
			),
			mcp.WithNumber("page_size",
				mcp.Description("Results per page (default 20, max 100)"),
				mcp.Min(1),
				mcp.Max(100),
			),
		),
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return tfListWorkspacesHandler(ctx, req, logger)
		},
	}
}

func tfListWorkspacesHandler(ctx context.Context, req mcp.CallToolRequest, logger *log.Logger) (*mcp.CallToolResult, error) {
	org, ok := req.GetArguments()["organization"].(string)
	if !ok || strings.TrimSpace(org) == "" {
		return ToolErrorf(logger, "organization parameter is required")
	}
	org = strings.TrimSpace(org)

	nameFilter, _ := req.GetArguments()["name_filter"].(string)

	page := 1
	if v, ok := req.GetArguments()["page"].(float64); ok && v >= 1 {
		page = int(v)
	}
	pageSize := 20
	if v, ok := req.GetArguments()["page_size"].(float64); ok && v >= 1 {
		pageSize = int(v)
		if pageSize > 100 {
			pageSize = 100
		}
	}

	tfeClient, err := client.GetTfeClientFromContext(ctx, logger)
	if err != nil {
		return ToolError(logger, "tf_list_workspaces requires a valid TFE token — ensure TFE_TOKEN and TFE_ADDRESS are configured", err)
	}

	opts := &tfe.WorkspaceListOptions{
		ListOptions: tfe.ListOptions{
			PageNumber: page,
			PageSize:   pageSize,
		},
	}
	if nameFilter != "" {
		opts.Search = nameFilter
	}

	list, err := tfeClient.Workspaces.List(ctx, org, opts)
	if err != nil {
		return ToolError(logger, fmt.Sprintf("listing workspaces in org %q", org), err)
	}

	type workspaceSummary struct {
		Name             string `json:"name"`
		ID               string `json:"id"`
		TerraformVersion string `json:"terraform_version"`
		ResourceCount    int    `json:"resource_count"`
		Locked           bool   `json:"locked"`
		UpdatedAt        string `json:"updated_at"`
	}

	summaries := make([]workspaceSummary, 0, len(list.Items))
	for _, ws := range list.Items {
		summaries = append(summaries, workspaceSummary{
			Name:             ws.Name,
			ID:               ws.ID,
			TerraformVersion: ws.TerraformVersion,
			ResourceCount:    ws.ResourceCount,
			Locked:           ws.Locked,
			UpdatedAt:        ws.UpdatedAt.Format(time.RFC3339),
		})
	}

	result := map[string]interface{}{
		"organization": org,
		"workspaces":   summaries,
		"pagination": map[string]interface{}{
			"current_page": list.CurrentPage,
			"total_pages":  list.TotalPages,
			"total_count":  list.TotalCount,
		},
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return ToolError(logger, "marshaling response", err)
	}
	return mcp.NewToolResultText(string(data)), nil
}
