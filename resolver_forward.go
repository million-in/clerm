package clerm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/million-in/clerm/clermresp"
	"github.com/million-in/clerm/internal/netutil"
	"github.com/million-in/clerm/platform"
	resolverpkg "github.com/million-in/clerm/resolver"
	"github.com/million-in/clerm/schema"
)

type HTTPPayloadMode string

const (
	HTTPPayloadCommand   HTTPPayloadMode = "command"
	HTTPPayloadArguments HTTPPayloadMode = "arguments"
)

const (
	defaultUpstreamResponseBytes int64 = 8 << 20
	maxUpstreamErrorBytes        int64 = 4 << 10
)

type RESTRoute struct {
	URL              string      `json:"url"`
	Method           string      `json:"method,omitempty"`
	Headers          http.Header `json:"headers,omitempty"`
	BodyMode         HTTPPayloadMode
	OutputPath       string `json:"output_path,omitempty"`
	MaxResponseBytes int64  `json:"max_response_bytes,omitempty"`
}

type GraphQLRoute struct {
	URL              string      `json:"url"`
	Query            string      `json:"query"`
	OperationName    string      `json:"operation_name,omitempty"`
	Headers          http.Header `json:"headers,omitempty"`
	VariablesMode    HTTPPayloadMode
	VariableName     string `json:"variable_name,omitempty"`
	DataPath         string `json:"data_path,omitempty"`
	MaxResponseBytes int64  `json:"max_response_bytes,omitempty"`
}

type ExecutedBinary struct {
	Command  *resolverpkg.Command `json:"command,omitempty"`
	Response *clermresp.Response  `json:"response,omitempty"`
	Payload  []byte               `json:"payload,omitempty"`
}

func (ResolverAPI) ResolveInvocation(service *resolverpkg.Service, payload []byte, target string) (*resolverpkg.Invocation, error) {
	if service == nil {
		return nil, platform.New(platform.CodeInvalidArgument, "resolver service is required")
	}
	return service.ResolveInvocationWithTarget(payload, strings.TrimSpace(target))
}

func (ResolverAPI) ExecuteBinary(ctx context.Context, service *resolverpkg.Service, payload []byte, target string) (*ExecutedBinary, error) {
	invocation, err := Resolver.ResolveInvocation(service, payload, target)
	if err != nil {
		return nil, err
	}
	response, execErr := service.ExecuteInvocation(ctx, invocation)
	if execErr != nil {
		response = buildExecutionErrorResponse(invocation, execErr)
	}
	encoded, err := clermresp.Encode(response)
	if err != nil {
		return nil, err
	}
	result := &ExecutedBinary{
		Command:  invocation.Command(),
		Response: response,
		Payload:  encoded,
	}
	if execErr != nil {
		return result, execErr
	}
	return result, nil
}

func (ResolverAPI) BindREST(service *resolverpkg.Service, methodRef string, client *http.Client, route RESTRoute) error {
	if service == nil {
		return platform.New(platform.CodeInvalidArgument, "resolver service is required")
	}
	routeURL, err := validateUpstreamURL(route.URL)
	if err != nil {
		return err
	}
	method := normalizeHTTPMethod(route.Method)
	bodyMode, err := normalizePayloadMode(route.BodyMode, HTTPPayloadCommand)
	if err != nil {
		return err
	}
	responseLimit := normalizeResponseLimit(route.MaxResponseBytes)
	headers := cloneHeader(route.Headers)
	client = httpClientOrDefault(client)
	return service.Bind(methodRef, func(ctx context.Context, invocation *resolverpkg.Invocation) (*resolverpkg.Result, error) {
		payload, err := marshalInvocationPayload(invocation, bodyMode)
		if err != nil {
			return nil, err
		}
		responseBody, err := sendJSONRequest(ctx, client, method, routeURL, headers, invocation, payload, responseLimit)
		if err != nil {
			return nil, err
		}
		outputsJSON, err := extractResponseJSON(responseBody.Body, route.OutputPath, "rest")
		if err != nil {
			return nil, err
		}
		response, err := clermresp.BuildSuccess(invocation.Method, outputsJSON)
		if err != nil {
			return nil, err
		}
		return resolverpkg.SuccessResponse(response), nil
	})
}

