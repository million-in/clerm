package clermcfg_test

import (
	"strings"
	"testing"

	"github.com/million-in/clerm/clermcfg"
	"github.com/million-in/clerm/schema"
)

func FuzzDecode(f *testing.F) {
	doc, err := schema.Parse(strings.NewReader(configSchemaSource()))
	if err != nil {
		f.Fatalf("Parse() error = %v", err)
	}
	encoded, err := clermcfg.Encode(doc)
	if err != nil {
		f.Fatalf("Encode() error = %v", err)
	}
	f.Add(encoded)
	f.Add([]byte("CLRC"))
	f.Add([]byte{0, 1, 2, 3, 4, 5})

	f.Fuzz(func(t *testing.T, data []byte) {
		doc, err := clermcfg.Decode(data)
		if err != nil {
			return
		}
		if _, err := clermcfg.Encode(doc); err != nil {
			t.Fatalf("Encode(decoded) error = %v", err)
		}
	})
}
