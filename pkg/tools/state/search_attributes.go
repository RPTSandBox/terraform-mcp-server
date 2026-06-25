// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	log "github.com/sirupsen/logrus"
)

// TFSearchAttributes returns a tool that searches attribute values across all resources.
func TFSearchAttributes(logger *log.Logger) server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("tf_search_attributes",
			mcp.WithDescription("Search for a substring across all resource attribute values in the Terraform state. "+
				"Returns matching resources with the attribute key/value pairs that matched. "+
				"Useful for finding resources that reference a specific ARN, IP, name, or identifier."),
			mcp.WithTitleAnnotation("Search Terraform Resource Attributes"),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithOpenWorldHintAnnotation(true),
			mcp.WithString("query",
				mcp.Required(),
				mcp.Description("Substring to search for in attribute values (case-insensitive)"),
			),
			mcp.WithString("organization",
				mcp.Description("TFC/TFE organization name (required when TF_STATE_BACKEND=tfc; falls back to TF_CLOUD_ORG env var)"),
			),
			mcp.WithString("workspace",
				mcp.Description("TFC/TFE workspace name (required when TF_STATE_BACKEND=tfc)"),
			),
			mcp.WithString("resource_type",
				mcp.Description("Restrict the search to resources of this type, e.g. 'aws_security_group'"),
			),
		),
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return tfSearchAttributesHandler(ctx, req, logger)
		},
	}
}

func tfSearchAttributesHandler(ctx context.Context, req mcp.CallToolRequest, logger *log.Logger) (*mcp.CallToolResult, error) {
	query, ok := req.GetArguments()["query"].(string)
	if !ok || strings.TrimSpace(query) == "" {
		return ToolErrorf(logger, "query parameter is required")
	}
	query = strings.ToLower(strings.TrimSpace(query))

	org, _ := req.GetArguments()["organization"].(string)
	workspace, _ := req.GetArguments()["workspace"].(string)
	resourceType, _ := req.GetArguments()["resource_type"].(string)

	loader := GetLoader()
	state, err := loader.Load(ctx, org, workspace, false, logger)
	if err != nil {
		return ToolError(logger, "loading Terraform state", err)
	}

	resources := ExtractResources(state, loader.SensitivePattern())

	type match struct {
		Address  string                 `json:"address"`
		Type     string                 `json:"type"`
		Module   string                 `json:"module"`
		Matches  map[string]interface{} `json:"matches"`
	}

	var results []match
	for _, r := range resources {
		if resourceType != "" && !strings.EqualFold(r.Type, resourceType) {
			continue
		}
		matched := searchAttrs(r.Attributes, query)
		if len(matched) > 0 {
			results = append(results, match{
				Address: r.Address,
				Type:    r.Type,
				Module:  r.Module,
				Matches: matched,
			})
		}
	}
	if results == nil {
		results = []match{}
	}

	data, err := json.MarshalIndent(map[string]interface{}{
		"query":   query,
		"count":   len(results),
		"results": results,
	}, "", "  ")
	if err != nil {
		return ToolError(logger, "marshaling response", err)
	}
	return mcp.NewToolResultText(string(data)), nil
}

// searchAttrs recursively searches attribute values for query, returning matching key/value pairs.
func searchAttrs(attrs map[string]interface{}, query string) map[string]interface{} {
	matches := make(map[string]interface{})
	for k, v := range attrs {
		if m := searchValue(v, query); m != nil {
			matches[k] = m
		}
	}
	return matches
}

// searchValue searches a single attribute value, descending into nested maps and slices.
func searchValue(v interface{}, query string) interface{} {
	switch val := v.(type) {
	case string:
		if strings.Contains(strings.ToLower(val), query) {
			return val
		}
	case map[string]interface{}:
		if sub := searchAttrs(val, query); len(sub) > 0 {
			return sub
		}
	case []interface{}:
		var hits []interface{}
		for _, e := range val {
			if m := searchValue(e, query); m != nil {
				hits = append(hits, m)
			}
		}
		if len(hits) > 0 {
			return hits
		}
	default:
		// Convert to string for search (numbers, booleans, etc.)
		if strings.Contains(strings.ToLower(fmt.Sprintf("%v", val)), query) {
			return val
		}
	}
	return nil
}
