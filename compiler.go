package clerm

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/million-in/clerm/capability"
	"github.com/million-in/clerm/clermcfg"
	"github.com/million-in/clerm/clermreq"
	"github.com/million-in/clerm/clermresp"
	"github.com/million-in/clerm/internal/jsonwire"
	"github.com/million-in/clerm/platform"
	"github.com/million-in/clerm/schema"
)

type CompileResult struct {
	InputPath  string
	OutputPath string
	Document   *schema.Document
	Payload    []byte
}

type BuildRequestInput struct {
	MethodReference  string
	AllowedRelations []string
	PayloadJSON      []byte
	Capability       *capability.Token
	CapabilityText   string
	CapabilityRaw    []byte
}

type BuildRequestResult struct {
	Method            schema.Method
	RelationCondition string
	Request           *clermreq.Request
}

type EncodedRequest struct {
	BuildRequestResult
	Payload []byte
}

type ToolDefinition struct {
	Name          string         `json:"name"`
	Description   string         `json:"description"`
	Method        string         `json:"clerm_method"`
	Relation      string         `json:"relation"`
	Condition     string         `json:"condition"`
	TokenRequired bool           `json:"token_required"`
	InputSchema   map[string]any `json:"input_schema"`
}

type BenchmarkInput struct {
	MethodReference string
	PayloadJSON     []byte
	Iterations      int
}

type BenchmarkSize struct {
	CLERMBytes   int     `json:"clerm_bytes"`
	JSONBytes    int     `json:"json_bytes"`
	SavedBytes   int     `json:"saved_bytes"`
	SavedPercent float64 `json:"saved_percent"`
}

type BenchmarkTiming struct {
	CLERMEncodeNS float64 `json:"clerm_encode_ns_per_op"`
	CLERMDecodeNS float64 `json:"clerm_decode_ns_per_op"`
	JSONEncodeNS  float64 `json:"json_encode_ns_per_op"`
	JSONDecodeNS  float64 `json:"json_decode_ns_per_op"`
}

type BenchmarkReport struct {
	Iterations      int             `json:"iterations"`
	MethodologyNote string          `json:"methodology_note"`
	ConfigSize      BenchmarkSize   `json:"config_size"`
	RequestSize     BenchmarkSize   `json:"request_size"`
	ConfigTime      BenchmarkTiming `json:"config_time"`
	RequestTime     BenchmarkTiming `json:"request_time"`
}

type InspectOptions struct {
	IncludeInternal bool
}

type Inspection struct {
	Kind     string               `json:"kind"`
	Document *InspectableDocument `json:"document,omitempty"`
	Request  *InspectableRequest  `json:"request,omitempty"`
	Response *InspectableResponse `json:"response,omitempty"`
}

type InspectableDocument struct {
	Name          string                `json:"name"`
	RelationsName string                `json:"relations_name"`
	Metadata      schema.Metadata       `json:"metadata,omitempty"`
	Route         string                `json:"route,omitempty"`
	Services      []schema.ServiceRef   `json:"services"`
	Methods       []schema.Method       `json:"methods"`
	Relations     []schema.RelationRule `json:"relations"`
}

type InspectableRequest struct {
	Method          string                  `json:"method"`
	Arguments       []clermreq.Argument     `json:"arguments"`
	Capability      *capability.InspectView `json:"capability,omitempty"`
	CapabilityError string                  `json:"capability_error,omitempty"`
}

type InspectableResponse struct {
	Method         string               `json:"method"`
	Error          *clermresp.ErrorBody `json:"error,omitempty"`
	Outputs        []clermresp.Value    `json:"outputs,omitempty"`
	DecodedOutputs map[string]any       `json:"decoded_outputs,omitempty"`
}

func (CompilerAPI) ParseDocument(r io.Reader) (*schema.Document, error) {
	return schema.Parse(r)
}

