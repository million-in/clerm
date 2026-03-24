package clermwire_test

import (
	"strings"
	"testing"

	"github.com/million-in/clerm/internal/clermwire"
	"github.com/million-in/clerm/internal/schema"
)

func TestValidateValueArrayUsesEnvelopeCheck(t *testing.T) {
	if err := clermwire.ValidateValue(schema.ArgArray, []byte(`[{"id":1}, invalid]`)); err != nil {
		t.Fatalf("ValidateValue() error = %v", err)
	}
}

func TestValidateValueArrayRejectsNonArrays(t *testing.T) {
	tests := []string{
		`{"id":1}`,
		`"text"`,
		`true`,
		`[`,
	}
	for _, raw := range tests {
		raw := raw
		t.Run(raw, func(t *testing.T) {
			err := clermwire.ValidateValue(schema.ArgArray, []byte(raw))
			if err == nil || !strings.Contains(err.Error(), "JSON array framing") {
				t.Fatalf("expected array framing error, got %v", err)
			}
		})
	}
}

func TestBuildValuesRejectsNullRequiredString(t *testing.T) {
	_, err := clermwire.BuildValues([]schema.Parameter{{Name: "provider_id", Type: schema.ArgString}}, []byte(`{"provider_id":null}`), "input")
	if err == nil || !strings.Contains(err.Error(), "invalid value for provider_id") {
		t.Fatalf("expected null string validation error, got %v", err)
	}
}
