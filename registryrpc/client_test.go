package registryrpc_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/million-in/clerm/platform"
	"github.com/million-in/clerm/registryrpc"
)

func TestRegisterSendsCompiledPayload(t *testing.T) {
	t.Parallel()

	client, err := registryrpc.New("http://registry.local", &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("Clerm-Target"); got != "registry.register" {
			t.Fatalf("unexpected Clerm-Target: %s", got)
		}
		if got := strings.TrimSpace(r.Header.Get("Content-Type")); got != "application/clermcfg" {
			t.Fatalf("unexpected Content-Type: %s", got)
		}
		if got := r.Header.Get("Clerm-Owner"); got != "seller-1" {
			t.Fatalf("unexpected Clerm-Owner: %s", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"schema":{"fingerprint":"abc","public_fingerprint":"def","schema_name":"schema","owner_id":"seller-1","status":"active","methods":[],"relations":[]}}`)),
		}, nil
	})})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	output, err := client.Register(context.Background(), registryrpc.RegisterInput{
		OwnerID: "seller-1",
		Status:  "active",
		Payload: []byte("cfg"),
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if output.Schema.Fingerprint != "abc" {
		t.Fatalf("unexpected fingerprint: %s", output.Schema.Fingerprint)
	}
}

func TestInvokeReturnsUpstreamResponse(t *testing.T) {
	t.Parallel()

	client, err := registryrpc.New("http://registry.local", &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("Clerm-Target"); got != "registry.invoke" {
			t.Fatalf("unexpected Clerm-Target: %s", got)
		}
		if got := r.Header.Get("Clerm-Schema-Fingerprint"); got != "schema-fp" {
			t.Fatalf("unexpected Clerm-Schema-Fingerprint: %s", got)
		}
		return &http.Response{
			StatusCode: http.StatusAccepted,
			Header: http.Header{
				"Clerm-Target":         []string{"registry.invoke"},
				"Clerm-Command-Method": []string{"@verified.books.purchase_book.v1"},
			},
			Body: io.NopCloser(strings.NewReader(`{"ok":true}`)),
		}, nil
	})})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	output, err := client.Invoke(context.Background(), registryrpc.InvokeInput{
		ProviderFingerprint: "schema-fp",
		Payload:             []byte("request"),
	})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if output.StatusCode != http.StatusAccepted {
		t.Fatalf("unexpected status code: %d", output.StatusCode)
	}
	if output.CommandMethod != "@verified.books.purchase_book.v1" {
		t.Fatalf("unexpected command method: %s", output.CommandMethod)
	}
	if string(output.Body) != `{"ok":true}` {
		t.Fatalf("unexpected body: %s", string(output.Body))
	}
}

func TestInvokeReturnsRegistryValidationErrors(t *testing.T) {
	t.Parallel()

	client, err := registryrpc.New("http://registry.local", &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("validation_error: capability token is required")),
		}, nil
	})})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = client.Invoke(context.Background(), registryrpc.InvokeInput{
		ProviderFingerprint: "schema-fp",
		Payload:             []byte("request"),
	})
	if err == nil {
		t.Fatal("expected invoke error")
	}
	if !platform.IsCode(err, platform.CodeValidation) {
		t.Fatalf("unexpected error code: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
