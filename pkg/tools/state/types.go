// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package tools

// TerraformState represents a Terraform state file (v4 format).
type TerraformState struct {
	Version          int                    `json:"version"`
	TerraformVersion string                 `json:"terraform_version"`
	Serial           int64                  `json:"serial"`
	Lineage          string                 `json:"lineage"`
	Outputs          map[string]StateOutput `json:"outputs"`
	Resources        []StateResource        `json:"resources"`
}

// StateOutput represents a single Terraform output value.
type StateOutput struct {
	Value     interface{} `json:"value"`
	Type      interface{} `json:"type"`
	Sensitive bool        `json:"sensitive"`
}

// StateResource represents a resource block in Terraform state.
type StateResource struct {
	Mode      string          `json:"mode"`
	Type      string          `json:"type"`
	Name      string          `json:"name"`
	Module    string          `json:"module"`
	Provider  string          `json:"provider"`
	Instances []StateInstance `json:"instances"`
}

// StateInstance represents one instance of a resource.
type StateInstance struct {
	IndexKey            interface{}            `json:"index_key"`
	Attributes          map[string]interface{} `json:"attributes"`
	SensitiveAttributes []interface{}          `json:"sensitive_attributes"`
	Dependencies        []string               `json:"dependencies"`
}

// ExtractedResource is a flattened, redacted resource instance with a computed address.
type ExtractedResource struct {
	Address             string                 `json:"address"`
	Type                string                 `json:"type"`
	Name                string                 `json:"name"`
	Module              string                 `json:"module"`
	Mode                string                 `json:"mode"`
	Provider            string                 `json:"provider"`
	Attributes          map[string]interface{} `json:"attributes"`
	SensitiveAttributes []interface{}          `json:"sensitive_attributes"`
	Dependencies        []string               `json:"dependencies"`
}
