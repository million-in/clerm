package clermcli_test

import (
	"testing"
	"time"

	"github.com/million-in/clerm/capability"
)

func validCapabilityTokenText(t *testing.T, schemaName string, schemaHash [32]byte, relation string, method string) string {
	t.Helper()
	_, privateKey, err := capability.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair() error = %v", err)
	}
	now := time.Unix(1711000000, 0).UTC()
	token, err := capability.Issue(capability.IssueOptions{
		KeyID:      "default",
		Issuer:     "registry",
		Subject:    "partner-123",
		TokenID:    "tok-1",
		Schema:     schemaName,
		SchemaHash: schemaHash,
		Relation:   relation,
		Condition:  "auth.required",
		Methods:    []string{method},
		IssuedAt:   now,
		NotBefore:  now,
		ExpiresAt:  now.Add(5 * time.Minute),
	}, privateKey)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	text, err := capability.EncodeText(token)
	if err != nil {
		t.Fatalf("EncodeText() error = %v", err)
	}
	return text
}