func (CompilerAPI) DecodeDocument(data []byte) (*schema.Document, error) {
	return clermcfg.Decode(data)
}

func (CompilerAPI) LoadDocument(path string) (*schema.Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read schema input")
	}
	return loadDocumentData(path, data)
}

func (CompilerAPI) CompileSource(source []byte) (*CompileResult, error) {
	doc, err := schema.Parse(bytes.NewReader(source))
	if err != nil {
		return nil, err
	}
	payload, err := clermcfg.Encode(doc)
	if err != nil {
		return nil, err
	}
	return &CompileResult{Document: doc, Payload: payload}, nil
}

func (CompilerAPI) CompilePath(path string) (*CompileResult, error) {
	trimmed := strings.TrimSpace(path)
	if !strings.HasSuffix(trimmed, ".clermfile") {
		return nil, platform.New(platform.CodeInvalidArgument, "compile input must be a .clermfile")
	}
	doc, err := Compiler.LoadDocument(trimmed)
	if err != nil {
		return nil, err
	}
	payload, err := clermcfg.Encode(doc)
	if err != nil {
		return nil, err
	}
	return &CompileResult{
		InputPath: trimmed,
		Document:  doc,
		Payload:   payload,
	}, nil
}

func (CompilerAPI) CompileDocument(doc *schema.Document) ([]byte, error) {
	return clermcfg.Encode(doc)
}

func (CompilerAPI) WriteCompiledConfig(inputPath string, outputPath string) (*CompileResult, error) {
	result, err := Compiler.CompilePath(inputPath)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(outputPath) == "" {
		outputPath = replaceExt(strings.TrimSpace(inputPath), ".clermcfg")
	}
	if err := os.WriteFile(outputPath, result.Payload, 0o644); err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "write compiled config")
	}
	result.OutputPath = outputPath
	return result, nil
}

func (CompilerAPI) BuildRequest(doc *schema.Document, input BuildRequestInput) (*BuildRequestResult, error) {
	if doc == nil {
		return nil, platform.New(platform.CodeInvalidArgument, "schema document is required")
	}
	method, ok := doc.MethodByReference(strings.TrimSpace(input.MethodReference))
	if !ok {
		return nil, platform.New(platform.CodeNotFound, "method not found in schema")
	}
	allowed := relationSet(input.AllowedRelations)
	if len(allowed) > 0 {
		if _, ok := allowed[method.Reference.Relation]; !ok {
			return nil, platform.New(platform.CodeValidation, fmt.Sprintf("method %s is not allowed by current relations", method.Reference.Raw))
		}
	}
	payload := input.PayloadJSON
	if len(payload) == 0 {
		payload = []byte("{}")
	}
	request, err := clermreq.Build(method, payload)
	if err != nil {
		return nil, err
	}
	condition, ok := doc.RelationCondition(method.Reference.Relation)
	if !ok {
		return nil, platform.New(platform.CodeValidation, "method relation is not defined in schema")
	}
	token, encodedToken, err := resolveCapabilityInput(input)
	if err != nil {
		return nil, err
	}
	if requiresCapability(condition) && token == nil {
		return nil, platform.New(platform.CodeValidation, "capability token is required for this relation; obtain one from clerm_registry with `clerm token issue -registry ...`")
	}
	if token != nil {
		if err := validateCapabilityScope(doc, method, condition, token); err != nil {
			return nil, err
		}
		if err := request.SetCapabilityRaw(encodedToken); err != nil {
			return nil, err
		}
	}
	return &BuildRequestResult{
		Method:            method,
		RelationCondition: condition,
		Request:           request,
	}, nil
}

func (CompilerAPI) EncodeRequest(doc *schema.Document, input BuildRequestInput) (*EncodedRequest, error) {
	result, err := Compiler.BuildRequest(doc, input)
	if err != nil {
		return nil, err
	}
	payload, err := clermreq.Encode(result.Request)
	if err != nil {
		return nil, err
	}
	return &EncodedRequest{
		BuildRequestResult: *result,
		Payload:            payload,
	}, nil
}

