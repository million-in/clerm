package resolver_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/million-in/clerm/internal/clermcfg"
	"github.com/million-in/clerm/internal/resolver"
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
