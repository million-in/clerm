package clermcli

import (
	"crypto/ed25519"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/million-in/clerm/internal/capability"
	"github.com/million-in/clerm/internal/clermcfg"
	"github.com/million-in/clerm/internal/clermreq"
	"github.com/million-in/clerm/internal/clermresp"
	"github.com/million-in/clerm/internal/jsonwire"
	"github.com/million-in/clerm/internal/platform"
	"github.com/million-in/clerm/internal/resolver"
	"github.com/million-in/clerm/internal/schema"
)

type Streams struct {
	Stdout io.Writer
	Stderr io.Writer
}

type ToolDefinition struct {
	Name          string         `json:"name"`
	Description   string         `json:"description"`
	Method        string         `json:"clerm_method"`
	Relation      string         `json:"relation"`
	Condition     string         `json:"condition"`
	TokenRequired bool           `json:"token_required"`
	InputSchema   map[string]any `json:"input_schema"`
}

func Run(logger *slog.Logger, args []string) error {
	return RunWithIO(logger, Streams{Stdout: os.Stdout, Stderr: os.Stderr}, args)
}

func RunWithIO(logger *slog.Logger, streams Streams, args []string) error {
	if streams.Stdout == nil {
		streams.Stdout = io.Discard
	}
	if streams.Stderr == nil {
		streams.Stderr = io.Discard
	}
	if len(args) == 0 {
		printUsage(streams.Stderr)
		return platform.New(platform.CodeInvalidArgument, "missing command")
	}
	switch args[0] {
	case "help", "-h", "--help":
		printUsage(streams.Stdout)
		return nil
	}
	if strings.HasSuffix(args[0], ".clermfile") {
		return runCompile(logger, streams, args)
	}

	switch args[0] {
	case "compile":
		return runCompile(logger, streams, args[1:])
	case "inspect":
		return runInspect(streams, args[1:])
	case "register":
		return runRegister(streams, args[1:])
	case "search":
		return runSearch(streams, args[1:])
	case "discover":
		return runDiscover(streams, args[1:])
	case "relationship":
		return runRelationship(streams, args[1:])
	case "token":
		return runToken(streams, args[1:])
	case "request":
		return runRequest(streams, args[1:])
	case "invoke":
		return runInvoke(streams, args[1:])
	case "resolve":
		return runResolve(streams, args[1:])
	case "serve":
		return runServe(logger, streams, args[1:])
	case "tools":
		return runTools(streams, args[1:])
	case "benchmark":
		return runBenchmark(streams, args[1:])
	case "shellenv":
		return runShellenv(streams)
	default:
		printUsage(streams.Stderr)
		return platform.New(platform.CodeInvalidArgument, "invalid command")
	}
}

func runCompile(_ *slog.Logger, streams Streams, args []string) error {
	fs := flag.NewFlagSet("compile", flag.ContinueOnError)
	fs.SetOutput(streams.Stderr)
	in := fs.String("in", "", "path to .clermfile")
	out := fs.String("out", "", "path to output .clermcfg")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *in == "" && fs.NArg() > 0 {
		*in = fs.Arg(0)
	}
	if *out == "" && fs.NArg() > 1 {
		*out = fs.Arg(1)
	}
	if *in == "" {
		return platform.New(platform.CodeInvalidArgument, "compile requires -in <file>.clermfile")
	}
	if !strings.HasSuffix(*in, ".clermfile") {
		return platform.New(platform.CodeInvalidArgument, "compile input must be a .clermfile")
	}
	doc, err := loadDocument(*in)
	if err != nil {
		return err
	}
	data, err := clermcfg.Encode(doc)
	if err != nil {
		return err
	}
	if *out == "" {
		*out = replaceExt(*in, ".clermcfg")
	}
	if err := os.WriteFile(*out, data, 0o644); err != nil {
		return platform.Wrap(platform.CodeIO, err, "write compiled config")
	}
	_, _ = fmt.Fprintf(streams.Stdout, "%s\n", *out)
	return nil
}

func runInspect(streams Streams, args []string) error {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	fs.SetOutput(streams.Stderr)
	in := fs.String("in", "", "path to .clermfile, .clermcfg, or .clerm")
	internal := fs.Bool("internal", false, "include internal-only config fields like route")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *in == "" && fs.NArg() > 0 {
		*in = fs.Arg(0)
	}
	if *in == "" {
		return platform.New(platform.CodeInvalidArgument, "inspect requires -in <path>")
	}
	result, err := inspectPath(*in, *internal)
	if err != nil {
		return err
	}
	return writeJSON(streams.Stdout, result)
}

