// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package tools

import (
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestListWorkspacePolicySets(t *testing.T) {
	logger := log.New()
	logger.SetLevel(log.ErrorLevel)

	t.Run("tool creation", func(t *testing.T) {
		tool := ListWorkspacePolicySets(logger)

		assert.Equal(t, "list_workspace_policy_sets", tool.Tool.Name)
		assert.Contains(t, tool.Tool.Description, "Read all policy sets")
		assert.NotNil(t, tool.Handler)

		assert.NotNil(t, tool.Tool.Annotations.ReadOnlyHint)
		assert.True(t, *tool.Tool.Annotations.ReadOnlyHint)

		// Check required parameters
		assert.Contains(t, tool.Tool.InputSchema.Required, "terraform_org_name")
		assert.Contains(t, tool.Tool.InputSchema.Required, "workspace_id")
	})
}
