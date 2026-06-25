// Copyright IBM Corp. 2025
// SPDX-License-Identifier: MPL-2.0

package tools

import (
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestListWorkspaceVariables(t *testing.T) {
	logger := log.New()
	logger.SetLevel(log.ErrorLevel)

	t.Run("tool creation", func(t *testing.T) {
		tool := ListWorkspaceVariables(logger)

		assert.Equal(t, "list_workspace_variables", tool.Tool.Name)
		assert.Contains(t, tool.Tool.Description, "List all variables in a Terraform workspace")
		assert.NotNil(t, tool.Handler)

		assert.Contains(t, tool.Tool.InputSchema.Required, "terraform_org_name")
		assert.Contains(t, tool.Tool.InputSchema.Required, "workspace_name")
	})
}
