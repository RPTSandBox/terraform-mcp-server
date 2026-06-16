// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package tools

import "testing"

// TestSearchAttrsRecursesArrays verifies Finding 2: search descends into arrays and returns only
// the matching sub-elements, never the whole raw array.
func TestSearchAttrsRecursesArrays(t *testing.T) {
	attrs := map[string]interface{}{
		"rules": []interface{}{
			map[string]interface{}{"cidr": "10.0.0.0/8", "note": "internal"},
			map[string]interface{}{"cidr": "1.2.3.4/32", "note": "match-here"},
		},
	}

	matches := searchAttrs(attrs, "match-here")
	rules, ok := matches["rules"].([]interface{})
	if !ok {
		t.Fatalf("expected rules array in matches, got %T", matches["rules"])
	}
	if len(rules) != 1 {
		t.Fatalf("expected only the matching element, got %d", len(rules))
	}
	entry := rules[0].(map[string]interface{})
	if entry["note"] != "match-here" {
		t.Errorf("wrong element returned: %v", entry)
	}
}

// TestSearchAttrsNoFalseArrayLeak verifies a non-matching array yields no match (and therefore
// cannot leak unrelated raw array contents).
func TestSearchAttrsNoFalseArrayLeak(t *testing.T) {
	attrs := map[string]interface{}{
		"rules": []interface{}{
			map[string]interface{}{"secret": "do-not-leak"},
		},
	}
	if m := searchAttrs(attrs, "unrelated-query"); len(m) != 0 {
		t.Errorf("expected no matches, got %v", m)
	}
}
