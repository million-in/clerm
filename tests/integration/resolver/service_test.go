package resolver_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/million-in/clerm/internal/capability"
	"github.com/million-in/clerm/internal/clermcfg"
	"github.com/million-in/clerm/internal/clermreq"
	"github.com/million-in/clerm/internal/clermresp"
	"github.com/million-in/clerm/internal/platform"
	"github.com/million-in/clerm/internal/resolver"
	"github.com/million-in/clerm/internal/schema"
)

func mustDocument(t *testing.T) *schema.Document {
	t.Helper()
	doc, err := schema.Parse(strings.NewReader(`
schema @general.avail.mandene
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
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	return doc
}

func mustEncodedRequest(t *testing.T, doc *schema.Document, methodRef string, payload string) []byte {
	t.Helper()
	method, ok := doc.MethodByReference(methodRef)
	if !ok {
		t.Fatalf("MethodByReference(%q) missing", methodRef)
	}
	request, err := clermreq.Build(method, []byte(payload))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	encoded, err := clermreq.Encode(request)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	return encoded
}

func mustVerifiedRequest(t *testing.T, doc *schema.Document, target string) (*resolver.Service, []byte) {
	t.Helper()
	method, ok := doc.MethodByReference("@verified.healthcare.book_visit.v1")
	if !ok {
		t.Fatal("verified method missing")
	}
	request, err := clermreq.Build(method, []byte(`{"provider_id":"abc123","user_token":"tok_123"}`))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	publicKey, privateKey, err := capability.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair() error = %v", err)
	}
	now := time.Now().UTC()
	token, err := capability.Issue(capability.IssueOptions{
		KeyID:      "registry",
		Issuer:     "clerm_registry",
		Subject:    "partner-123",
		Schema:     doc.Name,
		SchemaHash: doc.PublicFingerprint(),
		Relation:   "@verified",
		Condition:  "auth.required",
		Methods:    []string{method.Reference.Raw},
		Targets:    []string{target},
		IssuedAt:   now,
		NotBefore:  now,
		ExpiresAt:  now.Add(30 * time.Minute),
	}, privateKey)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	encodedToken, err := capability.Encode(token)
	if err != nil {
		t.Fatalf("Encode(token) error = %v", err)
	}
	if err := request.SetCapabilityRaw(encodedToken); err != nil {
		t.Fatalf("SetCapabilityRaw() error = %v", err)
	}
	encodedRequest, err := clermreq.Encode(request)
	if err != nil {
		t.Fatalf("Encode(request) error = %v", err)
	}
	service := resolver.New(doc)
	service.SetCapabilityKeyring(capability.NewKeyring(map[string]ed25519.PublicKey{"registry": publicKey}))
	return service, encodedRequest
}

func TestResolveBinaryReturnsCommand(t *testing.T) {
	doc := mustDocument(t)
	service := resolver.New(doc)
	payload := mustEncodedRequest(t, doc, "@global.healthcare.search_providers.v1", `{"specialty":"cardiology","latitude":40.7,"longitude":-73.9}`)

	command, err := service.ResolveBinaryWithTarget(payload, "internal.search")
	if err != nil {
		t.Fatalf("ResolveBinaryWithTarget() error = %v", err)
	}
	if command.Method != "@global.healthcare.search_providers.v1" {
		t.Fatalf("unexpected method: %s", command.Method)
	}
	if command.Target != "internal.search" {
		t.Fatalf("unexpected target: %s", command.Target)
	}
	if len(command.SchemaFingerprint) != 64 {
		t.Fatalf("unexpected schema fingerprint: %q", command.SchemaFingerprint)
	}
}

func TestLoadConfigRejectsOversizedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized.clermcfg")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := file.Truncate((8 << 20) + 1); err != nil {
		file.Close()
		t.Fatalf("Truncate() error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	_, err = resolver.LoadConfig(path)
	if err == nil || !strings.Contains(err.Error(), "exceeds configured size limit") {
		t.Fatalf("expected size limit error, got %v", err)
	}
}

func TestLoadConfigLoadsCompiledConfig(t *testing.T) {
	doc := mustDocument(t)
	payload, err := clermcfg.Encode(doc)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	path := filepath.Join(t.TempDir(), "schema.clermcfg")
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	service, err := resolver.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if service.Document().Name != doc.Name {
		t.Fatalf("unexpected schema name: %s", service.Document().Name)
	}
}

func TestMiddlewarePassesThroughNonCLERM(t *testing.T) {
	service := resolver.New(mustDocument(t))
	nextCalled := false
	handler := service.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !nextCalled {
		t.Fatal("expected next handler to be called")
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
}

func TestServeHTTPEncodesSuccessResponse(t *testing.T) {
	doc := mustDocument(t)
	service := resolver.New(doc)
	if err := service.Bind("@global.healthcare.search_providers.v1", func(_ context.Context, invocation *resolver.Invocation) (*resolver.Result, error) {
		if invocation.Target != "internal.search" {
			t.Fatalf("unexpected invocation target: %s", invocation.Target)
		}
		return resolver.Success(map[string]any{
			"request_id": "123e4567-e89b-12d3-a456-426614174000",
			"providers":  []map[string]any{{"id": "p-1"}},
		}), nil
	}); err != nil {
		t.Fatalf("Bind() error = %v", err)
	}
	payload := mustEncodedRequest(t, doc, "@global.healthcare.search_providers.v1", `{"specialty":"cardiology","latitude":40.7,"longitude":-73.9}`)
	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/clerm")
	req.Header.Set("Clerm-Target", "internal.search")
	rec := httptest.NewRecorder()

	service.Middleware(http.NotFoundHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/clerm" {
		t.Fatalf("unexpected content type: %s", got)
	}
	response, err := clermresp.Decode(rec.Body.Bytes())
	if err != nil {
		t.Fatalf("Decode(response) error = %v", err)
	}
	if response.Method != "@global.healthcare.search_providers.v1" {
		t.Fatalf("unexpected response method: %s", response.Method)
	}
	values, err := response.AsMap()
	if err != nil {
		t.Fatalf("AsMap() error = %v", err)
	}
	if values["request_id"] != "123e4567-e89b-12d3-a456-426614174000" {
		t.Fatalf("unexpected outputs: %#v", values)
	}
}

func TestConcurrentBindPreservesAllHandlers(t *testing.T) {
	doc, err := schema.Parse(strings.NewReader(`
schema @general.avail.mandene
  @route: https://resolver.health.example/clerm
  service: @global.healthcare.search_providers.v1
  service: @global.healthcare.list_providers.v1
  service: @global.healthcare.check_provider.v1

method @global.healthcare.search_providers.v1
  @exec: sync
  @args_input: 1
    decl_args: specialty.STRING
  @args_output: 1
    decl_args: request_id.UUID
    decl_format: json

method @global.healthcare.list_providers.v1
  @exec: sync
  @args_input: 1
    decl_args: region.STRING
  @args_output: 1
    decl_args: request_id.UUID
    decl_format: json

method @global.healthcare.check_provider.v1
  @exec: sync
  @args_input: 1
    decl_args: provider_id.STRING
  @args_output: 1
    decl_args: request_id.UUID
    decl_format: json

relations @general.mandene
  @global: any.protected
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	service := resolver.New(doc)
	methods := []struct {
		ref     string
		payload string
		output  string
	}{
		{ref: "@global.healthcare.search_providers.v1", payload: `{"specialty":"cardiology"}`, output: "123e4567-e89b-12d3-a456-426614174001"},
		{ref: "@global.healthcare.list_providers.v1", payload: `{"region":"ny"}`, output: "123e4567-e89b-12d3-a456-426614174002"},
		{ref: "@global.healthcare.check_provider.v1", payload: `{"provider_id":"p-1"}`, output: "123e4567-e89b-12d3-a456-426614174003"},
	}
	var wg sync.WaitGroup
	for _, item := range methods {
		item := item
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := service.Bind(item.ref, func(context.Context, *resolver.Invocation) (*resolver.Result, error) {
				return resolver.Success(map[string]any{"request_id": item.output}), nil
			}); err != nil {
				t.Errorf("Bind(%s) error = %v", item.ref, err)
			}
		}()
	}
	wg.Wait()
	for _, item := range methods {
		payload := mustEncodedRequest(t, doc, item.ref, item.payload)
		response, _, err := service.ExecuteBinary(context.Background(), payload, "")
		if err != nil {
			t.Fatalf("ExecuteBinary(%s) error = %v", item.ref, err)
		}
		values, err := response.AsMap()
		if err != nil {
			t.Fatalf("AsMap(%s) error = %v", item.ref, err)
		}
		if values["request_id"] != item.output {
			t.Fatalf("unexpected handler output for %s: %#v", item.ref, values)
		}
	}
}

func TestServeHTTPEncodesPrebuiltSuccessResponse(t *testing.T) {
	doc := mustDocument(t)
	service := resolver.New(doc)
	method, ok := doc.MethodByReference("@global.healthcare.search_providers.v1")
	if !ok {
		t.Fatal("method missing")
	}
	response, err := clermresp.BuildSuccessMap(method, map[string]any{
		"request_id": "123e4567-e89b-12d3-a456-426614174000",
		"providers":  []map[string]any{{"id": "p-1"}},
	})
	if err != nil {
		t.Fatalf("BuildSuccessMap() error = %v", err)
	}
	if err := service.Bind("@global.healthcare.search_providers.v1", func(context.Context, *resolver.Invocation) (*resolver.Result, error) {
		return resolver.SuccessResponse(response), nil
	}); err != nil {
		t.Fatalf("Bind() error = %v", err)
	}
	payload := mustEncodedRequest(t, doc, "@global.healthcare.search_providers.v1", `{"specialty":"cardiology","latitude":40.7,"longitude":-73.9}`)
	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/clerm")
	rec := httptest.NewRecorder()

	service.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	decoded, err := clermresp.Decode(rec.Body.Bytes())
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if decoded.Method != method.Reference.Raw {
		t.Fatalf("unexpected method: %s", decoded.Method)
	}
}

func TestInvocationArgumentsMapReturnsIndependentCopies(t *testing.T) {
	doc := mustDocument(t)
	service := resolver.New(doc)
	payload := mustEncodedRequest(t, doc, "@global.healthcare.search_providers.v1", `{"specialty":"cardiology","latitude":40.7,"longitude":-73.9}`)

	invocation, err := service.ResolveInvocationWithTarget(payload, "internal.search")
	if err != nil {
		t.Fatalf("ResolveInvocationWithTarget() error = %v", err)
	}
	first, err := invocation.ArgumentsMap()
	if err != nil {
		t.Fatalf("ArgumentsMap(first) error = %v", err)
	}
	second, err := invocation.ArgumentsMap()
	if err != nil {
		t.Fatalf("ArgumentsMap(second) error = %v", err)
	}
	first["specialty"] = "mutated"
	if second["specialty"] != "cardiology" {
		t.Fatalf("expected cached arguments to remain immutable, got %#v", second)
	}
}

func TestInvocationArgumentsViewSharesReadOnlyCache(t *testing.T) {
	doc := mustDocument(t)
	service := resolver.New(doc)
	payload := mustEncodedRequest(t, doc, "@global.healthcare.search_providers.v1", `{"specialty":"cardiology","latitude":40.7,"longitude":-73.9}`)

	invocation, err := service.ResolveInvocationWithTarget(payload, "internal.search")
	if err != nil {
		t.Fatalf("ResolveInvocationWithTarget() error = %v", err)
	}
	view, err := invocation.Arguments()
	if err != nil {
		t.Fatalf("Arguments() error = %v", err)
	}
	if view.Len() != 3 {
		t.Fatalf("unexpected argument count: %d", view.Len())
	}
	if value, ok := view.Lookup("specialty"); !ok || value != "cardiology" {
		t.Fatalf("Lookup() = (%#v, %t)", value, ok)
	}
	clone := view.Clone()
	clone["specialty"] = "mutated"
	if value, ok := view.Lookup("specialty"); !ok || value != "cardiology" {
		t.Fatalf("view changed after clone mutation: (%#v, %t)", value, ok)
	}
	seen := map[string]bool{}
	view.Range(func(name string, value any) bool {
		seen[name] = true
		return true
	})
	if len(seen) != 3 || !seen["specialty"] || !seen["latitude"] || !seen["longitude"] {
		t.Fatalf("unexpected range coverage: %#v", seen)
	}
}

func TestInvocationArgumentAvoidsPerCallMapClone(t *testing.T) {
	doc := mustDocument(t)
	service := resolver.New(doc)
	payload := mustEncodedRequest(t, doc, "@global.healthcare.search_providers.v1", `{"specialty":"cardiology","latitude":40.7,"longitude":-73.9}`)

	invocation, err := service.ResolveInvocationWithTarget(payload, "internal.search")
	if err != nil {
		t.Fatalf("ResolveInvocationWithTarget() error = %v", err)
	}
	if value, ok, err := invocation.Argument("specialty"); err != nil || !ok || value != "cardiology" {
		t.Fatalf("Argument(warmup) = (%#v, %t, %v)", value, ok, err)
	}

	allocs := testing.AllocsPerRun(100, func() {
		value, ok, err := invocation.Argument("specialty")
		if err != nil || !ok || value != "cardiology" {
			t.Fatalf("Argument() = (%#v, %t, %v)", value, ok, err)
		}
	})
	if allocs != 0 {
		t.Fatalf("expected zero allocations after decode warmup, got %f", allocs)
	}
}

func TestInvocationArgumentsViewAvoidsPerCallClone(t *testing.T) {
	doc := mustDocument(t)
	service := resolver.New(doc)
	payload := mustEncodedRequest(t, doc, "@global.healthcare.search_providers.v1", `{"specialty":"cardiology","latitude":40.7,"longitude":-73.9}`)

	invocation, err := service.ResolveInvocationWithTarget(payload, "internal.search")
	if err != nil {
		t.Fatalf("ResolveInvocationWithTarget() error = %v", err)
	}
	if _, err := invocation.Arguments(); err != nil {
		t.Fatalf("Arguments(warmup) error = %v", err)
	}

	allocs := testing.AllocsPerRun(100, func() {
		view, err := invocation.Arguments()
		if err != nil {
			t.Fatalf("Arguments() error = %v", err)
		}
		if value, ok := view.Lookup("specialty"); !ok || value != "cardiology" {
			t.Fatalf("Lookup() = (%#v, %t)", value, ok)
		}
	})
	if allocs != 0 {
		t.Fatalf("expected zero allocations after decode warmup, got %f", allocs)
	}
}

func TestServiceCloseIsIdempotent(t *testing.T) {
	service := resolver.New(mustDocument(t))
	if err := service.Close(); err != nil {
		t.Fatalf("Close(first) error = %v", err)
	}
	if err := service.Close(); err != nil {
		t.Fatalf("Close(second) error = %v", err)
	}
}

func TestServeHTTPEncodesHandlerErrorResponse(t *testing.T) {
	doc := mustDocument(t)
	service := resolver.New(doc)
	if err := service.Bind("@global.healthcare.search_providers.v1", func(context.Context, *resolver.Invocation) (*resolver.Result, error) {
		return nil, platform.New(platform.CodeValidation, "upstream input was rejected")
	}); err != nil {
		t.Fatalf("Bind() error = %v", err)
	}
	payload := mustEncodedRequest(t, doc, "@global.healthcare.search_providers.v1", `{"specialty":"cardiology","latitude":40.7,"longitude":-73.9}`)
	req := httptest.NewRequest(http.MethodPost, "/api", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/clerm")
	rec := httptest.NewRecorder()

	service.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	response, err := clermresp.Decode(rec.Body.Bytes())
	if err != nil {
		t.Fatalf("Decode(response) error = %v", err)
	}
	if response.Error.Code != string(platform.CodeValidation) {
		t.Fatalf("unexpected error code: %#v", response.Error)
	}
}

func TestServeHTTPFallsBackToCLERMErrorFrameWhenBuildErrorFails(t *testing.T) {
	service := resolver.New(&schema.Document{
		Name: "@general.avail.invalid",
		Methods: []schema.Method{{
			Reference:    schema.ServiceRef{Raw: "", Relation: "@global"},
			Execution:    schema.ExecutionSync,
			InputCount:   0,
			OutputCount:  0,
			OutputFormat: schema.FormatJSON,
		}},
		Relations: []schema.RelationRule{{
			Name:      "@global",
			Condition: "any.protected",
		}},
	})
	if err := service.Bind("", func(context.Context, *resolver.Invocation) (*resolver.Result, error) {
		return nil, platform.New(platform.CodeValidation, "upstream input was rejected")
	}); err != nil {
		t.Fatalf("Bind() error = %v", err)
	}
	payload, err := clermreq.Encode(&clermreq.Request{Method: ""})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/clerm")
	rec := httptest.NewRecorder()

	service.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/clerm" {
		t.Fatalf("unexpected content type: %q", got)
	}
	response, err := clermresp.Decode(rec.Body.Bytes())
	if err != nil {
		t.Fatalf("Decode(response) error = %v", err)
	}
	if response.Error.Code != string(platform.CodeValidation) {
		t.Fatalf("unexpected error code: %#v", response.Error)
	}
	if response.Error.Message != "upstream input was rejected" {
		t.Fatalf("unexpected error message: %#v", response.Error)
	}
}

func TestServeHTTPRequiresCapabilityForVerifiedRelation(t *testing.T) {
	doc := mustDocument(t)
	service := resolver.New(doc)
	payload := mustEncodedRequest(t, doc, "@verified.healthcare.book_visit.v1", `{"provider_id":"abc123","user_token":"tok_123"}`)
	req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/clerm")
	rec := httptest.NewRecorder()

	service.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "capability token is required") {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func TestResolveBinaryWithCapabilityAndReplayProtection(t *testing.T) {
	doc := mustDocument(t)
	service, payload := mustVerifiedRequest(t, doc, "registry.invoke")

	command, err := service.ResolveBinaryWithTarget(payload, "registry.invoke")
	if err != nil {
		t.Fatalf("ResolveBinaryWithTarget() error = %v", err)
	}
	if command.Relation != "@verified" {
		t.Fatalf("unexpected relation: %s", command.Relation)
	}
	if command.Capability == nil {
		t.Fatal("expected capability metadata")
	}
	if _, err := service.ResolveBinaryWithTarget(payload, "registry.invoke"); err == nil || !strings.Contains(err.Error(), "already been used") {
		t.Fatalf("expected replay rejection, got %v", err)
	}
}

func TestExecuteInvocationWithoutHandlerFails(t *testing.T) {
	doc := mustDocument(t)
	service := resolver.New(doc)
	payload := mustEncodedRequest(t, doc, "@global.healthcare.search_providers.v1", `{"specialty":"cardiology","latitude":40.7,"longitude":-73.9}`)

	_, command, err := service.ExecuteBinary(context.Background(), payload, "internal.search")
	if err == nil {
		t.Fatal("expected missing handler error")
	}
	if command == nil || command.Method != "@global.healthcare.search_providers.v1" {
		t.Fatalf("unexpected command: %#v", command)
	}
}

func TestDaemonDecodeAndEncodeEndpoints(t *testing.T) {
	doc := mustDocument(t)
	service := resolver.New(doc)
	handler := resolver.NewDaemonHandler(slog.New(slog.NewTextHandler(io.Discard, nil)), service)
	payload := mustEncodedRequest(t, doc, "@global.healthcare.search_providers.v1", `{"specialty":"cardiology","latitude":40.7,"longitude":-73.9}`)

	decodeReq := httptest.NewRequest(http.MethodPost, "/v1/requests/decode", bytes.NewReader(payload))
	decodeReq.Header.Set("Content-Type", "application/clerm")
	decodeReq.Header.Set("Clerm-Target", "internal.search")
	decodeRec := httptest.NewRecorder()
	handler.ServeHTTP(decodeRec, decodeReq)
	if decodeRec.Code != http.StatusOK {
		t.Fatalf("unexpected decode status: %d body=%s", decodeRec.Code, decodeRec.Body.String())
	}
	var command resolver.Command
	if err := json.Unmarshal(decodeRec.Body.Bytes(), &command); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if command.Target != "internal.search" {
		t.Fatalf("unexpected command target: %s", command.Target)
	}

	encodeReq := httptest.NewRequest(http.MethodPost, "/v1/responses/encode", strings.NewReader(`{"method":"@global.healthcare.search_providers.v1","outputs":{"request_id":"123e4567-e89b-12d3-a456-426614174000","providers":[]}}`))
	encodeReq.Header.Set("Content-Type", "application/json")
	encodeRec := httptest.NewRecorder()
	handler.ServeHTTP(encodeRec, encodeReq)
	if encodeRec.Code != http.StatusOK {
		t.Fatalf("unexpected encode status: %d body=%s", encodeRec.Code, encodeRec.Body.String())
	}
	response, err := clermresp.Decode(encodeRec.Body.Bytes())
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if response.Method != "@global.healthcare.search_providers.v1" {
		t.Fatalf("unexpected response method: %s", response.Method)
	}
}

func TestDaemonEncodeEndpointRespectsServiceBodyLimit(t *testing.T) {
	doc := mustDocument(t)
	service := resolver.New(doc)
	service.SetMaxBodyBytes(32)
	handler := resolver.NewDaemonHandler(slog.New(slog.NewTextHandler(io.Discard, nil)), service)

	encodeReq := httptest.NewRequest(http.MethodPost, "/v1/responses/encode", strings.NewReader(`{"method":"@global.healthcare.search_providers.v1","outputs":{"request_id":"123e4567-e89b-12d3-a456-426614174000","providers":[]}}`))
	encodeReq.Header.Set("Content-Type", "application/json")
	encodeRec := httptest.NewRecorder()
	handler.ServeHTTP(encodeRec, encodeReq)

	if encodeRec.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d body=%s", encodeRec.Code, encodeRec.Body.String())
	}
	if !strings.Contains(encodeRec.Body.String(), "configured body limit") {
		t.Fatalf("unexpected body: %s", encodeRec.Body.String())
	}
}

func TestDaemonRequestAccessLogsDefaultToDebug(t *testing.T) {
	doc := mustDocument(t)
	service := resolver.New(doc)
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	handler := resolver.NewDaemonHandler(logger, service)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	if strings.Contains(logs.String(), "clerm resolver daemon request") {
		t.Fatalf("unexpected request access log at info level: %s", logs.String())
	}
}
