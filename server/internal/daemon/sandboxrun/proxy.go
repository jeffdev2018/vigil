package sandboxrun

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// DefaultAllowedHosts are reachable from every container: package registries
// and the provider APIs the bootstrapped CLIs need. The daemon adds the
// multica server host and the run's own allowlist.
var DefaultAllowedHosts = []string{
	"registry.npmjs.org", "github.com", "api.github.com", "objects.githubusercontent.com",
	"api.anthropic.com", "api.openai.com", "statsig.anthropic.com", "sentry.io",
}

const proxyUser = "multica"

// Proxy is the daemon's allowlisting HTTP forward proxy (plain requests and
// CONNECT). Each run registers its own credential and host set, so one
// listener serves concurrent runs with different allowlists.
type Proxy struct {
	server   *http.Server
	listener net.Listener
	logger   *slog.Logger
	// transport serves plain (non-CONNECT) requests without consulting any
	// proxy environment of the daemon itself.
	transport *http.Transport

	mu   sync.Mutex
	runs map[string]map[string]bool // token -> allowed hosts
}

// StartProxy listens on addr ("0.0.0.0:0" for an ephemeral port).
func StartProxy(addr string, logger *slog.Logger) (*Proxy, error) {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen for sandbox egress proxy: %w", err)
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	p := &Proxy{listener: listener, logger: logger, runs: map[string]map[string]bool{},
		transport: &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 30 * time.Second}).DialContext}}
	p.server = &http.Server{Handler: p, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = p.server.Serve(listener) }()
	return p, nil
}

// Port is the listening port.
func (p *Proxy) Port() int { return p.listener.Addr().(*net.TCPAddr).Port }

// Close stops the listener.
func (p *Proxy) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return p.server.Shutdown(ctx)
}

// Register creates a credential allowing exactly hosts and returns the proxy
// URL to hand to the run. Unregister with the returned token.
func (p *Proxy) Register(hosts []string, advertiseHost string) (proxyURL, token string, err error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	token = hex.EncodeToString(raw)
	allowed := make(map[string]bool, len(hosts))
	for _, h := range hosts {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			allowed[h] = true
		}
	}
	p.mu.Lock()
	p.runs[token] = allowed
	p.mu.Unlock()
	u := url.URL{Scheme: "http", User: url.UserPassword(proxyUser, token), Host: fmt.Sprintf("%s:%d", advertiseHost, p.Port())}
	return u.String(), token, nil
}

// Unregister revokes a run's credential.
func (p *Proxy) Unregister(token string) {
	p.mu.Lock()
	delete(p.runs, token)
	p.mu.Unlock()
}

func (p *Proxy) allowedFor(r *http.Request) (map[string]bool, bool) {
	user, token, ok := parseProxyBasicAuth(r.Header.Get("Proxy-Authorization"))
	if !ok || user != proxyUser {
		return nil, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	allowed, ok := p.runs[token]
	return allowed, ok
}

func parseProxyBasicAuth(header string) (string, string, bool) {
	r := &http.Request{Header: http.Header{"Authorization": []string{header}}}
	return r.BasicAuth()
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	allowed, ok := p.allowedFor(r)
	if !ok {
		w.Header().Set("Proxy-Authenticate", `Basic realm="multica-sandbox"`)
		http.Error(w, "proxy authentication required", http.StatusProxyAuthRequired)
		return
	}
	target := r.Host
	if r.Method != http.MethodConnect && r.URL.Host != "" {
		target = r.URL.Host
	}
	host := strings.ToLower(target)
	if h, _, err := net.SplitHostPort(target); err == nil {
		host = strings.ToLower(h)
	}
	if !allowed[host] {
		p.logger.Warn("sandbox egress denied", "host", host, "method", r.Method)
		http.Error(w, "egress to "+host+" is not allowed", http.StatusForbidden)
		return
	}
	if r.Method == http.MethodConnect {
		p.serveConnect(w, r, target)
		return
	}
	p.serveForward(w, r)
}

func (p *Proxy) serveConnect(w http.ResponseWriter, r *http.Request, target string) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijack unsupported", http.StatusInternalServerError)
		return
	}
	upstream, err := net.DialTimeout("tcp", target, 30*time.Second)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusOK)
	client, buffered, err := hijacker.Hijack()
	if err != nil {
		_ = upstream.Close()
		return
	}
	go func() {
		defer client.Close()
		defer upstream.Close()
		_, _ = io.Copy(upstream, buffered)
	}()
	_, _ = io.Copy(client, upstream)
	_ = client.Close()
	_ = upstream.Close()
}

func (p *Proxy) serveForward(w http.ResponseWriter, r *http.Request) {
	out := r.Clone(r.Context())
	out.RequestURI = ""
	out.Header.Del("Proxy-Authorization")
	out.Header.Del("Proxy-Connection")
	if out.URL.Scheme == "" {
		out.URL.Scheme = "http"
	}
	resp, err := p.transport.RoundTrip(out)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}
