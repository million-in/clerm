package resolver_test

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/million-in/clerm/internal/capability"
	"github.com/million-in/clerm/internal/clermreq"
	"github.com/million-in/clerm/internal/resolver"
	"github.com/million-in/clerm/internal/schema"
)

func buildTestService(t *testing.T) (*resolver.Service, []byte) {
	t.Helper()

	doc, err := schema.Parse(strings.NewReader(`
schema @general.avail.mandene
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
	method, _ := doc.MethodByReference("@global.healthcare.search_providers.v1")
	request, err := clermreq.Build(method, []byte(`{"specialty":"cardiology","latitude":40.7,"longitude":-73.9}`))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	encoded, err := clermreq.Encode(request)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	return resolver.New(doc), encoded
}

func buildVerifiedService(t *testing.T) (*resolver.Service, []byte) {
	t.Helper()

	doc, err := schema.Parse(strings.NewReader(`
schema @general.avail.mandene
  @route: https://resolver.health.example/clerm
  service: @verified.healthcare.book_visit.v1

method @verified.healthcare.book_visit.v1
  @exec: sync
  @args_input: 2
    decl_args: provider_id.STRING, user_token.STRING
  @args_output: 2
    decl_args: order_id.STRING, status.STRING
    decl_format: json

relations @general.mandene
  @verified: auth.required
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	method, _ := doc.MethodByReference("@verified.healthcare.book_visit.v1")
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
		KeyID:      "default",
		Issuer:     "registry",
		Subject:    "partner-123",
		Schema:     doc.Name,
		SchemaHash: doc.PublicFingerprint(),
		Relation:   method.Reference.Relation,
		Condition:  "auth.required",
		Methods:    []string{method.Reference.Raw},
		IssuedAt:   now,
		NotBefore:  now,
		ExpiresAt:  now.Add(5 * time.Minute),
	}, privateKey)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	request.CapabilityRaw, err = capability.Encode(token)
	if err != nil {
		t.Fatalf("Encode(token) error = %v", err)
	}
	encoded, err := clermreq.Encode(request)
	if err != nil {
		t.Fatalf("Encode(request) error = %v", err)
	}
	service := resolver.New(doc)
	service.SetCapabilityKeyring(capability.NewKeyring(map[string]ed25519.PublicKey{"default": publicKey}))
	return service, encoded
}

func TestResolveBinary(t *testing.T) {
	service, payload := buildTestService(t)

	command, err := service.ResolveBinary(payload)
	if err != nil {
		t.Fatalf("ResolveBinary() error = %v", err)
	}
	if command.Method != "@global.healthcare.search_providers.v1" {
		t.Fatalf("unexpected method: %s", command.Method)
	}
	if command.Target != "@global.healthcare.search_providers.v1" {
		t.Fatalf("unexpected target: %s", command.Target)
	}
	if command.Relation != "@global" {
		t.Fatalf("unexpected relation: %s", command.Relation)
	}
}

func TestHandlerResolveEndpoint(t *testing.T) {
	service, payload := buildTestService(t)

	req := httptest.NewRequest(http.MethodPost, "/resolve", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/clerm")
	req.Header.Set("Clerm-Target", "registry.discover")
	rec := httptest.NewRecorder()

	service.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}

	var command resolver.Command
	if err := json.Unmarshal(rec.Body.Bytes(), &command); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if command.Arguments["specialty"] != "cardiology" {
		t.Fatalf("unexpected command arguments: %#v", command.Arguments)
	}
	if command.Target != "registry.discover" {
		t.Fatalf("unexpected target: %s", command.Target)
	}
}

func TestResolveBinaryRequiresCapabilityForVerifiedRelation(t *testing.T) {
	doc, err := schema.Parse(strings.NewReader(`
schema @general.avail.mandene
  @route: https://resolver.health.example/clerm
  service: @verified.healthcare.book_visit.v1

method @verified.healthcare.book_visit.v1
  @exec: sync
  @args_input: 2
    decl_args: provider_id.STRING, user_token.STRING
  @args_output: 2
    decl_args: order_id.STRING, status.STRING
    decl_format: json

relations @general.mandene
  @verified: auth.required
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	method, _ := doc.MethodByReference("@verified.healthcare.book_visit.v1")
	request, err := clermreq.Build(method, []byte(`{"provider_id":"abc123","user_token":"tok_123"}`))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	encoded, err := clermreq.Encode(request)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	_, err = resolver.New(doc).ResolveBinary(encoded)
	if err == nil || !strings.Contains(err.Error(), "capability token is required") {
		t.Fatalf("expected capability token requirement error, got %v", err)
	}
}

func TestResolveBinaryWithCapabilityAndReplayProtection(t *testing.T) {
	service, payload := buildVerifiedService(t)

	command, err := service.ResolveBinaryWithTarget(payload, "registry.invoke")
	if err != nil {
		t.Fatalf("ResolveBinaryWithTarget() error = %v", err)
	}
	if command.Relation != "@verified" {
		t.Fatalf("unexpected relation: %s", command.Relation)
	}
	if command.Condition != "auth.required" {
		t.Fatalf("unexpected condition: %s", command.Condition)
	}
	if command.Capability == nil {
		t.Fatal("expected capability metadata in resolved command")
	}
	if _, err := service.ResolveBinaryWithTarget(payload, "registry.invoke"); err == nil || !strings.Contains(err.Error(), "already been used") {
		t.Fatalf("expected replay rejection, got %v", err)
	}
}
