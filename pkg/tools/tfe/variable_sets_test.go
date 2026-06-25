// Copyright IBM Corp. 2025
// SPDX-License-Identifier: MPL-2.0

package tools

import (
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestListVariableSets(t *testing.T) {
	logger := log.New()
	logger.SetLevel(log.ErrorLevel)

	t.Run("tool creation", func(t *testing.T) {
		tool := ListVariableSets(logger)

		assert.Equal(t, "list_variable_sets", tool.Tool.Name)
		assert.Contains(t, tool.Tool.Description, "List all variable sets in an organization")
		assert.NotNil(t, tool.Handler)

		assert.Contains(t, tool.Tool.InputSchema.Required, "terraform_org_name")
	})
}
