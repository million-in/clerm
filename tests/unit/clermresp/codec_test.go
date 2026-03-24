package clermresp_test

import (
	"strings"
	"testing"

	"github.com/million-in/clerm/clermresp"
	"github.com/million-in/clerm/schema"
)

func mustMethod(t *testing.T) schema.Method {
	t.Helper()
	doc, err := schema.Parse(strings.NewReader(`
schema @general.avail.mandene
  @route: https://resolver.health.example/clerm
  service: @global.healthcare.search_providers.v1

method @global.healthcare.search_providers.v1
  @exec: async.pool
  @args_input: 1
    decl_args: specialty.STRING
  @args_output: 2
    decl_args: request_id.UUID, providers.ARRAY
    decl_format: json

relations @general.mandene
  @global: any.protected
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	method, ok := doc.MethodByReference("@global.healthcare.search_providers.v1")
	if !ok {
		t.Fatal("method missing")
	}
	return method
}

func TestEncodeDecodeSuccessResponse(t *testing.T) {
	method := mustMethod(t)
	response, err := clermresp.BuildSuccess(method, []byte(`{"request_id":"123e4567-e89b-12d3-a456-426614174000","providers":[]}`))
	if err != nil {
		t.Fatalf("BuildSuccess() error = %v", err)
	}
	encoded, err := clermresp.Encode(response)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	decoded, err := clermresp.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if err := decoded.ValidateAgainst(method); err != nil {
		t.Fatalf("ValidateAgainst() error = %v", err)
	}
	values, err := decoded.AsMap()
	if err != nil {
		t.Fatalf("AsMap() error = %v", err)
	}
	if values["request_id"] != "123e4567-e89b-12d3-a456-426614174000" {
		t.Fatalf("unexpected values: %#v", values)
	}
}

func TestEncodeDecodeErrorResponse(t *testing.T) {
	method := mustMethod(t)
	response, err := clermresp.BuildError(method, "validation_error", "provider lookup failed")
	if err != nil {
		t.Fatalf("BuildError() error = %v", err)
	}
	encoded, err := clermresp.Encode(response)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	decoded, err := clermresp.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if decoded.Error.Code != "validation_error" || decoded.Error.Message != "provider lookup failed" {
		t.Fatalf("unexpected error body: %#v", decoded.Error)
	}
}
