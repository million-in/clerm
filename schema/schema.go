package schema

import (
	"io"

	internal "github.com/million-in/clerm/internal/schema"
)

type ExecutionMode = internal.ExecutionMode
type ArgType = internal.ArgType
type PayloadFormat = internal.PayloadFormat
type ServiceRef = internal.ServiceRef
type Parameter = internal.Parameter
type Method = internal.Method
type RelationRule = internal.RelationRule
type Metadata = internal.Metadata
type Document = internal.Document
type FingerprintCache = internal.FingerprintCache

const (
	ExecutionUnknown   = internal.ExecutionUnknown
	ExecutionSync      = internal.ExecutionSync
	ExecutionAsyncPool = internal.ExecutionAsyncPool
	ArgUnknown         = internal.ArgUnknown
	ArgString          = internal.ArgString
	ArgDecimal         = internal.ArgDecimal
	ArgUUID            = internal.ArgUUID
	ArgArray           = internal.ArgArray
	ArgTimestamp       = internal.ArgTimestamp
	ArgInt             = internal.ArgInt
	ArgBool            = internal.ArgBool
	FormatUnknown      = internal.FormatUnknown
	FormatJSON         = internal.FormatJSON
	FormatXML          = internal.FormatXML
	FormatYAML         = internal.FormatYAML
)

var (
	Parse                             = func(r io.Reader) (*Document, error) { return internal.Parse(r) }
	ParseExecutionMode                = internal.ParseExecutionMode
	AvailableExecutionModes           = internal.AvailableExecutionModes
	ParseArgType                      = internal.ParseArgType
	AvailableArgTypes                 = internal.AvailableArgTypes
	ParsePayloadFormat                = internal.ParsePayloadFormat
	AvailablePayloadFormats           = internal.AvailablePayloadFormats
	ParseServiceRef                   = internal.ParseServiceRef
	NewFingerprintCache               = func() *FingerprintCache { return internal.NewFingerprintCache() }
	CachedPublicFingerprint           = internal.CachedPublicFingerprint
	InvalidateCachedPublicFingerprint = internal.InvalidateCachedPublicFingerprint
)
