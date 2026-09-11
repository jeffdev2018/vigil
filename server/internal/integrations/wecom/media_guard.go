package wecom

// media_guard.go — the media fetcher is pointed at an address by somebody else.
//
// A callback carries a pre-signed COS URL and we GET it. The URL is a string
// that arrived over the socket: WeCom put it there, but the adapter cannot
// prove that, and the fetch runs from inside the deployment's network with
// whatever reach that network has. On this machine alone that reach includes
// a Tailscale tailnet (100.64.0.0/10), a proxy's fake-IP range
// (198.18.0.0/15), Docker's bridges, and the loopback the backend's own
// admin endpoints listen on.
//
// The guard itself — dial-time resolution, the reserved and translation
// ranges, the redirect scheme check — lives in internal/util/netguard so
// every fetch of a somebody-else URL shares one implementation. What stays
// here is WeCom's operator allow-list.

import (
	"fmt"
	"net/http"
	"net/netip"
	"strings"

	"github.com/multica-ai/multica/server/internal/util/netguard"
)

// ErrMediaAddrBlocked is returned instead of a connection when every address
// a media host resolves to is one the deployment must not be pointed at.
var ErrMediaAddrBlocked = netguard.ErrAddrBlocked

// addrPolicy answers whether one resolved address may be dialed.
type addrPolicy = netguard.Policy

// mediaAllowedPrefixes are ranges an operator has declared safe for media
// fetches despite looking reserved. It exists for one real deployment shape:
// a machine behind a fake-IP proxy, where the resolver answers every public
// hostname with an address out of 198.18.0.0/15 and the proxy forwards the
// traffic onward. On such a machine WeCom's own COS host is indistinguishable
// from a link-local metadata endpoint by address alone, so the guard refuses
// every attachment and inbound media stops working entirely.
//
// Empty by default. Widening this is a decision with a cost: whatever range is
// listed here can be reached by a URL somebody else controls, which is exactly
// what the guard exists to prevent. It is opt-in per deployment, and worth
// setting only where the operator knows the range belongs to their proxy.
//
// What it CANNOT open, at any width: loopback, the private ranges, link-local
// and translation space (see netguard.PublicAddr).
var mediaAllowedPrefixes []netip.Prefix

// SetMediaAllowedPrefixes declares ranges the media guard may dial. Called at
// boot from MULTICA_WECOM_MEDIA_ALLOW_CIDRS. An unparseable entry is reported
// and skipped rather than silently widening or silently narrowing the guard.
func SetMediaAllowedPrefixes(cidrs []string) []error {
	var (
		out  []netip.Prefix
		errs []error
	)
	for _, raw := range cidrs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		p, err := netip.ParsePrefix(raw)
		if err != nil {
			errs = append(errs, fmt.Errorf("wecom: media allow cidr %q: %w", raw, err))
			continue
		}
		out = append(out, p)
	}
	mediaAllowedPrefixes = out
	return errs
}

// publicAddrOnly is the production media policy: public internet, plus the
// reserved ranges the operator reopened. The allow-list is read at dial time
// so a boot-time SetMediaAllowedPrefixes reaches clients built earlier.
func publicAddrOnly(a netip.Addr) bool {
	return netguard.PublicAddr(a, mediaAllowedPrefixes)
}

// mediaGuard is the dialer the media client connects through.
type mediaGuard struct {
	allow    addrPolicy
	resolve  netguard.Resolver
	onRefuse func(host string, addr netip.Addr) // test hook; nil in production
}

// newMediaHTTPClient builds the client every media download goes through.
func newMediaHTTPClient(g mediaGuard) *http.Client {
	allow := g.allow
	if allow == nil {
		allow = publicAddrOnly
	}
	return netguard.NewHTTPClient(netguard.Dialer{Allow: allow, Resolve: g.resolve, OnRefuse: g.onRefuse}, 0)
}
