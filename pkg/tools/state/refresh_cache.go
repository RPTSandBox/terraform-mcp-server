// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	log "github.com/sirupsen/logrus"
)

// TFRefreshCache returns a tool that forces the next state load to bypass the cache.
func TFRefreshCache(logger *log.Logger) server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("tf_refresh_cache",
			mcp.WithDescription("Invalidate the cached Terraform state for a workspace so the next read fetches fresh data from the backend. "+
				"Use this after applying changes or when you suspect the cached state is stale."),
			mcp.WithTitleAnnotation("Refresh Terraform State Cache"),
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithOpenWorldHintAnnotation(false),
			mcp.WithString("organization",
				mcp.Description("TFC/TFE organization name (required when TF_STATE_BACKEND=tfc; falls back to TF_CLOUD_ORG env var)"),
			),
			mcp.WithString("workspace",
				mcp.Description("TFC/TFE workspace name (required when TF_STATE_BACKEND=tfc)"),
			),
		),
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return tfRefreshCacheHandler(ctx, req, logger)
		},
	}
}

func tfRefreshCacheHandler(ctx context.Context, req mcp.CallToolRequest, logger *log.Logger) (*mcp.CallToolResult, error) {
	org, _ := req.GetArguments()["organization"].(string)
	workspace, _ := req.GetArguments()["workspace"].(string)
	org = strings.TrimSpace(org)
	workspace = strings.TrimSpace(workspace)

	loader := GetLoader()
	loader.Invalidate(org, workspace)

	var target string
	if loader.Backend() == "tfc" {
		target = fmt.Sprintf("workspace %s/%s", org, workspace)
	} else {
		target = fmt.Sprintf("%s backend state", loader.Backend())
	}

	return mcp.NewToolResultText(fmt.Sprintf("Cache cleared for %s. The next call to any state inspection tool will fetch fresh data from the backend.", target)), nil
}