func runToken(streams Streams, args []string) error {
	if len(args) == 0 {
		return platform.New(platform.CodeInvalidArgument, "token requires a subcommand: issue, refresh, inspect")
	}
	switch args[0] {
	case "issue":
		return runTokenIssue(streams, args[1:])
	case "refresh":
		return runTokenRefresh(streams, args[1:])
	case "inspect":
		return runTokenInspect(streams, args[1:])
	case "keygen":
		return platform.New(platform.CodeInvalidArgument, "token keygen is not supported in the public CLI; the registry manages signing keys")
	default:
		return platform.New(platform.CodeInvalidArgument, "invalid token subcommand")
	}
}

func runTokenKeygen(streams Streams, args []string) error {
	fs := flag.NewFlagSet("token keygen", flag.ContinueOnError)
	fs.SetOutput(streams.Stderr)
	outPrivate := fs.String("out-private", "clerm.ed25519", "path to output Ed25519 private key")
	outPublic := fs.String("out-public", "clerm.ed25519.pub", "path to output Ed25519 public key")
	if err := fs.Parse(args); err != nil {
		return err
	}
	publicKey, privateKey, err := capability.GenerateKeyPair()
	if err != nil {
		return err
	}
	if err := capability.WritePrivateKeyFile(*outPrivate, privateKey); err != nil {
		return err
	}
	if err := capability.WritePublicKeyFile(*outPublic, publicKey); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(streams.Stdout, "%s\n%s\n", *outPrivate, *outPublic)
	return nil
}

func runTokenIssue(streams Streams, args []string) error {
	return runTokenIssueRPC(streams, args)
}

func runTokenInspect(streams Streams, args []string) error {
	fs := flag.NewFlagSet("token inspect", flag.ContinueOnError)
	fs.SetOutput(streams.Stderr)
	in := fs.String("in", "", "path to token file")
	tokenText := fs.String("token", "", "inline capability token text")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *in == "" && *tokenText == "" {
		return platform.New(platform.CodeInvalidArgument, "token inspect requires -in or -token")
	}
	raw := *tokenText
	if *in != "" {
		data, err := os.ReadFile(*in)
		if err != nil {
			return platform.Wrap(platform.CodeIO, err, "read capability token file")
		}
		raw = string(data)
	}
	token, err := capability.DecodeText(raw)
	if err != nil {
		return err
	}
	return writeJSON(streams.Stdout, token.InspectView())
}

func runRequest(streams Streams, args []string) error {
	fs := flag.NewFlagSet("request", flag.ContinueOnError)
	fs.SetOutput(streams.Stderr)
	schemaPath := fs.String("schema", "", "path to .clermfile or .clermcfg")
	methodRef := fs.String("method", "", "fully-qualified method reference")
	allow := fs.String("allow", "", "comma-separated allowed relation types")
	capInline := fs.String("cap", "", "inline capability token text")
	capFile := fs.String("cap-file", "", "path to capability token text")
	dataInline := fs.String("data", "", "inline JSON payload")
	dataFile := fs.String("data-file", "", "path to JSON payload")
	out := fs.String("out", "", "path to output .clerm")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *schemaPath == "" || *methodRef == "" {
		return platform.New(platform.CodeInvalidArgument, "request requires -schema and -method")
	}
	doc, err := loadDocument(*schemaPath)
	if err != nil {
		return err
	}
	method, ok := doc.MethodByReference(*methodRef)
	if !ok {
		return platform.New(platform.CodeNotFound, "method not found in schema")
	}
	allowed := parseAllowedSet(*allow)
	if len(allowed) > 0 {
		if _, ok := allowed[method.Reference.Relation]; !ok {
			return platform.New(platform.CodeValidation, fmt.Sprintf("method %s is not allowed by current relations", method.Reference.Raw))
		}
	}
	payload, err := readPayload(*dataInline, *dataFile)
	if err != nil {
		return err
	}
	request, err := clermreq.Build(method, payload)
	if err != nil {
		return err
	}
	condition, ok := doc.RelationCondition(method.Reference.Relation)
	if !ok {
		return platform.New(platform.CodeValidation, "method relation is not defined in schema")
	}
	token, encodedToken, err := readCapabilityToken(*capInline, *capFile)
	if err != nil {
		return err
	}
	if requiresCapability(condition) && token == nil {
		return platform.New(platform.CodeValidation, "capability token is required for this relation; obtain one from clerm_registry with `clerm token issue -registry ...`")
	}
	if token != nil {
		if err := validateCapabilityScope(doc, method, condition, token); err != nil {
			return err
		}
		request.CapabilityRaw = encodedToken
	}
	encoded, err := clermreq.Encode(request)
	if err != nil {
		return err
	}
	if *out == "" {
		*out = method.Reference.Method + ".clerm"
	}
	if err := os.WriteFile(*out, encoded, 0o644); err != nil {
		return platform.Wrap(platform.CodeIO, err, "write request file")
	}
	_, _ = fmt.Fprintf(streams.Stdout, "%s\n", *out)
	return nil
}

