package clermresp_test

import (
	"strings"
	"testing"

	"github.com/million-in/clerm/clermresp"
	"github.com/million-in/clerm/schema"
)

func FuzzDecode(f *testing.F) {
	doc, err := schema.Parse(strings.NewReader(`
schema @general.avail.mandene
  @route: https://resolver.health.example/clerm
  service: @global.healthcare.search_providers.v1

method @global.healthcare.search_providers.v1
  @exec: async.pool
  @args_input: 1
    decl_args: specialty.STRING
  @args_output: 2
    decl_args: request_id.UUID, providers.ARRAY
    decl_format: json

relations @general.mandene
  @global: any.protected
`))
	if err != nil {
		f.Fatalf("Parse() error = %v", err)
	}
	method, _ := doc.MethodByReference("@global.healthcare.search_providers.v1")
	response, err := clermresp.BuildSuccess(method, []byte(`{"request_id":"123e4567-e89b-12d3-a456-426614174000","providers":[]}`))
	if err != nil {
		f.Fatalf("BuildSuccess() error = %v", err)
	}
	encoded, err := clermresp.Encode(response)
	if err != nil {
		f.Fatalf("Encode() error = %v", err)
	}
	f.Add(encoded)
	f.Add([]byte("CLRS"))
	f.Add([]byte{0, 1, 2, 3, 4, 5})

	f.Fuzz(func(t *testing.T, data []byte) {
		response, err := clermresp.Decode(data)
		if err != nil {
			return
		}
		_, _ = clermresp.Encode(response)
	})
}
