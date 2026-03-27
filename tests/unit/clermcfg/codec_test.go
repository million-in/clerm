package clermcfg_test

import (
	"strings"
	"testing"

	"github.com/million-in/clerm/clermcfg"
	"github.com/million-in/clerm/schema"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	doc, err := schema.Parse(strings.NewReader(`
schema @general.avail.mandene
  @metadata:
    description: Healthcare provider search
    tags: healthcare, providers
    display_name: Provider Search
    category: healthcare
  @route: https://resolver.health.example/clerm
  service: @global.healthcare.search_providers.v1

method @global.healthcare.search_providers.v1
  @exec: async.pool
  @args_input: 3
    decl_args: specialty.STRING, latitude.DECIMAL, longitude.DECIMAL
  @args_output: 2
    decl_args: request_id.UUID, providers.ARRAY
    decl_format: json

relations @general.mandene
  @global: any.protected
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	encoded, err := clermcfg.Encode(doc)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	decoded, err := clermcfg.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if decoded.Name != doc.Name {
		t.Fatalf("decoded schema name mismatch: got %s want %s", decoded.Name, doc.Name)
	}
	if decoded.Route != doc.Route {
		t.Fatalf("decoded route mismatch: got %s want %s", decoded.Route, doc.Route)
	}
	if decoded.Metadata.Description != doc.Metadata.Description {
		t.Fatalf("decoded metadata description mismatch: got %s want %s", decoded.Metadata.Description, doc.Metadata.Description)
	}
	if decoded.Metadata.DisplayName != doc.Metadata.DisplayName {
		t.Fatalf("decoded metadata display name mismatch: got %s want %s", decoded.Metadata.DisplayName, doc.Metadata.DisplayName)
	}
	if decoded.Metadata.Category != doc.Metadata.Category {
		t.Fatalf("decoded metadata category mismatch: got %s want %s", decoded.Metadata.Category, doc.Metadata.Category)
	}
	if len(decoded.Metadata.Tags) != len(doc.Metadata.Tags) {
		t.Fatalf("decoded metadata tags mismatch: got %d want %d", len(decoded.Metadata.Tags), len(doc.Metadata.Tags))
	}
	if len(decoded.Methods) != 1 {
		t.Fatalf("expected 1 method, got %d", len(decoded.Methods))
	}
	if decoded.Methods[0].Reference.Raw != "@global.healthcare.search_providers.v1" {
		t.Fatalf("unexpected decoded method: %s", decoded.Methods[0].Reference.Raw)
	}
}

func TestDecodeOwnsDecodedStrings(t *testing.T) {
	doc := mustConfigDocument(t)
	encoded, err := clermcfg.Encode(doc)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	decoded, err := clermcfg.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	for i := range encoded {
		encoded[i] = 'x'
	}

	if decoded.Name != doc.Name {
		t.Fatalf("decoded name corrupted after input mutation: got %q want %q", decoded.Name, doc.Name)
	}
	if decoded.Methods[0].Reference.Raw != doc.Methods[0].Reference.Raw {
		t.Fatalf("decoded method corrupted after input mutation: got %q want %q", decoded.Methods[0].Reference.Raw, doc.Methods[0].Reference.Raw)
	}
}

func TestDecoderDecodeCodecReturnsIndependentDocuments(t *testing.T) {
	first := mustConfigDocument(t)
	second, err := schema.Parse(strings.NewReader(strings.ReplaceAll(strings.ReplaceAll(configSchemaSource(), "@global.healthcare.search_providers.v1", "@global.healthcare.list_providers.v2"), "Provider Search", "Provider Directory")))
	if err != nil {
		t.Fatalf("Parse(second) error = %v", err)
	}
	firstEncoded, err := clermcfg.Encode(first)
	if err != nil {
		t.Fatalf("Encode(first) error = %v", err)
	}
	secondEncoded, err := clermcfg.Encode(second)
	if err != nil {
		t.Fatalf("Encode(second) error = %v", err)
	}

	var decoder clermcfg.Decoder
	firstDecoded, err := decoder.DecodeCodec(firstEncoded)
	if err != nil {
		t.Fatalf("DecodeCodec(first) error = %v", err)
	}
	secondDecoded, err := decoder.DecodeCodec(secondEncoded)
	if err != nil {
		t.Fatalf("DecodeCodec(second) error = %v", err)
	}

	if firstDecoded.Methods[0].Reference.Raw != first.Methods[0].Reference.Raw {
		t.Fatalf("first decoded method mutated after second decode: got %q want %q", firstDecoded.Methods[0].Reference.Raw, first.Methods[0].Reference.Raw)
	}
	if secondDecoded.Methods[0].Reference.Raw != second.Methods[0].Reference.Raw {
		t.Fatalf("unexpected second decoded method: got %q want %q", secondDecoded.Methods[0].Reference.Raw, second.Methods[0].Reference.Raw)
	}
}

func TestEncodeRejectsReservedDeclaredCount(t *testing.T) {
	doc := mustConfigDocument(t)
	doc.Methods[0].InputCount = 65535

	if _, err := clermcfg.Encode(doc); err == nil || !strings.Contains(err.Error(), "reserved for inferred counts") {
		t.Fatalf("expected reserved-count error, got %v", err)
	}
}

func TestEncodeRejectsInvalidNegativeDeclaredCount(t *testing.T) {
	doc := mustConfigDocument(t)
	doc.Methods[0].OutputCount = -2

	if _, err := clermcfg.Encode(doc); err == nil || !strings.Contains(err.Error(), "must be -1 or greater") {
		t.Fatalf("expected invalid negative-count error, got %v", err)
	}
}

func TestDecodeLegacyVersion1Config(t *testing.T) {
	doc := mustConfigDocument(t)

	decoded, err := clermcfg.Decode(encodeLegacyConfigV1(doc))
	if err != nil {
		t.Fatalf("Decode(v1) error = %v", err)
	}
	if decoded.Name != doc.Name {
		t.Fatalf("decoded name mismatch: got %q want %q", decoded.Name, doc.Name)
	}
	if decoded.Route != doc.Route {
		t.Fatalf("decoded route mismatch: got %q want %q", decoded.Route, doc.Route)
	}
	if decoded.Metadata.Description != "" || decoded.Metadata.DisplayName != "" || decoded.Metadata.Category != "" || len(decoded.Metadata.Tags) != 0 {
		t.Fatalf("expected empty metadata for v1 decode, got %#v", decoded.Metadata)
	}
	if decoded.Methods[0].Reference.Raw != doc.Methods[0].Reference.Raw {
		t.Fatalf("decoded method mismatch: got %q want %q", decoded.Methods[0].Reference.Raw, doc.Methods[0].Reference.Raw)
	}
}

func TestDecodeCodecRejectsImpossibleServiceCount(t *testing.T) {
	payload := append([]byte{'C', 'L', 'R', 'C', 0, 2},
		0, 0, // schema name
		0, 0, // relations name
		0, 0, // metadata description
		0, 0, // metadata display name
		0, 0, // metadata category
		0, 0, // metadata tags count
		0, 0, // route
		0xff, 0xff, // service count
	)

	if _, err := clermcfg.DecodeCodec(payload); err == nil || !strings.Contains(err.Error(), "service count exceeds remaining payload") {
		t.Fatalf("expected service count guard, got %v", err)
	}
}

func mustConfigDocument(t *testing.T) *schema.Document {
	t.Helper()
	doc, err := schema.Parse(strings.NewReader(configSchemaSource()))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	return doc
}

func configSchemaSource() string {
	return `
schema @general.avail.mandene
  @metadata:
    description: Healthcare provider search
    tags: healthcare, providers
    display_name: Provider Search
    category: healthcare
  @route: https://resolver.health.example/clerm
  service: @global.healthcare.search_providers.v1

method @global.healthcare.search_providers.v1
  @exec: async.pool
  @args_input: 3
    decl_args: specialty.STRING, latitude.DECIMAL, longitude.DECIMAL
  @args_output: 2
    decl_args: request_id.UUID, providers.ARRAY
    decl_format: json

relations @general.mandene
  @global: any.protected
`
}

func encodeLegacyConfigV1(doc *schema.Document) []byte {
	buf := append([]byte(nil), 'C', 'L', 'R', 'C', 0, 1)
	buf = appendLegacyString(buf, doc.Name)
	buf = appendLegacyString(buf, doc.RelationsName)
	buf = appendLegacyString(buf, doc.Route)
	buf = appendLegacyUint16(buf, uint16(len(doc.Services)))
	for _, service := range doc.Services {
		buf = appendLegacyString(buf, service.Raw)
	}
	buf = appendLegacyUint16(buf, uint16(len(doc.Methods)))
	for _, method := range doc.Methods {
		buf = appendLegacyString(buf, method.Reference.Raw)
		buf = append(buf, byte(method.Execution))
		buf = appendLegacyCount(buf, method.InputCount)
		buf = appendLegacyParameters(buf, method.InputArgs)
		buf = appendLegacyCount(buf, method.OutputCount)
		buf = appendLegacyParameters(buf, method.OutputArgs)
		buf = append(buf, byte(method.OutputFormat))
	}
	buf = appendLegacyUint16(buf, uint16(len(doc.Relations)))
	for _, relation := range doc.Relations {
		buf = appendLegacyString(buf, relation.Name)
		buf = appendLegacyString(buf, relation.Condition)
	}
	return buf
}

func appendLegacyParameters(dst []byte, params []schema.Parameter) []byte {
	dst = appendLegacyUint16(dst, uint16(len(params)))
	for _, param := range params {
		dst = appendLegacyString(dst, param.Name)
		dst = append(dst, byte(param.Type))
	}
	return dst
}

func appendLegacyCount(dst []byte, count int) []byte {
	if count < 0 {
		return appendLegacyUint16(dst, 0xFFFF)
	}
	return appendLegacyUint16(dst, uint16(count))
}

func appendLegacyString(dst []byte, value string) []byte {
	dst = appendLegacyUint16(dst, uint16(len(value)))
	return append(dst, value...)
}

func appendLegacyUint16(dst []byte, value uint16) []byte {
	return append(dst, byte(value>>8), byte(value))
}
