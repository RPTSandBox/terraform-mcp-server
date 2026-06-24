// Copyright IBM Corp. 2025
// SPDX-License-Identifier: MPL-2.0

package tools

import (
	"os"

	registryTools "github.com/hashicorp/terraform-mcp-server/pkg/tools/registry"
	stateTools "github.com/hashicorp/terraform-mcp-server/pkg/tools/state"
	"github.com/hashicorp/terraform-mcp-server/pkg/toolsets"
	"github.com/mark3labs/mcp-go/server"
	log "github.com/sirupsen/logrus"
)

// RegisterTools registers the enabled toolsets with the MCP server. isNetworkTransport must be
// true when the server is reachable over a network transport (HTTP/SSE) and false for a
// single-tenant stdio launch; it gates the state-inspection toolset, whose non-tfc backends
// serve one shared operator-configured state to every connected client.
func RegisterTools(hcServer *server.MCPServer, logger *log.Logger, enabledToolsets []string, isNetworkTransport bool) {
	// Register the dynamic tools (TFE tools that require authentication)
	registerDynamicTools(hcServer, logger, enabledToolsets)

	// Registry toolset - Provider tools
	if toolsets.IsToolEnabled("search_providers", enabledToolsets) {
		tool := registryTools.ResolveProviderDocID(logger)
		hcServer.AddTool(tool.Tool, tool.Handler)
	}

	if toolsets.IsToolEnabled("get_provider_details", enabledToolsets) {
		tool := registryTools.GetProviderDocs(logger)
		hcServer.AddTool(tool.Tool, tool.Handler)
	}

	if toolsets.IsToolEnabled("get_latest_provider_version", enabledToolsets) {
		tool := registryTools.GetLatestProviderVersion(logger)
		hcServer.AddTool(tool.Tool, tool.Handler)
	}

	if toolsets.IsToolEnabled("get_provider_capabilities", enabledToolsets) {
		tool := registryTools.GetProviderCapabilities(logger)
		hcServer.AddTool(tool.Tool, tool.Handler)
	}

	// Registry toolset - Module tools
	if toolsets.IsToolEnabled("search_modules", enabledToolsets) {
		tool := registryTools.SearchModules(logger)
		hcServer.AddTool(tool.Tool, tool.Handler)
	}

	if toolsets.IsToolEnabled("get_module_details", enabledToolsets) {
		tool := registryTools.ModuleDetails(logger)
		hcServer.AddTool(tool.Tool, tool.Handler)
	}

	if toolsets.IsToolEnabled("get_latest_module_version", enabledToolsets) {
		tool := registryTools.GetLatestModuleVersion(logger)
		hcServer.AddTool(tool.Tool, tool.Handler)
	}

	// Registry toolset - Policy tools
	if toolsets.IsToolEnabled("search_policies", enabledToolsets) {
		tool := registryTools.SearchPolicies(logger)
		hcServer.AddTool(tool.Tool, tool.Handler)
	}

	if toolsets.IsToolEnabled("get_policy_details", enabledToolsets) {
		tool := registryTools.PolicyDetails(logger)
		hcServer.AddTool(tool.Tool, tool.Handler)
	}

	// State inspection toolset.
	//
	// On a network transport (HTTP/SSE) the local/s3/gcs backends have no per-caller
	// authorization: the cache key is "<backend>:default", so every connected client would
	// read the single operator-configured state. Only the tfc backend is session-scoped.
	// Refuse to register the state tools in that unsafe configuration unless the operator
	// explicitly opts in with TF_STATE_ALLOW_SHARED=true. stdio launches are single-tenant
	// and unaffected.
	backend := stateTools.GetLoader().Backend()
	if isNetworkTransport && backend != "tfc" && os.Getenv("TF_STATE_ALLOW_SHARED") != "true" {
		logger.Warnf("state-inspection toolset disabled: backend %q on a network transport would serve one shared state to all clients with no per-caller authorization; set TF_STATE_ALLOW_SHARED=true to override, or use the tfc backend (session-scoped)", backend)
		return
	}

	if toolsets.IsToolEnabled("tf_list_workspaces", enabledToolsets) {
		tool := stateTools.TFListWorkspaces(logger)
		hcServer.AddTool(tool.Tool, tool.Handler)
	}

	if toolsets.IsToolEnabled("tf_list_resources", enabledToolsets) {
		tool := stateTools.TFListResources(logger)
		hcServer.AddTool(tool.Tool, tool.Handler)
	}

	if toolsets.IsToolEnabled("tf_get_resource", enabledToolsets) {
		tool := stateTools.TFGetResource(logger)
		hcServer.AddTool(tool.Tool, tool.Handler)
	}

	if toolsets.IsToolEnabled("tf_search_attributes", enabledToolsets) {
		tool := stateTools.TFSearchAttributes(logger)
		hcServer.AddTool(tool.Tool, tool.Handler)
	}

	if toolsets.IsToolEnabled("tf_get_outputs", enabledToolsets) {
		tool := stateTools.TFGetOutputs(logger)
		hcServer.AddTool(tool.Tool, tool.Handler)
	}

	if toolsets.IsToolEnabled("tf_dependency_graph", enabledToolsets) {
		tool := stateTools.TFDependencyGraph(logger)
		hcServer.AddTool(tool.Tool, tool.Handler)
	}

	if toolsets.IsToolEnabled("tf_diff_state", enabledToolsets) {
		tool := stateTools.TFDiffState(logger)
		hcServer.AddTool(tool.Tool, tool.Handler)
	}

	if toolsets.IsToolEnabled("tf_summary", enabledToolsets) {
		tool := stateTools.TFSummary(logger)
		hcServer.AddTool(tool.Tool, tool.Handler)
	}

	if toolsets.IsToolEnabled("tf_refresh_cache", enabledToolsets) {
		tool := stateTools.TFRefreshCache(logger)
		hcServer.AddTool(tool.Tool, tool.Handler)
	}
}
