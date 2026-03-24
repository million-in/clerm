package schema_test

import (
	"strings"
	"testing"

	"github.com/million-in/clerm/schema"
)

func benchmarkDocument(b *testing.B) *schema.Document {
	b.Helper()
	doc, err := schema.Parse(strings.NewReader(`
schema @general.avail.mandene
  @route: https://resolver.health.example/clerm
  service: @global.healthcare.search_providers.v1
  service: @verified.healthcare.book_visit.v1

method @global.healthcare.search_providers.v1
  @exec: async.pool
  @args_input: 3
    decl_args: specialty.STRING, latitude.DECIMAL, longitude.DECIMAL
  @args_output: 2
    decl_args: request_id.UUID, providers.ARRAY
    decl_format: json

method @verified.healthcare.book_visit.v1
  @exec: sync
  @args_input: 2
    decl_args: provider_id.STRING, user_token.STRING
  @args_output: 2
    decl_args: order_id.STRING, status.STRING
    decl_format: json

relations @general.mandene
  @global: any.protected
  @verified: auth.required
`))
	if err != nil {
		b.Fatalf("Parse() error = %v", err)
	}
	return doc
}

func BenchmarkPublicFingerprint(b *testing.B) {
	doc := benchmarkDocument(b)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = doc.PublicFingerprint()
	}
}

func BenchmarkCachedPublicFingerprint(b *testing.B) {
	doc := benchmarkDocument(b)
	cache := schema.NewFingerprintCache()
	_ = cache.Public(doc)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = cache.Public(doc)
	}
}
