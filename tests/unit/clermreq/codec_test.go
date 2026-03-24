package clermreq_test

import (
	"strings"
	"testing"
	"time"

	"github.com/million-in/clerm/capability"
	"github.com/million-in/clerm/clermreq"
	"github.com/million-in/clerm/schema"
)

func TestBuildEncodeDecodeRoundTrip(t *testing.T) {
	doc, err := schema.Parse(strings.NewReader(`
schema @general.avail.mandene
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
	method, _ := doc.MethodByReference("@global.healthcare.search_providers.v1")

	request, err := clermreq.Build(method, []byte(`{"specialty":"cardiology","latitude":40.7,"longitude":-73.9}`))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	encoded, err := clermreq.Encode(request)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	decoded, err := clermreq.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if err := decoded.ValidateAgainst(method); err != nil {
		t.Fatalf("ValidateAgainst() error = %v", err)
	}
	values, err := decoded.AsMap()
	if err != nil {
		t.Fatalf("AsMap() error = %v", err)
	}
	if values["specialty"] != "cardiology" {
		t.Fatalf("unexpected specialty value: %#v", values["specialty"])
	}
}

func TestEncodeDecodeRoundTripWithCapability(t *testing.T) {
	doc, err := schema.Parse(strings.NewReader(`
schema @general.avail.mandene
  @route: https://resolver.health.example/clerm
  service: @verified.healthcare.book_visit.v1

method @verified.healthcare.book_visit.v1
  @exec: sync
  @args_input: 2
    decl_args: provider_id.STRING, user_token.STRING
  @args_output: 2
    decl_args: order_id.STRING, status.STRING
    decl_format: json

relations @general.mandene
  @verified: auth.required
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	method, _ := doc.MethodByReference("@verified.healthcare.book_visit.v1")
	request, err := clermreq.Build(method, []byte(`{"provider_id":"abc123","user_token":"tok_123"}`))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	_, privateKey, err := capability.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair() error = %v", err)
	}
	now := time.Unix(1711000000, 0).UTC()
	token, err := capability.Issue(capability.IssueOptions{
		KeyID:      "default",
		Issuer:     "registry",
		Subject:    "partner-123",
		Schema:     doc.Name,
		SchemaHash: doc.PublicFingerprint(),
		Relation:   method.Reference.Relation,
		Condition:  "auth.required",
		Methods:    []string{method.Reference.Raw},
		IssuedAt:   now,
		NotBefore:  now,
		ExpiresAt:  now.Add(5 * time.Minute),
	}, privateKey)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	encodedToken, err := capability.Encode(token)
	if err != nil {
		t.Fatalf("Encode(token) error = %v", err)
	}
	if err := request.SetCapabilityRaw(encodedToken); err != nil {
		t.Fatalf("SetCapabilityRaw() error = %v", err)
	}
	encoded, err := clermreq.Encode(request)
	if err != nil {
		t.Fatalf("Encode(request) error = %v", err)
	}
	decoded, err := clermreq.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(decoded.CapabilityRaw) == 0 {
		t.Fatal("expected capability token to round-trip")
	}
	if err := decoded.ValidateAgainst(method); err != nil {
		t.Fatalf("ValidateAgainst() error = %v", err)
	}
}

func TestBuildRejectsUnknownArgument(t *testing.T) {
	doc, err := schema.Parse(strings.NewReader(`
schema @general.avail.mandene
  @route: https://resolver.health.example/clerm
  service: @global.healthcare.search_providers.v1

method @global.healthcare.search_providers.v1
  @exec: sync
  @args_input: 1
    decl_args: specialty.STRING
  @args_output: 1
    decl_args: providers.ARRAY
    decl_format: json

relations @general.mandene
  @global: any.protected
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	method, _ := doc.MethodByReference("@global.healthcare.search_providers.v1")

	_, err = clermreq.Build(method, []byte(`{"specialty":"cardiology","extra":"nope"}`))
	if err == nil {
		t.Fatal("expected unknown argument error")
	}
	if !strings.Contains(err.Error(), "unknown request argument values") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDecodeOwnsDecodedRequestData(t *testing.T) {
	doc, err := schema.Parse(strings.NewReader(`
schema @general.avail.mandene
  @route: https://resolver.health.example/clerm
  service: @verified.healthcare.book_visit.v1

method @verified.healthcare.book_visit.v1
  @exec: sync
  @args_input: 2
    decl_args: provider_id.STRING, user_token.STRING
  @args_output: 2
    decl_args: order_id.STRING, status.STRING
    decl_format: json

relations @general.mandene
  @verified: auth.required
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	method, _ := doc.MethodByReference("@verified.healthcare.book_visit.v1")
	request, err := clermreq.Build(method, []byte(`{"provider_id":"abc123","user_token":"tok_123"}`))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	request.CapabilityRaw = []byte("cap-token")
	encoded, err := clermreq.Encode(request)
	if err == nil {
		t.Fatal("expected invalid capability token error")
	}

	_, privateKey, err := capability.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair() error = %v", err)
	}
	now := time.Unix(1711000000, 0).UTC()
	token, err := capability.Issue(capability.IssueOptions{
		KeyID:      "default",
		Issuer:     "registry",
		Subject:    "partner-123",
		Schema:     doc.Name,
		SchemaHash: doc.PublicFingerprint(),
		Relation:   method.Reference.Relation,
		Condition:  "auth.required",
		Methods:    []string{method.Reference.Raw},
		IssuedAt:   now,
		NotBefore:  now,
		ExpiresAt:  now.Add(5 * time.Minute),
	}, privateKey)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	encodedToken, err := capability.Encode(token)
	if err != nil {
		t.Fatalf("Encode(token) error = %v", err)
	}
	if err := request.SetCapabilityRaw(encodedToken); err != nil {
		t.Fatalf("SetCapabilityRaw() error = %v", err)
	}
	encoded, err = clermreq.Encode(request)
	if err != nil {
		t.Fatalf("Encode(request) error = %v", err)
	}
	decoded, err := clermreq.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	wantMethod := decoded.Method
	wantName := decoded.Arguments[0].Name
	wantRaw := string(decoded.Arguments[0].Raw)
	wantCapability := string(decoded.CapabilityRaw)
	for i := range encoded {
		encoded[i] = 'x'
	}
	if decoded.Method != wantMethod || decoded.Arguments[0].Name != wantName {
		t.Fatalf("decoded strings corrupted after input mutation: method=%q arg=%q", decoded.Method, decoded.Arguments[0].Name)
	}
	if string(decoded.Arguments[0].Raw) != wantRaw {
		t.Fatalf("decoded raw argument corrupted after input mutation: got %q want %q", string(decoded.Arguments[0].Raw), wantRaw)
	}
	if string(decoded.CapabilityRaw) != wantCapability {
		t.Fatalf("decoded capability corrupted after input mutation: got %q want %q", string(decoded.CapabilityRaw), wantCapability)
	}
}

func TestSetCapabilityRawRejectsInvalidToken(t *testing.T) {
	request := &clermreq.Request{Method: "@global.healthcare.search_providers.v1"}

	if err := request.SetCapabilityRaw([]byte("not-a-token")); err == nil || !strings.Contains(err.Error(), "invalid capability token on request") {
		t.Fatalf("expected invalid capability token error, got %v", err)
	}
}

func TestSetCapabilityRawCopiesValidatedToken(t *testing.T) {
	doc := mustRequestDocument(t)
	method, _ := doc.MethodByReference("@verified.healthcare.book_visit.v1")
	request, err := clermreq.Build(method, []byte(`{"provider_id":"abc123","user_token":"tok_123"}`))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	_, privateKey, err := capability.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair() error = %v", err)
	}
	now := time.Unix(1711000000, 0).UTC()
	token, err := capability.Issue(capability.IssueOptions{
		KeyID:      "default",
		Issuer:     "registry",
		Subject:    "partner-123",
		Schema:     doc.Name,
		SchemaHash: doc.PublicFingerprint(),
		Relation:   method.Reference.Relation,
		Condition:  "auth.required",
		Methods:    []string{method.Reference.Raw},
		IssuedAt:   now,
		NotBefore:  now,
		ExpiresAt:  now.Add(5 * time.Minute),
	}, privateKey)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	encodedToken, err := capability.Encode(token)
	if err != nil {
		t.Fatalf("Encode(token) error = %v", err)
	}
	attached := append([]byte(nil), encodedToken...)
	if err := request.SetCapabilityRaw(attached); err != nil {
		t.Fatalf("SetCapabilityRaw() error = %v", err)
	}
	for i := range attached {
		attached[i] = 'x'
	}

	encoded, err := clermreq.Encode(request)
	if err != nil {
		t.Fatalf("Encode(request) error = %v", err)
	}
	decoded, err := clermreq.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if string(decoded.CapabilityRaw) != string(encodedToken) {
		t.Fatalf("attached capability token changed after caller mutation: got %q want %q", string(decoded.CapabilityRaw), string(encodedToken))
	}
}

func TestDecodeLegacyVersion1Request(t *testing.T) {
	doc := mustRequestDocument(t)
	method, _ := doc.MethodByReference("@global.healthcare.search_providers.v1")
	request, err := clermreq.Build(method, []byte(`{"specialty":"cardiology","latitude":40.7,"longitude":-73.9}`))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	encoded, err := clermreq.Encode(request)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	legacy := append([]byte(nil), encoded[:len(encoded)-1]...)
	legacy[4] = 0
	legacy[5] = 1

	decoded, err := clermreq.Decode(legacy)
	if err != nil {
		t.Fatalf("Decode(v1) error = %v", err)
	}
	if err := decoded.ValidateAgainst(method); err != nil {
		t.Fatalf("ValidateAgainst() error = %v", err)
	}
	if len(decoded.CapabilityRaw) != 0 {
		t.Fatalf("legacy request unexpectedly decoded a capability token: %q", string(decoded.CapabilityRaw))
	}
}

func mustRequestDocument(t *testing.T) *schema.Document {
	t.Helper()
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
		t.Fatalf("Parse() error = %v", err)
	}
	return doc
}
