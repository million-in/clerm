package resolvercli

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/million-in/clerm/internal/clermcfg"
	"github.com/million-in/clerm/internal/platform"
	"github.com/million-in/clerm/internal/schema"
)

func TestRunWithIOMissingSchema(t *testing.T) {
	logger, err := platform.NewLogger("error")
	if err != nil {
		t.Fatalf("NewLogger() error = %v", err)
	}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	err = RunWithIO(logger, Streams{Stdout: stdout, Stderr: stderr}, nil)
	if err == nil || !strings.Contains(err.Error(), "requires -schema or -schema-url") {
		t.Fatalf("expected missing schema error, got %v", err)
	}
}

func TestLoadServiceFromFile(t *testing.T) {
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
	path := filepath.Join(t.TempDir(), "schema.clermcfg")
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	service, err := loadService(path, "", "", "registry")
	if err != nil {
		t.Fatalf("loadService() error = %v", err)
	}
	if service.Document().Name != doc.Name {
		t.Fatalf("unexpected schema name: %s", service.Document().Name)
	}
}

func TestLoadServiceRejectsPrivateSchemaURL(t *testing.T) {
	_, err := loadService("", "http://127.0.0.1/schema.clermcfg", "", "registry")
	if err == nil || !strings.Contains(err.Error(), "host is not allowed") {
		t.Fatalf("expected private schema URL rejection, got %v", err)
	}
}

func TestDaemonListenerUnixSocket(t *testing.T) {
	socketPath := filepath.Join("/tmp", fmt.Sprintf("clerm-resolver-%d.sock", time.Now().UnixNano()))
	defer os.Remove(socketPath)
	listener, address, err := daemonListener("127.0.0.1:0", socketPath)
	if err != nil {
		if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
			t.Skipf("unix sockets are blocked in this environment: %v", err)
		}
		t.Fatalf("daemonListener() error = %v", err)
	}
	defer listener.Close()
	if listener.Addr().Network() != "unix" {
		t.Fatalf("unexpected network: %s", listener.Addr().Network())
	}
	if address != socketPath {
		t.Fatalf("unexpected address: %s", address)
	}
	info, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("expected Unix socket mode, got %s", info.Mode())
	}
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	_ = conn.Close()
}
