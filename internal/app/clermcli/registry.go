package clermcli

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/million-in/clerm/internal/clermcfg"
	"github.com/million-in/clerm/internal/platform"
	"github.com/million-in/clerm/registryrpc"
)

const defaultRegistryBaseURL = "http://127.0.0.1:8090"

const (
	defaultRegistryRequestTimeout = 30 * time.Second
	defaultInvokeInlineLimit      = 1 << 20
)

type invokeResultView struct {
	StatusCode    int                 `json:"status_code"`
	Headers       map[string][]string `json:"headers,omitempty"`
	Target        string              `json:"target,omitempty"`
	CommandMethod string              `json:"command_method,omitempty"`
	BodyBytes     int                 `json:"body_bytes,omitempty"`
	BodyFile      string              `json:"body_file,omitempty"`
	BodyText      string              `json:"body_text,omitempty"`
	BodyBase64    string              `json:"body_base64,omitempty"`
}

type RegistryClient interface {
	Register(context.Context, registryrpc.RegisterInput) (*registryrpc.RegisterOutput, error)
	Search(context.Context, registryrpc.SearchInput) (*registryrpc.SearchOutput, error)
	Discover(context.Context, registryrpc.SearchInput) (*registryrpc.SearchOutput, error)
	EstablishRelationship(context.Context, registryrpc.RelationshipInput) (*registryrpc.RelationshipOutput, error)
	RelationshipStatus(context.Context, registryrpc.RelationshipStatusInput) (*registryrpc.RelationshipStatusOutput, error)
	IssueToken(context.Context, registryrpc.IssueTokenInput) (*registryrpc.IssueTokenOutput, error)
	RefreshToken(context.Context, registryrpc.RefreshTokenInput) (*registryrpc.IssueTokenOutput, error)
	Invoke(context.Context, registryrpc.InvokeInput) (*registryrpc.InvokeOutput, error)
}

var registryClientFactory = func(baseURL string) (RegistryClient, error) {
	return registryrpc.New(strings.TrimSpace(baseURL), nil)
}

func SetRegistryClientFactoryForTest(factory func(string) (RegistryClient, error)) func() {
	previous := registryClientFactory
	if factory == nil {
		registryClientFactory = func(baseURL string) (RegistryClient, error) {
			return registryrpc.New(strings.TrimSpace(baseURL), nil)
		}
	} else {
		registryClientFactory = factory
	}
	return func() {
		registryClientFactory = previous
	}
}

type tokenCommandView struct {
	CapabilityToken   string `json:"capability_token"`
	ExpiresAt         string `json:"expires_at"`
	RefreshToken      string `json:"refresh_token"`
	RefreshExpiresAt  string `json:"refresh_expires_at"`
	Relation          string `json:"relation"`
	Condition         string `json:"condition"`
	CapabilityOutFile string `json:"capability_out_file,omitempty"`
	RefreshOutFile    string `json:"refresh_out_file,omitempty"`
}

func runRegister(streams Streams, args []string) error {
	fs := flag.NewFlagSet("register", flag.ContinueOnError)
	fs.SetOutput(streams.Stderr)
	registryURL := fs.String("registry", defaultRegistryURL(), "registry base URL")
	timeout := registryTimeoutFlag(fs)
	in := fs.String("in", "", "path to .clermfile or .clermcfg")
	ownerID := fs.String("owner", "", "schema owner identifier")
	status := fs.String("status", "active", "schema status")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *in == "" || strings.TrimSpace(*ownerID) == "" {
		return platform.New(platform.CodeInvalidArgument, "register requires -in and -owner")
	}
	payload, err := compileConfigPayload(*in)
	if err != nil {
		return err
	}
	client, err := newRegistryClient(*registryURL)
	if err != nil {
		return err
	}
	ctx, cancel := newRegistryContext(*timeout)
	defer cancel()
	output, err := client.Register(ctx, registryrpc.RegisterInput{
		OwnerID: strings.TrimSpace(*ownerID),
		Status:  strings.TrimSpace(*status),
		Payload: payload,
	})
	if err != nil {
		return err
	}
	return writeJSON(streams.Stdout, output)
}

func runSearch(streams Streams, args []string) error {
	return runSearchCommand(streams, "search", args)
}

func runDiscover(streams Streams, args []string) error {
	return runSearchCommand(streams, "discover", args)
}

