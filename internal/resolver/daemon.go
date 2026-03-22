package resolver

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/million-in/clerm/internal/clermresp"
	"github.com/million-in/clerm/internal/platform"
)

type daemonEncodeRequest struct {
	Method  string          `json:"method"`
	Outputs json.RawMessage `json:"outputs,omitempty"`
	Error   *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type daemonSchemaView struct {
	Name          string `json:"name"`
	Fingerprint   string `json:"fingerprint"`
	MethodCount   int    `json:"method_count"`
	RelationCount int    `json:"relation_count"`
}

func NewDaemonHandler(logger *slog.Logger, service *Service) http.Handler {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/v1/schema", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, daemonSchemaView{
			Name:          service.document.Name,
			Fingerprint:   fingerprintText(service.fingerprint),
			MethodCount:   len(service.document.Methods),
			RelationCount: len(service.document.Relations),
		})
	})
	mux.HandleFunc("/v1/requests/decode", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !isCLERMContentType(r.Header.Get("Content-Type")) {
			http.Error(w, "expected Content-Type: application/clerm", http.StatusUnsupportedMediaType)
			return
		}
		payload, err := service.readPayload(r.Body)
		if err != nil {
			http.Error(w, err.Error(), httpStatus(err))
			return
		}
		command, err := service.ResolveBinaryWithTarget(payload, strings.TrimSpace(r.Header.Get("Clerm-Target")))
		if err != nil {
			platform.LogError(logger, "daemon decode failed", err, "target", strings.TrimSpace(r.Header.Get("Clerm-Target")))
			http.Error(w, err.Error(), httpStatus(err))
			return
		}
		writeJSON(w, command)
	})
	mux.HandleFunc("/v1/responses/encode", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]) != "application/json" {
			http.Error(w, "expected Content-Type: application/json", http.StatusUnsupportedMediaType)
			return
		}
		var input daemonEncodeRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&input); err != nil {
			http.Error(w, platform.Wrap(platform.CodeParse, err, "decode daemon encode request").Error(), http.StatusBadRequest)
			return
		}
		method, ok := service.Method(input.Method)
		if !ok {
			err := platform.New(platform.CodeNotFound, "method not found in compiled config")
			platform.LogError(logger, "daemon encode failed", err, "method", input.Method)
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		var response *clermresp.Response
		var err error
		switch {
		case input.Error != nil:
			response, err = clermresp.BuildError(method, strings.TrimSpace(input.Error.Code), strings.TrimSpace(input.Error.Message))
		case len(bytesOrDefault(input.Outputs, []byte("{}"))) > 0:
			response, err = clermresp.BuildSuccess(method, bytesOrDefault(input.Outputs, []byte("{}")))
		default:
			response, err = clermresp.BuildSuccess(method, []byte("{}"))
		}
		if err != nil {
			platform.LogError(logger, "daemon encode failed", err, "method", input.Method)
			http.Error(w, err.Error(), httpStatus(err))
			return
		}
		w.Header().Set("Content-Type", "application/clerm")
		w.Header().Set("Clerm-Method", method.Reference.Raw)
		w.WriteHeader(http.StatusOK)
		if err := clermresp.WriteTo(w, response); err != nil {
			platform.LogError(logger, "daemon encode failed", err, "method", input.Method)
			return
		}
	})
	return requestLogMiddleware(logger, mux)
}

func requestLogMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(recorder, r)
		logger.Info("clerm resolver daemon request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.statusCode,
			"content_type", strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]),
			"clerm_target", strings.TrimSpace(r.Header.Get("Clerm-Target")),
			"elapsed_ms", time.Since(started).Milliseconds(),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (r *statusRecorder) WriteHeader(statusCode int) {
	r.statusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func bytesOrDefault(value []byte, fallback []byte) []byte {
	if len(value) == 0 {
		return fallback
	}
	return value
}
