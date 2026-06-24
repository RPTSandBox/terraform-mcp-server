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

// TFGetOutputs returns a tool that retrieves Terraform output values from state.
func TFGetOutputs(logger *log.Logger) server.ServerTool {
	return server.ServerTool{
		Tool: mcp.NewTool("tf_get_outputs",
			mcp.WithDescription("Get Terraform output values from state. "+
				"Outputs flagged sensitive in state, plus those whose name or nested keys match the "+
				"configured redaction pattern, are redacted; unflagged secrets may still appear, so review "+
				"output before sharing. Optionally retrieve a single named output."),
			mcp.WithTitleAnnotation("Get Terraform State Outputs"),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithOpenWorldHintAnnotation(true),
			mcp.WithString("organization",
				mcp.Description("TFC/TFE organization name (required when TF_STATE_BACKEND=tfc; falls back to TF_CLOUD_ORG env var)"),
			),
			mcp.WithString("workspace",
				mcp.Description("TFC/TFE workspace name (required when TF_STATE_BACKEND=tfc)"),
			),
			mcp.WithString("output_name",
				mcp.Description("Name of a specific output to retrieve; omit to return all outputs"),
			),
		),
		Handler: func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return tfGetOutputsHandler(ctx, req, logger)
		},
	}
}

func tfGetOutputsHandler(ctx context.Context, req mcp.CallToolRequest, logger *log.Logger) (*mcp.CallToolResult, error) {
	org, _ := req.GetArguments()["organization"].(string)
	workspace, _ := req.GetArguments()["workspace"].(string)
	outputName, _ := req.GetArguments()["output_name"].(string)
	outputName = strings.TrimSpace(outputName)

	loader := GetLoader()
	state, err := loader.Load(ctx, org, workspace, false, logger)
	if err != nil {
		return ToolError(logger, "loading Terraform state", err)
	}

	pattern := loader.SensitivePattern()

	type outputResult struct {
		Name      string      `json:"name"`
		Value     interface{} `json:"value"`
		Type      interface{} `json:"type"`
		Sensitive bool        `json:"sensitive"`
	}

	var outputs []outputResult
	for name, out := range state.Outputs {
		if outputName != "" && name != outputName {
			continue
		}
		val := out.Value
		switch {
		case out.Sensitive:
			val = "[REDACTED - sensitive output]"
		case pattern != nil && pattern.MatchString(name):
			// The output's own name matches the redaction pattern (e.g. "db_password").
			val = redactedPattern
		case pattern != nil:
			// Apply the same key-name pattern pass used for resource attributes so
			// secrets nested inside a structured output value are also redacted.
			val = redactValue(out.Value, pattern)
		}
		outputs = append(outputs, outputResult{
			Name:      name,
			Value:     val,
			Type:      out.Type,
			Sensitive: out.Sensitive,
		})
	}

	if outputName != "" && len(outputs) == 0 {
		return ToolErrorf(logger, "output %q not found in state", outputName)
	}
	if outputs == nil {
		outputs = []outputResult{}
	}

	data, err := json.MarshalIndent(outputs, "", "  ")
	if err != nil {
		return ToolError(logger, "marshaling response", err)
	}
	return mcp.NewToolResultText(string(data)), nil
}
