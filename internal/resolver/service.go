package resolver

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/million-in/clerm/internal/capability"
	"github.com/million-in/clerm/internal/clermcfg"
	"github.com/million-in/clerm/internal/clermreq"
	"github.com/million-in/clerm/internal/clermresp"
	"github.com/million-in/clerm/internal/clermwire"
	"github.com/million-in/clerm/internal/platform"
	"github.com/million-in/clerm/internal/schema"
)

type Command struct {
	Schema            string         `json:"schema"`
	SchemaFingerprint string         `json:"schema_fingerprint"`
	Target            string         `json:"target"`
	Method            string         `json:"method"`
	Relation          string         `json:"relation"`
	Condition         string         `json:"condition"`
	Execution         string         `json:"execution"`
	OutputFormat      string         `json:"output_format"`
	Arguments         map[string]any `json:"arguments"`
	Capability        any            `json:"capability,omitempty"`
}

// Invocation is created per request and must not be pooled.
// decodeOnce cannot be safely reset for reuse.
type Invocation struct {
	Schema            string
	SchemaFingerprint string
	Target            string
	Method            schema.Method
	Relation          string
	Condition         string
	Execution         string
	OutputFormat      string
	Capability        *capability.Token
	rawArguments      []clermreq.Argument
	decodedArguments  map[string]any
	decodeOnce        sync.Once
	decodeErr         error
}

// ArgumentsView exposes decoded invocation arguments as a read-only view.
// Nested values returned by Lookup should also be treated as read-only.
type ArgumentsView struct {
	values map[string]any
}

type Result struct {
	Outputs      map[string]any      `json:"outputs,omitempty"`
	Response     *clermresp.Response `json:"-"`
	ErrorCode    string              `json:"error_code,omitempty"`
	ErrorMessage string              `json:"error_message,omitempty"`
}

type HandlerFunc func(context.Context, *Invocation) (*Result, error)

type Service struct {
	document           *schema.Document
	fingerprint        [32]byte
	fingerprintString  string
	methods            map[string]schema.Method
	relationConditions map[string]string
	keyring            *capability.Keyring
	replay             capability.ReplayStore
	ownedReplayCloser  io.Closer
	now                func() time.Time
	skew               time.Duration
	maxBodyBytes       int64
	handlerMu          sync.Mutex
	handlers           atomic.Value
}

func LoadConfig(path string) (*Service, error) {
	data, err := readConfigFile(path, maxConfigPayloadBytes)
	if err != nil {
		return nil, err
	}
	doc, err := clermcfg.Decode(data)
	if err != nil {
		return nil, err
	}
	return New(doc), nil
}

func readConfigFile(path string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = maxConfigPayloadBytes
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read compiled config")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read compiled config")
	}
	if info.Size() > maxBytes {
		return nil, platform.New(platform.CodeValidation, "compiled config exceeds configured size limit")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read compiled config")
	}
	if int64(len(data)) > maxBytes {
		return nil, platform.New(platform.CodeValidation, "compiled config exceeds configured size limit")
	}
	return data, nil
}

func New(doc *schema.Document) *Service {
	methods := make(map[string]schema.Method, len(doc.Methods))
	for _, method := range doc.Methods {
		methods[method.Reference.Raw] = method
	}
	relationConditions := make(map[string]string, len(doc.Relations))
	for _, relation := range doc.Relations {
		relationConditions[relation.Name] = relation.Condition
	}
	sum := schema.CachedPublicFingerprint(doc)
	defaultReplayStore := capability.NewMemoryReplayStore()
	service := &Service{
		document:           doc,
		fingerprint:        sum,
		fingerprintString:  fingerprintText(sum),
		methods:            methods,
		relationConditions: relationConditions,
		// Default replay protection is process-local only. Replace this with a
		// distributed ReplayStore in multi-process production deployments.
		replay:            defaultReplayStore,
		ownedReplayCloser: defaultReplayStore,
		now:               time.Now,
		skew:              30 * time.Second,
		maxBodyBytes:      1 << 20,
	}
	service.handlers.Store(map[string]HandlerFunc{})
	return service
}