func runResolve(streams Streams, args []string) error {
	fs := flag.NewFlagSet("resolve", flag.ContinueOnError)
	fs.SetOutput(streams.Stderr)
	schemaPath := fs.String("schema", "", "path to .clermfile or .clermcfg")
	requestPath := fs.String("request", "", "path to .clerm")
	target := fs.String("target", "", "override command target, same value used by Clerm-Target")
	publicKeyPath := fs.String("cap-public-key", "", "path to Ed25519 public key for capability verification")
	keyID := fs.String("cap-key-id", "default", "capability verification key id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *schemaPath == "" || *requestPath == "" {
		return platform.New(platform.CodeInvalidArgument, "resolve requires -schema and -request")
	}
	service, err := loadService(*schemaPath, *publicKeyPath, *keyID)
	if err != nil {
		return err
	}
	payload, err := os.ReadFile(*requestPath)
	if err != nil {
		return platform.Wrap(platform.CodeIO, err, "read request file")
	}
	command, err := service.ResolveBinaryWithTarget(payload, *target)
	if err != nil {
		return err
	}
	return writeJSON(streams.Stdout, command)
}

func runServe(logger *slog.Logger, streams Streams, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(streams.Stderr)
	schemaPath := fs.String("schema", "", "path to .clermfile or .clermcfg")
	listen := fs.String("listen", ":8080", "listen address")
	publicKeyPath := fs.String("cap-public-key", "", "path to Ed25519 public key for capability verification")
	keyID := fs.String("cap-key-id", "default", "capability verification key id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *schemaPath == "" {
		return platform.New(platform.CodeInvalidArgument, "serve requires -schema")
	}
	service, err := loadService(*schemaPath, *publicKeyPath, *keyID)
	if err != nil {
		return err
	}
	server := &http.Server{Addr: *listen, Handler: resolver.NewDaemonHandler(logger, service)}
	if logger != nil {
		logger.Info("starting clerm resolver daemon", "listen", *listen, "schema", service.Document().Name, "schema_fingerprint", fmt.Sprintf("%x", service.Document().PublicFingerprint()))
	}
	_, _ = fmt.Fprintf(streams.Stdout, "%s\n", *listen)
	return server.ListenAndServe()
}

func runTools(streams Streams, args []string) error {
	fs := flag.NewFlagSet("tools", flag.ContinueOnError)
	fs.SetOutput(streams.Stderr)
	schemaPath := fs.String("schema", "", "path to .clermfile or .clermcfg")
	allow := fs.String("allow", "", "comma-separated allowed relation types")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *schemaPath == "" {
		return platform.New(platform.CodeInvalidArgument, "tools requires -schema")
	}
	doc, err := loadDocument(*schemaPath)
	if err != nil {
		return err
	}
	allowed := parseAllowedSet(*allow)
	methods := make([]schema.Method, 0, len(doc.Methods))
	for _, method := range doc.Methods {
		if len(allowed) > 0 {
			if _, ok := allowed[method.Reference.Relation]; !ok {
				continue
			}
		}
		methods = append(methods, method)
	}
	sort.Slice(methods, func(i, j int) bool { return methods[i].Reference.Raw < methods[j].Reference.Raw })
	tools := make([]ToolDefinition, 0, len(methods))
	for _, method := range methods {
		condition, _ := doc.RelationCondition(method.Reference.Relation)
		tools = append(tools, ToolDefinition{
			Name:          method.Reference.Method,
			Description:   fmt.Sprintf("Build a CLERM tool-call request for %s", method.Reference.Raw),
			Method:        method.Reference.Raw,
			Relation:      method.Reference.Relation,
			Condition:     condition,
			TokenRequired: requiresCapability(condition),
			InputSchema:   inputJSONSchema(method.InputArgs),
		})
	}
	return writeJSON(streams.Stdout, tools)
}

