package resolver

import (
	internal "github.com/million-in/clerm/internal/resolver"
	"github.com/million-in/clerm/schema"
)

type Command = internal.Command
type Service = internal.Service

var (
	LoadConfig = internal.LoadConfig
	New        = func(doc *schema.Document) *Service { return internal.New(doc) }
)
