package clermreq

import (
	internal "github.com/million-in/clerm/internal/clermreq"
	"github.com/million-in/clerm/schema"
)

type Argument = internal.Argument
type Request = internal.Request

var (
	Magic = internal.Magic
	Build = func(method schema.Method, payloadJSON []byte) (*Request, error) {
		return internal.Build(method, payloadJSON)
	}
	Encode    = internal.Encode
	Decode    = internal.Decode
	IsEncoded = internal.IsEncoded
)
