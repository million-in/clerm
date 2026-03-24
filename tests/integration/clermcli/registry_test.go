package clermcli_test

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

	"github.com/million-in/clerm/internal/app/clermcli"
	"github.com/million-in/clerm/platform"
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

func TestRunRegisterUsesRegistryServer(t *testing.T) {
	schemaPath := writeSchemaFile(t, `
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
`)

	restore := clermcli.SetRegistryClientFactoryForTest(func(baseURL string) (clermcli.RegistryClient, error) {
		if baseURL != "http://registry.local" {
			t.Fatalf("unexpected registry URL: %s", baseURL)
		}
		return mockRegistryClient{
			register: func(ctx context.Context, input registryrpc.RegisterInput) (*registryrpc.RegisterOutput, error) {
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("expected timeout context")
				}
				if input.OwnerID != "seller-1" {
					t.Fatalf("unexpected owner id: %s", input.OwnerID)
				}
				if input.Status != "active" || len(input.Payload) == 0 {
					t.Fatalf("unexpected register input: %#v", input)
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
	})
	defer restore()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	err := clermcli.RunWithIO(nil, clermcli.Streams{Stdout: stdout, Stderr: stderr}, []string{
		"register", "-registry", "http://registry.local", "-timeout", "30s", "-in", schemaPath, "-owner", "seller-1",
	})
	if err != nil {
		t.Fatalf("RunWithIO(register) error = %v stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"fingerprint": "schema-fp"`) {
		t.Fatalf("unexpected output: %s", stdout.String())
	}
}

func TestRunTokenIssueWritesOutputs(t *testing.T) {
	schemaPath := writeSchemaFile(t, `
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
`)
	capPath := filepath.Join(t.TempDir(), "token.cap")
	refreshPath := filepath.Join(t.TempDir(), "token.refresh")

	restore := clermcli.SetRegistryClientFactoryForTest(func(string) (clermcli.RegistryClient, error) {
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
				if input.ConsumerID != "buyer-1" || input.Method != "@verified.books.purchase_book.v1" {
					t.Fatalf("unexpected issue token input: %#v", input)
				}
				if input.ProviderFingerprint == "" {
					t.Fatal("expected provider fingerprint")
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
	})
	defer restore()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	err := clermcli.RunWithIO(nil, clermcli.Streams{Stdout: stdout, Stderr: stderr}, []string{
		"token", "issue",
		"-registry", "http://registry.local",
		"-consumer", "buyer-1",
		"-schema", schemaPath,
		"-method", "@verified.books.purchase_book.v1",
		"-out-cap", capPath,
		"-out-refresh", refreshPath,
	})
	if err != nil {
		t.Fatalf("RunWithIO(token issue) error = %v stderr=%s", err, stderr.String())
	}
	assertFileText(t, capPath, "cap-token\n", 0o600)
	assertFileText(t, refreshPath, "refresh-token\n", 0o600)
	if !strings.Contains(stdout.String(), `"capability_out_file":`) {
		t.Fatalf("unexpected output: %s", stdout.String())
	}
}

func TestRunInvokeWritesRawResponse(t *testing.T) {
	tmpDir := t.TempDir()
	requestPath := filepath.Join(tmpDir, "request.clerm")
	bodyPath := filepath.Join(tmpDir, "invoke.body")
	if err := os.WriteFile(requestPath, []byte("request-payload"), 0o644); err != nil {
		t.Fatalf("WriteFile(request) error = %v", err)
	}

	restore := clermcli.SetRegistryClientFactoryForTest(func(string) (clermcli.RegistryClient, error) {
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
				if input.ProviderFingerprint != "schema-fp" || string(input.Payload) != "request-payload" {
					t.Fatalf("unexpected invoke input: %#v", input)
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
	})
	defer restore()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	err := clermcli.RunWithIO(nil, clermcli.Streams{Stdout: stdout, Stderr: stderr}, []string{
		"invoke", "-registry", "http://registry.local", "-fingerprint", "schema-fp", "-request", requestPath, "-out", bodyPath,
	})
	if err != nil {
		t.Fatalf("RunWithIO(invoke) error = %v stderr=%s", err, stderr.String())
	}
	assertFileText(t, bodyPath, `{"ok":true}`, 0o600)

	var output map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("Unmarshal() error = %v output=%s", err, stdout.String())
	}
	if output["status_code"] != float64(202) {
		t.Fatalf("unexpected output: %#v", output)
	}
}

func TestRunInvokeRejectsLargeInlineBody(t *testing.T) {
	tmpDir := t.TempDir()
	requestPath := filepath.Join(tmpDir, "request.clerm")
	if err := os.WriteFile(requestPath, []byte("request-payload"), 0o644); err != nil {
		t.Fatalf("WriteFile(request) error = %v", err)
	}

	restore := clermcli.SetRegistryClientFactoryForTest(func(string) (clermcli.RegistryClient, error) {
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
			invoke: func(context.Context, registryrpc.InvokeInput) (*registryrpc.InvokeOutput, error) {
				return &registryrpc.InvokeOutput{
					StatusCode: http.StatusOK,
					Body:       []byte("12345"),
					Target:     "registry.invoke",
				}, nil
			},
		}, nil
	})
	defer restore()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	err := clermcli.RunWithIO(nil, clermcli.Streams{Stdout: stdout, Stderr: stderr}, []string{
		"invoke", "-registry", "http://registry.local", "-fingerprint", "schema-fp", "-request", requestPath, "-inline-limit", "4",
	})
	if err == nil || !platform.IsCode(err, platform.CodeValidation) || !strings.Contains(err.Error(), "exceeds inline limit") {
		t.Fatalf("expected inline limit error, got %v", err)
	}
}

func TestRunInvokeUsesRegistryContextFactory(t *testing.T) {
	tmpDir := t.TempDir()
	requestPath := filepath.Join(tmpDir, "request.clerm")
	if err := os.WriteFile(requestPath, []byte("request-payload"), 0o644); err != nil {
		t.Fatalf("WriteFile(request) error = %v", err)
	}

	restoreClient := clermcli.SetRegistryClientFactoryForTest(func(string) (clermcli.RegistryClient, error) {
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
			invoke: func(ctx context.Context, input registryrpc.InvokeInput) (*registryrpc.InvokeOutput, error) {
				if input.ProviderFingerprint != "schema-fp" {
					t.Fatalf("unexpected provider fingerprint: %s", input.ProviderFingerprint)
				}
				if ctx.Err() != context.Canceled {
					t.Fatalf("expected canceled invoke context, got %v", ctx.Err())
				}
				return &registryrpc.InvokeOutput{StatusCode: http.StatusNoContent}, nil
			},
		}, nil
	})
	defer restoreClient()

	restoreContext := clermcli.SetRegistryContextFactoryForTest(func(time.Duration) (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx, func() {}
	})
	defer restoreContext()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	if err := clermcli.RunWithIO(nil, clermcli.Streams{Stdout: stdout, Stderr: stderr}, []string{
		"invoke", "-registry", "http://registry.local", "-fingerprint", "schema-fp", "-request", requestPath,
	}); err != nil {
		t.Fatalf("RunWithIO(invoke) error = %v stderr=%s", err, stderr.String())
	}
}

func writeSchemaFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "schema.clermfile")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(contents)+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(schema) error = %v", err)
	}
	return path
}

func assertFileText(t *testing.T, path, want string, wantMode os.FileMode) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	if string(data) != want {
		t.Fatalf("unexpected file contents for %s: %q", path, string(data))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%s) error = %v", path, err)
	}
	if info.Mode().Perm() != wantMode {
		t.Fatalf("unexpected file mode for %s: %o", path, info.Mode().Perm())
	}
}