func (CompilerAPI) Tools(doc *schema.Document, allowedRelations []string) ([]ToolDefinition, error) {
	if doc == nil {
		return nil, platform.New(platform.CodeInvalidArgument, "schema document is required")
	}
	allowed := relationSet(allowedRelations)
	methods := make([]schema.Method, 0, len(doc.Methods))
	for _, method := range doc.Methods {
		if len(allowed) > 0 {
			if _, ok := allowed[method.Reference.Relation]; !ok {
				continue
			}
		}
		methods = append(methods, method)
	}
	sort.Slice(methods, func(i, j int) bool { return methods[i].Reference.Raw < methods[j].Reference.Raw })
	tools := make([]ToolDefinition, 0, len(methods))
	for _, method := range methods {
		condition, _ := doc.RelationCondition(method.Reference.Relation)
		tools = append(tools, ToolDefinition{
			Name:          method.Reference.Method,
			Description:   fmt.Sprintf("Build a CLERM tool-call request for %s", method.Reference.Raw),
			Method:        method.Reference.Raw,
			Relation:      method.Reference.Relation,
			Condition:     condition,
			TokenRequired: requiresCapability(condition),
			InputSchema:   inputJSONSchema(method.InputArgs),
		})
	}
	return tools, nil
}

func (CompilerAPI) Benchmark(doc *schema.Document, input BenchmarkInput) (*BenchmarkReport, error) {
	if doc == nil {
		return nil, platform.New(platform.CodeInvalidArgument, "schema document is required")
	}
	method, ok := doc.MethodByReference(strings.TrimSpace(input.MethodReference))
	if !ok {
		return nil, platform.New(platform.CodeNotFound, "method not found in schema")
	}
	payload := input.PayloadJSON
	if len(payload) == 0 {
		payload = []byte("{}")
	}
	request, err := clermreq.Build(method, payload)
	if err != nil {
		return nil, err
	}
	cfgBytes, err := clermcfg.Encode(doc)
	if err != nil {
		return nil, err
	}
	reqBytes, err := clermreq.Encode(request)
	if err != nil {
		return nil, err
	}
	cfgJSON, err := jsonwire.MarshalConfig(doc, true)
	if err != nil {
		return nil, err
	}
	reqJSON, err := jsonwire.MarshalRequest(request)
	if err != nil {
		return nil, err
	}
	iterations := input.Iterations
	if iterations <= 0 {
		iterations = 1
	}
	return &BenchmarkReport{
		Iterations:      iterations,
		MethodologyNote: "These loop timings include per-iteration function-call and error-check overhead. Use `go test -bench` for precise microbenchmarks.",
		ConfigSize:      sizeComparison(len(cfgBytes), len(cfgJSON)),
		RequestSize:     sizeComparison(len(reqBytes), len(reqJSON)),
		ConfigTime: BenchmarkTiming{
			CLERMEncodeNS: measureNS(iterations, func() error { _, err := clermcfg.Encode(doc); return err }),
			CLERMDecodeNS: measureNS(iterations, func() error { _, err := clermcfg.Decode(cfgBytes); return err }),
			JSONEncodeNS:  measureNS(iterations, func() error { _, err := jsonwire.MarshalConfig(doc, true); return err }),
			JSONDecodeNS:  measureNS(iterations, func() error { _, err := jsonwire.UnmarshalConfig(cfgJSON); return err }),
		},
		RequestTime: BenchmarkTiming{
			CLERMEncodeNS: measureNS(iterations, func() error { _, err := clermreq.Encode(request); return err }),
			CLERMDecodeNS: measureNS(iterations, func() error { _, err := clermreq.Decode(reqBytes); return err }),
			JSONEncodeNS:  measureNS(iterations, func() error { _, err := jsonwire.MarshalRequest(request); return err }),
			JSONDecodeNS:  measureNS(iterations, func() error { _, err := jsonwire.UnmarshalRequest(reqJSON); return err }),
		},
	}, nil
}

