// Copyright IBM Corp. 2025
// SPDX-License-Identifier: MPL-2.0

package tools

import (
	"context"
	"strings"
	"sync"

	"github.com/hashicorp/terraform-mcp-server/pkg/client"
	tfeTools "github.com/hashicorp/terraform-mcp-server/pkg/tools/tfe"
	"github.com/hashicorp/terraform-mcp-server/pkg/toolsets"
	"github.com/hashicorp/terraform-mcp-server/pkg/utils"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	log "github.com/sirupsen/logrus"
)

// DynamicToolRegistry manages the availability of tools based on session state
type DynamicToolRegistry struct {
	mu                 sync.RWMutex
	sessionsWithTFE    map[string]bool // sessionID -> hasTFEClient
	tfeToolsRegistered bool
	mcpServer          *server.MCPServer
	logger             *log.Logger
	enabledToolsets    []string
}

var globalToolRegistry *DynamicToolRegistry

// registerDynamicTools registers the global tool registry
func registerDynamicTools(mcpServer *server.MCPServer, logger *log.Logger, enabledToolsets []string) {
	globalToolRegistry = &DynamicToolRegistry{
		sessionsWithTFE:    make(map[string]bool),
		tfeToolsRegistered: false,
		mcpServer:          mcpServer,
		logger:             logger,
		enabledToolsets:    enabledToolsets,
	}

	// Set the callback in the client package to avoid circular imports
	client.SetToolRegistryCallback(globalToolRegistry)
}

// GetDynamicToolRegistry returns the global tool registry instance
func GetDynamicToolRegistry() *DynamicToolRegistry {
	return globalToolRegistry
}

// RegisterSessionWithTFE marks a session as having a valid TFE client
func (r *DynamicToolRegistry) RegisterSessionWithTFE(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.sessionsWithTFE[sessionID] = true
	r.logger.Info("Session registered with TFE client")

	// If this is the first session with TFE, register the tools
	if !r.tfeToolsRegistered {
		r.registerTFETools()
	}
}

// UnregisterSessionWithTFE removes a session from the TFE registry
func (r *DynamicToolRegistry) UnregisterSessionWithTFE(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.sessionsWithTFE, sessionID)
	r.logger.Info("Session unregistered from TFE client")

}

// HasSessionWithTFE checks if a specific session has a TFE client
func (r *DynamicToolRegistry) HasSessionWithTFE(sessionID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.sessionsWithTFE[sessionID]
}

// HasAnySessionWithTFE checks if any session has a TFE client
func (r *DynamicToolRegistry) HasAnySessionWithTFE() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.sessionsWithTFE) > 0
}

// isTerraformOperationsEnabled checks if ENABLE_TF_OPERATIONS is set to true
func isTerraformOperationsEnabled() bool {
	envVar := utils.GetEnv("ENABLE_TF_OPERATIONS", "false")
	return strings.ToLower(envVar) == "true"
}

var writeCapableTools = map[string]bool{
	"create_workspace":                    true,
	"update_workspace":                    true,
	"delete_workspace_safely":             true,
	"create_workspace_tags":               true,
	"create_run":                          true,
	"action_run":                          true,
	"create_no_code_workspace":            true,
	"create_variable_set":                 true,
	"create_variable_in_variable_set":     true,
	"delete_variable_in_variable_set":     true,
	"attach_variable_set_to_workspaces":   true,
	"detach_variable_set_from_workspaces": true,
	"attach_policy_set_to_workspaces":     true,
	"create_workspace_variable":           true,
	"update_workspace_variable":           true,
}

