package clermreq_test

import (
	"strings"
	"testing"

	"github.com/million-in/clerm/clermreq"
	"github.com/million-in/clerm/schema"
)

func FuzzDecode(f *testing.F) {
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
		f.Fatalf("Parse() error = %v", err)
	}
	method, _ := doc.MethodByReference("@global.healthcare.search_providers.v1")
	request, err := clermreq.Build(method, []byte(`{"specialty":"cardiology","latitude":40.7,"longitude":-73.9}`))
	if err != nil {
		f.Fatalf("Build() error = %v", err)
	}
	encoded, err := clermreq.Encode(request)
	if err != nil {
		f.Fatalf("Encode() error = %v", err)
	}
	f.Add(encoded)
	f.Add([]byte("CLRM"))
	f.Add([]byte{0, 1, 2, 3, 4, 5})

	f.Fuzz(func(t *testing.T, data []byte) {
		request, err := clermreq.Decode(data)
		if err != nil {
			return
		}
		_, _ = clermreq.Encode(request)
	})
}
