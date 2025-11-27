package Utils

import (
	"crypto/tls"
	"net/http"
	"net/url"
	"os"
)

func GetProxyTransport() *http.Transport {
	transport := &http.Transport{
		TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{},
	}
	if proxyURL := os.Getenv("PROXY_URL"); proxyURL != "" {
		if proxy, err := url.Parse(proxyURL); err == nil {
			transport.Proxy = http.ProxyURL(proxy)
		}
	}
	return transport
}
