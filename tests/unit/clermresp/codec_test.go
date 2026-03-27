package clermresp_test

import (
	"bytes"
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

func TestWriteToMatchesEncode(t *testing.T) {
	method := mustMethod(t)
	response, err := clermresp.BuildSuccess(method, []byte(`{"request_id":"123e4567-e89b-12d3-a456-426614174000","providers":[]}`))
	if err != nil {
		t.Fatalf("BuildSuccess() error = %v", err)
	}
	encoded, err := clermresp.Encode(response)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	var buf bytes.Buffer
	if err := clermresp.WriteTo(&buf, response); err != nil {
		t.Fatalf("WriteTo() error = %v", err)
	}
	if !bytes.Equal(buf.Bytes(), encoded) {
		t.Fatalf("WriteTo output did not match Encode output")
	}
}

func TestEncodeRejectsOversizedStringsWithoutPanic(t *testing.T) {
	oversized := strings.Repeat("m", 1<<16)
	response := &clermresp.Response{Method: oversized}
	if _, err := clermresp.Encode(response); err == nil || !strings.Contains(err.Error(), "response string too large") {
		t.Fatalf("expected oversized string error, got %v", err)
	}
}

func TestDecodeOwnsOutputPayloadBytes(t *testing.T) {
	method := mustMethod(t)
	response, err := clermresp.BuildSuccess(method, []byte(`{"request_id":"123e4567-e89b-12d3-a456-426614174000","providers":[{"id":"p-1"}]}`))
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
	want := string(decoded.Outputs[1].Raw)
	for i := range encoded {
		encoded[i] = 'x'
	}
	if string(decoded.Outputs[1].Raw) != want {
		t.Fatalf("decoded output payload corrupted after input mutation: got %q want %q", string(decoded.Outputs[1].Raw), want)
	}
}

func TestDecodeRejectsImpossibleOutputCount(t *testing.T) {
	payload := []byte{'C', 'L', 'R', 'S', 0, 1, 0, 0, 0, 0xff, 0xff}

	if _, err := clermresp.Decode(payload); err == nil || !strings.Contains(err.Error(), "response output count exceeds remaining payload") {
		t.Fatalf("expected output count guard, got %v", err)
	}
}
