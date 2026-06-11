// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package tools

import (
	"encoding/json"
	"fmt"
	"regexp"
)

const (
	redactedSensitive = "[REDACTED - sensitive]"
	redactedPattern   = "[REDACTED - pattern match]"
)

// redactSensitiveAttrs returns a deep copy of attrs with values at sensitive paths replaced.
// sensitivePaths matches the Terraform state v4 sensitive_attributes format:
// each element is either a string key or a []interface{} path of nested keys.
func redactSensitiveAttrs(attrs map[string]interface{}, sensitivePaths []interface{}) map[string]interface{} {
	result := deepCopyMap(attrs)
	if len(sensitivePaths) == 0 || len(result) == 0 {
		return result
	}
	for _, raw := range sensitivePaths {
		var segments []string
		switch p := raw.(type) {
		case string:
			if p != "" {
				segments = []string{p}
			}
		case []interface{}:
			for _, seg := range p {
				switch s := seg.(type) {
				case string:
					segments = append(segments, s)
				default:
					segments = append(segments, fmt.Sprintf("%v", s))
				}
			}
		}
		if len(segments) > 0 {
			setAtPath(result, segments)
		}
	}
	return result
}

func setAtPath(obj map[string]interface{}, segments []string) {
	if len(segments) == 0 {
		return
	}
	key := segments[0]
	if len(segments) == 1 {
		if _, exists := obj[key]; exists {
			obj[key] = redactedSensitive
		}
		return
	}
	if child, ok := obj[key].(map[string]interface{}); ok {
		setAtPath(child, segments[1:])
	}
}

// applyPatternRedaction recursively redacts attribute values whose keys match pattern.
func applyPatternRedaction(attrs map[string]interface{}, pattern *regexp.Regexp) map[string]interface{} {
	result := make(map[string]interface{}, len(attrs))
	for k, v := range attrs {
		if pattern.MatchString(k) {
			result[k] = redactedPattern
		} else if nested, ok := v.(map[string]interface{}); ok {
			result[k] = applyPatternRedaction(nested, pattern)
		} else {
			result[k] = v
		}
	}
	return result
}

func deepCopyMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	data, err := json.Marshal(m)
	if err != nil {
		return m
	}
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		return m
	}
	return out
}