func (s *Service) Document() *schema.Document {
	return s.document
}

func (s *Service) Fingerprint() [32]byte {
	if s == nil {
		return [32]byte{}
	}
	return s.fingerprint
}

func (s *Service) FingerprintText() string {
	if s == nil {
		return ""
	}
	return s.fingerprintString
}

func (s *Service) SetCapabilityKeyring(keyring *capability.Keyring) {
	s.keyring = keyring
}

func (s *Service) SetReplayStore(store capability.ReplayStore) {
	if s != nil && s.ownedReplayCloser != nil {
		_ = s.ownedReplayCloser.Close()
		s.ownedReplayCloser = nil
	}
	s.replay = store
}

func (s *Service) Close() error {
	if s == nil || s.ownedReplayCloser == nil {
		return nil
	}
	closer := s.ownedReplayCloser
	s.ownedReplayCloser = nil
	return closer.Close()
}

func (s *Service) SetMaxBodyBytes(limit int64) {
	if limit > 0 {
		s.maxBodyBytes = limit
	}
}

func (s *Service) Method(reference string) (schema.Method, bool) {
	method, ok := s.methods[strings.TrimSpace(reference)]
	return method, ok
}

func (s *Service) Bind(methodRef string, handler HandlerFunc) error {
	if handler == nil {
		return platform.New(platform.CodeInvalidArgument, "resolver handler is required")
	}
	methodRef = strings.TrimSpace(methodRef)
	if _, ok := s.methods[methodRef]; !ok {
		return platform.New(platform.CodeNotFound, "method not found in compiled config")
	}
	s.handlerMu.Lock()
	defer s.handlerMu.Unlock()
	current := s.handlerMap()
	next := make(map[string]HandlerFunc, len(current)+1)
	for key, value := range current {
		next[key] = value
	}
	next[methodRef] = handler
	s.handlers.Store(next)
	return nil
}

func (s *Service) Unbind(methodRef string) {
	methodRef = strings.TrimSpace(methodRef)
	s.handlerMu.Lock()
	defer s.handlerMu.Unlock()
	current := s.handlerMap()
	if _, ok := current[methodRef]; !ok {
		return
	}
	next := make(map[string]HandlerFunc, len(current)-1)
	for key, value := range current {
		if key == methodRef {
			continue
		}
		next[key] = value
	}
	s.handlers.Store(next)
}

func (s *Service) ResolveBinary(payload []byte) (*Command, error) {
	return s.ResolveBinaryWithTarget(payload, "")
}

func (s *Service) ResolveBinaryWithTarget(payload []byte, target string) (*Command, error) {
	invocation, err := s.ResolveInvocationWithTarget(payload, target)
	if err != nil {
		return nil, err
	}
	return invocation.Command(), nil
}

func (s *Service) ResolveInvocation(payload []byte) (*Invocation, error) {
	return s.ResolveInvocationWithTarget(payload, "")
}

func (s *Service) ResolveInvocationWithTarget(payload []byte, target string) (*Invocation, error) {
	request, err := clermreq.Decode(payload)
	if err != nil {
		return nil, err
	}
	method, ok := s.methods[request.Method]
	if !ok {
		return nil, platform.New(platform.CodeNotFound, "method not found in compiled config")
	}
	if err := request.ValidateAgainst(method); err != nil {
		return nil, err
	}
	if strings.TrimSpace(target) == "" {
		target = request.Method
	}
	condition := s.relationConditions[method.Reference.Relation]
	token, err := s.resolveCapability(request, method, condition, target)
	if err != nil {
		return nil, err
	}
	return &Invocation{
		Schema:            s.document.Name,
		SchemaFingerprint: s.fingerprintString,
		Target:            target,
		Method:            method,
		Relation:          method.Reference.Relation,
		Condition:         condition,
		Execution:         method.Execution.String(),
		OutputFormat:      method.OutputFormat.String(),
		Capability:        token,
		rawArguments:      request.Arguments,
	}, nil
}

