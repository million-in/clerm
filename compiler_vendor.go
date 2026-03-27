package clerm

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/million-in/clerm/capability"
	"github.com/million-in/clerm/platform"
	"github.com/million-in/clerm/schema"
)

type ToolCallOptions struct {
	AllowedRelations []string
	Capability       *capability.Token
	CapabilityText   string
	CapabilityRaw    []byte
}

type ToolBinding struct {
	Name          string `json:"name"`
	Method        string `json:"method"`
	Relation      string `json:"relation"`
	Condition     string `json:"condition"`
	TokenRequired bool   `json:"token_required"`
}

type OpenAITool struct {
	Type     string                   `json:"type"`
	Function OpenAIFunctionDefinition `json:"function"`
}

type OpenAIFunctionDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
	Strict      bool           `json:"strict,omitempty"`
}

type OpenAIToolCall struct {
	ID       string             `json:"id,omitempty"`
	Type     string             `json:"type,omitempty"`
	Function OpenAIFunctionCall `json:"function"`
}

type OpenAIFunctionCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type AnthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema"`
}

type AnthropicToolUse struct {
	Type  string         `json:"type,omitempty"`
	ID    string         `json:"id,omitempty"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input,omitempty"`
}

type toolDescriptor struct {
	Binding ToolBinding
	Method  schema.Method
}

type compilerDocumentIndex struct {
	methods            map[string]schema.Method
	relationConditions map[string]string
	descriptors        []toolDescriptor
	descriptorByName   map[string]toolDescriptor
}

type compilerDocumentIndexCache struct {
	mu     sync.RWMutex
	values map[*schema.Document]*compilerDocumentIndex
	order  []*schema.Document
	limit  int
}

const defaultCompilerDocumentIndexCacheSize = 128

var defaultCompilerDocumentIndexCache = newCompilerDocumentIndexCache(defaultCompilerDocumentIndexCacheSize)

func newCompilerDocumentIndexCache(limit int) *compilerDocumentIndexCache {
	if limit <= 0 {
		limit = defaultCompilerDocumentIndexCacheSize
	}
	return &compilerDocumentIndexCache{
		values: make(map[*schema.Document]*compilerDocumentIndex, limit),
		order:  make([]*schema.Document, 0, limit),
		limit:  limit,
	}
}

func (CompilerAPI) ToolBindings(doc *schema.Document, allowedRelations []string) ([]ToolBinding, error) {
	descriptors, err := buildToolDescriptors(doc, allowedRelations)
	if err != nil {
		return nil, err
	}
	bindings := make([]ToolBinding, len(descriptors))
	for i, descriptor := range descriptors {
		bindings[i] = descriptor.Binding
	}
	return bindings, nil
}

func (CompilerAPI) OpenAITools(doc *schema.Document, allowedRelations []string) ([]OpenAITool, error) {
	descriptors, err := buildToolDescriptors(doc, allowedRelations)
	if err != nil {
		return nil, err
	}
	tools := make([]OpenAITool, len(descriptors))
	for i, descriptor := range descriptors {
		tools[i] = OpenAITool{
			Type: "function",
			Function: OpenAIFunctionDefinition{
				Name:        descriptor.Binding.Name,
				Description: vendorToolDescription(descriptor.Binding),
				Parameters:  inputJSONSchema(descriptor.Method.InputArgs),
			},
		}
	}
	return tools, nil
}

func (CompilerAPI) AnthropicTools(doc *schema.Document, allowedRelations []string) ([]AnthropicTool, error) {
	descriptors, err := buildToolDescriptors(doc, allowedRelations)
	if err != nil {
		return nil, err
	}
	tools := make([]AnthropicTool, len(descriptors))
	for i, descriptor := range descriptors {
		tools[i] = AnthropicTool{
			Name:        descriptor.Binding.Name,
			Description: vendorToolDescription(descriptor.Binding),
			InputSchema: inputJSONSchema(descriptor.Method.InputArgs),
		}
	}
	return tools, nil
}

func (CompilerAPI) EncodeOpenAIToolCall(doc *schema.Document, call OpenAIToolCall, options ToolCallOptions) (*EncodedRequest, error) {
	name := strings.TrimSpace(call.Function.Name)
	if name == "" {
		return nil, platform.New(platform.CodeInvalidArgument, "openai tool call function name is required")
	}
	payload, err := normalizeOpenAIArguments(call.Function.Arguments)
	if err != nil {
		return nil, err
	}
	return Compiler.encodeVendorToolCall(doc, name, payload, options)
}

