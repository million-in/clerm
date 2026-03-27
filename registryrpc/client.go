package registryrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/million-in/clerm/internal/netutil"
	"github.com/million-in/clerm/platform"
)

const (
	defaultJSONResponseBytes   int64 = 8 << 20
	defaultInvokeResponseBytes int64 = 8 << 20
	maxErrorResponseBytes      int64 = 4 << 10
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func New(baseURL string, httpClient *http.Client) (*Client, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmed == "" {
		return nil, platform.New(platform.CodeInvalidArgument, "registry base URL is required")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, platform.Wrap(platform.CodeInvalidArgument, err, "parse registry base URL")
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, platform.New(platform.CodeInvalidArgument, "registry base URL must include scheme and host")
	}
	if !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		return nil, platform.New(platform.CodeInvalidArgument, "registry base URL must use http or https")
	}
	if parsed.User != nil {
		return nil, platform.New(platform.CodeInvalidArgument, "registry base URL must not include embedded credentials")
	}
	if httpClient == nil {
		httpClient = defaultHTTPClient()
	}
	return &Client{baseURL: trimmed, httpClient: httpClient}, nil
}

func (c *Client) Register(ctx context.Context, input RegisterInput) (*RegisterOutput, error) {
	if strings.TrimSpace(input.OwnerID) == "" {
		return nil, platform.New(platform.CodeInvalidArgument, "owner_id is required")
	}
	if len(input.Payload) == 0 {
		return nil, platform.New(platform.CodeInvalidArgument, "compiled schema payload is required")
	}
	headers := http.Header{}
	headers.Set("Clerm-Owner", strings.TrimSpace(input.OwnerID))
	if status := strings.TrimSpace(input.Status); status != "" {
		headers.Set("Clerm-Status", status)
	}
	resp, err := c.doBytes(ctx, "registry.register", "application/clermcfg", headers, input.Payload)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var output RegisterOutput
	if err := decodeJSONResponse(resp, &output); err != nil {
		return nil, err
	}
	return &output, nil
}

func (c *Client) Search(ctx context.Context, input SearchInput) (*SearchOutput, error) {
	var output SearchOutput
	if err := c.doJSON(ctx, "registry.search", input, &output); err != nil {
		return nil, err
	}
	return &output, nil
}

func (c *Client) Discover(ctx context.Context, input SearchInput) (*SearchOutput, error) {
	var output SearchOutput
	if err := c.doJSON(ctx, "registry.discover", input, &output); err != nil {
		return nil, err
	}
	return &output, nil
}

func (c *Client) EstablishRelationship(ctx context.Context, input RelationshipInput) (*RelationshipOutput, error) {
	var output RelationshipOutput
	if err := c.doJSON(ctx, "registry.relationship.establish", input, &output); err != nil {
		return nil, err
	}
	return &output, nil
}

func (c *Client) RelationshipStatus(ctx context.Context, input RelationshipStatusInput) (*RelationshipStatusOutput, error) {
	var output RelationshipStatusOutput
	if err := c.doJSON(ctx, "registry.relationship.status", input, &output); err != nil {
		return nil, err
	}
	return &output, nil
}

func (c *Client) IssueToken(ctx context.Context, input IssueTokenInput) (*IssueTokenOutput, error) {
	var output IssueTokenOutput
	if err := c.doJSON(ctx, "registry.token.issue", input, &output); err != nil {
		return nil, err
	}
	return &output, nil
}

func (c *Client) RefreshToken(ctx context.Context, input RefreshTokenInput) (*IssueTokenOutput, error) {
	var output IssueTokenOutput
	if err := c.doJSON(ctx, "registry.token.refresh", input, &output); err != nil {
		return nil, err
	}
	return &output, nil
}

func (c *Client) Invoke(ctx context.Context, input InvokeInput) (*InvokeOutput, error) {
	if strings.TrimSpace(input.ProviderFingerprint) == "" {
		return nil, platform.New(platform.CodeInvalidArgument, "provider fingerprint is required")
	}
	if len(input.Payload) == 0 {
		return nil, platform.New(platform.CodeInvalidArgument, "request payload is required")
	}
	headers := http.Header{}
	headers.Set("Clerm-Schema-Fingerprint", strings.TrimSpace(input.ProviderFingerprint))
	resp, err := c.doBytes(ctx, "registry.invoke", "application/clerm", headers, input.Payload)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := readLimitedBody(resp.Body, normalizeResponseLimit(input.MaxResponseBytes, defaultInvokeResponseBytes), "registry invoke response")
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 && strings.TrimSpace(resp.Header.Get("Clerm-Target")) != "registry.invoke" {
		return nil, responseError(resp, body)
	}
	return &InvokeOutput{
		StatusCode:    resp.StatusCode,
		Headers:       resp.Header.Clone(),
		Body:          body,
		Target:        strings.TrimSpace(resp.Header.Get("Clerm-Target")),
		CommandMethod: strings.TrimSpace(resp.Header.Get("Clerm-Command-Method")),
	}, nil
}