func (ResolverAPI) BindGraphQL(service *resolverpkg.Service, methodRef string, client *http.Client, route GraphQLRoute) error {
	if service == nil {
		return platform.New(platform.CodeInvalidArgument, "resolver service is required")
	}
	routeURL, err := validateUpstreamURL(route.URL)
	if err != nil {
		return err
	}
	if strings.TrimSpace(route.Query) == "" {
		return platform.New(platform.CodeInvalidArgument, "graphql query is required")
	}
	variablesMode, err := normalizePayloadMode(route.VariablesMode, HTTPPayloadArguments)
	if err != nil {
		return err
	}
	responseLimit := normalizeResponseLimit(route.MaxResponseBytes)
	headers := cloneHeader(route.Headers)
	client = httpClientOrDefault(client)
	return service.Bind(methodRef, func(ctx context.Context, invocation *resolverpkg.Invocation) (*resolverpkg.Result, error) {
		variables, err := graphqlVariables(invocation, variablesMode, route.VariableName)
		if err != nil {
			return nil, err
		}
		payload, err := marshalGraphQLRequest(route.Query, route.OperationName, variables)
		if err != nil {
			return nil, err
		}
		responseBody, err := sendJSONRequest(ctx, client, http.MethodPost, routeURL, headers, invocation, payload, responseLimit)
		if err != nil {
			return nil, err
		}
		outputsJSON, err := extractGraphQLResponseJSON(responseBody.Body, route.DataPath)
		if err != nil {
			return nil, err
		}
		response, err := clermresp.BuildSuccess(invocation.Method, outputsJSON)
		if err != nil {
			return nil, err
		}
		return resolverpkg.SuccessResponse(response), nil
	})
}

type upstreamHTTPResponse struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

func sendJSONRequest(ctx context.Context, client *http.Client, method string, rawURL string, headers http.Header, invocation *resolverpkg.Invocation, payload []byte, responseLimit int64) (*upstreamHTTPResponse, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, bytes.NewReader(payload))
	if err != nil {
		return nil, platform.Wrap(platform.CodeInternal, err, "create upstream request")
	}
	req.Header = cloneHeader(headers)
	if strings.TrimSpace(req.Header.Get("Content-Type")) == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if strings.TrimSpace(req.Header.Get("Accept")) == "" {
		req.Header.Set("Accept", "application/json")
	}
	setInvocationHeaders(req.Header, invocation)
	resp, err := client.Do(req)
	if err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "perform upstream request")
	}
	defer resp.Body.Close()
	body, err := readLimitedBody(resp.Body, responseLimit, "upstream response body")
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, platform.New(platform.CodeIO, upstreamErrorMessage(resp.Status, body))
	}
	return &upstreamHTTPResponse{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header.Clone(),
		Body:       body,
	}, nil
}

func marshalInvocationPayload(invocation *resolverpkg.Invocation, mode HTTPPayloadMode) ([]byte, error) {
	switch mode {
	case HTTPPayloadArguments:
		return invocation.MarshalArgumentsJSON()
	case HTTPPayloadCommand:
		return invocation.MarshalCommandJSON()
	default:
		return nil, platform.New(platform.CodeInvalidArgument, "unsupported http payload mode")
	}
}

func graphqlVariables(invocation *resolverpkg.Invocation, mode HTTPPayloadMode, variableName string) (json.RawMessage, error) {
	trimmedVariable := strings.TrimSpace(variableName)
	payload, err := marshalInvocationPayload(invocation, mode)
	if err != nil {
		return nil, err
	}
	if trimmedVariable == "" {
		return payload, nil
	}
	wrapped, err := json.Marshal(map[string]json.RawMessage{trimmedVariable: payload})
	if err != nil {
		return nil, platform.Wrap(platform.CodeParse, err, "encode graphql variables")
	}
	return wrapped, nil
}

func extractResponseJSON(body []byte, path string, label string) ([]byte, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return []byte("{}"), nil
	}
	if !json.Valid(trimmed) {
		return nil, platform.New(platform.CodeParse, fmt.Sprintf("%s upstream response must be valid JSON", label))
	}
	if strings.TrimSpace(path) == "" {
		return trimmed, nil
	}
	var decoded any
	if err := json.Unmarshal(trimmed, &decoded); err != nil {
		return nil, platform.Wrap(platform.CodeParse, err, "decode upstream json response")
	}
	value, err := extractJSONValuePath(decoded, path)
	if err != nil {
		return nil, err
	}
	outputs, err := json.Marshal(value)
	if err != nil {
		return nil, platform.Wrap(platform.CodeParse, err, "encode extracted upstream json response")
	}
	return outputs, nil
}

func extractGraphQLResponseJSON(body []byte, dataPath string) ([]byte, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, platform.New(platform.CodeParse, "graphql upstream response must not be empty")
	}
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors json.RawMessage `json:"errors,omitempty"`
	}
	if err := json.Unmarshal(trimmed, &envelope); err != nil {
		return nil, platform.Wrap(platform.CodeParse, err, "decode graphql response")
	}
	if hasGraphQLErrors(envelope.Errors) {
		return nil, platform.New(platform.CodeIO, graphQLErrorMessage(envelope.Errors))
	}
	return extractResponseJSON(envelope.Data, dataPath, "graphql")
}

