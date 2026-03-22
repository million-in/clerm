package clermcli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/million-in/clerm/registryrpc"
)

type mockRegistryClient struct {
	register              func(context.Context, registryrpc.RegisterInput) (*registryrpc.RegisterOutput, error)
	search                func(context.Context, registryrpc.SearchInput) (*registryrpc.SearchOutput, error)
	discover              func(context.Context, registryrpc.SearchInput) (*registryrpc.SearchOutput, error)
	establishRelationship func(context.Context, registryrpc.RelationshipInput) (*registryrpc.RelationshipOutput, error)
	relationshipStatus    func(context.Context, registryrpc.RelationshipStatusInput) (*registryrpc.RelationshipStatusOutput, error)
	issueToken            func(context.Context, registryrpc.IssueTokenInput) (*registryrpc.IssueTokenOutput, error)
	refreshToken          func(context.Context, registryrpc.RefreshTokenInput) (*registryrpc.IssueTokenOutput, error)
	invoke                func(context.Context, registryrpc.InvokeInput) (*registryrpc.InvokeOutput, error)
}

func (m mockRegistryClient) Register(ctx context.Context, input registryrpc.RegisterInput) (*registryrpc.RegisterOutput, error) {
	return m.register(ctx, input)
}

func (m mockRegistryClient) Search(ctx context.Context, input registryrpc.SearchInput) (*registryrpc.SearchOutput, error) {
	return m.search(ctx, input)
}

func (m mockRegistryClient) Discover(ctx context.Context, input registryrpc.SearchInput) (*registryrpc.SearchOutput, error) {
	return m.discover(ctx, input)
}

func (m mockRegistryClient) EstablishRelationship(ctx context.Context, input registryrpc.RelationshipInput) (*registryrpc.RelationshipOutput, error) {
	return m.establishRelationship(ctx, input)
}

func (m mockRegistryClient) RelationshipStatus(ctx context.Context, input registryrpc.RelationshipStatusInput) (*registryrpc.RelationshipStatusOutput, error) {
	return m.relationshipStatus(ctx, input)
}

func (m mockRegistryClient) IssueToken(ctx context.Context, input registryrpc.IssueTokenInput) (*registryrpc.IssueTokenOutput, error) {
	return m.issueToken(ctx, input)
}

func (m mockRegistryClient) RefreshToken(ctx context.Context, input registryrpc.RefreshTokenInput) (*registryrpc.IssueTokenOutput, error) {
	return m.refreshToken(ctx, input)
}

func (m mockRegistryClient) Invoke(ctx context.Context, input registryrpc.InvokeInput) (*registryrpc.InvokeOutput, error) {
	return m.invoke(ctx, input)
}