func (CompilerAPI) EncodeOpenAIToolCallJSON(doc *schema.Document, raw []byte, options ToolCallOptions) (*EncodedRequest, error) {
	type openAIToolCallEnvelope struct {
		Type      string          `json:"type,omitempty"`
		ID        string          `json:"id,omitempty"`
		Name      string          `json:"name,omitempty"`
		Arguments json.RawMessage `json:"arguments,omitempty"`
		Function  *struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments,omitempty"`
		} `json:"function,omitempty"`
	}
	var envelope openAIToolCallEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, platform.Wrap(platform.CodeParse, err, "decode openai tool call")
	}
	call := OpenAIToolCall{
		ID:   strings.TrimSpace(envelope.ID),
		Type: strings.TrimSpace(envelope.Type),
	}
	if envelope.Function != nil {
		call.Function = OpenAIFunctionCall{
			Name:      envelope.Function.Name,
			Arguments: envelope.Function.Arguments,
		}
	} else {
		call.Function = OpenAIFunctionCall{
			Name:      envelope.Name,
			Arguments: envelope.Arguments,
		}
	}
	return Compiler.EncodeOpenAIToolCall(doc, call, options)
}

func (CompilerAPI) EncodeAnthropicToolUse(doc *schema.Document, call AnthropicToolUse, options ToolCallOptions) (*EncodedRequest, error) {
	name := strings.TrimSpace(call.Name)
	if name == "" {
		return nil, platform.New(platform.CodeInvalidArgument, "anthropic tool name is required")
	}
	payload, err := normalizeAnthropicInput(call.Input)
	if err != nil {
		return nil, err
	}
	return Compiler.encodeVendorToolCall(doc, name, payload, options)
}

func (CompilerAPI) EncodeAnthropicToolUseJSON(doc *schema.Document, raw []byte, options ToolCallOptions) (*EncodedRequest, error) {
	type anthropicToolUseEnvelope struct {
		Type  string          `json:"type,omitempty"`
		ID    string          `json:"id,omitempty"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input,omitempty"`
	}
	var envelope anthropicToolUseEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, platform.Wrap(platform.CodeParse, err, "decode anthropic tool use")
	}
	name := strings.TrimSpace(envelope.Name)
	if name == "" {
		return nil, platform.New(platform.CodeInvalidArgument, "anthropic tool name is required")
	}
	payload, err := normalizeJSONPayload(envelope.Input, "anthropic tool input")
	if err != nil {
		return nil, err
	}
	return Compiler.encodeVendorToolCall(doc, name, payload, options)
}

func (CompilerAPI) encodeVendorToolCall(doc *schema.Document, toolName string, payload []byte, options ToolCallOptions) (*EncodedRequest, error) {
	descriptor, err := lookupToolDescriptor(doc, toolName)
	if err != nil {
		return nil, err
	}
	return Compiler.EncodeRequest(doc, BuildRequestInput{
		MethodReference:  descriptor.Method.Reference.Raw,
		AllowedRelations: options.AllowedRelations,
		PayloadJSON:      payload,
		Capability:       options.Capability,
		CapabilityText:   options.CapabilityText,
		CapabilityRaw:    options.CapabilityRaw,
	})
}

func buildToolDescriptors(doc *schema.Document, allowedRelations []string) ([]toolDescriptor, error) {
	index, err := compilerIndex(doc)
	if err != nil {
		return nil, err
	}
	if len(allowedRelations) == 0 {
		return index.descriptors, nil
	}
	descriptors := make([]toolDescriptor, 0, len(index.descriptors))
	for _, descriptor := range index.descriptors {
		if relationAllowed(allowedRelations, descriptor.Binding.Relation) {
			descriptors = append(descriptors, descriptor)
		}
	}
	return descriptors, nil
}

func lookupToolDescriptor(doc *schema.Document, toolName string) (toolDescriptor, error) {
	index, err := compilerIndex(doc)
	if err != nil {
		return toolDescriptor{}, err
	}
	trimmed := strings.TrimSpace(toolName)
	if descriptor, ok := index.descriptorByName[trimmed]; ok {
		return descriptor, nil
	}
	return toolDescriptor{}, platform.New(platform.CodeNotFound, fmt.Sprintf("tool %s is not defined in schema", trimmed))
}

func vendorToolDescription(binding ToolBinding) string {
	description := fmt.Sprintf("Build a CLERM request for %s over relation %s.", binding.Method, binding.Relation)
	if strings.TrimSpace(binding.Condition) != "" {
		description += " Condition: " + binding.Condition + "."
	}
	if binding.TokenRequired {
		description += " Capability token required."
	}
	return description
}

func normalizeOpenAIArguments(raw json.RawMessage) ([]byte, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return []byte("{}"), nil
	}
	if bytes.Equal(trimmed, []byte("null")) {
		return []byte("{}"), nil
	}
	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return nil, platform.Wrap(platform.CodeParse, err, "decode openai function arguments")
		}
		return normalizeJSONPayload([]byte(text), "openai function arguments")
	}
	return normalizeJSONPayload(trimmed, "openai function arguments")
}

func normalizeAnthropicInput(input map[string]any) ([]byte, error) {
	if input == nil {
		return []byte("{}"), nil
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, platform.Wrap(platform.CodeParse, err, "encode anthropic tool input")
	}
	return normalizeJSONPayload(raw, "anthropic tool input")
}

func normalizeJSONPayload(raw []byte, label string) ([]byte, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return []byte("{}"), nil
	}
	if !json.Valid(trimmed) {
		return nil, platform.New(platform.CodeParse, label+" must be valid JSON")
	}
	return trimmed, nil
}

