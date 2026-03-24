package resolvercli_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/million-in/clerm/clermcfg"
	"github.com/million-in/clerm/internal/app/resolvercli"
	"github.com/million-in/clerm/platform"
	"github.com/million-in/clerm/schema"
)

func TestRunWithIOMissingSchema(t *testing.T) {
	logger, err := platform.NewLogger("error")
	if err != nil {
		t.Fatalf("NewLogger() error = %v", err)
	}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	err = resolvercli.RunWithIO(logger, resolvercli.Streams{Stdout: stdout, Stderr: stderr}, nil)
	if err == nil || !strings.Contains(err.Error(), "requires -schema or -schema-url") {
		t.Fatalf("expected missing schema error, got %v", err)
	}
}

func TestRunWithIORejectsPrivateSchemaURL(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	err := resolvercli.RunWithIO(nil, resolvercli.Streams{Stdout: stdout, Stderr: stderr}, []string{
		"-schema-url", "http://127.0.0.1/schema.clermcfg",
	})
	if err == nil || !strings.Contains(err.Error(), "host is not allowed") {
		t.Fatalf("expected private schema URL rejection, got %v", err)
	}
}

func TestRunWithIOLoadsSchemaFileBeforeListenFailure(t *testing.T) {
	doc, err := schema.Parse(strings.NewReader(`
schema @general.avail.mandene
  @route: https://resolver.health.example/clerm
  service: @global.healthcare.search_providers.v1

method @global.healthcare.search_providers.v1
  @exec: sync
  @args_input: 1
    decl_args: specialty.STRING
  @args_output: 1
    decl_args: request_id.UUID
    decl_format: json

relations @general.mandene
  @global: any.protected
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	payload, err := clermcfg.Encode(doc)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	path := t.TempDir() + "/schema.clermcfg"
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	err = resolvercli.RunWithIO(nil, resolvercli.Streams{Stdout: stdout, Stderr: stderr}, []string{
		"-schema", path,
		"-listen", "bad:::addr",
	})
	if err == nil || !strings.Contains(err.Error(), "listen on TCP address") {
		t.Fatalf("expected listen error after schema load, got %v", err)
	}
}
