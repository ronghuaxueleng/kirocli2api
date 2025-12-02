package Utils

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/proxy"
)

func GetProxyTransport() *http.Transport {
	transport := &http.Transport{
		TLSNextProto:      map[string]func(string, *tls.Conn) http.RoundTripper{},
		ForceAttemptHTTP2: false,
		DialTLSContext:    makeUTLSDialer(),
	}
	if proxyURL := os.Getenv("PROXY_URL"); proxyURL != "" {
		if p, err := url.Parse(proxyURL); err == nil {
			transport.Proxy = http.ProxyURL(p)
		}
	}
	return transport
}

func makeUTLSDialer() func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			host = addr
		}

		var netConn net.Conn
		if proxyURL := os.Getenv("PROXY_URL"); proxyURL != "" {
			p, err := url.Parse(proxyURL)
			if err != nil {
				return nil, err
			}
			dialer, err := proxy.FromURL(p, proxy.Direct)
			if err != nil {
				return nil, err
			}
			netConn, err = dialer.Dial(network, addr)
			if err != nil {
				return nil, err
			}
		} else {
			d := &net.Dialer{Timeout: 30 * time.Second}
			netConn, err = d.DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
		}

		tlsConn := utls.UClient(netConn, &utls.Config{ServerName: host}, utls.HelloChrome_Auto)
		if err := tlsConn.Handshake(); err != nil {
			netConn.Close()
			return nil, err
		}
		return tlsConn, nil
	}
}

func GetHTTPClient() *http.Client {
	return &http.Client{
		Transport: GetProxyTransport(),
		Timeout:   30 * time.Second,
	}
}
