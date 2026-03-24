package netutil_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/million-in/clerm/internal/netutil"
)

func TestNewDefaultHTTPClientUsesDefaults(t *testing.T) {
	client := netutil.NewDefaultHTTPClient(netutil.HTTPClientOptions{})
	if client.Timeout != 15*time.Second {
		t.Fatalf("unexpected timeout: %s", client.Timeout)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("unexpected transport type: %T", client.Transport)
	}
	if transport.MaxIdleConns != 100 {
		t.Fatalf("unexpected max idle conns: %d", transport.MaxIdleConns)
	}
	if transport.MaxIdleConnsPerHost != 20 {
		t.Fatalf("unexpected max idle conns per host: %d", transport.MaxIdleConnsPerHost)
	}
	if !transport.ForceAttemptHTTP2 {
		t.Fatal("expected HTTP/2 enabled")
	}
}

func TestNewDefaultHTTPClientAppliesOverrides(t *testing.T) {
	client := netutil.NewDefaultHTTPClient(netutil.HTTPClientOptions{
		Timeout:             3 * time.Second,
		MaxIdleConns:        12,
		MaxIdleConnsPerHost: 7,
	})
	transport := client.Transport.(*http.Transport)
	if client.Timeout != 3*time.Second {
		t.Fatalf("unexpected timeout: %s", client.Timeout)
	}
	if transport.MaxIdleConns != 12 {
		t.Fatalf("unexpected max idle conns: %d", transport.MaxIdleConns)
	}
	if transport.MaxIdleConnsPerHost != 7 {
		t.Fatalf("unexpected max idle conns per host: %d", transport.MaxIdleConnsPerHost)
	}
}
