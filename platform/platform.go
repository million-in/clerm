package platform

import internal "github.com/million-in/clerm/internal/platform"

type Code = internal.Code
type Error = internal.Error

const (
	CodeInvalidArgument = internal.CodeInvalidArgument
	CodeParse           = internal.CodeParse
	CodeValidation      = internal.CodeValidation
	CodeIO              = internal.CodeIO
	CodeNotFound        = internal.CodeNotFound
	CodeInternal        = internal.CodeInternal
)

var (
	New       = internal.New
	Wrap      = internal.Wrap
	CodeOf    = internal.CodeOf
	As        = internal.As
	IsCode    = internal.IsCode
	NewLogger = internal.NewLogger
	LogError  = internal.LogError
)
