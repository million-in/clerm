package resolver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/million-in/clerm/clermresp"
	"github.com/million-in/clerm/internal/clermreq"
	"github.com/million-in/clerm/internal/resolver"
	"github.com/million-in/clerm/internal/schema"
)

type resolverBenchCase struct {
	name             string
	payload          []byte
	response         map[string]any
	prebuiltResponse *clermresp.Response
	byteLength       int64
}

func benchmarkDocument(b *testing.B) *schema.Document {
	b.Helper()
	doc, err := schema.Parse(strings.NewReader(`
schema @general.avail.mandene
  @route: https://resolver.benchmark.example/clerm
  service: @global.benchmark.search.v1

method @global.benchmark.search.v1
  @exec: sync
  @args_input: 2
    decl_args: query.STRING, filters.ARRAY
  @args_output: 2
    decl_args: request_id.UUID, results.ARRAY
    decl_format: json

relations @general.mandene
  @global: any.protected
`))
	if err != nil {
		b.Fatalf("Parse() error = %v", err)
	}
	return doc
}

func benchmarkCases(b *testing.B) []resolverBenchCase {
	b.Helper()
	doc := benchmarkDocument(b)
	method, ok := doc.MethodByReference("@global.benchmark.search.v1")
	if !ok {
		b.Fatal("benchmark method missing")
	}
	build := func(name string, items int) resolverBenchCase {
		filters := make([]string, items)
		results := make([]map[string]any, items)
		for i := 0; i < items; i++ {
			filters[i] = fmt.Sprintf("tag-%d", i)
			results[i] = map[string]any{"id": fmt.Sprintf("item-%d", i)}
		}
		payload := fmt.Sprintf(`{"query":"shoes","filters":%s}`, mustJSON(filters))
		request, err := clermreq.Build(method, []byte(payload))
		if err != nil {
			b.Fatalf("Build(%s) error = %v", name, err)
		}
		encoded, err := clermreq.Encode(request)
		if err != nil {
			b.Fatalf("Encode(%s) error = %v", name, err)
		}
		outputs := map[string]any{"request_id": "123e4567-e89b-12d3-a456-426614174000", "results": results}
		response, err := clermresp.BuildSuccessMap(method, outputs)
		if err != nil {
			b.Fatalf("BuildSuccessResponse(%s) error = %v", name, err)
		}
		return resolverBenchCase{
			name:             name,
			payload:          encoded,
			response:         outputs,
			prebuiltResponse: response,
			byteLength:       int64(len(encoded)),
		}
	}
	return []resolverBenchCase{
		build("tiny", 1),
		build("medium", 32),
		build("large", 512),
	}
}

func BenchmarkResolveBinary(b *testing.B) {
	service := resolver.New(benchmarkDocument(b))
	for _, benchCase := range benchmarkCases(b) {
		b.Run(benchCase.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(benchCase.byteLength)
			for i := 0; i < b.N; i++ {
				if _, err := service.ResolveBinaryWithTarget(benchCase.payload, "internal.search"); err != nil {
					b.Fatalf("ResolveBinaryWithTarget() error = %v", err)
				}
			}
		})
	}
}

func BenchmarkResolveInvocation(b *testing.B) {
	service := resolver.New(benchmarkDocument(b))
	for _, benchCase := range benchmarkCases(b) {
		b.Run(benchCase.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(benchCase.byteLength)
			for i := 0; i < b.N; i++ {
				invocation, err := service.ResolveInvocationWithTarget(benchCase.payload, "internal.search")
				if err != nil {
					b.Fatalf("ResolveInvocationWithTarget() error = %v", err)
				}
				if invocation == nil {
					b.Fatal("expected invocation")
				}
			}
		})
	}
}

func BenchmarkInvocationArgumentsAccess(b *testing.B) {
	service := resolver.New(benchmarkDocument(b))
	for _, benchCase := range benchmarkCases(b) {
		benchCase := benchCase
		b.Run(benchCase.name+"/map", func(b *testing.B) {
			invocation, err := service.ResolveInvocationWithTarget(benchCase.payload, "internal.search")
			if err != nil {
				b.Fatalf("ResolveInvocationWithTarget() error = %v", err)
			}
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				values, err := invocation.ArgumentsMap()
				if err != nil {
					b.Fatalf("ArgumentsMap() error = %v", err)
				}
				if values["query"] != "shoes" {
					b.Fatalf("unexpected query: %#v", values["query"])
				}
			}
		})
		b.Run(benchCase.name+"/view", func(b *testing.B) {
			invocation, err := service.ResolveInvocationWithTarget(benchCase.payload, "internal.search")
			if err != nil {
				b.Fatalf("ResolveInvocationWithTarget() error = %v", err)
			}
			if _, err := invocation.Arguments(); err != nil {
				b.Fatalf("Arguments() warmup error = %v", err)
			}
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				view, err := invocation.Arguments()
				if err != nil {
					b.Fatalf("Arguments() error = %v", err)
				}
				if value, ok := view.Lookup("query"); !ok || value != "shoes" {
					b.Fatalf("unexpected query lookup: (%#v, %t)", value, ok)
				}
			}
		})
	}
}

