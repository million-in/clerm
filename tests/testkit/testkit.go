package testkit

import (
	"crypto/ed25519"
	"strings"
	"testing"
	"time"

	"github.com/million-in/clerm/capability"
	"github.com/million-in/clerm/clermcfg"
	"github.com/million-in/clerm/clermreq"
	"github.com/million-in/clerm/schema"
)

const (
	SampleSchemaSource = `
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
`
	GlobalMethodRef     = "@global.healthcare.search_providers.v1"
	VerifiedMethodRef   = "@verified.healthcare.book_visit.v1"
	GlobalPayloadJSON   = `{"specialty":"cardiology","latitude":40.7,"longitude":-73.9}`
	VerifiedPayloadJSON = `{"provider_id":"abc123","user_token":"tok_123"}`
)

type CapabilityFixture struct {
	Token      *capability.Token
	Encoded    []byte
	Keyring    *capability.Keyring
	PublicKey  ed25519.PublicKey
	PrivateKey ed25519.PrivateKey
}

func SampleDocument(tb testing.TB) *schema.Document {
	tb.Helper()
	doc, err := schema.Parse(strings.NewReader(strings.TrimSpace(SampleSchemaSource) + "\n"))
	if err != nil {
		tb.Fatalf("schema.Parse() error = %v", err)
	}
	return doc
}

func WriteSchemaFile(tb testing.TB, path string) {
	tb.Helper()
	if err := osWriteFile(path, []byte(strings.TrimSpace(SampleSchemaSource)+"\n"), 0o644); err != nil {
		tb.Fatalf("osWriteFile(schema) error = %v", err)
	}
}

func CompiledConfig(tb testing.TB, doc *schema.Document) []byte {
	tb.Helper()
	payload, err := clermcfg.Encode(doc)
	if err != nil {
		tb.Fatalf("clermcfg.Encode() error = %v", err)
	}
	return payload
}

func EncodedRequest(tb testing.TB, doc *schema.Document, methodRef string, payload string) []byte {
	tb.Helper()
	method, ok := doc.MethodByReference(methodRef)
	if !ok {
		tb.Fatalf("MethodByReference(%q) missing", methodRef)
	}
	request, err := clermreq.Build(method, []byte(payload))
	if err != nil {
		tb.Fatalf("clermreq.Build() error = %v", err)
	}
	encoded, err := clermreq.Encode(request)
	if err != nil {
		tb.Fatalf("clermreq.Encode() error = %v", err)
	}
	return encoded
}

func EncodedRequestWithCapability(tb testing.TB, doc *schema.Document, methodRef string, payload string, capabilityRaw []byte) []byte {
	tb.Helper()
	method, ok := doc.MethodByReference(methodRef)
	if !ok {
		tb.Fatalf("MethodByReference(%q) missing", methodRef)
	}
	request, err := clermreq.Build(method, []byte(payload))
	if err != nil {
		tb.Fatalf("clermreq.Build() error = %v", err)
	}
	if err := request.SetCapabilityRaw(capabilityRaw); err != nil {
		tb.Fatalf("request.SetCapabilityRaw() error = %v", err)
	}
	encoded, err := clermreq.Encode(request)
	if err != nil {
		tb.Fatalf("clermreq.Encode() error = %v", err)
	}
	return encoded
}

func VerifiedCapability(tb testing.TB, doc *schema.Document, target string) CapabilityFixture {
	tb.Helper()
	publicKey, privateKey, err := capability.GenerateKeyPair()
	if err != nil {
		tb.Fatalf("capability.GenerateKeyPair() error = %v", err)
	}
	now := time.Now().UTC()
	token, err := capability.Issue(capability.IssueOptions{
		KeyID:      "registry",
		Issuer:     "clerm_registry",
		Subject:    "partner-123",
		Schema:     doc.Name,
		SchemaHash: doc.PublicFingerprint(),
		Relation:   "@verified",
		Condition:  "auth.required",
		Methods:    []string{VerifiedMethodRef},
		Targets:    []string{target},
		IssuedAt:   now,
		NotBefore:  now,
		ExpiresAt:  now.Add(30 * time.Minute),
	}, privateKey)
	if err != nil {
		tb.Fatalf("capability.Issue() error = %v", err)
	}
	encoded, err := capability.Encode(token)
	if err != nil {
		tb.Fatalf("capability.Encode() error = %v", err)
	}
	return CapabilityFixture{
		Token:      token,
		Encoded:    encoded,
		Keyring:    capability.NewKeyring(map[string]ed25519.PublicKey{"registry": publicKey}),
		PublicKey:  publicKey,
		PrivateKey: privateKey,
	}
}

func osWriteFile(path string, data []byte, perm uint32) error {
	return writeFile(path, data, perm)
}
