// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package tools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	log "github.com/sirupsen/logrus"
)

// TFGetResource returns a tool that retrieves full details for a single resource by address.
func TFGetResource(logger *log.Logger) server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("tf_get_resource",
			mcp.WithDescription("Get complete details for a single Terraform resource by its state address, "+
				"including all attributes. Values flagged sensitive in state, plus those whose key matches "+
				"the configured redaction pattern, are redacted; unflagged secrets may still appear, so review "+
				"output before sharing."),
			mcp.WithTitleAnnotation("Get Terraform Resource Details"),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithOpenWorldHintAnnotation(true),
			mcp.WithString("address",
				mcp.Required(),
				mcp.Description(`Full resource address from Terraform state, e.g. "aws_s3_bucket.my_bucket" or "module.vpc.aws_vpc.main"`),
			),
			mcp.WithString("organization",
				mcp.Description("TFC/TFE organization name (required when TF_STATE_BACKEND=tfc; falls back to TF_CLOUD_ORG env var)"),
			),
			mcp.WithString("workspace",
				mcp.Description("TFC/TFE workspace name (required when TF_STATE_BACKEND=tfc)"),
			),
		),
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return tfGetResourceHandler(ctx, req, logger)
		},
	}
}

func tfGetResourceHandler(ctx context.Context, req mcp.CallToolRequest, logger *log.Logger) (*mcp.CallToolResult, error) {
	address, ok := req.GetArguments()["address"].(string)
	if !ok || strings.TrimSpace(address) == "" {
		return ToolErrorf(logger, "address parameter is required")
	}
	address = strings.TrimSpace(address)

	org, _ := req.GetArguments()["organization"].(string)
	workspace, _ := req.GetArguments()["workspace"].(string)

	loader := GetLoader()
	state, err := loader.Load(ctx, org, workspace, false, logger)
	if err != nil {
		return ToolError(logger, "loading Terraform state", err)
	}

	resources := ExtractResources(state, loader.SensitivePattern())
	for _, r := range resources {
		if r.Address == address {
			data, err := json.MarshalIndent(r, "", "  ")
			if err != nil {
				return ToolError(logger, "marshaling resource", err)
			}
			return mcp.NewToolResultText(string(data)), nil
		}
	}

	return ToolErrorf(logger, "resource %q not found in state — use tf_list_resources to see available addresses", address)
}
