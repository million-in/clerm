package schema_test

import (
	"strings"
	"testing"

	"github.com/million-in/clerm/internal/schema"
)

const validSchema = `
# provider search schema
schema @general.avail.mandene
  @metadata:
    description: Healthcare provider search and booking
    tags: healthcare, search, booking
    display_name: Clinic Gateway
    category: healthcare
  @route: https://resolver.health.example/clerm
  service: @global.healthcare.search_providers.v1
  service: @verified.healthcare.book_visit.v1

method @global.healthcare.search_providers.v1
  @exec: async.pool
  @args_input: 3
    decl_args: specialty.STRING, latitude.DECIMAL, longitude.DECIMAL
  @args_output: 2
    decl_args: request_id.UUID, providers.ARRAY
    decl_format: json

method @verified.healthcare.book_visit.v1
  @exec: sync
  @args_input: 2
    decl_args: provider_id.STRING, user_token.STRING
  @args_output: 2
    decl_args: order_id.STRING, status.STRING
    decl_format: json

relations @general.mandene
  @global: any.protected
  @verified: auth.required
`

func TestParseValidDocument(t *testing.T) {
	doc, err := schema.Parse(strings.NewReader(validSchema))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if doc.Name != "@general.avail.mandene" {
		t.Fatalf("unexpected schema name: %s", doc.Name)
	}
	if doc.Route != "https://resolver.health.example/clerm" {
		t.Fatalf("unexpected route: %s", doc.Route)
	}
	if doc.Metadata.Description != "Healthcare provider search and booking" {
		t.Fatalf("unexpected description: %s", doc.Metadata.Description)
	}
	if doc.Metadata.DisplayName != "Clinic Gateway" {
		t.Fatalf("unexpected display name: %s", doc.Metadata.DisplayName)
	}
	if doc.Metadata.Category != "healthcare" {
		t.Fatalf("unexpected category: %s", doc.Metadata.Category)
	}
	if len(doc.Metadata.Tags) != 3 {
		t.Fatalf("expected 3 tags, got %d", len(doc.Metadata.Tags))
	}
	if len(doc.Services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(doc.Services))
	}
	if len(doc.Methods) != 2 {
		t.Fatalf("expected 2 methods, got %d", len(doc.Methods))
	}
}

func TestParseRejectsUnknownMetadataField(t *testing.T) {
	source := `
schema @general.avail.mandene
  @metadata:
    owner: platform
  @route: https://resolver.health.example/clerm
  service: @global.healthcare.search_providers.v1

method @global.healthcare.search_providers.v1
  @exec: sync
  @args_input: 1
    decl_args: specialty.STRING
  @args_output: 1
    decl_args: providers.ARRAY
    decl_format: json

relations @general.mandene
  @global: any.protected
`

	_, err := schema.Parse(strings.NewReader(source))
	if err == nil {
		t.Fatal("expected metadata error")
	}
	if !strings.Contains(err.Error(), "unknown metadata field") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseRejectsRoutes(t *testing.T) {
	source := `
schema @general.avail.mandene
  @route: https://resolver.health.example/clerm
  service: @global.healthcare.search_providers.v1

method @global.healthcare.search_providers.v1
  @exec: sync
  @routes.search_providers: https://internal.example
  @args_input: 1
    decl_args: specialty.STRING
  @args_output: 1
    decl_args: providers.ARRAY
    decl_format: json

relations @general.mandene
  @global: any.protected
`

	_, err := schema.Parse(strings.NewReader(source))
	if err == nil {
		t.Fatal("expected route rejection error")
	}
	if !strings.Contains(err.Error(), "@routes declarations are not allowed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseShowsOffendingSourceLine(t *testing.T) {
	source := `
schema @general.avail.mandene
  @route: https://resolver.health.example/clerm
  service: @global.healthcare.search_providers.v1

method @global.healthcare.search_providers.v1
  @exec: sync
  @args_input: 1
    decl_argz: specialty.STRING
  @args_output: 1
    decl_args: providers.ARRAY
    decl_format: json

relations @general.mandene
  @global: any.protected
`

	_, err := schema.Parse(strings.NewReader(source))
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "unknown nested declaration keyword; expected decl_args or decl_format") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "column 1") {
		t.Fatalf("missing column in error: %v", err)
	}
	if !strings.Contains(err.Error(), `source: "decl_argz: specialty.STRING"`) {
		t.Fatalf("missing source line in error: %v", err)
	}
	if !strings.Contains(err.Error(), `pointer: "^^^^^^^^^"`) {
		t.Fatalf("missing pointer in error: %v", err)
	}
}

func TestParseShowsAvailableArgumentTypes(t *testing.T) {
	source := `
schema @general.avail.mandene
  @route: https://resolver.health.example/clerm
  service: @global.healthcare.search_providers.v1

method @global.healthcare.search_providers.v1
  @exec: sync
  @args_input: 3
    decl_args: product_id.STRING, user_id.STRING, quantity.NUMBER
  @args_output: 1
    decl_args: providers.ARRAY
    decl_format: json

relations @general.mandene
  @global: any.protected
`

	_, err := schema.Parse(strings.NewReader(source))
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), `unknown argument type "NUMBER"; available types: STRING, DECIMAL, UUID, ARRAY, TIMESTAMP, INT, BOOL`) {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), `source: "decl_args: product_id.STRING, user_id.STRING, quantity.NUMBER"`) {
		t.Fatalf("missing source line in error: %v", err)
	}
	if !strings.Contains(err.Error(), "column 56") {
		t.Fatalf("missing type column in error: %v", err)
	}
	if !strings.Contains(err.Error(), `pointer: "                                                       ^^^^^^"`) {
		t.Fatalf("missing type pointer in error: %v", err)
	}
}

func TestParseRejectsArgCountMismatch(t *testing.T) {
	source := `
schema @general.avail.mandene
  @route: https://resolver.health.example/clerm
  service: @global.healthcare.search_providers.v1

method @global.healthcare.search_providers.v1
  @exec: sync
  @args_input: 2
    decl_args: specialty.STRING
  @args_output: 1
    decl_args: providers.ARRAY
    decl_format: json

relations @general.mandene
  @global: any.protected
`

	_, err := schema.Parse(strings.NewReader(source))
	if err == nil {
		t.Fatal("expected count mismatch error")
	}
	if !strings.Contains(err.Error(), "@args_input count does not match decl_args") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseShowsUndeclaredMethodDeclarationHint(t *testing.T) {
	source := `
schema @general.avail.mandene
  @route: https://resolver.health.example/clerm
  service: @global.healthcare.search_providers.v1

method @verified.healthcare.book_visit.v1
  @exec: sync
  @args_input: 2
    decl_args: provider_id.STRING, user_token.STRING
  @args_output: 2
    decl_args: order_id.STRING, status.STRING
    decl_format: json

relations @general.mandene
  @global: any.protected
  @verified: auth.required
`

	_, err := schema.Parse(strings.NewReader(source))
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "method @verified.healthcare.book_visit.v1 must be declared in schema avail before it is defined") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), `"service: @verified.healthcare.book_visit.v1"`) {
		t.Fatalf("missing declaration hint: %v", err)
	}
	if !strings.Contains(err.Error(), "declared services: @global.healthcare.search_providers.v1") {
		t.Fatalf("missing declared service list: %v", err)
	}
}