func (s *Service) ExecuteBinary(ctx context.Context, payload []byte, target string) (*clermresp.Response, *Command, error) {
	invocation, err := s.ResolveInvocationWithTarget(payload, target)
	if err != nil {
		return nil, nil, err
	}
	response, err := s.ExecuteInvocation(ctx, invocation)
	return response, invocation.Command(), err
}

func (s *Service) ExecuteInvocation(ctx context.Context, invocation *Invocation) (*clermresp.Response, error) {
	if invocation == nil {
		return nil, platform.New(platform.CodeInvalidArgument, "resolver invocation is required")
	}
	handler, ok := s.handlerMap()[invocation.Method.Reference.Raw]
	if !ok {
		return nil, platform.New(platform.CodeNotFound, "no resolver handler is bound for this method")
	}
	result, err := handler(ctx, invocation)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, platform.New(platform.CodeInternal, "resolver handler returned no result")
	}
	if result.Response != nil {
		if err := result.Response.ValidateAgainst(invocation.Method); err != nil {
			return nil, platform.Wrap(platform.CodeValidation, err, "invalid resolver response")
		}
		return result.Response, nil
	}
	if strings.TrimSpace(result.ErrorCode) != "" || strings.TrimSpace(result.ErrorMessage) != "" {
		return clermresp.BuildError(invocation.Method, strings.TrimSpace(result.ErrorCode), strings.TrimSpace(result.ErrorMessage))
	}
	return clermresp.BuildSuccessMap(invocation.Method, result.Outputs)
}

func Success(outputs map[string]any) *Result {
	return &Result{Outputs: outputs}
}

func SuccessResponse(response *clermresp.Response) *Result {
	return &Result{Response: response}
}

func Failure(code string, message string) *Result {
	return &Result{ErrorCode: strings.TrimSpace(code), ErrorMessage: strings.TrimSpace(message)}
}

func (i *Invocation) Command() *Command {
	if i == nil {
		return nil
	}
	arguments, err := i.Arguments()
	if err != nil {
		arguments = ArgumentsView{}
	}
	var capabilityView any
	if i.Capability != nil {
		capabilityView = i.Capability.InspectView()
	}
	return &Command{
		Schema:            i.Schema,
		SchemaFingerprint: i.SchemaFingerprint,
		Target:            i.Target,
		Method:            i.Method.Reference.Raw,
		Relation:          i.Relation,
		Condition:         i.Condition,
		Execution:         i.Execution,
		OutputFormat:      i.OutputFormat,
		Arguments:         arguments.Clone(),
		Capability:        capabilityView,
	}
}

func (i *Invocation) Arguments() (ArgumentsView, error) {
	decoded, err := i.decodedArgumentMap()
	if err != nil {
		return ArgumentsView{}, err
	}
	return ArgumentsView{values: decoded}, nil
}

func (i *Invocation) ArgumentsMap() (map[string]any, error) {
	view, err := i.Arguments()
	if err != nil {
		return nil, err
	}
	return view.Clone(), nil
}

func (i *Invocation) MarshalArgumentsJSON() ([]byte, error) {
	decoded, err := i.decodedArgumentMap()
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(decoded)
	if err != nil {
		return nil, platform.Wrap(platform.CodeParse, err, "encode invocation arguments")
	}
	return payload, nil
}

func (i *Invocation) MarshalCommandJSON() ([]byte, error) {
	if i == nil {
		return nil, platform.New(platform.CodeInvalidArgument, "resolver invocation is required")
	}
	decoded, err := i.decodedArgumentMap()
	if err != nil {
		return nil, err
	}
	var capabilityView any
	if i.Capability != nil {
		capabilityView = i.Capability.InspectView()
	}
	payload, err := json.Marshal(struct {
		Schema            string         `json:"schema"`
		SchemaFingerprint string         `json:"schema_fingerprint"`
		Target            string         `json:"target"`
		Method            string         `json:"method"`
		Relation          string         `json:"relation"`
		Condition         string         `json:"condition"`
		Execution         string         `json:"execution"`
		OutputFormat      string         `json:"output_format"`
		Arguments         map[string]any `json:"arguments"`
		Capability        any            `json:"capability,omitempty"`
	}{
		Schema:            i.Schema,
		SchemaFingerprint: i.SchemaFingerprint,
		Target:            i.Target,
		Method:            i.Method.Reference.Raw,
		Relation:          i.Relation,
		Condition:         i.Condition,
		Execution:         i.Execution,
		OutputFormat:      i.OutputFormat,
		Arguments:         decoded,
		Capability:        capabilityView,
	})
	if err != nil {
		return nil, platform.Wrap(platform.CodeParse, err, "encode invocation command")
	}
	return payload, nil
}

