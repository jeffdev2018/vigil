package sandboxrun

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func startTestProxy(t *testing.T, hosts []string) (*Proxy, string, string) {
	t.Helper()
	p, err := StartProxy("127.0.0.1:0", slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	proxyURL, token, err := p.Register(hosts, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	return p, proxyURL, token
}

func TestProxyForwardsAllowedPlainRequest(t *testing.T) {
	t.Parallel()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("proxy credentials must not reach the target")
		}
		fmt.Fprint(w, "hello "+r.URL.Path)
	}))
	defer target.Close()
	targetHost, _, _ := net.SplitHostPort(strings.TrimPrefix(target.URL, "http://"))
	_, proxyURL, _ := startTestProxy(t, []string{targetHost})

	pu, _ := url.Parse(proxyURL)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(pu)}}
	resp, err := client.Get(target.URL + "/x")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(body) != "hello /x" {
		t.Fatalf("status %d body %q", resp.StatusCode, body)
	}
}

func TestProxyConnectTunnelsToAllowedHost(t *testing.T) {
	t.Parallel()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "tunnelled") }))
	defer target.Close()
	targetAddr := strings.TrimPrefix(target.URL, "http://")
	targetHost, _, _ := net.SplitHostPort(targetAddr)
	_, proxyURL, _ := startTestProxy(t, []string{targetHost})
	pu, _ := url.Parse(proxyURL)

	conn, err := net.Dial("tcp", pu.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	pass, _ := pu.User.Password()
	req := &http.Request{Method: http.MethodConnect, URL: &url.URL{Host: targetAddr}, Host: targetAddr, Header: http.Header{}}
	req.Header.Set("Proxy-Authorization", "Basic "+basicAuth(pu.User.Username(), pass))
	if err := req.Write(conn); err != nil {
		t.Fatal(err)
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("CONNECT status %d", resp.StatusCode)
	}
	// Plain HTTP through the tunnel (a TLS-less target is enough to prove the pipe).
	inner, _ := http.NewRequest(http.MethodGet, "http://"+targetAddr+"/", nil)
	if err := inner.Write(conn); err != nil {
		t.Fatal(err)
	}
	innerResp, err := http.ReadResponse(br, inner)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(innerResp.Body)
	if string(body) != "tunnelled" {
		t.Fatalf("body %q", body)
	}
}

func TestProxyDeniesUnknownHostAndBadCredentials(t *testing.T) {
	t.Parallel()
	_, proxyURL, token := startTestProxy(t, []string{"api.anthropic.com"})
	pu, _ := url.Parse(proxyURL)

	do := func(auth string, target string) int {
		conn, err := net.Dial("tcp", pu.Host)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		req := &http.Request{Method: http.MethodConnect, URL: &url.URL{Host: target}, Host: target, Header: http.Header{}}
		if auth != "" {
			req.Header.Set("Proxy-Authorization", "Basic "+basicAuth("multica", auth))
		}
		if err := req.Write(conn); err != nil {
			t.Fatal(err)
		}
		resp, err := http.ReadResponse(bufio.NewReader(conn), req)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode
	}
	if got := do(token, "evil.example:443"); got != http.StatusForbidden {
		t.Fatalf("unknown host status %d", got)
	}
	if got := do("wrong", "api.anthropic.com:443"); got != http.StatusProxyAuthRequired {
		t.Fatalf("bad credentials status %d", got)
	}
	if got := do("", "api.anthropic.com:443"); got != http.StatusProxyAuthRequired {
		t.Fatalf("missing credentials status %d", got)
	}
}

func basicAuth(user, pass string) string {
	r, _ := http.NewRequest(http.MethodGet, "http://x", nil)
	r.SetBasicAuth(user, pass)
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Basic ")
}
