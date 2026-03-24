package capability_test

import (
	"testing"
	"time"

	"github.com/million-in/clerm/capability"
)

func FuzzDecode(f *testing.F) {
	_, privateKey, err := capability.GenerateKeyPair()
	if err != nil {
		f.Fatalf("GenerateKeyPair() error = %v", err)
	}
	now := time.Unix(1711000000, 0).UTC()
	token, err := capability.Issue(capability.IssueOptions{
		KeyID:      "default",
		Issuer:     "registry",
		Subject:    "partner-123",
		Schema:     "@general.avail.mandene",
		SchemaHash: [32]byte{1},
		Relation:   "@global",
		Condition:  "any.protected",
		Methods:    []string{"@global.healthcare.search_providers.v1"},
		IssuedAt:   now,
		NotBefore:  now,
		ExpiresAt:  now.Add(5 * time.Minute),
	}, privateKey)
	if err != nil {
		f.Fatalf("Issue() error = %v", err)
	}
	encoded, err := capability.Encode(token)
	if err != nil {
		f.Fatalf("Encode() error = %v", err)
	}
	f.Add(encoded)
	f.Add([]byte("CLCP"))
	f.Add([]byte{0, 1, 2, 3, 4, 5})

	f.Fuzz(func(t *testing.T, data []byte) {
		token, err := capability.Decode(data)
		if err != nil {
			return
		}
		if _, err := capability.Encode(token); err != nil {
			t.Fatalf("Encode(decoded) error = %v", err)
		}
	})
}
