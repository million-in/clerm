package clermwire_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/million-in/clerm/internal/clermwire"
	"github.com/million-in/clerm/internal/schema"
)

func BenchmarkValidateArrayEnvelope(b *testing.B) {
	parts := make([]string, 0, 512)
	for i := 0; i < 512; i++ {
		parts = append(parts, fmt.Sprintf(`{"provider_id":"p-%d","score":%d}`, i, i))
	}
	raw := []byte("[" + strings.Join(parts, ",") + "]")
	b.ReportAllocs()
	b.SetBytes(int64(len(raw)))
	for i := 0; i < b.N; i++ {
		if err := clermwire.ValidateValue(schema.ArgArray, raw); err != nil {
			b.Fatalf("ValidateValue() error = %v", err)
		}
	}
}

func BenchmarkBuildValues(b *testing.B) {
	params := []schema.Parameter{
		{Name: "specialty", Type: schema.ArgString},
		{Name: "latitude", Type: schema.ArgDecimal},
		{Name: "longitude", Type: schema.ArgDecimal},
	}
	raw := []byte(` { "specialty" : "cardiology" , "latitude" : 40.7 , "longitude" : -73.9 } `)
	b.ReportAllocs()
	b.SetBytes(int64(len(raw)))
	for i := 0; i < b.N; i++ {
		values, err := clermwire.BuildValues(params, raw, "input")
		if err != nil {
			b.Fatalf("BuildValues() error = %v", err)
		}
		if len(values) != 3 {
			b.Fatalf("unexpected value count: %d", len(values))
		}
	}
}
