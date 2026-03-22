package clermcfg_test

import (
	"fmt"
	"testing"

	"github.com/million-in/clerm/internal/clermcfg"
	"github.com/million-in/clerm/internal/jsonwire"
	"github.com/million-in/clerm/internal/schema"
)

type configBenchCase struct {
	name    string
	doc     *schema.Document
	clerm   []byte
	json    []byte
	metrics configBenchMetrics
}

type configBenchMetrics struct {
	serviceCount int
	methodCount  int
	inputCount   int
	outputCount  int
}

func benchmarkConfigCases(tb testing.TB) []configBenchCase {
	tb.Helper()
	profiles := []struct {
		name string
		cfg  configBenchMetrics
	}{
		{name: "tiny", cfg: configBenchMetrics{serviceCount: 1, methodCount: 1, inputCount: 1, outputCount: 1}},
		{name: "medium", cfg: configBenchMetrics{serviceCount: 4, methodCount: 4, inputCount: 3, outputCount: 2}},
		{name: "large", cfg: configBenchMetrics{serviceCount: 16, methodCount: 16, inputCount: 8, outputCount: 6}},
	}

	out := make([]configBenchCase, 0, len(profiles))
	for _, profile := range profiles {
		doc := buildBenchmarkDocument(profile.cfg)
		if err := doc.Validate(); err != nil {
			tb.Fatalf("%s Validate() error = %v", profile.name, err)
		}
		clermData, err := clermcfg.Encode(doc)
		if err != nil {
			tb.Fatalf("%s Encode() error = %v", profile.name, err)
		}
		jsonData, err := jsonwire.MarshalConfig(doc, true)
		if err != nil {
			tb.Fatalf("%s MarshalConfig() error = %v", profile.name, err)
		}
		out = append(out, configBenchCase{
			name:    profile.name,
			doc:     doc,
			clerm:   clermData,
			json:    jsonData,
			metrics: profile.cfg,
		})
	}
	return out
}

func BenchmarkEncodeCLERMCFG(b *testing.B) {
	for _, tc := range benchmarkConfigCases(b) {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.clerm)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := clermcfg.Encode(tc.doc); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkDecodeCLERMCFG(b *testing.B) {
	for _, tc := range benchmarkConfigCases(b) {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.clerm)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := clermcfg.Decode(tc.clerm); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkDecodeCLERMCFGCodecOnly(b *testing.B) {
	for _, tc := range benchmarkConfigCases(b) {
		var decoder clermcfg.Decoder
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.clerm)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := decoder.DecodeCodec(tc.clerm); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkValidateCLERMCFGSemantics(b *testing.B) {
	for _, tc := range benchmarkConfigCases(b) {
		doc, err := clermcfg.DecodeCodec(tc.clerm)
		if err != nil {
			b.Fatalf("%s DecodeCodec() error = %v", tc.name, err)
		}
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := doc.Validate(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkMarshalJSONConfig(b *testing.B) {
	for _, tc := range benchmarkConfigCases(b) {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.json)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := jsonwire.MarshalConfig(tc.doc, true); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkUnmarshalJSONConfig(b *testing.B) {
	for _, tc := range benchmarkConfigCases(b) {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.json)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := jsonwire.UnmarshalConfig(tc.json); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkRoundTripCLERMCFG(b *testing.B) {
	for _, tc := range benchmarkConfigCases(b) {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.clerm) * 2))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				encoded, err := clermcfg.Encode(tc.doc)
				if err != nil {
					b.Fatal(err)
				}
				if _, err := clermcfg.Decode(encoded); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkRoundTripCLERMCFGCodecOnly(b *testing.B) {
	for _, tc := range benchmarkConfigCases(b) {
		var decoder clermcfg.Decoder
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.clerm) * 2))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				encoded, err := clermcfg.Encode(tc.doc)
				if err != nil {
					b.Fatal(err)
				}
				if _, err := decoder.DecodeCodec(encoded); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkRoundTripJSONConfig(b *testing.B) {
	for _, tc := range benchmarkConfigCases(b) {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.json) * 2))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				encoded, err := jsonwire.MarshalConfig(tc.doc, true)
				if err != nil {
					b.Fatal(err)
				}
				if _, err := jsonwire.UnmarshalConfig(encoded); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkParallelDecodeCLERMCFG(b *testing.B) {
	for _, tc := range benchmarkConfigCases(b) {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.clerm)))
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					if _, err := clermcfg.Decode(tc.clerm); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}

func BenchmarkParallelRoundTripCLERMCFG(b *testing.B) {
	for _, tc := range benchmarkConfigCases(b) {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.clerm) * 2))
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					encoded, err := clermcfg.Encode(tc.doc)
					if err != nil {
						b.Fatal(err)
					}
					if _, err := clermcfg.Decode(encoded); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}

func buildBenchmarkDocument(cfg configBenchMetrics) *schema.Document {
	doc := &schema.Document{
		Name:          "@general.avail.mandene",
		RelationsName: "@general.mandene",
		Route:         "https://resolver.health.example/clerm/benchmark/schema",
		Relations: []schema.RelationRule{
			{Name: "@global", Condition: "any.protected"},
			{Name: "@verified", Condition: "auth.required"},
			{Name: "@private", Condition: "registered.required"},
		},
	}

	relations := []string{"@global", "@verified", "@private"}
	for i := 0; i < cfg.serviceCount; i++ {
		relation := relations[i%len(relations)]
		ref := newServiceRef(relation, fmt.Sprintf("benchsvc%02d", i), fmt.Sprintf("op%02d", i), "v1")
		doc.Services = append(doc.Services, ref)
		if i < cfg.methodCount {
			doc.Methods = append(doc.Methods, schema.Method{
				Reference:    ref,
				Execution:    chooseExecution(i),
				InputCount:   cfg.inputCount,
				InputArgs:    buildParameters("in", cfg.inputCount),
				OutputCount:  cfg.outputCount,
				OutputArgs:   buildParameters("out", cfg.outputCount),
				OutputFormat: schema.FormatJSON,
			})
		}
	}
	return doc
}

func newServiceRef(relation string, namespace string, method string, version string) schema.ServiceRef {
	raw := fmt.Sprintf("%s.%s.%s.%s", relation, namespace, method, version)
	return schema.ServiceRef{
		Raw:       raw,
		Relation:  relation,
		Namespace: namespace,
		Method:    method,
		Version:   version,
	}
}

func chooseExecution(i int) schema.ExecutionMode {
	if i%2 == 0 {
		return schema.ExecutionSync
	}
	return schema.ExecutionAsyncPool
}

func buildParameters(prefix string, count int) []schema.Parameter {
	types := []schema.ArgType{
		schema.ArgString,
		schema.ArgDecimal,
		schema.ArgUUID,
		schema.ArgArray,
		schema.ArgTimestamp,
		schema.ArgInt,
		schema.ArgBool,
	}
	params := make([]schema.Parameter, 0, count)
	for i := 0; i < count; i++ {
		params = append(params, schema.Parameter{
			Name: fmt.Sprintf("%s_%02d", prefix, i),
			Type: types[i%len(types)],
		})
	}
	return params
}