type benchmarkSize struct {
	CLERMBytes   int     `json:"clerm_bytes"`
	JSONBytes    int     `json:"json_bytes"`
	SavedBytes   int     `json:"saved_bytes"`
	SavedPercent float64 `json:"saved_percent"`
}

type benchmarkTiming struct {
	CLERMEncodeNS float64 `json:"clerm_encode_ns_per_op"`
	CLERMDecodeNS float64 `json:"clerm_decode_ns_per_op"`
	JSONEncodeNS  float64 `json:"json_encode_ns_per_op"`
	JSONDecodeNS  float64 `json:"json_decode_ns_per_op"`
}

type benchmarkReport struct {
	Iterations  int             `json:"iterations"`
	ConfigSize  benchmarkSize   `json:"config_size"`
	RequestSize benchmarkSize   `json:"request_size"`
	ConfigTime  benchmarkTiming `json:"config_time"`
	RequestTime benchmarkTiming `json:"request_time"`
}

func runBenchmark(streams Streams, args []string) error {
	fs := flag.NewFlagSet("benchmark", flag.ContinueOnError)
	fs.SetOutput(streams.Stderr)
	schemaPath := fs.String("schema", "", "path to .clermfile or .clermcfg")
	methodRef := fs.String("method", "", "fully-qualified method reference")
	dataInline := fs.String("data", "", "inline JSON payload")
	dataFile := fs.String("data-file", "", "path to JSON payload")
	iterations := fs.Int("iterations", 100000, "benchmark loop iterations")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *schemaPath == "" || *methodRef == "" {
		return platform.New(platform.CodeInvalidArgument, "benchmark requires -schema and -method")
	}
	doc, err := loadDocument(*schemaPath)
	if err != nil {
		return err
	}
	method, ok := doc.MethodByReference(*methodRef)
	if !ok {
		return platform.New(platform.CodeNotFound, "method not found in schema")
	}
	payload, err := readPayload(*dataInline, *dataFile)
	if err != nil {
		return err
	}
	request, err := clermreq.Build(method, payload)
	if err != nil {
		return err
	}
	cfgBytes, err := clermcfg.Encode(doc)
	if err != nil {
		return err
	}
	reqBytes, err := clermreq.Encode(request)
	if err != nil {
		return err
	}
	cfgJSON, err := jsonwire.MarshalConfig(doc, true)
	if err != nil {
		return err
	}
	reqJSON, err := jsonwire.MarshalRequest(request)
	if err != nil {
		return err
	}
	report := benchmarkReport{
		Iterations:  *iterations,
		ConfigSize:  sizeComparison(len(cfgBytes), len(cfgJSON)),
		RequestSize: sizeComparison(len(reqBytes), len(reqJSON)),
		ConfigTime: benchmarkTiming{
			CLERMEncodeNS: measureNS(*iterations, func() error { _, err := clermcfg.Encode(doc); return err }),
			CLERMDecodeNS: measureNS(*iterations, func() error { _, err := clermcfg.Decode(cfgBytes); return err }),
			JSONEncodeNS:  measureNS(*iterations, func() error { _, err := jsonwire.MarshalConfig(doc, true); return err }),
			JSONDecodeNS:  measureNS(*iterations, func() error { _, err := jsonwire.UnmarshalConfig(cfgJSON); return err }),
		},
		RequestTime: benchmarkTiming{
			CLERMEncodeNS: measureNS(*iterations, func() error { _, err := clermreq.Encode(request); return err }),
			CLERMDecodeNS: measureNS(*iterations, func() error { _, err := clermreq.Decode(reqBytes); return err }),
			JSONEncodeNS:  measureNS(*iterations, func() error { _, err := jsonwire.MarshalRequest(request); return err }),
			JSONDecodeNS:  measureNS(*iterations, func() error { _, err := jsonwire.UnmarshalRequest(reqJSON); return err }),
		},
	}
	return writeJSON(streams.Stdout, report)
}

