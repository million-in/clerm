package clermreq_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/million-in/clerm/internal/clermreq"
	"github.com/million-in/clerm/internal/jsonwire"
	"github.com/million-in/clerm/internal/schema"
)

type requestBenchCase struct {
	name   string
	method schema.Method
	req    *clermreq.Request
	clerm  []byte
	json   []byte
}

func benchmarkRequestCases(tb testing.TB) []requestBenchCase {
	tb.Helper()
	cases := []struct {
		name    string
		method  schema.Method
		payload []byte
	}{
		{
			name: "tiny",
			method: schema.Method{
				Reference: schema.ServiceRef{
					Raw:       "@global.system.handshake.v1",
					Relation:  "@global",
					Namespace: "system",
					Method:    "handshake",
					Version:   "v1",
				},
				InputArgs: []schema.Parameter{
					{Name: "hello", Type: schema.ArgString},
				},
			},
			payload: mustCompactJSON(tb, map[string]any{
				"hello": "ping",
			}),
		},
		{
			name: "medium",
			method: schema.Method{
				Reference: schema.ServiceRef{
					Raw:       "@global.healthcare.search_providers.v1",
					Relation:  "@global",
					Namespace: "healthcare",
					Method:    "search_providers",
					Version:   "v1",
				},
				InputArgs: []schema.Parameter{
					{Name: "specialty", Type: schema.ArgString},
					{Name: "latitude", Type: schema.ArgDecimal},
					{Name: "longitude", Type: schema.ArgDecimal},
				},
			},
			payload: mustCompactJSON(tb, map[string]any{
				"specialty": "cardiology",
				"latitude":  40.7,
				"longitude": -73.9,
			}),
		},
		{
			name: "large",
			method: schema.Method{
				Reference: schema.ServiceRef{
					Raw:       "@verified.registry.invoke_capability.v1",
					Relation:  "@verified",
					Namespace: "registry",
					Method:    "invoke_capability",
					Version:   "v1",
				},
				InputArgs: []schema.Parameter{
					{Name: "schema_ref", Type: schema.ArgString},
					{Name: "capability_token", Type: schema.ArgString},
					{Name: "nonce", Type: schema.ArgString},
					{Name: "issued_at", Type: schema.ArgTimestamp},
					{Name: "scopes", Type: schema.ArgArray},
				},
			},
			payload: mustCompactJSON(tb, map[string]any{
				"schema_ref":       "@verified.registry.invoke_capability.v1",
				"capability_token": strings.Repeat("cap_", 512),
				"nonce":            strings.Repeat("n", 128),
				"issued_at":        "2026-03-21T12:34:56Z",
				"scopes":           buildScopeList(128),
			}),
		},
	}

	out := make([]requestBenchCase, 0, len(cases))
	for _, tc := range cases {
		request, err := clermreq.Build(tc.method, tc.payload)
		if err != nil {
			tb.Fatalf("%s Build() error = %v", tc.name, err)
		}
		clermData, err := clermreq.Encode(request)
		if err != nil {
			tb.Fatalf("%s Encode() error = %v", tc.name, err)
		}
		jsonData, err := jsonwire.MarshalRequest(request)
		if err != nil {
			tb.Fatalf("%s MarshalRequest() error = %v", tc.name, err)
		}
		out = append(out, requestBenchCase{
			name:   tc.name,
			method: tc.method,
			req:    request,
			clerm:  clermData,
			json:   jsonData,
		})
	}
	return out
}

func BenchmarkEncodeCLERMRequest(b *testing.B) {
	for _, tc := range benchmarkRequestCases(b) {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.clerm)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := clermreq.Encode(tc.req); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkDecodeCLERMRequest(b *testing.B) {
	for _, tc := range benchmarkRequestCases(b) {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.clerm)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := clermreq.Decode(tc.clerm); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkDecodeCLERMRequestCodecOnly(b *testing.B) {
	for _, tc := range benchmarkRequestCases(b) {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.clerm)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := clermreq.Decode(tc.clerm); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkValidateCLERMRequestSemantics(b *testing.B) {
	for _, tc := range benchmarkRequestCases(b) {
		req, err := clermreq.Decode(tc.clerm)
		if err != nil {
			b.Fatalf("%s Decode() error = %v", tc.name, err)
		}
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := req.ValidateAgainst(tc.method); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkMarshalJSONRequest(b *testing.B) {
	for _, tc := range benchmarkRequestCases(b) {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.json)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := jsonwire.MarshalRequest(tc.req); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkUnmarshalJSONRequest(b *testing.B) {
	for _, tc := range benchmarkRequestCases(b) {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.json)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := jsonwire.UnmarshalRequest(tc.json); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkRoundTripCLERMRequest(b *testing.B) {
	for _, tc := range benchmarkRequestCases(b) {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.clerm) * 2))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				encoded, err := clermreq.Encode(tc.req)
				if err != nil {
					b.Fatal(err)
				}
				decoded, err := clermreq.Decode(encoded)
				if err != nil {
					b.Fatal(err)
				}
				if err := decoded.ValidateAgainst(tc.method); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkRoundTripCLERMRequestCodecOnly(b *testing.B) {
	for _, tc := range benchmarkRequestCases(b) {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.clerm) * 2))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				encoded, err := clermreq.Encode(tc.req)
				if err != nil {
					b.Fatal(err)
				}
				if _, err := clermreq.Decode(encoded); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkRoundTripJSONRequest(b *testing.B) {
	for _, tc := range benchmarkRequestCases(b) {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.json) * 2))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				encoded, err := jsonwire.MarshalRequest(tc.req)
				if err != nil {
					b.Fatal(err)
				}
				decoded, err := jsonwire.UnmarshalRequest(encoded)
				if err != nil {
					b.Fatal(err)
				}
				if err := decoded.ValidateAgainst(tc.method); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkParallelDecodeCLERMRequest(b *testing.B) {
	for _, tc := range benchmarkRequestCases(b) {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.clerm)))
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					if _, err := clermreq.Decode(tc.clerm); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}

func BenchmarkParallelRoundTripCLERMRequest(b *testing.B) {
	for _, tc := range benchmarkRequestCases(b) {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.clerm) * 2))
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					encoded, err := clermreq.Encode(tc.req)
					if err != nil {
						b.Fatal(err)
					}
					if _, err := clermreq.Decode(encoded); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}

func buildScopeList(count int) []string {
	out := make([]string, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, "scope_"+strings.Repeat("x", i%16+4))
	}
	return out
}

func mustCompactJSON(tb testing.TB, value any) []byte {
	tb.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		tb.Fatalf("json.Marshal() error = %v", err)
	}
	return data
}