func (CompilerAPI) InspectPath(path string, options InspectOptions) (*Inspection, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read inspect input")
	}
	return Compiler.InspectData(path, data, options)
}

func (CompilerAPI) InspectData(name string, data []byte, options InspectOptions) (*Inspection, error) {
	switch {
	case strings.HasSuffix(strings.TrimSpace(name), ".clermfile"):
		doc, err := schema.Parse(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		return &Inspection{
			Kind:     "clermfile",
			Document: inspectableDocument(doc, options.IncludeInternal),
		}, nil
	case clermcfg.IsEncoded(data):
		doc, err := clermcfg.Decode(data)
		if err != nil {
			return nil, err
		}
		return &Inspection{
			Kind:     "clermcfg",
			Document: inspectableDocument(doc, options.IncludeInternal),
		}, nil
	case clermreq.IsEncoded(data):
		request, err := clermreq.Decode(data)
		if err != nil {
			return nil, err
		}
		return &Inspection{
			Kind:    "clerm",
			Request: inspectableRequest(request),
		}, nil
	case clermresp.IsEncoded(data):
		response, err := clermresp.Decode(data)
		if err != nil {
			return nil, err
		}
		return &Inspection{
			Kind:     "clerm_response",
			Response: inspectableResponse(response),
		}, nil
	default:
		return nil, platform.New(platform.CodeValidation, "unsupported inspect input")
	}
}

func loadDocumentData(path string, data []byte) (*schema.Document, error) {
	switch {
	case strings.HasSuffix(strings.TrimSpace(path), ".clermfile"):
		return schema.Parse(bytes.NewReader(data))
	case clermcfg.IsEncoded(data):
		return clermcfg.Decode(data)
	default:
		return nil, platform.New(platform.CodeValidation, "unsupported schema input; expected .clermfile or .clermcfg")
	}
}

func resolveCapabilityInput(input BuildRequestInput) (*capability.Token, []byte, error) {
	present := 0
	if input.Capability != nil {
		present++
	}
	if strings.TrimSpace(input.CapabilityText) != "" {
		present++
	}
	if len(input.CapabilityRaw) > 0 {
		present++
	}
	if present > 1 {
		return nil, nil, platform.New(platform.CodeInvalidArgument, "use only one of Capability, CapabilityText, or CapabilityRaw")
	}
	switch {
	case input.Capability != nil:
		encoded, err := capability.Encode(input.Capability)
		if err != nil {
			return nil, nil, err
		}
		return input.Capability, encoded, nil
	case strings.TrimSpace(input.CapabilityText) != "":
		token, err := capability.DecodeText(input.CapabilityText)
		if err != nil {
			return nil, nil, err
		}
		encoded, err := capability.Encode(token)
		if err != nil {
			return nil, nil, err
		}
		return token, encoded, nil
	case len(input.CapabilityRaw) > 0:
		token, err := capability.Decode(input.CapabilityRaw)
		if err != nil {
			return nil, nil, err
		}
		return token, append([]byte(nil), input.CapabilityRaw...), nil
	default:
		return nil, nil, nil
	}
}

func validateCapabilityScope(doc *schema.Document, method schema.Method, condition string, token *capability.Token) error {
	if token.Schema != doc.Name {
		return platform.New(platform.CodeValidation, "capability token schema does not match request schema")
	}
	if token.SchemaHash != schema.CachedPublicFingerprint(doc) {
		return platform.New(platform.CodeValidation, "capability token schema fingerprint does not match request schema")
	}
	if token.Condition != condition {
		return platform.New(platform.CodeValidation, "capability token condition does not match method relation")
	}
	if !token.AllowsMethod(method.Reference.Raw, method.Reference.Relation) {
		return platform.New(platform.CodeValidation, "capability token does not allow this method")
	}
	return nil
}

func requiresCapability(condition string) bool {
	return strings.TrimSpace(strings.ToLower(condition)) != "any.protected"
}

func inspectableDocument(doc *schema.Document, includeInternal bool) *InspectableDocument {
	if doc == nil {
		return nil
	}
	result := &InspectableDocument{
		Name:          doc.Name,
		RelationsName: doc.RelationsName,
		Metadata:      doc.Metadata,
		Services:      append([]schema.ServiceRef(nil), doc.Services...),
		Methods:       append([]schema.Method(nil), doc.Methods...),
		Relations:     append([]schema.RelationRule(nil), doc.Relations...),
	}
	if includeInternal {
		result.Route = doc.Route
	}
	return result
}

func inspectableRequest(request *clermreq.Request) *InspectableRequest {
	if request == nil {
		return nil
	}
	result := &InspectableRequest{
		Method:    request.Method,
		Arguments: append([]clermreq.Argument(nil), request.Arguments...),
	}
	if len(request.CapabilityRaw) == 0 {
		return result
	}
	token, err := capability.Decode(request.CapabilityRaw)
	if err != nil {
		result.CapabilityError = err.Error()
		return result
	}
	view := token.InspectView()
	result.Capability = &view
	return result
}

func inspectableResponse(response *clermresp.Response) *InspectableResponse {
	if response == nil {
		return nil
	}
	result := &InspectableResponse{
		Method:  response.Method,
		Outputs: append([]clermresp.Value(nil), response.Outputs...),
	}
	if response.Error.Code != "" || response.Error.Message != "" {
		errBody := response.Error
		result.Error = &errBody
		result.Outputs = nil
		return result
	}
	values, err := response.AsMap()
	if err == nil {
		result.DecodedOutputs = values
	}
	return result
}

func relationSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		set[trimmed] = struct{}{}
	}
	return set
}