func runShellenv(streams Streams) error {
	root := strings.TrimSpace(os.Getenv("CLERM_ROOT"))
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return platform.Wrap(platform.CodeIO, err, "resolve current working directory")
		}
		root = cwd
	}
	_, _ = fmt.Fprintf(streams.Stdout, "export PATH=%q:$PATH\n", filepath.Join(root, "bin"))
	return nil
}

func printUsage(w io.Writer) {
	_, _ = io.WriteString(w, "clerm\n")
	_, _ = io.WriteString(w, "\n")
	_, _ = io.WriteString(w, "CLERM compiler, request builder, registry RPC client, local resolver daemon, and benchmark tool.\n")
	_, _ = io.WriteString(w, "\n")
	_, _ = io.WriteString(w, "usage:\n")
	_, _ = io.WriteString(w, "  clerm <schema.clermfile> [out.clermcfg]\n")
	_, _ = io.WriteString(w, "  clerm compile   -in schema.clermfile [-out schema.clermcfg]\n")
	_, _ = io.WriteString(w, "  clerm inspect   -in <schema.clermfile|schema.clermcfg|request.clerm|response.clerm> [-internal]\n")
	_, _ = io.WriteString(w, "  clerm register  -registry http://127.0.0.1:8090 -in schema.clermcfg -owner seller-1\n")
	_, _ = io.WriteString(w, "  clerm search    -registry http://127.0.0.1:8090 -consumer buyer-1 -query books\n")
	_, _ = io.WriteString(w, "  clerm discover  -registry http://127.0.0.1:8090 -consumer buyer-1 -query books\n")
	_, _ = io.WriteString(w, "  clerm relationship establish -registry http://127.0.0.1:8090 -consumer buyer-1 (-fingerprint <registered-fingerprint>|-schema schema.clermcfg) -relation @verified\n")
	_, _ = io.WriteString(w, "  clerm relationship status -registry http://127.0.0.1:8090 -consumer buyer-1 (-fingerprint <registered-fingerprint>|-schema schema.clermcfg)\n")
	_, _ = io.WriteString(w, "  clerm token issue -registry http://127.0.0.1:8090 -consumer buyer-1 (-fingerprint <registered-fingerprint>|-schema schema.clermcfg) (-method @...|-relation @verified)\n")
	_, _ = io.WriteString(w, "  clerm token refresh -registry http://127.0.0.1:8090 -refresh-token <token>\n")
	_, _ = io.WriteString(w, "  clerm token inspect -in capability.token\n")
	_, _ = io.WriteString(w, "  clerm tools     -schema schema.clermcfg -allow @global,@verified\n")
	_, _ = io.WriteString(w, "  clerm request   -schema schema.clermcfg -method @... -data '{...}' [-cap-file capability.token] [-out request.clerm]\n")
	_, _ = io.WriteString(w, "  clerm invoke    -registry http://127.0.0.1:8090 (-fingerprint <registered-fingerprint>|-schema schema.clermcfg) -request request.clerm\n")
	_, _ = io.WriteString(w, "  clerm resolve   -schema schema.clermcfg -request request.clerm [-target registry.discover] [-cap-public-key clerm.ed25519.pub]  # debug\n")
	_, _ = io.WriteString(w, "  clerm serve     -schema schema.clermcfg [-listen 127.0.0.1:8181] [-cap-public-key clerm.ed25519.pub]  # local resolver daemon\n")
	_, _ = io.WriteString(w, "  clerm benchmark -schema schema.clermcfg -method @... -data-file payload [-iterations 100000]\n")
	_, _ = io.WriteString(w, "  clerm shellenv\n")
	_, _ = io.WriteString(w, "\n")
	_, _ = io.WriteString(w, "HTTP headers:\n")
	_, _ = io.WriteString(w, "  Content-Type: application/clerm\n")
	_, _ = io.WriteString(w, "  Clerm-Target: exact operation to execute, for example registry.discover\n")
	_, _ = io.WriteString(w, "\n")
	_, _ = io.WriteString(w, "registry identity:\n")
	_, _ = io.WriteString(w, "  -fingerprint is the primary identifier once a schema is registered\n")
	_, _ = io.WriteString(w, "  -schema is only a local convenience to derive that registered fingerprint; routing still comes only from clerm_registry\n")
	_, _ = io.WriteString(w, "\n")
	_, _ = io.WriteString(w, "examples:\n")
	_, _ = io.WriteString(w, "  make build\n")
	_, _ = io.WriteString(w, "  eval \"$(bin/clerm shellenv)\"\n")
	_, _ = io.WriteString(w, "  clerm compile -in examples/provider_search.clermfile -out schemas/provider_search.clermcfg\n")
	_, _ = io.WriteString(w, "  clerm register -registry http://127.0.0.1:8090 -in schemas/provider_search.clermcfg -owner seller-1\n")
	_, _ = io.WriteString(w, "  clerm token issue -registry http://127.0.0.1:8090 -consumer buyer-1 -schema schemas/provider_search.clermcfg -method @verified.healthcare.book_visit.v1 -out-cap visit.token -out-refresh visit.refresh\n")
	_, _ = io.WriteString(w, "  clerm request -schema schemas/provider_search.clermcfg -method @global.healthcare.search_providers.v1 -data-file examples/search_providers.payload\n")
	_, _ = io.WriteString(w, "  clerm invoke -registry http://127.0.0.1:8090 -schema schemas/provider_search.clermcfg -request search_providers.clerm\n")
}