func toolNameForReference(reference string) string {
	base := sanitizeToolName(reference)
	if len(base) <= 64 {
		return base
	}
	return hashedToolName(reference)
}

func hashedToolName(reference string) string {
	base := sanitizeToolName(reference)
	sum := sha256.Sum256([]byte(strings.TrimSpace(reference)))
	suffix := hex.EncodeToString(sum[:4])
	maxBaseLen := 64 - 1 - len(suffix)
	if maxBaseLen < 1 {
		maxBaseLen = 1
	}
	if len(base) > maxBaseLen {
		base = strings.Trim(base[:maxBaseLen], "_")
		if base == "" {
			base = "clerm"
		}
	}
	return base + "_" + suffix
}

func sanitizeToolName(reference string) string {
	trimmed := strings.TrimSpace(strings.TrimPrefix(strings.ToLower(reference), "@"))
	var builder strings.Builder
	builder.Grow(len(trimmed) + len("clerm_"))
	builder.WriteString("clerm_")
	lastUnderscore := true
	for _, r := range trimmed {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			builder.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				builder.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	name := strings.Trim(builder.String(), "_")
	if name == "" {
		return "clerm"
	}
	if len(name) == 0 || !startsWithLetter(name) {
		return "clerm_" + name
	}
	return name
}

func startsWithLetter(value string) bool {
	for _, r := range value {
		return unicode.IsLetter(r)
	}
	return false
}

func extractJSONValuePath(value any, path string) (any, error) {
	current := value
	for _, segment := range strings.Split(path, ".") {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		switch typed := current.(type) {
		case map[string]any:
			next, ok := typed[segment]
			if !ok {
				return nil, platform.New(platform.CodeNotFound, fmt.Sprintf("json path %q not found", path))
			}
			current = next
		case []any:
			index, err := strconv.Atoi(segment)
			if err != nil {
				return nil, platform.New(platform.CodeValidation, fmt.Sprintf("json path segment %q must be an array index", segment))
			}
			if index < 0 || index >= len(typed) {
				return nil, platform.New(platform.CodeNotFound, fmt.Sprintf("json path %q not found", path))
			}
			current = typed[index]
		default:
			return nil, platform.New(platform.CodeValidation, fmt.Sprintf("json path %q is not traversable", path))
		}
	}
	return current, nil
}

func compilerIndex(doc *schema.Document) (*compilerDocumentIndex, error) {
	if doc == nil {
		return nil, platform.New(platform.CodeInvalidArgument, "schema document is required")
	}
	if cached := defaultCompilerDocumentIndexCache.get(doc); cached != nil {
		return cached, nil
	}
	index := buildCompilerDocumentIndex(doc)
	return defaultCompilerDocumentIndexCache.getOrStore(doc, index), nil
}

func buildCompilerDocumentIndex(doc *schema.Document) *compilerDocumentIndex {
	methods := make(map[string]schema.Method, len(doc.Methods))
	relationConditions := make(map[string]string, len(doc.Relations))
	for _, relation := range doc.Relations {
		relationConditions[relation.Name] = relation.Condition
	}
	sortedMethods := append([]schema.Method(nil), doc.Methods...)
	sort.Slice(sortedMethods, func(i, j int) bool { return sortedMethods[i].Reference.Raw < sortedMethods[j].Reference.Raw })
	descriptors := make([]toolDescriptor, 0, len(sortedMethods))
	descriptorByName := make(map[string]toolDescriptor, len(sortedMethods))
	usedNames := make(map[string]string, len(sortedMethods))
	for _, method := range sortedMethods {
		methods[method.Reference.Raw] = method
		condition := relationConditions[method.Reference.Relation]
		name := toolNameForReference(method.Reference.Raw)
		if existing, ok := usedNames[name]; ok && existing != method.Reference.Raw {
			name = hashedToolName(method.Reference.Raw)
		}
		usedNames[name] = method.Reference.Raw
		descriptor := toolDescriptor{
			Method: method,
			Binding: ToolBinding{
				Name:          name,
				Method:        method.Reference.Raw,
				Relation:      method.Reference.Relation,
				Condition:     condition,
				TokenRequired: requiresCapability(condition),
			},
		}
		descriptors = append(descriptors, descriptor)
		descriptorByName[name] = descriptor
	}
	return &compilerDocumentIndex{
		methods:            methods,
		relationConditions: relationConditions,
		descriptors:        descriptors,
		descriptorByName:   descriptorByName,
	}
}

func (c *compilerDocumentIndexCache) getOrStore(doc *schema.Document, built *compilerDocumentIndex) *compilerDocumentIndex {
	c.mu.RLock()
	if cached, ok := c.values[doc]; ok {
		c.mu.RUnlock()
		return cached
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	if cached, ok := c.values[doc]; ok {
		return cached
	}
	if len(c.order) >= c.limit {
		evicted := c.order[0]
		c.order = c.order[1:]
		delete(c.values, evicted)
	}
	c.values[doc] = built
	c.order = append(c.order, doc)
	return built
}

func (c *compilerDocumentIndexCache) get(doc *schema.Document) *compilerDocumentIndex {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.values[doc]
}
