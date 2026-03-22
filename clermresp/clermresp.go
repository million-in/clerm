package clermresp

import (
	internal "github.com/million-in/clerm/internal/clermresp"
	"github.com/million-in/clerm/schema"
)

type ErrorBody = internal.ErrorBody
type Value = internal.Value
type Response = internal.Response

var (
	BuildSuccess = func(method schema.Method, payloadJSON []byte) (*Response, error) {
		return internal.BuildSuccess(method, payloadJSON)
	}
	BuildSuccessMap = func(method schema.Method, payload map[string]any) (*Response, error) {
		return internal.BuildSuccessMap(method, payload)
	}
	BuildSuccessValues = internal.BuildSuccessValues
	BuildError         = func(method schema.Method, code string, message string) (*Response, error) {
		return internal.BuildError(method, code, message)
	}
	Encode    = internal.Encode
	WriteTo   = internal.WriteTo
	Decode    = internal.Decode
	IsEncoded = internal.IsEncoded
)
