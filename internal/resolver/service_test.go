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
	"strings"
	"testing"
	"time"

	"github.com/million-in/clerm/internal/capability"
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
	request.CapabilityRaw = encodedToken
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
