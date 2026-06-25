// Copyright IBM Corp. 2025
// SPDX-License-Identifier: MPL-2.0

package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-mcp-server/pkg/client"
	log "github.com/sirupsen/logrus"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// READ-ONLY SERVER: CreateWorkspaceTags (write) has been removed entirely.

// ReadWorkspaceTags creates a tool to read tags from a workspace.
func ReadWorkspaceTags(logger *log.Logger) server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("read_workspace_tags",
			mcp.WithDescription("Read all tags from a Terraform workspace."),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithString("terraform_org_name",
				mcp.Required(),
				mcp.Description("Organization name"),
			),
			mcp.WithString("workspace_name",
				mcp.Required(),
				mcp.Description("Workspace name"),
			),
		),
		Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			orgName, err := request.RequireString("terraform_org_name")
			if err != nil {
				return ToolError(logger, "missing required input: terraform_org_name", err)
			}
			workspaceName, err := request.RequireString("workspace_name")
			if err != nil {
				return ToolError(logger, "missing required input: workspace_name", err)
			}

			tfeClient, err := client.GetTfeClientFromContext(ctx, logger)
			if err != nil {
				return ToolError(logger, "failed to get Terraform client", err)
			}

			workspace, err := tfeClient.Workspaces.Read(ctx, orgName, workspaceName)
			if err != nil {
				return ToolErrorf(logger, "workspace '%s' not found in org '%s'", workspaceName, orgName)
			}

			var tagNames []string
			tags, err := tfeClient.Workspaces.ListTags(ctx, workspace.ID, nil)
			if err != nil {
				return ToolError(logger, "failed to list tags", err)
			}
			for _, tag := range tags.Items {
				tagNames = append(tagNames, tag.Name)
			}

			var tagBindings []string
			bindings, err := tfeClient.Workspaces.ListTagBindings(ctx, workspace.ID)
			if err != nil {
				return ToolError(logger, "failed to list tag bindings", err)
			}
			for _, binding := range bindings {
				if binding.Value != "" {
					tagBindings = append(tagBindings, fmt.Sprintf("%s:%s", binding.Key, binding.Value))
				} else {
					tagBindings = append(tagBindings, binding.Key)
				}
			}

			tagResponse := fmt.Sprintf("Workspace %s has %d tags: %s", workspaceName, len(tagNames), strings.Join(tagNames, ", "))
			if len(tagBindings) > 0 {
				tagResponse += fmt.Sprintf("Workspace %s has %d tag bindings: %s", workspaceName, len(tagBindings), strings.Join(tagBindings, ", "))
			}

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(tagResponse),
				},
			}, nil
		},
	}
}
