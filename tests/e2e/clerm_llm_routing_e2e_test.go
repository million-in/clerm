package e2e_test

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/million-in/clerm"
	"github.com/million-in/clerm/tests/testkit"
)

func TestLLMToolToCLERMToRESTE2E(t *testing.T) {
	schemaPath := filepath.Join(t.TempDir(), "schema.clermfile")
	testkit.WriteSchemaFile(t, schemaPath)

	compileResult, err := clerm.Compiler.WriteCompiledConfig(schemaPath, "")
	if err != nil {
		t.Fatalf("Compiler.WriteCompiledConfig() error = %v", err)
	}
	service, err := clerm.Resolver.LoadService(compileResult.OutputPath, clerm.ServiceOptions{})
	if err != nil {
		t.Fatalf("Resolver.LoadService() error = %v", err)
	}

	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"request_id":"123e4567-e89b-12d3-a456-426614174000","providers":[{"id":"provider-1"}]}`)),
		}, nil
	})}

	if err := clerm.Resolver.BindREST(service, testkit.GlobalMethodRef, client, clerm.RESTRoute{
		URL: "https://internal.example/search",
	}); err != nil {
		t.Fatalf("Resolver.BindREST() error = %v", err)
	}

	tools, err := clerm.Compiler.OpenAITools(compileResult.Document, []string{"@global"})
	if err != nil {
		t.Fatalf("Compiler.OpenAITools() error = %v", err)
	}
	request, err := clerm.Compiler.EncodeOpenAIToolCallJSON(compileResult.Document, []byte(`{"type":"function","function":{"name":"`+tools[0].Function.Name+`","arguments":"{\"specialty\":\"cardiology\",\"latitude\":40.7,\"longitude\":-73.9}"}}`), clerm.ToolCallOptions{
		AllowedRelations: []string{"@global"},
	})
	if err != nil {
		t.Fatalf("Compiler.EncodeOpenAIToolCallJSON() error = %v", err)
	}

	executed, err := clerm.Resolver.ExecuteBinary(context.Background(), service, request.Payload, "internal.search")
	if err != nil {
		t.Fatalf("Resolver.ExecuteBinary() error = %v", err)
	}
	if executed.Command == nil || executed.Command.Method != testkit.GlobalMethodRef {
		t.Fatalf("unexpected execution command: %#v", executed.Command)
	}
	values, err := executed.Response.AsMap()
	if err != nil {
		t.Fatalf("response.AsMap() error = %v", err)
	}
	if values["request_id"] != "123e4567-e89b-12d3-a456-426614174000" {
		t.Fatalf("unexpected response values: %#v", values)
	}
	if len(executed.Payload) == 0 {
		t.Fatal("expected encoded response payload")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