func runSearchCommand(streams Streams, target string, args []string) error {
	fs := flag.NewFlagSet(target, flag.ContinueOnError)
	fs.SetOutput(streams.Stderr)
	registryURL := fs.String("registry", defaultRegistryURL(), "registry base URL")
	timeout := registryTimeoutFlag(fs)
	consumerID := fs.String("consumer", "", "consumer identifier")
	query := fs.String("query", "", "search text")
	relations := fs.String("relations", "", "comma-separated relation filters")
	categories := fs.String("categories", "", "comma-separated category filters")
	tags := fs.String("tags", "", "comma-separated tag filters")
	limit := fs.Int("limit", 20, "maximum number of results")
	offset := fs.Int("offset", 0, "result offset")
	if err := fs.Parse(args); err != nil {
		return err
	}
	client, err := newRegistryClient(*registryURL)
	if err != nil {
		return err
	}
	ctx, cancel := newRegistryContext(*timeout)
	defer cancel()
	input := registryrpc.SearchInput{
		ConsumerID: strings.TrimSpace(*consumerID),
		Query:      strings.TrimSpace(*query),
		Relations:  splitCSV(*relations),
		Categories: splitCSV(*categories),
		Tags:       splitCSV(*tags),
		Limit:      *limit,
		Offset:     *offset,
	}
	var output *registryrpc.SearchOutput
	switch target {
	case "discover":
		output, err = client.Discover(ctx, input)
	default:
		output, err = client.Search(ctx, input)
	}
	if err != nil {
		return err
	}
	return writeJSON(streams.Stdout, output)
}

func runRelationship(streams Streams, args []string) error {
	if len(args) == 0 {
		return platform.New(platform.CodeInvalidArgument, "relationship requires a subcommand: establish or status")
	}
	switch args[0] {
	case "establish":
		return runRelationshipEstablish(streams, args[1:])
	case "status":
		return runRelationshipStatus(streams, args[1:])
	default:
		return platform.New(platform.CodeInvalidArgument, "invalid relationship subcommand")
	}
}

func runRelationshipEstablish(streams Streams, args []string) error {
	fs := flag.NewFlagSet("relationship establish", flag.ContinueOnError)
	fs.SetOutput(streams.Stderr)
	registryURL := fs.String("registry", defaultRegistryURL(), "registry base URL")
	timeout := registryTimeoutFlag(fs)
	consumerID := fs.String("consumer", "", "consumer identifier")
	schemaPath := fs.String("schema", "", "path to .clermfile or .clermcfg")
	fingerprint := fs.String("fingerprint", "", "registered schema fingerprint")
	relation := fs.String("relation", "", "relation name to establish")
	status := fs.String("status", "active", "relationship status")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*consumerID) == "" || strings.TrimSpace(*relation) == "" {
		return platform.New(platform.CodeInvalidArgument, "relationship establish requires -consumer and -relation")
	}
	providerFingerprint, err := resolveProviderFingerprint(*schemaPath, *fingerprint)
	if err != nil {
		return err
	}
	client, err := newRegistryClient(*registryURL)
	if err != nil {
		return err
	}
	ctx, cancel := newRegistryContext(*timeout)
	defer cancel()
	output, err := client.EstablishRelationship(ctx, registryrpc.RelationshipInput{
		ConsumerID:          strings.TrimSpace(*consumerID),
		ProviderFingerprint: providerFingerprint,
		Relation:            strings.TrimSpace(*relation),
		Status:              strings.TrimSpace(*status),
	})
	if err != nil {
		return err
	}
	return writeJSON(streams.Stdout, output)
}

func runRelationshipStatus(streams Streams, args []string) error {
	fs := flag.NewFlagSet("relationship status", flag.ContinueOnError)
	fs.SetOutput(streams.Stderr)
	registryURL := fs.String("registry", defaultRegistryURL(), "registry base URL")
	timeout := registryTimeoutFlag(fs)
	consumerID := fs.String("consumer", "", "consumer identifier")
	schemaPath := fs.String("schema", "", "path to .clermfile or .clermcfg")
	fingerprint := fs.String("fingerprint", "", "registered schema fingerprint")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*consumerID) == "" {
		return platform.New(platform.CodeInvalidArgument, "relationship status requires -consumer")
	}
	providerFingerprint, err := resolveProviderFingerprint(*schemaPath, *fingerprint)
	if err != nil {
		return err
	}
	client, err := newRegistryClient(*registryURL)
	if err != nil {
		return err
	}
	ctx, cancel := newRegistryContext(*timeout)
	defer cancel()
	output, err := client.RelationshipStatus(ctx, registryrpc.RelationshipStatusInput{
		ConsumerID:          strings.TrimSpace(*consumerID),
		ProviderFingerprint: providerFingerprint,
	})
	if err != nil {
		return err
	}
	return writeJSON(streams.Stdout, output)
}

