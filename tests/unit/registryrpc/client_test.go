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
			Body:       io.NopCloser(strings.NewReader(`{"schema":{"fingerprint":"abc","public_fingerprint":"def","schema_name":"schema","owner_id":"seller-1","status":"active","schema_url":"https://signed.example/schema.clermcfg","methods":[],"relations":[]},"registration_status":"created"}`)),
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
	if output.Schema.SchemaURL != "https://signed.example/schema.clermcfg" {
		t.Fatalf("unexpected schema url: %s", output.Schema.SchemaURL)
	}
	if output.RegistrationStatus != "created" {
		t.Fatalf("unexpected registration status: %s", output.RegistrationStatus)
	}
}

func TestNewRejectsMissingBaseURL(t *testing.T) {
	t.Parallel()

	_, err := registryrpc.New("   ", nil)
	if err == nil || !platform.IsCode(err, platform.CodeInvalidArgument) {
		t.Fatalf("expected invalid base URL error, got %v", err)
	}
}

func TestNewRejectsUnsupportedSchemeAndEmbeddedCredentials(t *testing.T) {
	t.Parallel()

	if _, err := registryrpc.New("ftp://registry.local", nil); err == nil || !platform.IsCode(err, platform.CodeInvalidArgument) {
		t.Fatalf("expected unsupported-scheme error, got %v", err)
	}
	if _, err := registryrpc.New("https://user:pass@registry.local", nil); err == nil || !platform.IsCode(err, platform.CodeInvalidArgument) {
		t.Fatalf("expected embedded-credentials error, got %v", err)
	}
}

func TestSearchSendsJSONRequest(t *testing.T) {
	t.Parallel()

	client, err := registryrpc.New("http://registry.local", &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("Clerm-Target"); got != "registry.search" {
			t.Fatalf("unexpected Clerm-Target: %s", got)
		}
		if got := strings.TrimSpace(r.Header.Get("Content-Type")); got != "application/json" {
			t.Fatalf("unexpected Content-Type: %s", got)
		}
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			t.Fatalf("ReadAll() error = %v", readErr)
		}
		if !strings.Contains(string(body), `"consumer_id":"buyer-1"`) {
			t.Fatalf("unexpected request body: %s", string(body))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"results":[{"fingerprint":"fp-1","public_fingerprint":"public-fp-1","schema_name":"books","owner_id":"seller-1","status":"active","schema_url":"https://signed.example/books.clermcfg","relations":[{"name":"@global","condition":"any.protected","token_required":false}],"methods":[{"reference":"@global.books.search_books.v1","relation":"@global","condition":"any.protected","execution":"sync","input_count":1,"output_count":1,"output_format":"json"}],"metadata":{"display_name":"Books"}}]}`)),
		}, nil
	})})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	output, err := client.Search(context.Background(), registryrpc.SearchInput{
		ConsumerID: "buyer-1",
		Query:      "books",
		Relations:  []string{"@global"},
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(output.Results) != 1 {
		t.Fatalf("unexpected search output: %#v", output)
	}
	if output.Results[0].SchemaURL != "https://signed.example/books.clermcfg" {
		t.Fatalf("unexpected schema url: %s", output.Results[0].SchemaURL)
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

func TestInvokeRejectsOversizedResponseBody(t *testing.T) {
	t.Parallel()

	client, err := registryrpc.New("http://registry.local", &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Clerm-Target": []string{"registry.invoke"},
			},
			Body: io.NopCloser(strings.NewReader(`12345`)),
		}, nil
	})})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = client.Invoke(context.Background(), registryrpc.InvokeInput{
		ProviderFingerprint: "schema-fp",
		Payload:             []byte("request"),
		MaxResponseBytes:    4,
	})
	if err == nil || !platform.IsCode(err, platform.CodeValidation) {
		t.Fatalf("expected oversized invoke body error, got %v", err)
	}
}

func TestInvokePassesThroughUpstreamErrorResponses(t *testing.T) {
	t.Parallel()

	client, err := registryrpc.New("http://registry.local", &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Header: http.Header{
				"Clerm-Target": []string{"registry.invoke"},
			},
			Body: io.NopCloser(strings.NewReader(`{"ok":false}`)),
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
	if output.StatusCode != http.StatusBadGateway {
		t.Fatalf("unexpected status code: %d", output.StatusCode)
	}
	if string(output.Body) != `{"ok":false}` {
		t.Fatalf("unexpected body: %s", string(output.Body))
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
