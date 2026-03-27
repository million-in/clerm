package resolver

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/million-in/clerm/clermresp"
	internal "github.com/million-in/clerm/internal/resolver"
	"github.com/million-in/clerm/schema"
)

type Command = internal.Command
type Invocation = internal.Invocation
type ArgumentsView = internal.ArgumentsView
type Result = internal.Result
type HandlerFunc = internal.HandlerFunc
type Service = internal.Service
type URLPolicy = internal.URLPolicy
type LoadConfigURLOptions = internal.LoadConfigURLOptions
type DaemonOptions = internal.DaemonOptions

var (
	LoadConfig    = internal.LoadConfig
	LoadConfigURL = func(ctx context.Context, rawURL string, httpClient *http.Client) (*Service, error) {
		return internal.LoadConfigURL(ctx, rawURL, httpClient)
	}
	LoadConfigURLWithOptions = func(ctx context.Context, rawURL string, options LoadConfigURLOptions) (*Service, error) {
		return internal.LoadConfigURLWithOptions(ctx, rawURL, options)
	}
	DenyPrivateHostPolicy = internal.DenyPrivateHostPolicy
	New                   = func(doc *schema.Document) *Service { return internal.New(doc) }
	Success               = internal.Success
	SuccessResponse       = internal.SuccessResponse
	Failure               = internal.Failure
	NewDaemonHandler      = func(logger *slog.Logger, service *Service) http.Handler {
		return internal.NewDaemonHandler(logger, service)
	}
	NewDaemonHandlerWithOptions = func(logger *slog.Logger, service *Service, options DaemonOptions) http.Handler {
		return internal.NewDaemonHandlerWithOptions(logger, service, options)
	}
	BuildSuccessResponse = func(method schema.Method, outputs map[string]any) (*clermresp.Response, error) {
		return clermresp.BuildSuccessMap(method, outputs)
	}
)
