package resolver_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/million-in/clerm/clermcfg"
	"github.com/million-in/clerm/resolver"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestLoadConfigURL(t *testing.T) {
	doc := mustDocument(t)
	payload, err := clermcfg.Encode(doc)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/clermcfg"}},
			Body:       io.NopCloser(bytes.NewReader(payload)),
			Request:    request,
		}, nil
	})}

	service, err := resolver.LoadConfigURL(context.Background(), "https://registry.example/schema/shopify.clermcfg", client)
	if err != nil {
		t.Fatalf("LoadConfigURL() error = %v", err)
	}
	if service.Document().Name != doc.Name {
		t.Fatalf("unexpected schema name: %s", service.Document().Name)
	}
}

func TestLoadConfigURLRejectsOversizedPayload(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Header:        http.Header{"Content-Type": []string{"application/clermcfg"}},
			ContentLength: 5,
			Body:          io.NopCloser(strings.NewReader("12345")),
			Request:       request,
		}, nil
	})}

	_, err := resolver.LoadConfigURLWithOptions(context.Background(), "https://registry.example/schema/shopify.clermcfg", resolver.LoadConfigURLOptions{
		HTTPClient:      client,
		MaxPayloadBytes: 4,
	})
	if err == nil || !strings.Contains(err.Error(), "exceeds configured size limit") {
		t.Fatalf("expected payload limit error, got %v", err)
	}
}

func TestLoadConfigURLRejectsPrivateHostWithPolicy(t *testing.T) {
	_, err := resolver.LoadConfigURLWithOptions(context.Background(), "http://127.0.0.1/schema.clermcfg", resolver.LoadConfigURLOptions{
		URLPolicy: resolver.DenyPrivateHostPolicy,
	})
	if err == nil || !strings.Contains(err.Error(), "host is not allowed") {
		t.Fatalf("expected private-host rejection, got %v", err)
	}
}

func TestLoadConfigURLRejectsBlockedRedirectTarget(t *testing.T) {
	doc := mustDocument(t)
	payload, err := clermcfg.Encode(doc)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Host {
		case "allowed.example":
			return &http.Response{
				StatusCode: http.StatusFound,
				Header:     http.Header{"Location": []string{"https://blocked.example/schema/shopify.clermcfg"}},
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    request,
			}, nil
		case "blocked.example":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/clermcfg"}},
				Body:       io.NopCloser(bytes.NewReader(payload)),
				Request:    request,
			}, nil
		default:
			t.Fatalf("unexpected request host: %s", request.URL.Host)
			return nil, nil
		}
	})}

	policy := func(_ context.Context, rawURL *url.URL) error {
		if rawURL.Hostname() == "blocked.example" {
			return errors.New("schema URL host is not allowed")
		}
		return nil
	}
	_, err = resolver.LoadConfigURLWithOptions(context.Background(), "https://allowed.example/schema/shopify.clermcfg", resolver.LoadConfigURLOptions{
		HTTPClient: client,
		URLPolicy:  policy,
	})
	if err == nil || !strings.Contains(err.Error(), "host is not allowed") {
		t.Fatalf("expected redirect policy rejection, got %v", err)
	}
}

func TestLoadConfigURLRedirectPolicyUsesRequestContext(t *testing.T) {
	doc := mustDocument(t)
	payload, err := clermcfg.Encode(doc)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	client := &http.Client{
		Timeout: 200 * time.Millisecond,
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			switch request.URL.Host {
			case "allowed.example":
				return &http.Response{
					StatusCode: http.StatusFound,
					Header:     http.Header{"Location": []string{"https://redirected.example/schema/shopify.clermcfg"}},
					Body:       io.NopCloser(strings.NewReader("")),
					Request:    request,
				}, nil
			case "redirected.example":
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/clermcfg"}},
					Body:       io.NopCloser(bytes.NewReader(payload)),
					Request:    request,
				}, nil
			default:
				t.Fatalf("unexpected request host: %s", request.URL.Host)
				return nil, nil
			}
		}),
	}
	var redirectContext context.Context
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		redirectContext = req.Context()
		return nil
	}

	policy := func(ctx context.Context, rawURL *url.URL) error {
		if rawURL.Hostname() != "redirected.example" {
			return nil
		}
		if redirectContext == nil || ctx != redirectContext {
			return errors.New("redirect policy missing redirect request context")
		}
		return errors.New("redirect blocked")
	}

	_, err = resolver.LoadConfigURLWithOptions(context.Background(), "https://allowed.example/schema/shopify.clermcfg", resolver.LoadConfigURLOptions{
		HTTPClient: client,
		URLPolicy:  policy,
	})
	if err == nil || !strings.Contains(err.Error(), "redirect blocked") {
		t.Fatalf("expected redirect policy error, got %v", err)
	}
}

func TestLoadConfigURLPinsResolvedIPForTransportDials(t *testing.T) {
	var dialedAddress string
	dialErr := errors.New("dial blocked for test")
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				dialedAddress = address
				return nil, dialErr
			},
		},
	}

	_, err := resolver.LoadConfigURLWithOptions(context.Background(), "http://localhost:80/schema/shopify.clermcfg", resolver.LoadConfigURLOptions{
		HTTPClient: client,
		URLPolicy: func(context.Context, *url.URL) error {
			return nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), dialErr.Error()) {
		t.Fatalf("expected dial error, got %v", err)
	}
	if gotHost, _, splitErr := net.SplitHostPort(dialedAddress); splitErr != nil || net.ParseIP(gotHost) == nil {
		t.Fatalf("expected pinned IP dial address, got %q (err=%v)", dialedAddress, splitErr)
	}
}
