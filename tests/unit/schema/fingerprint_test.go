package schema_test

import (
	"strings"
	"testing"

	"github.com/million-in/clerm/schema"
)

func TestCachedPublicFingerprintMatchesCurrentDocumentState(t *testing.T) {
	doc, err := schema.Parse(strings.NewReader(validSchema))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	got := schema.CachedPublicFingerprint(doc)
	want := doc.PublicFingerprint()
	if got != want {
		t.Fatalf("CachedPublicFingerprint() = %x, want %x", got, want)
	}
}

func TestFingerprintCacheInvalidateRecomputesAfterMutation(t *testing.T) {
	doc, err := schema.Parse(strings.NewReader(validSchema))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	cache := schema.NewFingerprintCache()

	first := cache.Public(doc)
	doc.Name = "@general.avail.changed"
	cache.Invalidate(doc)
	second := cache.Public(doc)
	want := doc.PublicFingerprint()

	if first == second {
		t.Fatalf("expected fingerprint change after mutation and invalidation: first=%x second=%x", first, second)
	}
	if second != want {
		t.Fatalf("cache.Public(doc) = %x, want %x", second, want)
	}
}

func TestInvalidateCachedPublicFingerprintRecomputesDefaultCache(t *testing.T) {
	doc, err := schema.Parse(strings.NewReader(validSchema))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	first := schema.CachedPublicFingerprint(doc)
	doc.Relations[0].Condition = "auth.required"
	schema.InvalidateCachedPublicFingerprint(doc)
	second := schema.CachedPublicFingerprint(doc)

	if first == second {
		t.Fatalf("expected fingerprint change after invalidation: first=%x second=%x", first, second)
	}
}
