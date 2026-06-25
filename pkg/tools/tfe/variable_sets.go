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

// ListVariableSets creates a tool to list variable sets.
func ListVariableSets(logger *log.Logger) server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("list_variable_sets",
			mcp.WithDescription("List all variable sets in an organization. Returns all if query is empty."),
			mcp.WithString("terraform_org_name", mcp.Required(), mcp.Description("Organization name")),
			mcp.WithString("query", mcp.Description("Optional filter query for variable set names")),
			utils.WithPagination(),
		),
		Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			orgName, err := request.RequireString("terraform_org_name")
			if err != nil {
				return ToolError(logger, "missing required input: terraform_org_name", err)
			}
			query := request.GetString("query", "")

			tfeClient, err := client.GetTfeClientFromContext(ctx, logger)
			if err != nil {
				return ToolError(logger, "failed to get Terraform client", err)
			}

			pagination, err := utils.OptionalPaginationParams(request)
			if err != nil {
				return ToolError(logger, "invalid pagination parameters", err)
			}

			varSets, err := tfeClient.VariableSets.List(ctx, orgName, &tfe.VariableSetListOptions{
				Query: query,
				ListOptions: tfe.ListOptions{
					PageNumber: pagination.Page,
					PageSize:   pagination.PageSize,
				},
			})
			if err != nil {
				return ToolErrorf(logger, "failed to list variable sets in org '%s'", orgName)
			}

			buf := bytes.NewBuffer(nil)
			err = jsonapi.MarshalPayloadWithoutIncluded(buf, varSets.Items)
			if err != nil {
				return ToolError(logger, "failed to marshal variable sets", err)
			}

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(buf.String()),
				},
			}, nil
		},
	}
}
