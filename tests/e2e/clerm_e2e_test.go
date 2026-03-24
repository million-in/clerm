package e2e_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/million-in/clerm/internal/app/clermcli"
	"github.com/million-in/clerm/internal/capability"
	"github.com/million-in/clerm/internal/clermcfg"
	"github.com/million-in/clerm/internal/clermresp"
	"github.com/million-in/clerm/internal/platform"
	"github.com/million-in/clerm/internal/resolver"
)

func TestCLICompileRequestResolveAndEmbeddedResolver(t *testing.T) {
	tmpDir := t.TempDir()
	schemaPath := filepath.Join(tmpDir, "provider_search.clermfile")
	cfgPath := filepath.Join(tmpDir, "provider_search.clermcfg")
	reqPath := filepath.Join(tmpDir, "search_providers.clerm")
	verifiedReqPath := filepath.Join(tmpDir, "book_visit.clerm")
	responsePath := filepath.Join(tmpDir, "search_providers_response.clerm")
	payloadPath := filepath.Join(tmpDir, "payload")
	verifiedPayloadPath := filepath.Join(tmpDir, "book_visit.payload")
	tokenPath := filepath.Join(tmpDir, "visit.token")
	publicKeyPath := filepath.Join(tmpDir, "registry.ed25519.pub")

	if err := os.WriteFile(schemaPath, []byte(`
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
`), 0o644); err != nil {
		t.Fatalf("WriteFile(schema) error = %v", err)
	}
	if err := os.WriteFile(payloadPath, []byte(`{"specialty":"cardiology","latitude":40.7,"longitude":-73.9}`), 0o644); err != nil {
		t.Fatalf("WriteFile(payload) error = %v", err)
	}
	if err := os.WriteFile(verifiedPayloadPath, []byte(`{"provider_id":"abc123","user_token":"tok_123"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(verified payload) error = %v", err)
	}

	logger, err := platform.NewLogger("error")
	if err != nil {
		t.Fatalf("NewLogger() error = %v", err)
	}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	if err := clermcli.RunWithIO(logger, clermcli.Streams{Stdout: stdout, Stderr: stderr}, []string{"compile", "-in", schemaPath, "-out", cfgPath}); err != nil {
		t.Fatalf("compile error = %v stderr=%s", err, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	if err := clermcli.RunWithIO(logger, clermcli.Streams{Stdout: stdout, Stderr: stderr}, []string{"request", "-schema", cfgPath, "-method", "@global.healthcare.search_providers.v1", "-allow", "@global", "-data-file", payloadPath, "-out", reqPath}); err != nil {
		t.Fatalf("request error = %v stderr=%s", err, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	cfgBytes, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("ReadFile(config) error = %v", err)
	}
	doc, err := clermcfg.Decode(cfgBytes)
	if err != nil {
		t.Fatalf("Decode(config) error = %v", err)
	}
	publicKey, privateKey, err := capability.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair() error = %v", err)
	}
	if err := capability.WritePublicKeyFile(publicKeyPath, publicKey); err != nil {
		t.Fatalf("WritePublicKeyFile() error = %v", err)
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
		Methods:    []string{"@verified.healthcare.book_visit.v1"},
		Targets:    []string{"registry.invoke"},
		IssuedAt:   now,
		NotBefore:  now,
		ExpiresAt:  now.Add(30 * time.Minute),
	}, privateKey)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	encodedToken, err := capability.EncodeText(token)
	if err != nil {
		t.Fatalf("EncodeText() error = %v", err)
	}
	if err := os.WriteFile(tokenPath, []byte(encodedToken+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(token) error = %v", err)
	}

	if err := clermcli.RunWithIO(logger, clermcli.Streams{Stdout: stdout, Stderr: stderr}, []string{"request", "-schema", cfgPath, "-method", "@verified.healthcare.book_visit.v1", "-allow", "@verified", "-data-file", verifiedPayloadPath, "-cap-file", tokenPath, "-out", verifiedReqPath}); err != nil {
		t.Fatalf("verified request error = %v stderr=%s", err, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	if err := clermcli.RunWithIO(logger, clermcli.Streams{Stdout: stdout, Stderr: stderr}, []string{"inspect", "-in", cfgPath}); err != nil {
		t.Fatalf("inspect cfg error = %v stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "\"kind\": \"clermcfg\"") {
		t.Fatalf("inspect output missing clermcfg kind: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()

	if err := clermcli.RunWithIO(logger, clermcli.Streams{Stdout: stdout, Stderr: stderr}, []string{"resolve", "-schema", cfgPath, "-request", reqPath, "-target", "internal.search"}); err != nil {
		t.Fatalf("resolve error = %v stderr=%s", err, stderr.String())
	}
	var command map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &command); err != nil {
		t.Fatalf("json.Unmarshal(resolve output) error = %v output=%s", err, stdout.String())
	}
	if command["method"] != "@global.healthcare.search_providers.v1" {
		t.Fatalf("unexpected resolve output: %#v", command)
	}
	stdout.Reset()
	stderr.Reset()

	service, err := resolver.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if err := service.Bind("@global.healthcare.search_providers.v1", func(context.Context, *resolver.Invocation) (*resolver.Result, error) {
		return resolver.Success(map[string]any{
			"request_id": "123e4567-e89b-12d3-a456-426614174000",
			"providers":  []map[string]any{{"id": "provider-1"}},
		}), nil
	}); err != nil {
		t.Fatalf("Bind(global) error = %v", err)
	}
	if err := service.Bind("@verified.healthcare.book_visit.v1", func(context.Context, *resolver.Invocation) (*resolver.Result, error) {
		return resolver.Success(map[string]any{
			"order_id": "order-123",
			"status":   "confirmed",
		}), nil
	}); err != nil {
		t.Fatalf("Bind(verified) error = %v", err)
	}
	publicKey, err = capability.ReadPublicKeyFile(publicKeyPath)
	if err != nil {
		t.Fatalf("ReadPublicKeyFile() error = %v", err)
	}
	service.SetCapabilityKeyring(capability.NewKeyring(map[string]ed25519.PublicKey{"registry": publicKey}))

	requestBytes, err := os.ReadFile(reqPath)
	if err != nil {
		t.Fatalf("ReadFile(request) error = %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(requestBytes))
	req.Header.Set("Content-Type", "application/clerm")
	req.Header.Set("Clerm-Target", "internal.search")
	rec := httptest.NewRecorder()
	service.Middleware(http.NotFoundHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected embedded resolver status: %d body=%s", rec.Code, rec.Body.String())
	}
	response, err := clermresp.Decode(rec.Body.Bytes())
	if err != nil {
		t.Fatalf("Decode(response) error = %v", err)
	}
	values, err := response.AsMap()
	if err != nil {
		t.Fatalf("AsMap() error = %v", err)
	}
	if values["request_id"] != "123e4567-e89b-12d3-a456-426614174000" {
		t.Fatalf("unexpected embedded resolver outputs: %#v", values)
	}
	if err := os.WriteFile(responsePath, rec.Body.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile(response) error = %v", err)
	}

	if err := clermcli.RunWithIO(logger, clermcli.Streams{Stdout: stdout, Stderr: stderr}, []string{"inspect", "-in", responsePath}); err != nil {
		t.Fatalf("inspect response error = %v stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "\"kind\": \"clerm_response\"") {
		t.Fatalf("inspect response output missing kind: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()

	verifiedBytes, err := os.ReadFile(verifiedReqPath)
	if err != nil {
		t.Fatalf("ReadFile(verified request) error = %v", err)
	}
	verifiedReq := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewReader(verifiedBytes))
	verifiedReq.Header.Set("Content-Type", "application/clerm")
	verifiedReq.Header.Set("Clerm-Target", "registry.invoke")
	verifiedRec := httptest.NewRecorder()
	service.ServeHTTP(verifiedRec, verifiedReq)
	if verifiedRec.Code != http.StatusOK {
		t.Fatalf("unexpected verified status: %d body=%s", verifiedRec.Code, verifiedRec.Body.String())
	}
	verifiedResponse, err := clermresp.Decode(verifiedRec.Body.Bytes())
	if err != nil {
		t.Fatalf("Decode(verified response) error = %v", err)
	}
	verifiedValues, err := verifiedResponse.AsMap()
	if err != nil {
		t.Fatalf("AsMap(verified response) error = %v", err)
	}
	if verifiedValues["status"] != "confirmed" {
		t.Fatalf("unexpected verified outputs: %#v", verifiedValues)
	}

	daemon := resolver.NewDaemonHandler(logger, service)
	decodeReq := httptest.NewRequest(http.MethodPost, "/v1/requests/decode", bytes.NewReader(requestBytes))
	decodeReq.Header.Set("Content-Type", "application/clerm")
	decodeReq.Header.Set("Clerm-Target", "internal.search")
	decodeRec := httptest.NewRecorder()
	daemon.ServeHTTP(decodeRec, decodeReq)
	if decodeRec.Code != http.StatusOK {
		t.Fatalf("unexpected daemon decode status: %d body=%s", decodeRec.Code, decodeRec.Body.String())
	}
	var daemonCommand map[string]any
	if err := json.Unmarshal(decodeRec.Body.Bytes(), &daemonCommand); err != nil {
		t.Fatalf("json.Unmarshal(daemon decode) error = %v", err)
	}
	if daemonCommand["target"] != "internal.search" {
		t.Fatalf("unexpected daemon decode output: %#v", daemonCommand)
	}
}