func (c *Client) doJSON(ctx context.Context, target string, input any, output any) error {
	body, err := json.Marshal(input)
	if err != nil {
		return platform.Wrap(platform.CodeInternal, err, "encode registry request")
	}
	resp, err := c.doBytes(ctx, target, "application/json", nil, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decodeJSONResponse(resp, output)
}

func (c *Client) doBytes(ctx context.Context, target string, contentType string, headers http.Header, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/rpc", bytes.NewReader(body))
	if err != nil {
		return nil, platform.Wrap(platform.CodeInternal, err, "create registry request")
	}
	req.Header.Set("Clerm-Target", strings.TrimSpace(target))
	if strings.TrimSpace(contentType) != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "perform registry request")
	}
	if resp.StatusCode < 400 {
		return resp, nil
	}
	if strings.TrimSpace(target) == "registry.invoke" {
		return resp, nil
	}
	bodyBytes, readErr := readLimitedBody(resp.Body, maxErrorResponseBytes, "registry error response")
	resp.Body.Close()
	if readErr != nil {
		return nil, readErr
	}
	return nil, responseError(resp, bodyBytes)
}

func decodeJSONResponse(resp *http.Response, output any) error {
	if output == nil {
		_, err := io.Copy(io.Discard, resp.Body)
		return err
	}
	body, err := readLimitedBody(resp.Body, defaultJSONResponseBytes, "registry response")
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, output); err != nil {
		return platform.Wrap(platform.CodeParse, err, "decode registry response")
	}
	return nil
}

func responseError(resp *http.Response, body []byte) error {
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = resp.Status
	}
	code := codeFromStatus(resp.StatusCode, message)
	return platform.New(code, trimCodePrefix(message))
}

func codeFromStatus(statusCode int, message string) platform.Code {
	switch {
	case strings.HasPrefix(message, string(platform.CodeValidation)+":"):
		return platform.CodeValidation
	case strings.HasPrefix(message, string(platform.CodeParse)+":"):
		return platform.CodeParse
	case strings.HasPrefix(message, string(platform.CodeNotFound)+":"):
		return platform.CodeNotFound
	case strings.HasPrefix(message, string(platform.CodeIO)+":"):
		return platform.CodeIO
	case strings.HasPrefix(message, string(platform.CodeInternal)+":"):
		return platform.CodeInternal
	}
	switch statusCode {
	case http.StatusNotFound:
		return platform.CodeNotFound
	case http.StatusBadRequest:
		return platform.CodeInvalidArgument
	default:
		if statusCode >= 500 {
			return platform.CodeIO
		}
		return platform.CodeInvalidArgument
	}
}

func trimCodePrefix(message string) string {
	trimmed := strings.TrimSpace(message)
	for _, code := range []platform.Code{
		platform.CodeInvalidArgument,
		platform.CodeParse,
		platform.CodeValidation,
		platform.CodeIO,
		platform.CodeNotFound,
		platform.CodeInternal,
	} {
		prefix := string(code) + ":"
		if strings.HasPrefix(trimmed, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
		}
	}
	return trimmed
}

func defaultHTTPClient() *http.Client {
	return netutil.NewDefaultHTTPClient(netutil.HTTPClientOptions{
		MaxIdleConns:        128,
		MaxIdleConnsPerHost: 128,
	})
}

func normalizeResponseLimit(limit int64, fallback int64) int64 {
	if limit <= 0 {
		return fallback
	}
	return limit
}

func readLimitedBody(body io.Reader, maxBytes int64, label string) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, platform.New(platform.CodeInvalidArgument, label+" limit is invalid")
	}
	data, err := io.ReadAll(io.LimitReader(body, maxBytes+1))
	if err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read "+label)
	}
	if int64(len(data)) > maxBytes {
		return nil, platform.New(platform.CodeValidation, label+" exceeds configured size limit")
	}
	return data, nil
}
