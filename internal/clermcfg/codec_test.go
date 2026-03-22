package clermcfg_test

import (
	"strings"
	"testing"

	"github.com/million-in/clerm/internal/clermcfg"
	"github.com/million-in/clerm/internal/schema"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	doc, err := schema.Parse(strings.NewReader(`
schema @general.avail.mandene
  @metadata:
    description: Healthcare provider search
    tags: healthcare, providers
    display_name: Provider Search
    category: healthcare
  @route: https://resolver.health.example/clerm
  service: @global.healthcare.search_providers.v1

method @global.healthcare.search_providers.v1
  @exec: async.pool
  @args_input: 3
    decl_args: specialty.STRING, latitude.DECIMAL, longitude.DECIMAL
  @args_output: 2
    decl_args: request_id.UUID, providers.ARRAY
    decl_format: json

relations @general.mandene
  @global: any.protected
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	encoded, err := clermcfg.Encode(doc)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	decoded, err := clermcfg.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if decoded.Name != doc.Name {
		t.Fatalf("decoded schema name mismatch: got %s want %s", decoded.Name, doc.Name)
	}
	if decoded.Route != doc.Route {
		t.Fatalf("decoded route mismatch: got %s want %s", decoded.Route, doc.Route)
	}
	if decoded.Metadata.Description != doc.Metadata.Description {
		t.Fatalf("decoded metadata description mismatch: got %s want %s", decoded.Metadata.Description, doc.Metadata.Description)
	}
	if decoded.Metadata.DisplayName != doc.Metadata.DisplayName {
		t.Fatalf("decoded metadata display name mismatch: got %s want %s", decoded.Metadata.DisplayName, doc.Metadata.DisplayName)
	}
	if decoded.Metadata.Category != doc.Metadata.Category {
		t.Fatalf("decoded metadata category mismatch: got %s want %s", decoded.Metadata.Category, doc.Metadata.Category)
	}
	if len(decoded.Metadata.Tags) != len(doc.Metadata.Tags) {
		t.Fatalf("decoded metadata tags mismatch: got %d want %d", len(decoded.Metadata.Tags), len(doc.Metadata.Tags))
	}
	if len(decoded.Methods) != 1 {
		t.Fatalf("expected 1 method, got %d", len(decoded.Methods))
	}
	if decoded.Methods[0].Reference.Raw != "@global.healthcare.search_providers.v1" {
		t.Fatalf("unexpected decoded method: %s", decoded.Methods[0].Reference.Raw)
	}
}
