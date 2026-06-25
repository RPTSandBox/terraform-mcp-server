// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/go-tfe"
	"github.com/hashicorp/terraform-mcp-server/pkg/client"
	log "github.com/sirupsen/logrus"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// MatchingPolicySet represents a policy set that applies to a workspace.
type MatchingPolicySet struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Kind        string `json:"kind"`
	Global      bool   `json:"global"`
	Reason      string `json:"reason"`
}

// READ-ONLY SERVER: AttachPolicySetToWorkspaces (write) has been removed entirely.

// ListWorkspacePolicySets creates a tool to read all policy sets attached to a workspace.
func ListWorkspacePolicySets(logger *log.Logger) server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("list_workspace_policy_sets",
			mcp.WithDescription("Read all policy sets attached to a workspace. Returns both directly attached policy sets and global policy sets that apply to all workspaces."),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithString("terraform_org_name", mcp.Required(), mcp.Description("Organization name")),
			mcp.WithString("workspace_id", mcp.Required(), mcp.Description("The workspace ID to get policy sets for (e.g., ws-2HRvNs49EWPjDqT1)")),
		),
		Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return listWorkspacePolicySetsHandler(ctx, request, logger)
		},
	}
}

func listWorkspacePolicySetsHandler(ctx context.Context, request mcp.CallToolRequest, logger *log.Logger) (*mcp.CallToolResult, error) {
	orgName, err := request.RequireString("terraform_org_name")
	if err != nil {
		return ToolError(logger, "missing required input: terraform_org_name", err)
	}
	workspaceID, err := request.RequireString("workspace_id")
	if err != nil {
		return ToolError(logger, "missing required input: workspace_id", err)
	}

	tfeClient, err := client.GetTfeClientFromContext(ctx, logger)
	if err != nil {
		return ToolError(logger, "failed to get Terraform client", err)
	}

	// Paginate through all policy sets with the workspaces included
	var matchingPolicySets []*MatchingPolicySet
	pageNumber := 1

	for {
		policySets, err := tfeClient.PolicySets.List(ctx, orgName, &tfe.PolicySetListOptions{
			Include: []tfe.PolicySetIncludeOpt{tfe.PolicySetWorkspaces},
			ListOptions: tfe.ListOptions{
				PageNumber: pageNumber,
				PageSize:   100,
			},
		})
		if err != nil {
			return ToolErrorf(logger, "failed to list policy sets for org '%s': %v", orgName, err)
		}

		// Filter policy sets that apply to this workspace
		for _, ps := range policySets.Items {
			applies := false
			reason := ""

			// Global policy sets apply to all workspaces
			if ps.Global {
				applies = true
				reason = "global"
			} else {
				for _, ws := range ps.Workspaces {
					if ws.ID == workspaceID {
						applies = true
						reason = "directly attached"
						break
					}
				}
			}

			if applies {
				matchingPolicySets = append(matchingPolicySets, &MatchingPolicySet{
					ID:          ps.ID,
					Name:        ps.Name,
					Description: ps.Description,
					Kind:        string(ps.Kind),
					Global:      ps.Global,
					Reason:      reason,
				})
			}
		}

		// Check if there are more pages
		if policySets.NextPage == 0 {
			break
		}
		pageNumber++
	}

	if len(matchingPolicySets) == 0 {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("No policy sets are attached to workspace %s", workspaceID)),
			},
		}, nil
	}

	result, err := json.MarshalIndent(matchingPolicySets, "", "  ")
	if err != nil {
		return ToolError(logger, "failed to marshal policy sets", err)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.NewTextContent(string(result)),
		},
	}, nil
}