// registerTFETools registers TFE tools with the MCP server
func (r *DynamicToolRegistry) registerTFETools() {
	if r.tfeToolsRegistered {
		return
	}

	r.logger.Info("Registering TFE tools - first session with valid TFE client detected")

	// Terraform toolset - Organization and Project tools
	if toolsets.IsToolEnabled("list_terraform_orgs", r.enabledToolsets) {
		tool := r.createDynamicTFETool("list_terraform_orgs", tfeTools.ListTerraformOrgs)
		r.mcpServer.AddTool(tool.Tool, tool.Handler)
	}

	if toolsets.IsToolEnabled("list_terraform_projects", r.enabledToolsets) {
		tool := r.createDynamicTFETool("list_terraform_projects", tfeTools.ListTerraformProjects)
		r.mcpServer.AddTool(tool.Tool, tool.Handler)
	}

	// Terraform toolset - Workspace management tools
	if toolsets.IsToolEnabled("list_workspaces", r.enabledToolsets) {
		tool := r.createDynamicTFETool("list_workspaces", tfeTools.ListWorkspaces)
		r.mcpServer.AddTool(tool.Tool, tool.Handler)
	}

	if toolsets.IsToolEnabled("get_workspace_details", r.enabledToolsets) {
		tool := r.createDynamicTFETool("get_workspace_details", tfeTools.GetWorkspaceDetails)
		r.mcpServer.AddTool(tool.Tool, tool.Handler)
	}

	// Registry-private toolset - Private provider tools
	if toolsets.IsToolEnabled("search_private_providers", r.enabledToolsets) {
		tool := r.createDynamicTFETool("search_private_providers", tfeTools.SearchPrivateProviders)
		r.mcpServer.AddTool(tool.Tool, tool.Handler)
	}

	if toolsets.IsToolEnabled("get_private_provider_details", r.enabledToolsets) {
		tool := r.createDynamicTFETool("get_private_provider_details", tfeTools.GetPrivateProviderDetails)
		r.mcpServer.AddTool(tool.Tool, tool.Handler)
	}

	// Registry-private toolset - Private module tools
	if toolsets.IsToolEnabled("search_private_modules", r.enabledToolsets) {
		tool := r.createDynamicTFETool("search_private_modules", tfeTools.SearchPrivateModules)
		r.mcpServer.AddTool(tool.Tool, tool.Handler)
	}

	if toolsets.IsToolEnabled("get_private_module_details", r.enabledToolsets) {
		tool := r.createDynamicTFETool("get_private_module_details", tfeTools.GetPrivateModuleDetails)
		r.mcpServer.AddTool(tool.Tool, tool.Handler)
	}

	// Terraform toolset - Workspace tags tools (read-only; create_workspace_tags omitted)
	if toolsets.IsToolEnabled("read_workspace_tags", r.enabledToolsets) {
		tool := r.createDynamicTFETool("read_workspace_tags", tfeTools.ReadWorkspaceTags)
		r.mcpServer.AddTool(tool.Tool, tool.Handler)
	}

	// Terraform toolset - Run tools
	if toolsets.IsToolEnabled("list_runs", r.enabledToolsets) {
		tool := r.createDynamicTFETool("list_runs", tfeTools.ListRuns)
		r.mcpServer.AddTool(tool.Tool, tool.Handler)
	}

	if toolsets.IsToolEnabled("get_run_details", r.enabledToolsets) {
		tool := r.createDynamicTFETool("get_run_details", tfeTools.GetRunDetails)
		r.mcpServer.AddTool(tool.Tool, tool.Handler)
	}

	if toolsets.IsToolEnabled("get_plan_details", r.enabledToolsets) {
		tool := r.createDynamicTFETool("get_plan_details", tfeTools.GetPlanDetails)
		r.mcpServer.AddTool(tool.Tool, tool.Handler)
	}

	if toolsets.IsToolEnabled("get_plan_logs", r.enabledToolsets) {
		tool := r.createDynamicTFETool("get_plan_logs", tfeTools.GetPlanLogs)
		r.mcpServer.AddTool(tool.Tool, tool.Handler)
	}

	if toolsets.IsToolEnabled("get_plan_json_output", r.enabledToolsets) {
		tool := r.createDynamicTFETool("get_plan_json_output", tfeTools.GetPlanJSONOutput)
		r.mcpServer.AddTool(tool.Tool, tool.Handler)
	}

	if toolsets.IsToolEnabled("get_apply_details", r.enabledToolsets) {
		tool := r.createDynamicTFETool("get_apply_details", tfeTools.GetApplyDetails)
		r.mcpServer.AddTool(tool.Tool, tool.Handler)
	}

	if toolsets.IsToolEnabled("get_apply_logs", r.enabledToolsets) {
		tool := r.createDynamicTFETool("get_apply_logs", tfeTools.GetApplyLogs)
		r.mcpServer.AddTool(tool.Tool, tool.Handler)
	}
	if toolsets.IsToolEnabled("get_sentinel_mock", r.enabledToolsets) {
		tool := r.createDynamicTFETool("get_sentinel_mock", tfeTools.GetSentinelMock)
		r.mcpServer.AddTool(tool.Tool, tool.Handler)
	}

	// Terraform toolset - Variable set tools
	if toolsets.IsToolEnabled("list_variable_sets", r.enabledToolsets) {
		tool := r.createDynamicTFETool("list_variable_sets", tfeTools.ListVariableSets)
		r.mcpServer.AddTool(tool.Tool, tool.Handler)
	}

	if toolsets.IsToolEnabled("list_workspace_policy_sets", r.enabledToolsets) {
		tool := r.createDynamicTFETool("list_workspace_policy_sets", tfeTools.ListWorkspacePolicySets)
		r.mcpServer.AddTool(tool.Tool, tool.Handler)
	}

	// Terraform toolset - Variable tools
	if toolsets.IsToolEnabled("list_workspace_variables", r.enabledToolsets) {
		tool := r.createDynamicTFETool("list_workspace_variables", tfeTools.ListWorkspaceVariables)
		r.mcpServer.AddTool(tool.Tool, tool.Handler)
	}
	
	if toolsets.IsToolEnabled("get_token_permissions", r.enabledToolsets) {
		tool := r.createDynamicTFETool("get_token_permissions", tfeTools.GetTokenPermissions)
		r.mcpServer.AddTool(tool.Tool, tool.Handler)
	}

	// Terraform toolset - Stacks
	if toolsets.IsToolEnabled("list_stacks", r.enabledToolsets) {
		tool := r.createDynamicTFETool("list_stacks", tfeTools.ListStacks)
		r.mcpServer.AddTool(tool.Tool, tool.Handler)
	}
	if toolsets.IsToolEnabled("get_stack_details", r.enabledToolsets) {
		tool := r.createDynamicTFETool("get_stack_details", tfeTools.GetStackDetails)
		r.mcpServer.AddTool(tool.Tool, tool.Handler)
	}

	r.tfeToolsRegistered = true
}

