package reader

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"time"
)

type FetchResult struct {
	Feed           ParsedFeed
	ETag, Modified string
	Unchanged      bool
	NotBefore      time.Time
}
type Fetcher struct{ Client *http.Client }

// Dial the validated IP itself, preventing a second DNS lookup from rebinding
// the request to a private network. Redirects use this same transport.
func NewFetcher() *Fetcher {
	t := &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS12,
		// Some publishers challenge Go 1.26's expanded TLS group profile. Keep
		// the widely deployed hybrid exchange and modern classical groups.
		CurvePreferences: []tls.CurveID{tls.X25519MLKEM768, tls.X25519, tls.CurveP256, tls.CurveP384},
	}, ResponseHeaderTimeout: 15 * time.Second, MaxResponseHeaderBytes: 64 << 10, MaxIdleConns: 8, IdleConnTimeout: 30 * time.Second, ForceAttemptHTTP2: true}
	t.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, errors.New("no addresses found")
		}
		for _, ip := range ips {
			if !publicIP(ip) {
				return nil, errors.New("feed addresses must resolve to the public internet")
			}
		}
		dialer := net.Dialer{Timeout: 10 * time.Second}
		var last error
		for _, ip := range ips {
			c, e := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if e == nil {
				return c, nil
			}
			last = e
		}
		return nil, last
	}
	return &Fetcher{Client: &http.Client{Transport: t, Timeout: 25 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many feed redirects")
		}
		_, err := WebURL(req.URL.String())
		return err
	}}}
}

func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return false
	}
	for _, prefix := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001::/23", "2001:db8::/32", "2002::/16", "64:ff9b::/96", "64:ff9b:1::/48"} {
		if netip.MustParsePrefix(prefix).Contains(ip) {
			return false
		}
	}
	return true
}

func (f *Fetcher) Fetch(ctx context.Context, raw, etag, modified string) (FetchResult, error) {
	var out FetchResult
	u, err := WebURL(raw)
	if err != nil {
		return out, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return out, err
	}
	req.Header.Set("User-Agent", "Tidings/0.1 (+https://github.com/airencracken/tidings)")
	req.Header.Set("Accept", "application/atom+xml, application/rss+xml, application/xml, text/xml;q=0.9")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if modified != "" {
		req.Header.Set("If-Modified-Since", modified)
	}
	res, err := f.Client.Do(req)
	if err != nil {
		return out, err
	}
	defer res.Body.Close()
	out.NotBefore = publisherDelay(res.Header, time.Now())
	if res.StatusCode == http.StatusNotModified {
		out.Unchanged = true
		return out, nil
	}
	if res.StatusCode != http.StatusOK {
		failure := &publisherError{Status: res.StatusCode}
		if res.StatusCode == http.StatusTooManyRequests || res.StatusCode == http.StatusServiceUnavailable {
			failure.Retry = retryAfter(res.Header.Get("Retry-After"), time.Now())
		}
		return out, failure
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, MaxFeedBytes+1))
	if err != nil {
		return out, err
	}
	out.Feed, err = ParseFeed(body, res.Request.URL.String(), time.Now().UTC())
	if err != nil {
		return out, err
	}
	out.ETag = res.Header.Get("ETag")
	out.Modified = res.Header.Get("Last-Modified")
	return out, nil
}