func loadDocument(path string) (*schema.Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read schema input")
	}
	switch {
	case strings.HasSuffix(path, ".clermfile"):
		return schema.Parse(strings.NewReader(string(data)))
	case clermcfg.IsEncoded(data):
		return clermcfg.Decode(data)
	default:
		return nil, platform.New(platform.CodeValidation, "unsupported schema input; expected .clermfile or .clermcfg")
	}
}

func loadService(path string, publicKeyPath string, keyID string) (*resolver.Service, error) {
	doc, err := loadDocument(path)
	if err != nil {
		return nil, err
	}
	service := resolver.New(doc)
	if strings.TrimSpace(publicKeyPath) != "" {
		publicKey, err := capability.ReadPublicKeyFile(publicKeyPath)
		if err != nil {
			return nil, err
		}
		service.SetCapabilityKeyring(capability.NewKeyring(map[string]ed25519.PublicKey{
			strings.TrimSpace(keyID): publicKey,
		}))
	}
	return service, nil
}

func inspectPath(path string, internal bool) (any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read inspect input")
	}
	if strings.HasSuffix(path, ".clermfile") {
		doc, err := schema.Parse(strings.NewReader(string(data)))
		if err != nil {
			return nil, err
		}
		return map[string]any{"kind": "clermfile", "document": inspectableDocument(doc, internal)}, nil
	}
	if clermcfg.IsEncoded(data) {
		doc, err := clermcfg.Decode(data)
		if err != nil {
			return nil, err
		}
		return map[string]any{"kind": "clermcfg", "document": inspectableDocument(doc, internal)}, nil
	}
	if clermreq.IsEncoded(data) {
		request, err := clermreq.Decode(data)
		if err != nil {
			return nil, err
		}
		return map[string]any{"kind": "clerm", "request": inspectableRequest(request)}, nil
	}
	if clermresp.IsEncoded(data) {
		response, err := clermresp.Decode(data)
		if err != nil {
			return nil, err
		}
		return map[string]any{"kind": "clerm_response", "response": inspectableResponse(response)}, nil
	}
	return nil, platform.New(platform.CodeValidation, "unsupported inspect input")
}

func inspectableDocument(doc *schema.Document, internal bool) any {
	if !internal {
		return doc
	}
	return struct {
		Name          string                `json:"name"`
		RelationsName string                `json:"relations_name"`
		Metadata      schema.Metadata       `json:"metadata,omitempty"`
		Route         string                `json:"route"`
		Services      []schema.ServiceRef   `json:"services"`
		Methods       []schema.Method       `json:"methods"`
		Relations     []schema.RelationRule `json:"relations"`
	}{
		Name:          doc.Name,
		RelationsName: doc.RelationsName,
		Metadata:      doc.Metadata,
		Route:         doc.Route,
		Services:      doc.Services,
		Methods:       doc.Methods,
		Relations:     doc.Relations,
	}
}

func inspectableRequest(request *clermreq.Request) any {
	result := map[string]any{
		"method":    request.Method,
		"arguments": request.Arguments,
	}
	if len(request.CapabilityRaw) > 0 {
		token, err := capability.Decode(request.CapabilityRaw)
		if err == nil {
			result["capability"] = token.InspectView()
		} else {
			result["capability_error"] = err.Error()
		}
	}
	return result
}

