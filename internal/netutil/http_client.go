package netutil

import (
	"net"
	"net/http"
	"time"
)

const (
	defaultClientTimeout         = 15 * time.Second
	defaultDialTimeout           = 5 * time.Second
	defaultKeepAlive             = 30 * time.Second
	defaultMaxIdleConns          = 100
	defaultMaxIdleConnsPerHost   = 20
	defaultIdleConnTimeout       = 90 * time.Second
	defaultTLSHandshakeTimeout   = 5 * time.Second
	defaultExpectContinueTimeout = time.Second
)

type HTTPClientOptions struct {
	Timeout             time.Duration
	MaxIdleConns        int
	MaxIdleConnsPerHost int
}

func NewDefaultHTTPClient(options HTTPClientOptions) *http.Client {
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = defaultClientTimeout
	}
	maxIdleConns := options.MaxIdleConns
	if maxIdleConns <= 0 {
		maxIdleConns = defaultMaxIdleConns
	}
	maxIdleConnsPerHost := options.MaxIdleConnsPerHost
	if maxIdleConnsPerHost <= 0 {
		maxIdleConnsPerHost = defaultMaxIdleConnsPerHost
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   defaultDialTimeout,
				KeepAlive: defaultKeepAlive,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          maxIdleConns,
			MaxIdleConnsPerHost:   maxIdleConnsPerHost,
			IdleConnTimeout:       defaultIdleConnTimeout,
			TLSHandshakeTimeout:   defaultTLSHandshakeTimeout,
			ExpectContinueTimeout: defaultExpectContinueTimeout,
		},
	}
}
