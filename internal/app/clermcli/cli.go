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
	"strings"
	"time"

	"github.com/million-in/clerm"
	"github.com/million-in/clerm/internal/capability"
	"github.com/million-in/clerm/internal/platform"
	"github.com/million-in/clerm/resolver"
	"github.com/million-in/clerm/schema"
)

const defaultServeListenAddress = "127.0.0.1:8181"

type Streams struct {
	Stdout io.Writer
	Stderr io.Writer
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
	result, err := clerm.Compiler.WriteCompiledConfig(*in, *out)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(streams.Stdout, "%s\n", result.OutputPath)
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
	result, err := clerm.Compiler.InspectPath(*in, clerm.InspectOptions{IncludeInternal: *internal})
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
	payload, err := readPayload(*dataInline, *dataFile)
	if err != nil {
		return err
	}
	_, encodedToken, err := readCapabilityToken(*capInline, *capFile)
	if err != nil {
		return err
	}
	encoded, err := clerm.Compiler.EncodeRequest(doc, clerm.BuildRequestInput{
		MethodReference:  *methodRef,
		AllowedRelations: splitCSV(*allow),
		PayloadJSON:      payload,
		CapabilityRaw:    encodedToken,
	})
	if err != nil {
		return err
	}
	if *out == "" {
		*out = encoded.Method.Reference.Method + ".clerm"
	}
	if err := os.WriteFile(*out, encoded.Payload, 0o600); err != nil {
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
	defer service.Close()
	payload, err := os.ReadFile(*requestPath)
	if err != nil {
		return platform.Wrap(platform.CodeIO, err, "read request file")
	}
	command, err := clerm.Resolver.ResolveBinary(service, payload, *target)
	if err != nil {
		return err
	}
	return writeJSON(streams.Stdout, command)
}

func runServe(logger *slog.Logger, streams Streams, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(streams.Stderr)
	schemaPath := fs.String("schema", "", "path to .clermfile or .clermcfg")
	listen := fs.String("listen", defaultServeListenAddress, "listen address")
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
	defer service.Close()
	server := &http.Server{
		Addr:              *listen,
		Handler:           clerm.Resolver.NewDaemonHandler(logger, service),
		ReadTimeout:       15 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	if logger != nil {
		logger.Info("starting clerm resolver daemon", "listen", *listen, "schema", service.Document().Name, "schema_fingerprint", service.FingerprintText())
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
	tools, err := clerm.Compiler.Tools(doc, splitCSV(*allow))
	if err != nil {
		return err
	}
	return writeJSON(streams.Stdout, tools)
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
	payload, err := readPayload(*dataInline, *dataFile)
	if err != nil {
		return err
	}
	report, err := clerm.Compiler.Benchmark(doc, clerm.BenchmarkInput{
		MethodReference: *methodRef,
		PayloadJSON:     payload,
		Iterations:      *iterations,
	})
	if err != nil {
		return err
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
	return clerm.Compiler.LoadDocument(path)
}

func loadService(path string, publicKeyPath string, keyID string) (*resolver.Service, error) {
	options := clerm.ServiceOptions{}
	if strings.TrimSpace(publicKeyPath) != "" {
		publicKey, err := capability.ReadPublicKeyFile(publicKeyPath)
		if err != nil {
			return nil, err
		}
		options.CapabilityKeyring = capability.NewKeyring(map[string]ed25519.PublicKey{
			strings.TrimSpace(keyID): publicKey,
		})
	}
	return clerm.Resolver.LoadService(path, options)
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

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
