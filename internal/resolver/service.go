package resolver

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/million-in/clerm/internal/capability"
	"github.com/million-in/clerm/internal/clermcfg"
	"github.com/million-in/clerm/internal/clermreq"
	"github.com/million-in/clerm/internal/platform"
	"github.com/million-in/clerm/internal/schema"
)

type Command struct {
	Schema       string         `json:"schema"`
	Target       string         `json:"target"`
	Method       string         `json:"method"`
	Relation     string         `json:"relation"`
	Condition    string         `json:"condition"`
	Execution    string         `json:"execution"`
	OutputFormat string         `json:"output_format"`
	Arguments    map[string]any `json:"arguments"`
	Capability   any            `json:"capability,omitempty"`
}

type Service struct {
	document           *schema.Document
	methods            map[string]schema.Method
	relationConditions map[string]string
	keyring            *capability.Keyring
	replay             capability.ReplayStore
	now                func() time.Time
	skew               time.Duration
}

func LoadConfig(path string) (*Service, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read compiled config")
	}
	doc, err := clermcfg.Decode(data)
	if err != nil {
		return nil, err
	}
	return New(doc), nil
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
	return &Service{
		document:           doc,
		methods:            methods,
		relationConditions: relationConditions,
		replay:             capability.NewMemoryReplayStore(),
		now:                time.Now,
		skew:               30 * time.Second,
	}
}

func (s *Service) Document() *schema.Document {
	return s.document
}

func (s *Service) SetCapabilityKeyring(keyring *capability.Keyring) {
	s.keyring = keyring
}

func (s *Service) SetReplayStore(store capability.ReplayStore) {
	s.replay = store
}

func (s *Service) ResolveBinary(payload []byte) (*Command, error) {
	return s.ResolveBinaryWithTarget(payload, "")
}

func (s *Service) ResolveBinaryWithTarget(payload []byte, target string) (*Command, error) {
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
	args, err := request.AsMap()
	if err != nil {
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
	var capabilityView any
	if token != nil {
		view := token.InspectView()
		capabilityView = view
	}
	return &Command{
		Schema:       s.document.Name,
		Target:       target,
		Method:       method.Reference.Raw,
		Relation:     method.Reference.Relation,
		Condition:    condition,
		Execution:    method.Execution.String(),
		OutputFormat: method.OutputFormat.String(),
		Arguments:    args,
		Capability:   capabilityView,
	}, nil
}

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/resolve", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		contentType := strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0])
		if contentType != "application/clerm" {
			http.Error(w, "expected Content-Type: application/clerm", http.StatusUnsupportedMediaType)
			return
		}
		payload, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		target := strings.TrimSpace(r.Header.Get("Clerm-Target"))
		command, err := s.ResolveBinaryWithTarget(payload, target)
		if err != nil {
			status := http.StatusBadRequest
			if platform.IsCode(err, platform.CodeNotFound) {
				status = http.StatusNotFound
			}
			http.Error(w, err.Error(), status)
			return
		}
		w.Header().Set("Clerm-Target", command.Target)
		respondJSON(w, http.StatusOK, command)
	})
	return mux
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
	now := time.Now()
	if s.now != nil {
		now = s.now()
	}
	if err := capability.VerifyTime(token, now, s.skew); err != nil {
		return nil, err
	}
	if token.Schema != s.document.Name {
		return nil, platform.New(platform.CodeValidation, "capability token schema does not match compiled config")
	}
	if token.SchemaHash != s.document.PublicFingerprint() {
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

func respondJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
