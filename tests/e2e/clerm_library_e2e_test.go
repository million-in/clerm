package e2e_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/million-in/clerm"
	"github.com/million-in/clerm/resolver"
	"github.com/million-in/clerm/tests/testkit"
)

func TestLibraryWorkflowE2E(t *testing.T) {
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
	if err := service.Bind(testkit.GlobalMethodRef, func(context.Context, *resolver.Invocation) (*resolver.Result, error) {
		return resolver.Success(map[string]any{
			"request_id": "123e4567-e89b-12d3-a456-426614174000",
			"providers":  []map[string]any{{"id": "provider-1"}},
		}), nil
	}); err != nil {
		t.Fatalf("service.Bind() error = %v", err)
	}

	request, err := clerm.Compiler.EncodeRequest(compileResult.Document, clerm.BuildRequestInput{
		MethodReference:  testkit.GlobalMethodRef,
		AllowedRelations: []string{"@global"},
		PayloadJSON:      []byte(testkit.GlobalPayloadJSON),
	})
	if err != nil {
		t.Fatalf("Compiler.EncodeRequest() error = %v", err)
	}

	response, command, err := service.ExecuteBinary(context.Background(), request.Payload, "internal.search")
	if err != nil {
		t.Fatalf("service.ExecuteBinary() error = %v", err)
	}
	if command.Target != "internal.search" {
		t.Fatalf("unexpected command target: %#v", command)
	}
	values, err := response.AsMap()
	if err != nil {
		t.Fatalf("response.AsMap() error = %v", err)
	}
	if values["request_id"] != "123e4567-e89b-12d3-a456-426614174000" {
		t.Fatalf("unexpected response values: %#v", values)
	}
}
