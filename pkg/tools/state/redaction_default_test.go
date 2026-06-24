// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package tools

import (
	"testing"
)

// TestDefaultSensitivePatternEnabledWhenUnset verifies Finding 1: with no
// TF_SENSITIVE_ATTR_PATTERN configured, the loader still ships a non-nil default
// pattern so unflagged secret-keyed attributes are redacted rather than returned raw.
func TestDefaultSensitivePatternEnabledWhenUnset(t *testing.T) {
	t.Setenv("TF_SENSITIVE_ATTR_PATTERN", "")
	l := newStateLoader()
	if l.SensitivePattern() == nil {
		t.Fatal("default sensitive pattern is nil; redaction is opt-in (Finding 1 regression)")
	}

	// An attribute the state never flagged as sensitive but whose key name screams secret.
	state := stateJSON(t, `{
		"version": 4,
		"resources": [{
			"mode": "managed", "type": "x", "name": "y", "provider": "p",
			"instances": [{
				"attributes": {"password": "hunter2", "connection_string": "postgres://u:p@h/db", "region": "us-east-1"},
				"sensitive_attributes": []
			}]
		}]
	}`)

	attrs := ExtractResources(state, l.SensitivePattern())[0].Attributes
	if attrs["password"] != redactedPattern {
		t.Errorf("unflagged password not redacted by default: %v", attrs["password"])
	}
	if attrs["connection_string"] != redactedPattern {
		t.Errorf("unflagged connection_string not redacted by default: %v", attrs["connection_string"])
	}
	if attrs["region"] != "us-east-1" {
		t.Errorf("non-sensitive attribute altered: %v", attrs["region"])
	}
}

// TestInvalidPatternFailsClosed verifies an invalid operator override does not silently
// disable redaction: it falls back to the built-in default pattern (fail closed).
func TestInvalidPatternFailsClosed(t *testing.T) {
	t.Setenv("TF_SENSITIVE_ATTR_PATTERN", "(unclosed[group")
	l := newStateLoader()
	if l.SensitivePattern() == nil {
		t.Fatal("invalid pattern disabled redaction instead of falling back to default")
	}
	if !l.SensitivePattern().MatchString("password") {
		t.Error("fallback pattern does not match a known secret key name")
	}
}

// TestOperatorPatternOverridesDefault verifies a valid override replaces the default.
func TestOperatorPatternOverridesDefault(t *testing.T) {
	t.Setenv("TF_SENSITIVE_ATTR_PATTERN", "supersekret")
	l := newStateLoader()
	if !l.SensitivePattern().MatchString("SUPERSEKRET_value") {
		t.Error("operator override pattern not applied (case-insensitive expected)")
	}
}
