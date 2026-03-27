package clerm

import (
	"context"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"

	"github.com/million-in/clerm/capability"
	"github.com/million-in/clerm/clermresp"
	"github.com/million-in/clerm/platform"
	resolverpkg "github.com/million-in/clerm/resolver"
	"github.com/million-in/clerm/schema"
)

type ServiceOptions struct {
	CapabilityKeyring *capability.Keyring
	ReplayStore       capability.ReplayStore
	MaxBodyBytes      int64
}

type RemoteServiceOptions struct {
	Load    resolverpkg.LoadConfigURLOptions
	Service ServiceOptions
}

type DaemonOptions = resolverpkg.DaemonOptions

type EncodeResponseInput struct {
	MethodReference string
	OutputsJSON     []byte
	Outputs         map[string]any
	ErrorCode       string
	ErrorMessage    string
}

type EncodedResponse struct {
	Method   schema.Method
	Response *clermresp.Response
	Payload  []byte
}

type SchemaInfo struct {
	Name          string `json:"name"`
	Fingerprint   string `json:"fingerprint"`
	MethodCount   int    `json:"method_count"`
	RelationCount int    `json:"relation_count"`
}

func (ResolverAPI) NewService(doc *schema.Document, options ServiceOptions) (*resolverpkg.Service, error) {
	if doc == nil {
		return nil, platform.New(platform.CodeInvalidArgument, "schema document is required")
	}
	service := resolverpkg.New(doc)
	if err := Resolver.ConfigureService(service, options); err != nil {
		return nil, err
	}
	return service, nil
}

func (ResolverAPI) ConfigureService(service *resolverpkg.Service, options ServiceOptions) error {
	if service == nil {
		return platform.New(platform.CodeInvalidArgument, "resolver service is required")
	}
	if options.CapabilityKeyring != nil {
		service.SetCapabilityKeyring(options.CapabilityKeyring)
	}
	if options.ReplayStore != nil {
		service.SetReplayStore(options.ReplayStore)
	}
	if options.MaxBodyBytes > 0 {
		service.SetMaxBodyBytes(options.MaxBodyBytes)
	}
	return nil
}

func (ResolverAPI) LoadService(path string, options ServiceOptions) (*resolverpkg.Service, error) {
	doc, err := Compiler.LoadDocument(path)
	if err != nil {
		return nil, err
	}
	return Resolver.NewService(doc, options)
}

func (ResolverAPI) LoadServiceURL(ctx context.Context, rawURL string, options RemoteServiceOptions) (*resolverpkg.Service, error) {
	service, err := resolverpkg.LoadConfigURLWithOptions(ctx, strings.TrimSpace(rawURL), options.Load)
	if err != nil {
		return nil, err
	}
	if err := Resolver.ConfigureService(service, options.Service); err != nil {
		return nil, err
	}
	return service, nil
}

func (ResolverAPI) ResolveBinary(service *resolverpkg.Service, payload []byte, target string) (*resolverpkg.Command, error) {
	if service == nil {
		return nil, platform.New(platform.CodeInvalidArgument, "resolver service is required")
	}
	return service.ResolveBinaryWithTarget(payload, strings.TrimSpace(target))
}

func (ResolverAPI) EncodeResponse(service *resolverpkg.Service, input EncodeResponseInput) (*EncodedResponse, error) {
	if service == nil {
		return nil, platform.New(platform.CodeInvalidArgument, "resolver service is required")
	}
	method, ok := service.Method(strings.TrimSpace(input.MethodReference))
	if !ok {
		return nil, platform.New(platform.CodeNotFound, "method not found in compiled config")
	}
	hasError := strings.TrimSpace(input.ErrorCode) != "" || strings.TrimSpace(input.ErrorMessage) != ""
	hasOutputsJSON := len(input.OutputsJSON) > 0
	hasOutputsMap := input.Outputs != nil
	switch {
	case hasError && (hasOutputsJSON || hasOutputsMap):
		return nil, platform.New(platform.CodeInvalidArgument, "use either outputs or an error response, not both")
	case hasOutputsJSON && hasOutputsMap:
		return nil, platform.New(platform.CodeInvalidArgument, "use either OutputsJSON or Outputs, not both")
	}
	var (
		response *clermresp.Response
		err      error
	)
	switch {
	case hasError:
		response, err = clermresp.BuildError(method, strings.TrimSpace(input.ErrorCode), strings.TrimSpace(input.ErrorMessage))
	case hasOutputsMap:
		response, err = clermresp.BuildSuccessMap(method, input.Outputs)
	case hasOutputsJSON:
		response, err = clermresp.BuildSuccess(method, input.OutputsJSON)
	default:
		response, err = clermresp.BuildSuccess(method, []byte("{}"))
	}
	if err != nil {
		return nil, err
	}
	payload, err := clermresp.Encode(response)
	if err != nil {
		return nil, err
	}
	return &EncodedResponse{
		Method:   method,
		Response: response,
		Payload:  payload,
	}, nil
}

func (ResolverAPI) SchemaInfo(service *resolverpkg.Service) (*SchemaInfo, error) {
	if service == nil {
		return nil, platform.New(platform.CodeInvalidArgument, "resolver service is required")
	}
	doc := service.Document()
	if doc == nil {
		return nil, platform.New(platform.CodeInternal, "resolver service schema is not loaded")
	}
	sum := service.Fingerprint()
	return &SchemaInfo{
		Name:          doc.Name,
		Fingerprint:   hex.EncodeToString(sum[:]),
		MethodCount:   len(doc.Methods),
		RelationCount: len(doc.Relations),
	}, nil
}

func (ResolverAPI) NewDaemonHandler(logger *slog.Logger, service *resolverpkg.Service) http.Handler {
	return resolverpkg.NewDaemonHandler(logger, service)
}

func (ResolverAPI) NewDaemonHandlerWithOptions(logger *slog.Logger, service *resolverpkg.Service, options DaemonOptions) http.Handler {
	return resolverpkg.NewDaemonHandlerWithOptions(logger, service, options)
}
