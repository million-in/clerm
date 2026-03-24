package resolver

import (
	"context"
	"crypto/tls"
	"errors"
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

var lookupIPAddr = net.DefaultResolver.LookupIPAddr

var blockedIPNetworks = mustParseCIDRs(
	"0.0.0.0/8",
	"100.64.0.0/10",
	"192.0.2.0/24",
	"198.18.0.0/15",
	"198.51.100.0/24",
	"203.0.113.0/24",
	"240.0.0.0/4",
	"2001:db8::/32",
)

type URLPolicy func(context.Context, *url.URL) error

type LoadConfigURLOptions struct {
	HTTPClient      *http.Client
	URLPolicy       URLPolicy
	MaxPayloadBytes int64
	LookupIPAddr    func(context.Context, string) ([]net.IPAddr, error)
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
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = defaultHTTPClient()
	}
	httpClient = clientWithURLPolicy(httpClient, options.URLPolicy, resolveLookupIPAddr(options.LookupIPAddr))
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

func clientWithURLPolicy(httpClient *http.Client, policy URLPolicy, lookup func(context.Context, string) ([]net.IPAddr, error)) *http.Client {
	if policy == nil {
		return httpClient
	}
	cloned := *httpClient
	previousCheckRedirect := cloned.CheckRedirect
	cloned.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if previousCheckRedirect != nil {
			if err := previousCheckRedirect(req, via); err != nil {
				return err
			}
		} else if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return policy(req.Context(), req.URL)
	}
	cloned.Transport = roundTripperWithURLPolicy(cloned.Transport, policy, lookup)
	return &cloned
}

func resolveLookupIPAddr(lookup func(context.Context, string) ([]net.IPAddr, error)) func(context.Context, string) ([]net.IPAddr, error) {
	if lookup != nil {
		return lookup
	}
	return lookupIPAddr
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

// DenyPrivateHostPolicy rejects localhost-style hosts and blocked literal IPs.
// When used with LoadConfigURLWithOptions, resolved addresses are validated
// on the wrapped transport using the same resolution that is pinned for dialing.
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
	for _, address := range resolvedIPsFromContext(ctx) {
		if isBlockedIP(address) {
			return platform.New(platform.CodeInvalidArgument, "schema URL host is not allowed")
		}
	}
	return nil
}

func isBlockedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	return containsBlockedIP(ip)
}

func containsBlockedIP(ip net.IP) bool {
	for _, network := range blockedIPNetworks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func mustParseCIDRs(cidrs ...string) []*net.IPNet {
	networks := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(err)
		}
		networks = append(networks, network)
	}
	return networks
}

type pinnedResolution struct {
	host string
	port string
	ips  []net.IP
}

type pinnedResolutionContextKey struct{}
type resolvedIPsContextKey struct{}

type urlPolicyRoundTripper struct {
	base      http.RoundTripper
	policy    URLPolicy
	lookup    func(context.Context, string) ([]net.IPAddr, error)
	enablePin bool
}

func roundTripperWithURLPolicy(base http.RoundTripper, policy URLPolicy, lookup func(context.Context, string) ([]net.IPAddr, error)) http.RoundTripper {
	base, supportsPinnedResolution := transportWithPinnedDial(base)
	return urlPolicyRoundTripper{
		base:      base,
		policy:    policy,
		lookup:    resolveLookupIPAddr(lookup),
		enablePin: supportsPinnedResolution,
	}
}

func (rt urlPolicyRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil || request.URL == nil {
		return rt.base.RoundTrip(request)
	}
	ctx := request.Context()
	if rt.enablePin {
		resolution, err := resolvePinnedResolution(ctx, request.URL, rt.lookup)
		if err != nil {
			return nil, platform.Wrap(platform.CodeIO, err, "resolve schema URL host")
		}
		ctx = context.WithValue(ctx, pinnedResolutionContextKey{}, resolution)
		ctx = context.WithValue(ctx, resolvedIPsContextKey{}, append([]net.IP(nil), resolution.ips...))
	}
	request = request.Clone(ctx)
	if rt.policy != nil {
		if err := rt.policy(request.Context(), request.URL); err != nil {
			return nil, err
		}
	}
	return rt.base.RoundTrip(request)
}

