package capability_test

import (
	"crypto/ed25519"
	"testing"
	"time"

	"github.com/million-in/clerm/internal/capability"
)

func TestIssueEncodeDecodeVerify(t *testing.T) {
	publicKey, privateKey, err := capability.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair() error = %v", err)
	}
	var schemaHash [32]byte
	schemaHash[0] = 1
	now := time.Unix(1711000000, 0).UTC()
	token, err := capability.Issue(capability.IssueOptions{
		KeyID:      "default",
		Issuer:     "registry",
		Subject:    "partner-123",
		Schema:     "@general.avail.mandene",
		SchemaHash: schemaHash,
		Relation:   "@verified",
		Condition:  "auth.required",
		Methods:    []string{"@verified.healthcare.book_visit.v1"},
		Targets:    []string{"registry.invoke"},
		IssuedAt:   now,
		NotBefore:  now,
		ExpiresAt:  now.Add(5 * time.Minute),
	}, privateKey)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	encoded, err := capability.Encode(token)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	decoded, err := capability.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	keyring := capability.NewKeyring(map[string]ed25519.PublicKey{"default": publicKey})
	if err := keyring.Verify(decoded); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
}

func TestVerifyRejectsTamper(t *testing.T) {
	publicKey, privateKey, err := capability.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair() error = %v", err)
	}
	var schemaHash [32]byte
	schemaHash[0] = 9
	token, err := capability.Issue(capability.IssueOptions{
		KeyID:      "default",
		Issuer:     "registry",
		Subject:    "partner-123",
		Schema:     "@general.avail.mandene",
		SchemaHash: schemaHash,
		Relation:   "@verified",
		Condition:  "auth.required",
		Methods:    []string{"@verified.healthcare.book_visit.v1"},
		IssuedAt:   time.Unix(1711000000, 0).UTC(),
		NotBefore:  time.Unix(1711000000, 0).UTC(),
		ExpiresAt:  time.Unix(1711000300, 0).UTC(),
	}, privateKey)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	encoded, err := capability.Encode(token)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	encoded[len(encoded)-1] ^= 0x01
	decoded, err := capability.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode(tampered) error = %v", err)
	}
	keyring := capability.NewKeyring(map[string]ed25519.PublicKey{"default": publicKey})
	if err := keyring.Verify(decoded); err == nil {
		t.Fatal("expected signature verification failure")
	}
}

func TestMemoryReplayStoreRejectsReuse(t *testing.T) {
	store := capability.NewMemoryReplayStore()
	if err := store.Reserve("tok-1", time.Minute); err != nil {
		t.Fatalf("Reserve(first) error = %v", err)
	}
	if err := store.Reserve("tok-1", time.Minute); err == nil {
		t.Fatal("expected replay rejection")
	}
}