func runTokenIssueRPC(streams Streams, args []string) error {
	fs := flag.NewFlagSet("token issue", flag.ContinueOnError)
	fs.SetOutput(streams.Stderr)
	registryURL := fs.String("registry", defaultRegistryURL(), "registry base URL")
	timeout := registryTimeoutFlag(fs)
	consumerID := fs.String("consumer", "", "consumer identifier")
	schemaPath := fs.String("schema", "", "path to .clermfile or .clermcfg")
	fingerprint := fs.String("fingerprint", "", "registered schema fingerprint")
	methodRef := fs.String("method", "", "method reference to scope the token to")
	relation := fs.String("relation", "", "relation to scope the token to")
	subject := fs.String("subject", "", "optional subject override")
	targets := fs.String("targets", "", "comma-separated exact targets")
	invokeTTL := fs.Duration("invoke-ttl", 30*time.Minute, "invoke token lifetime")
	refreshTTL := fs.Duration("refresh-ttl", 7*24*time.Hour, "refresh token lifetime")
	outCap := fs.String("out-cap", "", "optional path to write capability token")
	outRefresh := fs.String("out-refresh", "", "optional path to write refresh token")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*consumerID) == "" {
		return platform.New(platform.CodeInvalidArgument, "token issue requires -consumer")
	}
	providerFingerprint, err := resolveProviderFingerprint(*schemaPath, *fingerprint)
	if err != nil {
		return err
	}
	client, err := newRegistryClient(*registryURL)
	if err != nil {
		return err
	}
	ctx, cancel := newRegistryContext(*timeout)
	defer cancel()
	output, err := client.IssueToken(ctx, registryrpc.IssueTokenInput{
		ConsumerID:          strings.TrimSpace(*consumerID),
		ProviderFingerprint: providerFingerprint,
		Method:              strings.TrimSpace(*methodRef),
		Relation:            strings.TrimSpace(*relation),
		Subject:             strings.TrimSpace(*subject),
		Targets:             splitCSV(*targets),
		InvokeTTLSeconds:    durationSeconds(*invokeTTL),
		RefreshTTLSeconds:   durationSeconds(*refreshTTL),
	})
	if err != nil {
		return err
	}
	view, err := writeTokenOutputs(output, *outCap, *outRefresh)
	if err != nil {
		return err
	}
	return writeJSON(streams.Stdout, view)
}

func runTokenRefresh(streams Streams, args []string) error {
	fs := flag.NewFlagSet("token refresh", flag.ContinueOnError)
	fs.SetOutput(streams.Stderr)
	registryURL := fs.String("registry", defaultRegistryURL(), "registry base URL")
	timeout := registryTimeoutFlag(fs)
	refreshToken := fs.String("refresh-token", "", "refresh token value")
	targets := fs.String("targets", "", "comma-separated exact targets")
	invokeTTL := fs.Duration("invoke-ttl", 30*time.Minute, "invoke token lifetime")
	refreshTTL := fs.Duration("refresh-ttl", 7*24*time.Hour, "refresh token lifetime")
	outCap := fs.String("out-cap", "", "optional path to write capability token")
	outRefresh := fs.String("out-refresh", "", "optional path to write refresh token")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*refreshToken) == "" {
		return platform.New(platform.CodeInvalidArgument, "token refresh requires -refresh-token")
	}
	client, err := newRegistryClient(*registryURL)
	if err != nil {
		return err
	}
	ctx, cancel := newRegistryContext(*timeout)
	defer cancel()
	output, err := client.RefreshToken(ctx, registryrpc.RefreshTokenInput{
		RefreshToken:      strings.TrimSpace(*refreshToken),
		Targets:           splitCSV(*targets),
		InvokeTTLSeconds:  durationSeconds(*invokeTTL),
		RefreshTTLSeconds: durationSeconds(*refreshTTL),
	})
	if err != nil {
		return err
	}
	view, err := writeTokenOutputs(output, *outCap, *outRefresh)
	if err != nil {
		return err
	}
	return writeJSON(streams.Stdout, view)
}

func runInvoke(streams Streams, args []string) error {
	fs := flag.NewFlagSet("invoke", flag.ContinueOnError)
	fs.SetOutput(streams.Stderr)
	registryURL := fs.String("registry", defaultRegistryURL(), "registry base URL")
	timeout := registryTimeoutFlag(fs)
	requestPath := fs.String("request", "", "path to .clerm request payload")
	schemaPath := fs.String("schema", "", "path to .clermfile or .clermcfg")
	fingerprint := fs.String("fingerprint", "", "registered schema fingerprint")
	out := fs.String("out", "", "optional path to write raw upstream body")
	inlineLimit := fs.Int("inline-limit", defaultInvokeInlineLimit, "maximum body bytes to inline into JSON output when -out is omitted")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*requestPath) == "" {
		return platform.New(platform.CodeInvalidArgument, "invoke requires -request")
	}
	payload, err := os.ReadFile(*requestPath)
	if err != nil {
		return platform.Wrap(platform.CodeIO, err, "read request file")
	}
	providerFingerprint, err := resolveProviderFingerprint(*schemaPath, *fingerprint)
	if err != nil {
		return err
	}
	client, err := newRegistryClient(*registryURL)
	if err != nil {
		return err
	}
	ctx, cancel := newRegistryContext(*timeout)
	defer cancel()
	output, err := client.Invoke(ctx, registryrpc.InvokeInput{
		ProviderFingerprint: providerFingerprint,
		Payload:             payload,
	})
	if err != nil {
		return err
	}
	view, err := buildInvokeView(output, *out, *inlineLimit)
	if err != nil {
		return err
	}
	return writeJSON(streams.Stdout, view)
}