func replaceExt(path string, ext string) string {
	base := strings.TrimSuffix(path, filepath.Ext(path))
	return base + ext
}

func inputJSONSchema(params []schema.Parameter) map[string]any {
	properties := map[string]any{}
	required := make([]string, 0, len(params))
	for _, param := range params {
		properties[param.Name] = jsonSchemaType(param.Type)
		required = append(required, param.Name)
	}
	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
}

func jsonSchemaType(argType schema.ArgType) map[string]any {
	switch argType {
	case schema.ArgString:
		return map[string]any{"type": "string"}
	case schema.ArgDecimal:
		return map[string]any{"type": "number"}
	case schema.ArgUUID:
		return map[string]any{"type": "string", "format": "uuid"}
	case schema.ArgArray:
		return map[string]any{"type": "array"}
	case schema.ArgTimestamp:
		return map[string]any{"type": "string", "format": "date-time"}
	case schema.ArgInt:
		return map[string]any{"type": "integer"}
	case schema.ArgBool:
		return map[string]any{"type": "boolean"}
	default:
		return map[string]any{"type": "string"}
	}
}

func sizeComparison(clermBytes int, jsonBytes int) BenchmarkSize {
	saved := jsonBytes - clermBytes
	savedPercent := 0.0
	if jsonBytes > 0 {
		savedPercent = (float64(saved) / float64(jsonBytes)) * 100
	}
	return BenchmarkSize{
		CLERMBytes:   clermBytes,
		JSONBytes:    jsonBytes,
		SavedBytes:   saved,
		SavedPercent: savedPercent,
	}
}

func measureNS(iterations int, fn func() error) float64 {
	if iterations <= 0 {
		iterations = 1
	}
	start := time.Now()
	for i := 0; i < iterations; i++ {
		if err := fn(); err != nil {
			return -1
		}
	}
	return float64(time.Since(start).Nanoseconds()) / float64(iterations)
}
