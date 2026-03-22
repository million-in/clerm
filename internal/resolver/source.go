package resolver

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/million-in/clerm/internal/clermcfg"
	"github.com/million-in/clerm/internal/platform"
)

func LoadConfigURL(ctx context.Context, rawURL string, httpClient *http.Client) (*Service, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return nil, platform.New(platform.CodeInvalidArgument, "schema URL is required")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, platform.Wrap(platform.CodeInvalidArgument, err, "parse schema URL")
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, platform.New(platform.CodeInvalidArgument, "schema URL must include scheme and host")
	}
	if httpClient == nil {
		httpClient = defaultHTTPClient()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, trimmed, nil)
	if err != nil {
		return nil, platform.Wrap(platform.CodeInternal, err, "create schema request")
	}
	request.Header.Set("Accept", "application/clermcfg, application/octet-stream")
	response, err := httpClient.Do(request)
	if err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "fetch schema config URL")
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = response.Status
		}
		if response.StatusCode == http.StatusNotFound {
			return nil, platform.New(platform.CodeNotFound, message)
		}
		return nil, platform.New(platform.CodeIO, message)
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read schema config URL payload")
	}
	doc, err := clermcfg.Decode(payload)
	if err != nil {
		return nil, err
	}
	return New(doc), nil
}

func defaultHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   5 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   20,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   5 * time.Second,
			ExpectContinueTimeout: time.Second,
		},
	}
}
