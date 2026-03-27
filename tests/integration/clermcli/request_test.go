package clermcli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/million-in/clerm"
	"github.com/million-in/clerm/internal/app/clermcli"
)

func TestRunRequestWritesCapabilityBearingPayloadWithOwnerOnlyPermissions(t *testing.T) {
	schemaPath := writeSchemaFile(t, `
schema @general.avail.mandene
  @route: https://provider.internal/clerm
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
`)
	doc, err := clerm.Compiler.LoadDocument(schemaPath)
	if err != nil {
		t.Fatalf("LoadDocument() error = %v", err)
	}
	tmpDir := t.TempDir()
	requestPath := filepath.Join(tmpDir, "request.clerm")
	tokenPath := filepath.Join(tmpDir, "token.cap")
	tokenText := validCapabilityTokenText(t, doc.Name, doc.PublicFingerprint(), "@verified", "@verified.healthcare.book_visit.v1")
	if err := os.WriteFile(tokenPath, []byte(tokenText+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(token) error = %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	err = clermcli.RunWithIO(nil, clermcli.Streams{Stdout: stdout, Stderr: stderr}, []string{
		"request",
		"-schema", schemaPath,
		"-method", "@verified.healthcare.book_visit.v1",
		"-allow", "@verified",
		"-cap-file", tokenPath,
		"-data", `{"provider_id":"provider-1","user_token":"tok-123"}`,
		"-out", requestPath,
	})
	if err != nil {
		t.Fatalf("RunWithIO(request) error = %v stderr=%s", err, stderr.String())
	}
	info, err := os.Stat(requestPath)
	if err != nil {
		t.Fatalf("Stat(request) error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("unexpected request file mode: %o", got)
	}
}
