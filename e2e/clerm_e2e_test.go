package e2e_test

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/million-in/clerm/internal/app/clermcli"
	"github.com/million-in/clerm/internal/capability"
	"github.com/million-in/clerm/internal/platform"
	"github.com/million-in/clerm/internal/resolver"
)

func TestCLICompileRequestResolveAndServe(t *testing.T) {
	tmpDir := t.TempDir()
	schemaPath := filepath.Join(tmpDir, "provider_search.clermfile")
	cfgPath := filepath.Join(tmpDir, "provider_search.clermcfg")
	reqPath := filepath.Join(tmpDir, "search_providers.clerm")
	verifiedReqPath := filepath.Join(tmpDir, "book_visit.clerm")
	payloadPath := filepath.Join(tmpDir, "payload")
	verifiedPayloadPath := filepath.Join(tmpDir, "book_visit.payload")
	tokenPath := filepath.Join(tmpDir, "visit.token")
	privateKeyPath := filepath.Join(tmpDir, "dev.ed25519")
	publicKeyPath := filepath.Join(tmpDir, "dev.ed25519.pub")

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

	if err := clermcli.RunWithIO(logger, clermcli.Streams{Stdout: stdout, Stderr: stderr}, []string{"token", "keygen", "-out-private", privateKeyPath, "-out-public", publicKeyPath}); err != nil {
		t.Fatalf("token keygen error = %v stderr=%s", err, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	if err := clermcli.RunWithIO(logger, clermcli.Streams{Stdout: stdout, Stderr: stderr}, []string{"request", "-schema", cfgPath, "-method", "@global.healthcare.search_providers.v1", "-allow", "@global", "-data-file", payloadPath, "-out", reqPath}); err != nil {
		t.Fatalf("request error = %v stderr=%s", err, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	if err := clermcli.RunWithIO(logger, clermcli.Streams{Stdout: stdout, Stderr: stderr}, []string{"request", "-schema", cfgPath, "-method", "@verified.healthcare.book_visit.v1", "-allow", "@verified", "-data-file", verifiedPayloadPath, "-out", verifiedReqPath}); err == nil {
		t.Fatal("expected verified request without token to fail")
	}
	stdout.Reset()
	stderr.Reset()

	if err := clermcli.RunWithIO(logger, clermcli.Streams{Stdout: stdout, Stderr: stderr}, []string{"token", "issue", "-schema", cfgPath, "-method", "@verified.healthcare.book_visit.v1", "-issuer", "registry", "-subject", "partner-123", "-private-key", privateKeyPath, "-out", tokenPath}); err != nil {
		t.Fatalf("token issue error = %v stderr=%s", err, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	if err := clermcli.RunWithIO(logger, clermcli.Streams{Stdout: stdout, Stderr: stderr}, []string{"request", "-schema", cfgPath, "-method", "@verified.healthcare.book_visit.v1", "-allow", "@verified", "-data-file", verifiedPayloadPath, "-cap-file", tokenPath, "-out", verifiedReqPath}); err != nil {
		t.Fatalf("verified request error = %v stderr=%s", err, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	if err := clermcli.RunWithIO(logger, clermcli.Streams{Stdout: stdout, Stderr: stderr}, []string{"inspect", "-in", cfgPath}); err != nil {
		t.Fatalf("inspect error = %v stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "\"kind\": \"clermcfg\"") {
		t.Fatalf("inspect output missing clermcfg kind: %s", stdout.String())
	}
	if strings.Contains(stdout.String(), "\"route\"") {
		t.Fatalf("public inspect leaked route: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()

	if err := clermcli.RunWithIO(logger, clermcli.Streams{Stdout: stdout, Stderr: stderr}, []string{"inspect", "-in", cfgPath, "-internal"}); err != nil {
		t.Fatalf("internal inspect error = %v stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "\"route\": \"https://resolver.health.example/clerm\"") {
		t.Fatalf("internal inspect missing route: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()

	if err := clermcli.RunWithIO(logger, clermcli.Streams{Stdout: stdout, Stderr: stderr}, []string{"resolve", "-schema", cfgPath, "-request", reqPath, "-target", "registry.discover"}); err != nil {
		t.Fatalf("resolve error = %v stderr=%s", err, stderr.String())
	}

	var offline map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &offline); err != nil {
		t.Fatalf("json.Unmarshal(resolve output) error = %v output=%s", err, stdout.String())
	}
	if offline["method"] != "@global.healthcare.search_providers.v1" {
		t.Fatalf("unexpected resolve output: %#v", offline)
	}
	if offline["target"] != "registry.discover" {
		t.Fatalf("unexpected target in resolve output: %#v", offline)
	}

	service, err := resolver.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	payload, err := os.ReadFile(reqPath)
	if err != nil {
		t.Fatalf("ReadFile(request) error = %v", err)
	}
	req := httptest.NewRequest("POST", "/resolve", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/clerm")
	req.Header.Set("Clerm-Target", "registry.discover")
	rec := httptest.NewRecorder()
	service.Handler().ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("unexpected serve status: %d body=%s", rec.Code, rec.Body.String())
	}

	var online map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&online); err != nil {
		t.Fatalf("Decode(response) error = %v", err)
	}
	if online["relation"] != "@global" {
		t.Fatalf("unexpected serve output: %#v", online)
	}
	if online["target"] != "registry.discover" {
		t.Fatalf("unexpected serve target: %#v", online)
	}

	stdout.Reset()
	stderr.Reset()
	if err := clermcli.RunWithIO(logger, clermcli.Streams{Stdout: stdout, Stderr: stderr}, []string{"inspect", "-in", verifiedReqPath}); err != nil {
		t.Fatalf("inspect verified request error = %v stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "\"capability\"") {
		t.Fatalf("verified request inspect missing capability: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()

	if err := clermcli.RunWithIO(logger, clermcli.Streams{Stdout: stdout, Stderr: stderr}, []string{"resolve", "-schema", cfgPath, "-request", verifiedReqPath, "-target", "registry.invoke", "-cap-public-key", publicKeyPath}); err != nil {
		t.Fatalf("verified resolve error = %v stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "\"condition\": \"auth.required\"") {
		t.Fatalf("verified resolve output missing auth condition: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()

	verifiedService, err := resolver.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig(verified) error = %v", err)
	}
	publicKey, err := capability.ReadPublicKeyFile(publicKeyPath)
	if err != nil {
		t.Fatalf("ReadPublicKeyFile() error = %v", err)
	}
	verifiedService.SetCapabilityKeyring(capability.NewKeyring(map[string]ed25519.PublicKey{"default": publicKey}))
	verifiedPayload, err := os.ReadFile(verifiedReqPath)
	if err != nil {
		t.Fatalf("ReadFile(verified request) error = %v", err)
	}
	verifiedReq := httptest.NewRequest("POST", "/resolve", bytes.NewReader(verifiedPayload))
	verifiedReq.Header.Set("Content-Type", "application/clerm")
	verifiedReq.Header.Set("Clerm-Target", "registry.invoke")
	verifiedRec := httptest.NewRecorder()
	verifiedService.Handler().ServeHTTP(verifiedRec, verifiedReq)
	if verifiedRec.Code != 200 {
		t.Fatalf("unexpected verified serve status: %d body=%s", verifiedRec.Code, verifiedRec.Body.String())
	}
}