func transportWithPinnedDial(base http.RoundTripper) (http.RoundTripper, bool) {
	if base == nil {
		base = http.DefaultTransport
	}
	transport, ok := base.(*http.Transport)
	if !ok {
		return base, false
	}
	cloned := transport.Clone()
	baseDialContext := cloned.DialContext
	if baseDialContext == nil {
		baseDialContext = (&net.Dialer{}).DialContext
	}
	cloned.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return dialPinnedContext(ctx, network, address, baseDialContext)
	}
	if cloned.DialTLSContext != nil {
		previousDialTLSContext := cloned.DialTLSContext
		cloned.DialTLSContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			resolution, ok := pinnedResolutionFromContext(ctx)
			if !ok || !matchesPinnedAddress(address, resolution.host, resolution.port) {
				return previousDialTLSContext(ctx, network, address)
			}
			return dialPinnedTLSContext(ctx, network, resolution, cloned.TLSClientConfig)
		}
	}
	return cloned, true
}

func dialPinnedContext(ctx context.Context, network, address string, dial func(context.Context, string, string) (net.Conn, error)) (net.Conn, error) {
	resolution, ok := pinnedResolutionFromContext(ctx)
	if !ok || !matchesPinnedAddress(address, resolution.host, resolution.port) {
		return dial(ctx, network, address)
	}
	return dialPinnedAddresses(ctx, network, resolution, dial)
}

func dialPinnedTLSContext(ctx context.Context, network string, resolution pinnedResolution, config *tls.Config) (net.Conn, error) {
	conn, err := dialPinnedAddresses(ctx, network, resolution, (&net.Dialer{}).DialContext)
	if err != nil {
		return nil, err
	}
	tlsConfig := &tls.Config{ServerName: resolution.host}
	if config != nil {
		tlsConfig = config.Clone()
		if tlsConfig.ServerName == "" {
			tlsConfig.ServerName = resolution.host
		}
	}
	tlsConn := tls.Client(conn, tlsConfig)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		conn.Close()
		return nil, err
	}
	return tlsConn, nil
}

func dialPinnedAddresses(ctx context.Context, network string, resolution pinnedResolution, dial func(context.Context, string, string) (net.Conn, error)) (net.Conn, error) {
	var lastErr error
	for _, ip := range resolution.ips {
		conn, err := dial(ctx, network, net.JoinHostPort(ip.String(), resolution.port))
		if err != nil {
			lastErr = err
			continue
		}
		if remoteIP := remoteConnIP(conn); remoteIP != nil && !remoteIP.Equal(ip) {
			conn.Close()
			lastErr = errors.New("resolved schema URL host dialed unexpected remote address")
			continue
		}
		return conn, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("schema URL host could not be resolved")
}

func resolvePinnedResolution(ctx context.Context, rawURL *url.URL, lookup func(context.Context, string) ([]net.IPAddr, error)) (pinnedResolution, error) {
	host := strings.TrimSpace(rawURL.Hostname())
	if host == "" {
		return pinnedResolution{}, errors.New("schema URL host is required")
	}
	port := rawURL.Port()
	if port == "" {
		switch strings.ToLower(rawURL.Scheme) {
		case "https":
			port = "443"
		default:
			port = "80"
		}
	}
	if ip := net.ParseIP(host); ip != nil {
		return pinnedResolution{host: host, port: port, ips: []net.IP{ip}}, nil
	}
	addresses, err := resolveLookupIPAddr(lookup)(ctx, host)
	if err != nil {
		return pinnedResolution{}, err
	}
	if len(addresses) == 0 {
		return pinnedResolution{}, errors.New("schema URL host could not be resolved")
	}
	ips := make([]net.IP, 0, len(addresses))
	for _, address := range addresses {
		if address.IP != nil {
			ips = append(ips, address.IP)
		}
	}
	if len(ips) == 0 {
		return pinnedResolution{}, errors.New("schema URL host could not be resolved")
	}
	return pinnedResolution{host: host, port: port, ips: ips}, nil
}

func pinnedResolutionFromContext(ctx context.Context) (pinnedResolution, bool) {
	if ctx == nil {
		return pinnedResolution{}, false
	}
	resolution, ok := ctx.Value(pinnedResolutionContextKey{}).(pinnedResolution)
	return resolution, ok
}

func resolvedIPsFromContext(ctx context.Context) []net.IP {
	if ctx == nil {
		return nil
	}
	addresses, _ := ctx.Value(resolvedIPsContextKey{}).([]net.IP)
	return addresses
}

func matchesPinnedAddress(address, host, port string) bool {
	dialHost, dialPort, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	return strings.EqualFold(dialHost, host) && dialPort == port
}

func remoteConnIP(conn net.Conn) net.IP {
	if conn == nil || conn.RemoteAddr() == nil {
		return nil
	}
	host, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		return nil
	}
	return net.ParseIP(host)
}