// createDynamicTFETool creates a TFE tool with dynamic availability checking
func (r *DynamicToolRegistry) createDynamicTFETool(toolName string, toolFactory func(*log.Logger) server.ServerTool) server.ServerTool {
	if writeCapableTools[toolName] {
		r.logger.Fatalf("refusing to register write-capable tool %q: this MCP server is read-only", toolName)
	}
	originalTool := toolFactory(r.logger)
	return server.ServerTool{
		Tool:    originalTool.Tool,
		Handler: r.wrapWithAvailabilityCheck(toolName, originalTool.Handler),
	}
}

// createDynamicTFEToolWithElicitation creates a TFE tool with dynamic availability checking that also needs MCPServer for elicitation
func (r *DynamicToolRegistry) createDynamicTFEToolWithElicitation(toolName string, toolFactory func(*log.Logger, *server.MCPServer) server.ServerTool) server.ServerTool {
	if writeCapableTools[toolName] {
		r.logger.Fatalf("refusing to register write-capable tool %q: this MCP server is read-only", toolName)
	}
	originalTool := toolFactory(r.logger, r.mcpServer)
	return server.ServerTool{
		Tool:    originalTool.Tool,
		Handler: r.wrapWithAvailabilityCheck(toolName, originalTool.Handler),
	}
}

// wrapWithAvailabilityCheck wraps a tool handler with dynamic TFE availability checking
func (r *DynamicToolRegistry) wrapWithAvailabilityCheck(toolName string, originalHandler server.ToolHandlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Get session from context
		session := server.ClientSessionFromContext(ctx)
		if session == nil {
			r.logger.WithField("tool", toolName).Warn("TFE tool called without session context")
			return mcp.NewToolResultError("This tool requires an active session with valid Terraform Cloud/Enterprise configuration."), nil
		}

		// Check if this session has a valid TFE client
		sessionID := session.SessionID()
		if !r.HasSessionWithTFE(sessionID) {
			// Double-check by looking at the actual client state
			tfeClient := client.GetTfeClient(sessionID)
			if tfeClient == nil {
				r.logger.WithFields(log.Fields{
					"tool": toolName,
				}).Warn("TFE tool called but session has no valid TFE client")

				return mcp.NewToolResultError("This tool is not available. This tool requires a valid Terraform Cloud/Enterprise token and configuration. Please ensure TFE_TOKEN and TFE_ADDRESS environment variables are properly set."), nil
			}
			// If we found a valid client that wasn't registered, register it now
			r.RegisterSessionWithTFE(sessionID)
		}

		// Tool is available, proceed with original handler
		return originalHandler(ctx, req)
	}
}