func TestRunRegisterUsesRegistryClient(t *testing.T) {
	tmpDir := t.TempDir()
	schemaPath := filepath.Join(tmpDir, "schema.clermfile")
	if err := os.WriteFile(schemaPath, []byte(`
schema @general.avail.mandene
  @route: https://provider.internal/clerm
  service: @global.books.search_books.v1

method @global.books.search_books.v1
  @exec: async.pool
  @args_input: 1
    decl_args: query.STRING
  @args_output: 1
    decl_args: results.ARRAY
    decl_format: json

relations @general.mandene
  @global: any.protected
`), 0o644); err != nil {
		t.Fatalf("WriteFile(schema) error = %v", err)
	}

	previousFactory := registryClientFactory
	defer func() { registryClientFactory = previousFactory }()

	registryClientFactory = func(baseURL string) (registryClient, error) {
		if baseURL != "http://registry.local" {
			t.Fatalf("unexpected registry URL: %s", baseURL)
		}
		return mockRegistryClient{
			register: func(ctx context.Context, input registryrpc.RegisterInput) (*registryrpc.RegisterOutput, error) {
				deadline, ok := ctx.Deadline()
				if !ok {
					t.Fatal("expected timeout context")
				}
				until := time.Until(deadline)
				if until <= 0 || until > 31*time.Second {
					t.Fatalf("unexpected timeout window: %s", until)
				}
				if input.OwnerID != "seller-1" {
					t.Fatalf("unexpected owner id: %s", input.OwnerID)
				}
				if input.Status != "active" {
					t.Fatalf("unexpected status: %s", input.Status)
				}
				if len(input.Payload) == 0 {
					t.Fatal("expected compiled payload")
				}
				return &registryrpc.RegisterOutput{
					Schema: registryrpc.SchemaSummary{
						Fingerprint: "schema-fp",
						SchemaName:  "books",
						OwnerID:     "seller-1",
						Status:      "active",
					},
				}, nil
			},
			search:   func(context.Context, registryrpc.SearchInput) (*registryrpc.SearchOutput, error) { return nil, nil },
			discover: func(context.Context, registryrpc.SearchInput) (*registryrpc.SearchOutput, error) { return nil, nil },
			establishRelationship: func(context.Context, registryrpc.RelationshipInput) (*registryrpc.RelationshipOutput, error) {
				return nil, nil
			},
			relationshipStatus: func(context.Context, registryrpc.RelationshipStatusInput) (*registryrpc.RelationshipStatusOutput, error) {
				return nil, nil
			},
			issueToken: func(context.Context, registryrpc.IssueTokenInput) (*registryrpc.IssueTokenOutput, error) {
				return nil, nil
			},
			refreshToken: func(context.Context, registryrpc.RefreshTokenInput) (*registryrpc.IssueTokenOutput, error) {
				return nil, nil
			},
			invoke: func(context.Context, registryrpc.InvokeInput) (*registryrpc.InvokeOutput, error) { return nil, nil },
		}, nil
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	if err := RunWithIO(nil, Streams{Stdout: stdout, Stderr: stderr}, []string{"register", "-registry", "http://registry.local", "-in", schemaPath, "-owner", "seller-1"}); err != nil {
		t.Fatalf("RunWithIO(register) error = %v stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "\"fingerprint\": \"schema-fp\"") {
		t.Fatalf("unexpected register output: %s", stdout.String())
	}
}

func TestRunTokenIssueWritesOutputs(t *testing.T) {
	tmpDir := t.TempDir()
	schemaPath := filepath.Join(tmpDir, "schema.clermfile")
	capPath := filepath.Join(tmpDir, "token.cap")
	refreshPath := filepath.Join(tmpDir, "token.refresh")
	if err := os.WriteFile(schemaPath, []byte(`
schema @general.avail.mandene
  @route: https://provider.internal/clerm
  service: @verified.books.purchase_book.v1

method @verified.books.purchase_book.v1
  @exec: sync
  @args_input: 2
    decl_args: book_id.STRING, user_token.STRING
  @args_output: 2
    decl_args: order_id.STRING, status.STRING
    decl_format: json

relations @general.mandene
  @verified: auth.required
`), 0o644); err != nil {
		t.Fatalf("WriteFile(schema) error = %v", err)
	}

	previousFactory := registryClientFactory
	defer func() { registryClientFactory = previousFactory }()

	registryClientFactory = func(baseURL string) (registryClient, error) {
		return mockRegistryClient{
			register: func(context.Context, registryrpc.RegisterInput) (*registryrpc.RegisterOutput, error) { return nil, nil },
			search:   func(context.Context, registryrpc.SearchInput) (*registryrpc.SearchOutput, error) { return nil, nil },
			discover: func(context.Context, registryrpc.SearchInput) (*registryrpc.SearchOutput, error) { return nil, nil },
			establishRelationship: func(context.Context, registryrpc.RelationshipInput) (*registryrpc.RelationshipOutput, error) {
				return nil, nil
			},
			relationshipStatus: func(context.Context, registryrpc.RelationshipStatusInput) (*registryrpc.RelationshipStatusOutput, error) {
				return nil, nil
			},
			issueToken: func(_ context.Context, input registryrpc.IssueTokenInput) (*registryrpc.IssueTokenOutput, error) {
				if input.ConsumerID != "buyer-1" {
					t.Fatalf("unexpected consumer id: %s", input.ConsumerID)
				}
				if input.Method != "@verified.books.purchase_book.v1" {
					t.Fatalf("unexpected method: %s", input.Method)
				}
				if input.ProviderFingerprint == "" {
					t.Fatal("expected derived provider fingerprint")
				}
				return &registryrpc.IssueTokenOutput{
					CapabilityToken: "cap-token",
					ExpiresAt:       "2026-03-22T12:00:00Z",
					RefreshToken:    "refresh-token",
					RefreshExpires:  "2026-03-29T12:00:00Z",
					Relation:        "@verified",
					Condition:       "auth.required",
				}, nil
			},
			refreshToken: func(context.Context, registryrpc.RefreshTokenInput) (*registryrpc.IssueTokenOutput, error) {
				return nil, nil
			},
			invoke: func(context.Context, registryrpc.InvokeInput) (*registryrpc.InvokeOutput, error) { return nil, nil },
		}, nil
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	if err := RunWithIO(nil, Streams{Stdout: stdout, Stderr: stderr}, []string{
		"token", "issue",
		"-registry", "http://registry.local",
		"-consumer", "buyer-1",
		"-schema", schemaPath,
		"-method", "@verified.books.purchase_book.v1",
		"-out-cap", capPath,
		"-out-refresh", refreshPath,
	}); err != nil {
		t.Fatalf("RunWithIO(token issue) error = %v stderr=%s", err, stderr.String())
	}
	if data, err := os.ReadFile(capPath); err != nil || string(data) != "cap-token\n" {
		t.Fatalf("unexpected capability file: %q err=%v", string(data), err)
	}
	if data, err := os.ReadFile(refreshPath); err != nil || string(data) != "refresh-token\n" {
		t.Fatalf("unexpected refresh file: %q err=%v", string(data), err)
	}
	if !strings.Contains(stdout.String(), "\"capability_out_file\":") {
		t.Fatalf("unexpected token issue output: %s", stdout.String())
	}
}

func TestRunInvokeWritesRawResponse(t *testing.T) {
	tmpDir := t.TempDir()
	requestPath := filepath.Join(tmpDir, "request.clerm")
	bodyPath := filepath.Join(tmpDir, "invoke.body")
	if err := os.WriteFile(requestPath, []byte("request-payload"), 0o644); err != nil {
		t.Fatalf("WriteFile(request) error = %v", err)
	}

	previousFactory := registryClientFactory
	defer func() { registryClientFactory = previousFactory }()

	registryClientFactory = func(baseURL string) (registryClient, error) {
		return mockRegistryClient{
			register: func(context.Context, registryrpc.RegisterInput) (*registryrpc.RegisterOutput, error) { return nil, nil },
			search:   func(context.Context, registryrpc.SearchInput) (*registryrpc.SearchOutput, error) { return nil, nil },
			discover: func(context.Context, registryrpc.SearchInput) (*registryrpc.SearchOutput, error) { return nil, nil },
			establishRelationship: func(context.Context, registryrpc.RelationshipInput) (*registryrpc.RelationshipOutput, error) {
				return nil, nil
			},
			relationshipStatus: func(context.Context, registryrpc.RelationshipStatusInput) (*registryrpc.RelationshipStatusOutput, error) {
				return nil, nil
			},
			issueToken: func(context.Context, registryrpc.IssueTokenInput) (*registryrpc.IssueTokenOutput, error) {
				return nil, nil
			},
			refreshToken: func(context.Context, registryrpc.RefreshTokenInput) (*registryrpc.IssueTokenOutput, error) {
				return nil, nil
			},
			invoke: func(_ context.Context, input registryrpc.InvokeInput) (*registryrpc.InvokeOutput, error) {
				if input.ProviderFingerprint != "schema-fp" {
					t.Fatalf("unexpected fingerprint: %s", input.ProviderFingerprint)
				}
				if string(input.Payload) != "request-payload" {
					t.Fatalf("unexpected request payload: %s", string(input.Payload))
				}
				return &registryrpc.InvokeOutput{
					StatusCode:    202,
					Headers:       map[string][]string{"Content-Type": {"application/json"}},
					Target:        "registry.invoke",
					CommandMethod: "@global.books.search_books.v1",
					Body:          []byte(`{"ok":true}`),
				}, nil
			},
		}, nil
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	if err := RunWithIO(nil, Streams{Stdout: stdout, Stderr: stderr}, []string{
		"invoke",
		"-registry", "http://registry.local",
		"-fingerprint", "schema-fp",
		"-request", requestPath,
		"-out", bodyPath,
	}); err != nil {
		t.Fatalf("RunWithIO(invoke) error = %v stderr=%s", err, stderr.String())
	}
	if data, err := os.ReadFile(bodyPath); err != nil || string(data) != `{"ok":true}` {
		t.Fatalf("unexpected invoke body file: %q err=%v", string(data), err)
	}
	info, err := os.Stat(bodyPath)
	if err != nil {
		t.Fatalf("Stat(body) error = %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("unexpected body file mode: %o", info.Mode().Perm())
	}
	var output map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("json.Unmarshal() error = %v output=%s", err, stdout.String())
	}
	if output["status_code"] != float64(202) {
		t.Fatalf("unexpected invoke output: %#v", output)
	}
}

func TestBuildInvokeViewRejectsLargeInlineBody(t *testing.T) {
	_, err := buildInvokeView(&registryrpc.InvokeOutput{
		StatusCode: http.StatusOK,
		Body:       []byte("12345"),
	}, "", 4)
	if err == nil || !strings.Contains(err.Error(), "exceeds inline limit") {
		t.Fatalf("expected inline limit error, got %v", err)
	}
}