func (i *Invocation) decodedArgumentMap() (map[string]any, error) {
	if i == nil {
		return nil, platform.New(platform.CodeInvalidArgument, "resolver invocation is required")
	}
	i.decodeOnce.Do(func() {
		decoded := make(map[string]any, len(i.rawArguments))
		for _, arg := range i.rawArguments {
			value, err := clermwire.DecodeValue(arg.Type, arg.Raw)
			if err != nil {
				i.decodeErr = err
				return
			}
			decoded[arg.Name] = value
		}
		i.decodedArguments = decoded
	})
	if i.decodeErr != nil {
		return nil, i.decodeErr
	}
	return i.decodedArguments, nil
}

func (i *Invocation) Argument(name string) (any, bool, error) {
	values, err := i.Arguments()
	if err != nil {
		return nil, false, err
	}
	value, ok := values.Lookup(name)
	return value, ok, nil
}

func (v ArgumentsView) Len() int {
	return len(v.values)
}

func (v ArgumentsView) Lookup(name string) (any, bool) {
	value, ok := v.values[strings.TrimSpace(name)]
	return value, ok
}

func (v ArgumentsView) Clone() map[string]any {
	return cloneArguments(v.values)
}

func (v ArgumentsView) Range(yield func(string, any) bool) {
	for key, value := range v.values {
		if !yield(key, value) {
			return
		}
	}
}

func (i *Invocation) RawArgument(name string) (json.RawMessage, schema.ArgType, bool) {
	if i == nil {
		return nil, schema.ArgUnknown, false
	}
	target := strings.TrimSpace(name)
	for _, arg := range i.rawArguments {
		if arg.Name == target {
			return arg.Raw, arg.Type, true
		}
	}
	return nil, schema.ArgUnknown, false
}

func (s *Service) Handler() http.Handler {
	return s
}

func (s *Service) nowTime() time.Time {
	if s != nil && s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *Service) Middleware(next http.Handler) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isCLERMContentType(r.Header.Get("Content-Type")) {
			next.ServeHTTP(w, r)
			return
		}
		s.ServeHTTP(w, r)
	})
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !isCLERMContentType(r.Header.Get("Content-Type")) {
		http.Error(w, "expected Content-Type: application/clerm", http.StatusUnsupportedMediaType)
		return
	}
	payload, err := s.readPayload(r.Body)
	if err != nil {
		http.Error(w, err.Error(), httpStatus(err))
		return
	}
	invocation, err := s.ResolveInvocationWithTarget(payload, strings.TrimSpace(r.Header.Get("Clerm-Target")))
	if err != nil {
		http.Error(w, err.Error(), httpStatus(err))
		return
	}
	response, execErr := s.ExecuteInvocation(r.Context(), invocation)
	if execErr != nil {
		response = buildExecutionErrorResponse(invocation, execErr)
		w.Header().Set("Clerm-Error-Code", string(platform.CodeOf(execErr)))
	}
	w.Header().Set("Content-Type", "application/clerm")
	w.Header().Set("Clerm-Method", invocation.Method.Reference.Raw)
	w.Header().Set("Clerm-Target", invocation.Target)
	w.WriteHeader(http.StatusOK)
	if err := clermresp.WriteTo(w, response); err != nil {
		return
	}
}

