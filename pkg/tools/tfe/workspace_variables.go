// Copyright IBM Corp. 2025
// SPDX-License-Identifier: MPL-2.0

package tools

import (
	"bytes"
	"context"

	"github.com/hashicorp/go-tfe"
	"github.com/hashicorp/jsonapi"
	"github.com/hashicorp/terraform-mcp-server/pkg/client"
	"github.com/hashicorp/terraform-mcp-server/pkg/utils"
	log "github.com/sirupsen/logrus"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// ListWorkspaceVariables creates a tool to list workspace variables.
func ListWorkspaceVariables(logger *log.Logger) server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("list_workspace_variables",
			mcp.WithDescription("List all variables in a Terraform workspace. Returns all variables if query is empty."),
			mcp.WithString("terraform_org_name", mcp.Required(), mcp.Description("Organization name")),
			mcp.WithString("workspace_name", mcp.Required(), mcp.Description("Workspace name")),
			utils.WithPagination(),
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

			pagination, err := utils.OptionalPaginationParams(request)
			if err != nil {
				return ToolError(logger, "invalid pagination parameters", err)
			}

			workspace, err := tfeClient.Workspaces.Read(ctx, orgName, workspaceName)
			if err != nil {
				return ToolErrorf(logger, "workspace '%s' not found in org '%s'", workspaceName, orgName)
			}

			vars, err := tfeClient.Variables.List(ctx, workspace.ID, &tfe.VariableListOptions{
				ListOptions: tfe.ListOptions{
					PageNumber: pagination.Page,
					PageSize:   pagination.PageSize,
				},
			})
			if err != nil {
				return ToolError(logger, "failed to list variables", err)
			}

			buf := bytes.NewBuffer(nil)
			err = jsonapi.MarshalPayload(buf, vars.Items)
			if err != nil {
				return ToolError(logger, "failed to marshal variables", err)
			}

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(buf.String()),
				},
			}, nil
		},
	}
}