func hasGraphQLErrors(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || bytes.Equal(trimmed, []byte("[]")) {
		return false
	}
	return true
}

func graphQLErrorMessage(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return "graphql upstream returned errors"
	}
	var values []struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(trimmed, &values); err == nil {
		for _, value := range values {
			if strings.TrimSpace(value.Message) != "" {
				return value.Message
			}
		}
	}
	return strings.TrimSpace(string(trimmed))
}

func normalizeHTTPMethod(raw string) string {
	method := strings.ToUpper(strings.TrimSpace(raw))
	if method == "" {
		return http.MethodPost
	}
	return method
}

func normalizePayloadMode(raw HTTPPayloadMode, fallback HTTPPayloadMode) (HTTPPayloadMode, error) {
	value := HTTPPayloadMode(strings.TrimSpace(strings.ToLower(string(raw))))
	if value == "" {
		return fallback, nil
	}
	switch value {
	case HTTPPayloadCommand, HTTPPayloadArguments:
		return value, nil
	default:
		return "", platform.New(platform.CodeInvalidArgument, "unsupported http payload mode")
	}
}

func cloneHeader(headers http.Header) http.Header {
	if headers == nil {
		return make(http.Header)
	}
	return headers.Clone()
}

func httpClientOrDefault(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return netutil.NewDefaultHTTPClient(netutil.HTTPClientOptions{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
	})
}

func setInvocationHeaders(headers http.Header, invocation *resolverpkg.Invocation) {
	if invocation == nil {
		return
	}
	headers.Set("Clerm-Schema", invocation.Schema)
	headers.Set("Clerm-Schema-Fingerprint", invocation.SchemaFingerprint)
	headers.Set("Clerm-Method", invocation.Method.Reference.Raw)
	headers.Set("Clerm-Relation", invocation.Relation)
	headers.Set("Clerm-Condition", invocation.Condition)
	headers.Set("Clerm-Execution", invocation.Execution)
	headers.Set("Clerm-Output-Format", invocation.OutputFormat)
	headers.Set("Clerm-Target", invocation.Target)
}

func upstreamErrorMessage(status string, body []byte) string {
	if int64(len(body)) > maxUpstreamErrorBytes {
		body = body[:maxUpstreamErrorBytes]
	}
	message := strings.TrimSpace(strings.ToValidUTF8(string(body), "?"))
	if message == "" {
		return status
	}
	return status + ": " + message
}

func validateUpstreamURL(rawURL string) (string, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return "", platform.New(platform.CodeInvalidArgument, "upstream url is required")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", platform.Wrap(platform.CodeInvalidArgument, err, "parse upstream url")
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", platform.New(platform.CodeInvalidArgument, "upstream url must include scheme and host")
	}
	if !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		return "", platform.New(platform.CodeInvalidArgument, "upstream url must use http or https")
	}
	if parsed.User != nil {
		return "", platform.New(platform.CodeInvalidArgument, "upstream url must not include embedded credentials")
	}
	return parsed.String(), nil
}

func normalizeResponseLimit(limit int64) int64 {
	if limit <= 0 {
		return defaultUpstreamResponseBytes
	}
	return limit
}

func readLimitedBody(body io.Reader, maxBytes int64, label string) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = defaultUpstreamResponseBytes
	}
	payload, err := io.ReadAll(io.LimitReader(body, maxBytes+1))
	if err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read "+label)
	}
	if int64(len(payload)) > maxBytes {
		return nil, platform.New(platform.CodeIO, label+" exceeds configured size limit")
	}
	return payload, nil
}

func marshalGraphQLRequest(query string, operationName string, variables json.RawMessage) ([]byte, error) {
	payload, err := json.Marshal(struct {
		Query         string          `json:"query"`
		OperationName string          `json:"operationName,omitempty"`
		Variables     json.RawMessage `json:"variables"`
	}{
		Query:         query,
		OperationName: strings.TrimSpace(operationName),
		Variables:     variables,
	})
	if err != nil {
		return nil, platform.Wrap(platform.CodeParse, err, "encode graphql request")
	}
	return payload, nil
}

func buildExecutionErrorResponse(invocation *resolverpkg.Invocation, execErr error) *clermresp.Response {
	method := schema.Method{}
	if invocation != nil {
		method = invocation.Method
	}
	message := strings.TrimSpace(execErr.Error())
	if coded := platform.As(execErr); coded != nil && strings.TrimSpace(coded.Message) != "" {
		message = strings.TrimSpace(coded.Message)
	}
	response, err := clermresp.BuildError(method, string(platform.CodeOf(execErr)), message)
	if err == nil {
		return response
	}
	return &clermresp.Response{
		Method: strings.TrimSpace(method.Reference.Raw),
		Error: clermresp.ErrorBody{
			Code:    string(platform.CodeOf(execErr)),
			Message: message,
		},
	}
}
