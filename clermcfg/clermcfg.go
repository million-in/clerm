package clermcfg

import (
	internal "github.com/million-in/clerm/internal/clermcfg"
	"github.com/million-in/clerm/schema"
)

type Decoder = internal.Decoder

var (
	Magic           = internal.Magic
	Encode          = func(doc *schema.Document) ([]byte, error) { return internal.Encode(doc) }
	Decode          = internal.Decode
	DecodeCodec     = internal.DecodeCodec
	DecodeInto      = func(doc *schema.Document, data []byte) error { return internal.DecodeInto(doc, data) }
	DecodeCodecInto = func(doc *schema.Document, data []byte) error { return internal.DecodeCodecInto(doc, data) }
	IsEncoded       = internal.IsEncoded
)