func (s *Service) readPayload(body io.ReadCloser) ([]byte, error) {
	defer body.Close()
	// Read one byte past the configured limit so oversize payloads can be
	// rejected deterministically without trusting Content-Length.
	reader := io.LimitReader(body, s.maxBodyBytes+1)
	payload, err := io.ReadAll(reader)
	if err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read CLERM payload")
	}
	if int64(len(payload)) > s.maxBodyBytes {
		return nil, platform.New(platform.CodeValidation, "CLERM payload exceeds configured body limit")
	}
	return payload, nil
}

func (s *Service) resolveCapability(request *clermreq.Request, method schema.Method, condition string, target string) (*capability.Token, error) {
	if len(request.CapabilityRaw) == 0 {
		if requiresCapability(condition) {
			return nil, platform.New(platform.CodeValidation, "capability token is required for this relation")
		}
		return nil, nil
	}
	if s.keyring == nil {
		return nil, platform.New(platform.CodeValidation, "capability token verification is not configured")
	}
	token, err := capability.Decode(request.CapabilityRaw)
	if err != nil {
		return nil, err
	}
	if err := s.keyring.Verify(token); err != nil {
		return nil, err
	}
	now := s.nowTime()
	if err := capability.VerifyTime(token, now, s.skew); err != nil {
		return nil, err
	}
	if token.Schema != s.document.Name {
		return nil, platform.New(platform.CodeValidation, "capability token schema does not match compiled config")
	}
	if token.SchemaHash != s.fingerprint {
		return nil, platform.New(platform.CodeValidation, "capability token schema fingerprint does not match compiled config")
	}
	if token.Condition != condition {
		return nil, platform.New(platform.CodeValidation, "capability token condition does not match schema relation")
	}
	if !token.AllowsMethod(method.Reference.Raw, method.Reference.Relation) {
		return nil, platform.New(platform.CodeValidation, "capability token does not allow this method")
	}
	if !token.AllowsTarget(target) {
		return nil, platform.New(platform.CodeValidation, "capability token does not allow this target")
	}
	if s.replay == nil {
		return nil, platform.New(platform.CodeValidation, "capability token replay protection is not configured")
	}
	if err := s.replay.Reserve(token.TokenID, token.TTL(now)); err != nil {
		return nil, err
	}
	return token, nil
}

func requiresCapability(condition string) bool {
	return strings.TrimSpace(strings.ToLower(condition)) != "any.protected"
}

func isCLERMContentType(value string) bool {
	return strings.TrimSpace(strings.Split(value, ";")[0]) == "application/clerm"
}

func errorMessage(err error) string {
	if coded := platform.As(err); coded != nil && strings.TrimSpace(coded.Message) != "" {
		return strings.TrimSpace(coded.Message)
	}
	return strings.TrimSpace(err.Error())
}

func cloneArguments(values map[string]any) map[string]any {
	if len(values) == 0 {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func buildExecutionErrorResponse(invocation *Invocation, execErr error) *clermresp.Response {
	method := ""
	if invocation != nil {
		method = strings.TrimSpace(invocation.Method.Reference.Raw)
	}
	response, err := clermresp.BuildError(schema.Method{
		Reference: schema.ServiceRef{Raw: method},
	}, string(platform.CodeOf(execErr)), errorMessage(execErr))
	if err == nil {
		return response
	}
	return &clermresp.Response{
		Method: method,
		Error: clermresp.ErrorBody{
			Code:    string(platform.CodeOf(execErr)),
			Message: errorMessage(execErr),
		},
	}
}

func httpStatus(err error) int {
	switch platform.CodeOf(err) {
	case platform.CodeNotFound:
		return http.StatusNotFound
	case platform.CodeValidation, platform.CodeParse, platform.CodeInvalidArgument:
		return http.StatusBadRequest
	case platform.CodeIO:
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func (s *Service) handlerMap() map[string]HandlerFunc {
	current, _ := s.handlers.Load().(map[string]HandlerFunc)
	if current == nil {
		return map[string]HandlerFunc{}
	}
	return current
}

func fingerprintText(sum [32]byte) string {
	return hex.EncodeToString(sum[:])
}
