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

	"log/slog"

	"github.com/million-in/clerm/internal/app/clermcli"
	"github.com/million-in/clerm/internal/capability"
	"github.com/million-in/clerm/internal/clermcfg"
	"github.com/million-in/clerm/internal/clermresp"
	"github.com/million-in/clerm/internal/platform"
	"github.com/million-in/clerm/internal/resolver"
)

func TestCLIWorkflow(t *testing.T) {
	t.Run("compile_inspect_and_resolve", func(t *testing.T) {
		fixture := newE2EFixture(t)
		fixture.compileConfig(t)
		fixture.writeGlobalRequest(t)

		inspectOutput := fixture.runCLI(t, "inspect", "-in", fixture.cfgPath)
		if !strings.Contains(inspectOutput, "\"kind\": \"clermcfg\"") {
			t.Fatalf("inspect output missing clermcfg kind: %s", inspectOutput)
		}

		resolveOutput := fixture.runCLI(t, "resolve", "-schema", fixture.cfgPath, "-request", fixture.reqPath, "-target", "internal.search")
		var command map[string]any
		if err := json.Unmarshal([]byte(resolveOutput), &command); err != nil {
			t.Fatalf("json.Unmarshal(resolve output) error = %v output=%s", err, resolveOutput)
		}
		if command["method"] != "@global.healthcare.search_providers.v1" {
			t.Fatalf("unexpected resolve output: %#v", command)
		}
		if command["target"] != "internal.search" {
			t.Fatalf("unexpected resolve target: %#v", command["target"])
		}
	})

	t.Run("embedded_resolver_serves_global_and_verified_requests", func(t *testing.T) {
		fixture := newE2EFixture(t)
		fixture.compileConfig(t)
		fixture.writeGlobalRequest(t)
		fixture.issueCapabilityToken(t)
		fixture.writeVerifiedRequest(t)
		service := fixture.loadBoundService(t)

		requestBytes := fixture.readFile(t, fixture.reqPath)
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
		if err := os.WriteFile(fixture.responsePath, rec.Body.Bytes(), 0o644); err != nil {
			t.Fatalf("WriteFile(response) error = %v", err)
		}

		inspectResponseOutput := fixture.runCLI(t, "inspect", "-in", fixture.responsePath)
		if !strings.Contains(inspectResponseOutput, "\"kind\": \"clerm_response\"") {
			t.Fatalf("inspect response output missing kind: %s", inspectResponseOutput)
		}

		verifiedBytes := fixture.readFile(t, fixture.verifiedReqPath)
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
	})

	t.Run("daemon_decode_endpoint", func(t *testing.T) {
		fixture := newE2EFixture(t)
		fixture.compileConfig(t)
		fixture.writeGlobalRequest(t)
		service := fixture.loadBoundService(t)
		daemon := resolver.NewDaemonHandler(fixture.logger, service)

		requestBytes := fixture.readFile(t, fixture.reqPath)
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
	})
}

type e2eFixture struct {
	logger              *slog.Logger
	stdout              *bytes.Buffer
	stderr              *bytes.Buffer
	schemaPath          string
	cfgPath             string
	reqPath             string
	verifiedReqPath     string
	responsePath        string
	payloadPath         string
	verifiedPayloadPath string
	tokenPath           string
	publicKeyPath       string
}

func newE2EFixture(t *testing.T) *e2eFixture {
	t.Helper()
	tmpDir := t.TempDir()
	fixture := &e2eFixture{
		stdout:              &bytes.Buffer{},
		stderr:              &bytes.Buffer{},
		schemaPath:          filepath.Join(tmpDir, "provider_search.clermfile"),
		cfgPath:             filepath.Join(tmpDir, "provider_search.clermcfg"),
		reqPath:             filepath.Join(tmpDir, "search_providers.clerm"),
		verifiedReqPath:     filepath.Join(tmpDir, "book_visit.clerm"),
		responsePath:        filepath.Join(tmpDir, "search_providers_response.clerm"),
		payloadPath:         filepath.Join(tmpDir, "payload"),
		verifiedPayloadPath: filepath.Join(tmpDir, "book_visit.payload"),
		tokenPath:           filepath.Join(tmpDir, "visit.token"),
		publicKeyPath:       filepath.Join(tmpDir, "registry.ed25519.pub"),
	}
	logger, err := platform.NewLogger("error")
	if err != nil {
		t.Fatalf("NewLogger() error = %v", err)
	}
	fixture.logger = logger
	fixture.writeInputs(t)
	return fixture
}

func (f *e2eFixture) writeInputs(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(f.schemaPath, []byte(strings.TrimSpace(`
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
`)+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(schema) error = %v", err)
	}
	if err := os.WriteFile(f.payloadPath, []byte(`{"specialty":"cardiology","latitude":40.7,"longitude":-73.9}`), 0o644); err != nil {
		t.Fatalf("WriteFile(payload) error = %v", err)
	}
	if err := os.WriteFile(f.verifiedPayloadPath, []byte(`{"provider_id":"abc123","user_token":"tok_123"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(verified payload) error = %v", err)
	}
}

func (f *e2eFixture) runCLI(t *testing.T, args ...string) string {
	t.Helper()
	f.stdout.Reset()
	f.stderr.Reset()
	if err := clermcli.RunWithIO(f.logger, clermcli.Streams{Stdout: f.stdout, Stderr: f.stderr}, args); err != nil {
		t.Fatalf("RunWithIO(%s) error = %v stderr=%s", strings.Join(args, " "), err, f.stderr.String())
	}
	return f.stdout.String()
}

func (f *e2eFixture) compileConfig(t *testing.T) {
	t.Helper()
	f.runCLI(t, "compile", "-in", f.schemaPath, "-out", f.cfgPath)
}

func (f *e2eFixture) writeGlobalRequest(t *testing.T) {
	t.Helper()
	f.runCLI(t, "request", "-schema", f.cfgPath, "-method", "@global.healthcare.search_providers.v1", "-allow", "@global", "-data-file", f.payloadPath, "-out", f.reqPath)
}

func (f *e2eFixture) issueCapabilityToken(t *testing.T) {
	t.Helper()
	cfgBytes := f.readFile(t, f.cfgPath)
	doc, err := clermcfg.Decode(cfgBytes)
	if err != nil {
		t.Fatalf("Decode(config) error = %v", err)
	}
	publicKey, privateKey, err := capability.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair() error = %v", err)
	}
	if err := capability.WritePublicKeyFile(f.publicKeyPath, publicKey); err != nil {
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
	if err := os.WriteFile(f.tokenPath, []byte(encodedToken+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(token) error = %v", err)
	}
}

func (f *e2eFixture) writeVerifiedRequest(t *testing.T) {
	t.Helper()
	f.runCLI(t, "request", "-schema", f.cfgPath, "-method", "@verified.healthcare.book_visit.v1", "-allow", "@verified", "-data-file", f.verifiedPayloadPath, "-cap-file", f.tokenPath, "-out", f.verifiedReqPath)
}

func (f *e2eFixture) loadBoundService(t *testing.T) *resolver.Service {
	t.Helper()
	service, err := resolver.LoadConfig(f.cfgPath)
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
	if _, err := os.Stat(f.publicKeyPath); err == nil {
		publicKey, err := capability.ReadPublicKeyFile(f.publicKeyPath)
		if err != nil {
			t.Fatalf("ReadPublicKeyFile() error = %v", err)
		}
		service.SetCapabilityKeyring(capability.NewKeyring(map[string]ed25519.PublicKey{"registry": publicKey}))
	}
	return service
}

func (f *e2eFixture) readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	return data
}
