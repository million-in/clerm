package resolver_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
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