func inspectableResponse(response *clermresp.Response) any {
	result := map[string]any{
		"method": response.Method,
	}
	if response.Error.Code != "" || response.Error.Message != "" {
		result["error"] = response.Error
		return result
	}
	result["outputs"] = response.Outputs
	if values, err := response.AsMap(); err == nil {
		result["decoded_outputs"] = values
	}
	return result
}

func parseAllowedSet(raw string) map[string]struct{} {
	allowed := map[string]struct{}{}
	for _, part := range strings.Split(raw, ",") {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		allowed[value] = struct{}{}
	}
	return allowed
}

func splitCSV(raw string) []string {
	values := make([]string, 0)
	for _, part := range strings.Split(raw, ",") {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		values = append(values, value)
	}
	return values
}

func readPayload(inline string, path string) ([]byte, error) {
	if inline != "" && path != "" {
		return nil, platform.New(platform.CodeInvalidArgument, "use either -data or -data-file, not both")
	}
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, platform.Wrap(platform.CodeIO, err, "read payload file")
		}
		return data, nil
	}
	if inline == "" {
		return []byte("{}"), nil
	}
	return []byte(inline), nil
}

func readCapabilityToken(inline string, path string) (*capability.Token, []byte, error) {
	if inline != "" && path != "" {
		return nil, nil, platform.New(platform.CodeInvalidArgument, "use either -cap or -cap-file, not both")
	}
	if inline == "" && path == "" {
		return nil, nil, nil
	}
	raw := inline
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, platform.Wrap(platform.CodeIO, err, "read capability token file")
		}
		raw = string(data)
	}
	token, err := capability.DecodeText(raw)
	if err != nil {
		return nil, nil, err
	}
	encoded, err := capability.Encode(token)
	if err != nil {
		return nil, nil, err
	}
	return token, encoded, nil
}

func validateCapabilityScope(doc *schema.Document, method schema.Method, condition string, token *capability.Token) error {
	if token.Schema != doc.Name {
		return platform.New(platform.CodeValidation, "capability token schema does not match request schema")
	}
	if token.SchemaHash != doc.PublicFingerprint() {
		return platform.New(platform.CodeValidation, "capability token schema fingerprint does not match request schema")
	}
	if token.Condition != condition {
		return platform.New(platform.CodeValidation, "capability token condition does not match method relation")
	}
	if !token.AllowsMethod(method.Reference.Raw, method.Reference.Relation) {
		return platform.New(platform.CodeValidation, "capability token does not allow this method")
	}
	return nil
}

func requiresCapability(condition string) bool {
	return strings.TrimSpace(strings.ToLower(condition)) != "any.protected"
}

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func replaceExt(path string, ext string) string {
	base := strings.TrimSuffix(path, filepath.Ext(path))
	return base + ext
}

func inputJSONSchema(params []schema.Parameter) map[string]any {
	properties := map[string]any{}
	required := make([]string, 0, len(params))
	for _, param := range params {
		properties[param.Name] = jsonSchemaType(param.Type)
		required = append(required, param.Name)
	}
	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
}

func jsonSchemaType(argType schema.ArgType) map[string]any {
	switch argType {
	case schema.ArgString:
		return map[string]any{"type": "string"}
	case schema.ArgDecimal:
		return map[string]any{"type": "number"}
	case schema.ArgUUID:
		return map[string]any{"type": "string", "format": "uuid"}
	case schema.ArgArray:
		return map[string]any{"type": "array"}
	case schema.ArgTimestamp:
		return map[string]any{"type": "string", "format": "date-time"}
	case schema.ArgInt:
		return map[string]any{"type": "integer"}
	case schema.ArgBool:
		return map[string]any{"type": "boolean"}
	default:
		return map[string]any{"type": "string"}
	}
}

func sizeComparison(clermBytes int, jsonBytes int) benchmarkSize {
	saved := jsonBytes - clermBytes
	savedPercent := 0.0
	if jsonBytes > 0 {
		savedPercent = (float64(saved) / float64(jsonBytes)) * 100
	}
	return benchmarkSize{
		CLERMBytes:   clermBytes,
		JSONBytes:    jsonBytes,
		SavedBytes:   saved,
		SavedPercent: savedPercent,
	}
}

func measureNS(iterations int, fn func() error) float64 {
	if iterations <= 0 {
		iterations = 1
	}
	start := time.Now()
	for i := 0; i < iterations; i++ {
		if err := fn(); err != nil {
			return -1
		}
	}
	return float64(time.Since(start).Nanoseconds()) / float64(iterations)
}
