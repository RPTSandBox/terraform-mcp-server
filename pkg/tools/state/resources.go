// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package tools

import (
	"fmt"
	"regexp"
)

// ExtractResources flattens all resource instances from state, applying sensitive-attribute
// redaction from the manifest and any operator-configured pattern.
func ExtractResources(state *TerraformState, sensitivePattern *regexp.Regexp) []ExtractedResource {
	var out []ExtractedResource
	for _, res := range state.Resources {
		for _, inst := range res.Instances {
			attrs := redactSensitiveAttrs(inst.Attributes, inst.SensitiveAttributes)
			if sensitivePattern != nil {
				attrs = applyPatternRedaction(attrs, sensitivePattern)
			}
			module := res.Module
			if module == "" {
				module = "(root)"
			}
			deps := inst.Dependencies
			if deps == nil {
				deps = []string{}
			}
			sa := inst.SensitiveAttributes
			if sa == nil {
				sa = []interface{}{}
			}
			out = append(out, ExtractedResource{
				Address:             buildAddress(res, inst),
				Type:                res.Type,
				Name:                res.Name,
				Module:              module,
				Mode:                res.Mode,
				Provider:            res.Provider,
				Attributes:          attrs,
				SensitiveAttributes: sa,
				Dependencies:        deps,
			})
		}
	}
	return out
}

func buildAddress(res StateResource, inst StateInstance) string {
	prefix := ""
	if res.Module != "" {
		prefix = res.Module + "."
	}
	if res.Mode == "data" {
		prefix = prefix + "data."
	}
	base := fmt.Sprintf("%s%s.%s", prefix, res.Type, res.Name)
	if inst.IndexKey == nil {
		return base
	}
	switch v := inst.IndexKey.(type) {
	case string:
		return fmt.Sprintf(`%s["%s"]`, base, v)
	case float64:
		return fmt.Sprintf("%s[%d]", base, int(v))
	default:
		return fmt.Sprintf("%s[%v]", base, v)
	}
}
