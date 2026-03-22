package resolver

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/million-in/clerm/internal/clermcfg"
	"github.com/million-in/clerm/internal/netutil"
	"github.com/million-in/clerm/internal/platform"
)

const maxConfigPayloadBytes int64 = 8 << 20

type URLPolicy func(context.Context, *url.URL) error

type LoadConfigURLOptions struct {
	HTTPClient      *http.Client
	URLPolicy       URLPolicy
	MaxPayloadBytes int64
}

func LoadConfigURL(ctx context.Context, rawURL string, httpClient *http.Client) (*Service, error) {
	return LoadConfigURLWithOptions(ctx, rawURL, LoadConfigURLOptions{HTTPClient: httpClient})
}

func LoadConfigURLWithOptions(ctx context.Context, rawURL string, options LoadConfigURLOptions) (*Service, error) {
	if ctx == nil {
		ctx = context.Background()
	}
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
	if !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		return nil, platform.New(platform.CodeInvalidArgument, "schema URL must use http or https")
	}
	if options.URLPolicy != nil {
		if err := options.URLPolicy(ctx, parsed); err != nil {
			return nil, err
		}
	}
	httpClient := options.HTTPClient
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
	payload, err := readConfigPayload(response, options.MaxPayloadBytes)
	if err != nil {
		return nil, err
	}
	doc, err := clermcfg.Decode(payload)
	if err != nil {
		return nil, err
	}
	return New(doc), nil
}

func defaultHTTPClient() *http.Client {
	return netutil.NewDefaultHTTPClient(netutil.HTTPClientOptions{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
	})
}

func readConfigPayload(response *http.Response, maxPayloadBytes int64) ([]byte, error) {
	if maxPayloadBytes <= 0 {
		maxPayloadBytes = maxConfigPayloadBytes
	}
	if response.ContentLength > maxPayloadBytes {
		return nil, platform.New(platform.CodeValidation, "schema config URL payload exceeds configured size limit")
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxPayloadBytes+1))
	if err != nil {
		return nil, platform.Wrap(platform.CodeIO, err, "read schema config URL payload")
	}
	if int64(len(payload)) > maxPayloadBytes {
		return nil, platform.New(platform.CodeValidation, "schema config URL payload exceeds configured size limit")
	}
	return payload, nil
}

func DenyPrivateHostPolicy(ctx context.Context, rawURL *url.URL) error {
	host := strings.ToLower(strings.TrimSpace(rawURL.Hostname()))
	switch {
	case host == "":
		return platform.New(platform.CodeInvalidArgument, "schema URL host is required")
	case host == "localhost", strings.HasSuffix(host, ".localhost"):
		return platform.New(platform.CodeInvalidArgument, "schema URL host is not allowed")
	}
	ip := net.ParseIP(host)
	if ip != nil {
		if isBlockedIP(ip) {
			return platform.New(platform.CodeInvalidArgument, "schema URL host is not allowed")
		}
		return nil
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return platform.Wrap(platform.CodeIO, err, "resolve schema URL host")
	}
	if len(addresses) == 0 {
		return platform.New(platform.CodeIO, "schema URL host could not be resolved")
	}
	for _, address := range addresses {
		if isBlockedIP(address.IP) {
			return platform.New(platform.CodeInvalidArgument, "schema URL host is not allowed")
		}
	}
	return nil
}

func isBlockedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	return ip.IsGlobalUnicast() && inCIDR(ip, "100.64.0.0/10")
}

func inCIDR(ip net.IP, cidr string) bool {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return false
	}
	return network.Contains(ip)
}