func newRegistryClient(baseURL string) (RegistryClient, error) {
	return registryClientFactory(baseURL)
}

func defaultRegistryURL() string {
	value := strings.TrimSpace(os.Getenv("CLERM_REGISTRY_URL"))
	if value == "" {
		return defaultRegistryBaseURL
	}
	return value
}

func registryTimeoutFlag(fs *flag.FlagSet) *time.Duration {
	return fs.Duration("timeout", defaultRegistryRequestTimeout, "per-request registry timeout (0 disables)")
}

func newRegistryContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(context.Background())
	}
	return context.WithTimeout(context.Background(), timeout)
}

func resolveProviderFingerprint(schemaPath string, fingerprint string) (string, error) {
	if strings.TrimSpace(fingerprint) != "" {
		return strings.TrimSpace(fingerprint), nil
	}
	if strings.TrimSpace(schemaPath) == "" {
		return "", platform.New(platform.CodeInvalidArgument, "either -schema or -fingerprint is required")
	}
	payload, err := compileConfigPayload(schemaPath)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return fmt.Sprintf("%x", sum[:]), nil
}

func compileConfigPayload(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read schema input")
	}
	switch {
	case strings.HasSuffix(path, ".clermfile"):
		doc, err := loadDocument(path)
		if err != nil {
			return nil, err
		}
		encoded, err := clermcfg.Encode(doc)
		if err != nil {
			return nil, err
		}
		return encoded, nil
	case clermcfg.IsEncoded(data):
		return data, nil
	default:
		return nil, platform.New(platform.CodeValidation, "unsupported schema input; expected .clermfile or .clermcfg")
	}
}

func writeTokenOutputs(output *registryrpc.IssueTokenOutput, capPath string, refreshPath string) (*tokenCommandView, error) {
	if output == nil {
		return nil, platform.New(platform.CodeInternal, "token output is required")
	}
	view := &tokenCommandView{
		CapabilityToken:  output.CapabilityToken,
		ExpiresAt:        output.ExpiresAt,
		RefreshToken:     output.RefreshToken,
		RefreshExpiresAt: output.RefreshExpires,
		Relation:         output.Relation,
		Condition:        output.Condition,
	}
	if strings.TrimSpace(capPath) != "" {
		if err := os.WriteFile(capPath, []byte(output.CapabilityToken+"\n"), 0o600); err != nil {
			return nil, platform.Wrap(platform.CodeIO, err, "write capability token")
		}
		view.CapabilityOutFile = capPath
	}
	if strings.TrimSpace(refreshPath) != "" {
		if err := os.WriteFile(refreshPath, []byte(output.RefreshToken+"\n"), 0o600); err != nil {
			return nil, platform.Wrap(platform.CodeIO, err, "write refresh token")
		}
		view.RefreshOutFile = refreshPath
	}
	return view, nil
}

func buildInvokeView(output *registryrpc.InvokeOutput, outPath string, inlineLimit int) (*invokeResultView, error) {
	view := &invokeResultView{
		StatusCode:    output.StatusCode,
		Headers:       output.Headers,
		Target:        output.Target,
		CommandMethod: output.CommandMethod,
		BodyBytes:     len(output.Body),
	}
	if strings.TrimSpace(outPath) != "" {
		if err := os.WriteFile(outPath, output.Body, 0o600); err != nil {
			return nil, platform.Wrap(platform.CodeIO, err, "write invoke response body")
		}
		view.BodyFile = outPath
		return view, nil
	}
	if inlineLimit > 0 && len(output.Body) > inlineLimit {
		return nil, platform.New(platform.CodeValidation, "invoke response body exceeds inline limit; use -out or raise -inline-limit")
	}
	if utf8.Valid(output.Body) {
		view.BodyText = string(output.Body)
		return view, nil
	}
	view.BodyBase64 = base64.RawStdEncoding.EncodeToString(output.Body)
	return view, nil
}

func durationSeconds(value time.Duration) int64 {
	if value <= 0 {
		return 0
	}
	return int64(value / time.Second)
}