func BenchmarkInvocationMarshalJSON(b *testing.B) {
	service := resolver.New(benchmarkDocument(b))
	for _, benchCase := range benchmarkCases(b) {
		benchCase := benchCase
		b.Run(benchCase.name+"/arguments", func(b *testing.B) {
			invocation, err := service.ResolveInvocationWithTarget(benchCase.payload, "internal.search")
			if err != nil {
				b.Fatalf("ResolveInvocationWithTarget() error = %v", err)
			}
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				payload, err := invocation.MarshalArgumentsJSON()
				if err != nil {
					b.Fatalf("MarshalArgumentsJSON() error = %v", err)
				}
				if len(payload) == 0 {
					b.Fatal("expected payload")
				}
			}
		})
		b.Run(benchCase.name+"/command", func(b *testing.B) {
			invocation, err := service.ResolveInvocationWithTarget(benchCase.payload, "internal.search")
			if err != nil {
				b.Fatalf("ResolveInvocationWithTarget() error = %v", err)
			}
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				payload, err := invocation.MarshalCommandJSON()
				if err != nil {
					b.Fatalf("MarshalCommandJSON() error = %v", err)
				}
				if len(payload) == 0 {
					b.Fatal("expected payload")
				}
			}
		})
	}
}

func BenchmarkExecuteBinary(b *testing.B) {
	service := resolver.New(benchmarkDocument(b))
	for _, benchCase := range benchmarkCases(b) {
		benchCase := benchCase
		if err := service.Bind("@global.benchmark.search.v1", func(context.Context, *resolver.Invocation) (*resolver.Result, error) {
			return resolver.SuccessResponse(benchCase.prebuiltResponse), nil
		}); err != nil {
			b.Fatalf("Bind() error = %v", err)
		}
		b.Run(benchCase.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(benchCase.byteLength)
			for i := 0; i < b.N; i++ {
				response, _, err := service.ExecuteBinary(context.Background(), benchCase.payload, "internal.search")
				if err != nil {
					b.Fatalf("ExecuteBinary() error = %v", err)
				}
				if response == nil {
					b.Fatal("expected response")
				}
			}
		})
	}
}

func BenchmarkExecuteBinaryMap(b *testing.B) {
	service := resolver.New(benchmarkDocument(b))
	for _, benchCase := range benchmarkCases(b) {
		benchCase := benchCase
		if err := service.Bind("@global.benchmark.search.v1", func(context.Context, *resolver.Invocation) (*resolver.Result, error) {
			return resolver.Success(benchCase.response), nil
		}); err != nil {
			b.Fatalf("Bind() error = %v", err)
		}
		b.Run(benchCase.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(benchCase.byteLength)
			for i := 0; i < b.N; i++ {
				response, _, err := service.ExecuteBinary(context.Background(), benchCase.payload, "internal.search")
				if err != nil {
					b.Fatalf("ExecuteBinary() error = %v", err)
				}
				if response == nil {
					b.Fatal("expected response")
				}
			}
		})
	}
}

func BenchmarkMiddlewareCLERM(b *testing.B) {
	service := resolver.New(benchmarkDocument(b))
	prebuiltResponse, err := clermresp.BuildSuccessMap(service.Document().Methods[0], map[string]any{
		"request_id": "123e4567-e89b-12d3-a456-426614174000",
		"results":    []map[string]any{{"id": "item-1"}},
	})
	if err != nil {
		b.Fatalf("BuildSuccessResponse() error = %v", err)
	}
	if err := service.Bind("@global.benchmark.search.v1", func(context.Context, *resolver.Invocation) (*resolver.Result, error) {
		return resolver.SuccessResponse(prebuiltResponse), nil
	}); err != nil {
		b.Fatalf("Bind() error = %v", err)
	}
	handler := service.Middleware(http.NotFoundHandler())
	for _, benchCase := range benchmarkCases(b) {
		b.Run(benchCase.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(benchCase.byteLength)
			headers := make(http.Header, 2)
			headers.Set("Content-Type", "application/clerm")
			headers.Set("Clerm-Target", "internal.search")
			body := &benchReadCloser{}
			request := &http.Request{
				Method: http.MethodPost,
				Header: headers,
				Body:   body,
			}
			writer := &benchResponseWriter{header: make(http.Header, 2)}
			for i := 0; i < b.N; i++ {
				body.Reset(benchCase.payload)
				writer.Reset()
				handler.ServeHTTP(writer, request)
				if writer.statusCode != http.StatusOK {
					b.Fatalf("unexpected status: %d", writer.statusCode)
				}
			}
		})
	}
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}

type benchReadCloser struct {
	reader bytes.Reader
}

func (b *benchReadCloser) Read(p []byte) (int, error) {
	return b.reader.Read(p)
}

func (b *benchReadCloser) Close() error {
	return nil
}

func (b *benchReadCloser) Reset(payload []byte) {
	b.reader.Reset(payload)
}

type benchResponseWriter struct {
	header     http.Header
	statusCode int
}

func (b *benchResponseWriter) Header() http.Header {
	return b.header
}

func (b *benchResponseWriter) Write(payload []byte) (int, error) {
	return len(payload), nil
}

func (b *benchResponseWriter) WriteHeader(statusCode int) {
	b.statusCode = statusCode
}

func (b *benchResponseWriter) Reset() {
	b.statusCode = http.StatusOK
	clear(b.header)
}
