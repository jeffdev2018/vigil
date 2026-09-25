// Package netguard refuses outbound connections to anything that is not the
// public internet. Use it for every fetch whose destination somebody else
// chose: a URL that arrived on a webhook, a base URL a workspace admin typed.
//
// The guard is not on the URL, it is on the CONNECTION. Checking the hostname
// is worth nothing on its own: the URL can redirect (a 302 to
// http://169.254.169.254/ is one line of attacker-controlled response), and a
// hostname that passed a check can resolve to something else a moment later.
// A DialContext that resolves the destination and refuses a non-public
// address runs on every hop of every redirect, against the answer actually
// being connected to, which is the only place both of those are covered at
// once.
//
// It started life as the WeCom media guard (#6524, newSecureMediaClient /
// isAllowedMediaIP, extended with the reserved ranges below and an injectable
// resolver so the rebinding case can be tested rather than argued about).
package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"time"
)

// ErrAddrBlocked is returned instead of a connection when every address a
// host resolves to is one the deployment must not be pointed at. It is
// deliberately distinct from a dial failure: only this one means somebody
// handed us a destination they should not have.
var ErrAddrBlocked = errors.New("host resolves to a non-public address")

// dialTimeout bounds one TCP connect.
const dialTimeout = 10 * time.Second

// The reserved ranges come in two groups, and the difference is not
// taxonomy — it is whether an operator is allowed to open them.
//
// Both lists are the IANA IPv4 and IPv6 Special-Purpose Address Registries
// minus what netip's own predicates already catch (IsLoopback / IsPrivate /
// IsLinkLocal* / IsMulticast / IsUnspecified), minus the handful of
// special-purpose blocks that are ordinary globally-routed unicast (the AS112
// delegations 192.31.196.0/24, 192.175.48.0/24 and 2620:4f:8000::/48, and
// AMT's 192.52.193.0/24) — those are "special" in who runs them, not in where
// they point, and refusing them would buy nothing.
//
// reservedPrefixes is space that is not the public internet: an address
// resolving there is either a mistake or an attempt, and either way there is
// no object at the end of it. 100.64.0.0/10 is the one that matters most —
// RFC 6598 shared address space, which no standard-library predicate reports
// as private, and which is exactly what Tailscale hands out. A tailnet peer
// is a machine inside the trust boundary reachable by IP with no credential.
//
// This group is what the allowed prefixes can reopen, because a real
// deployment shape lives in it: a fake-IP proxy hands out 198.18.0.0/15 for
// every public hostname, WeCom's own COS host included. An operator who knows
// their proxy's pool can say so.
var reservedPrefixes = []netip.Prefix{
	// ---- IPv4 ----
	netip.MustParsePrefix("0.0.0.0/8"),       // "this network"
	netip.MustParsePrefix("100.64.0.0/10"),   // RFC 6598 CGNAT — Tailscale lives here
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF protocol assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // TEST-NET-1
	netip.MustParsePrefix("198.18.0.0/15"),   // benchmarking — and a proxy's fake-IP range
	netip.MustParsePrefix("198.51.100.0/24"), // TEST-NET-2
	netip.MustParsePrefix("203.0.113.0/24"),  // TEST-NET-3
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved, includes 255.255.255.255
	netip.MustParsePrefix("192.88.99.0/24"),  // deprecated 6to4 relay anycast — the v4 end of 2002::/16

	// ---- IPv6 ----
	netip.MustParsePrefix("100::/64"),       // discard-only
	netip.MustParsePrefix("100:0:0:1::/64"), // dummy prefix, also discard-only
	// The whole IETF protocol-assignments block, not its dozen sub-entries.
	// It holds Teredo (2001::/32), benchmarking (2001:2::/48 — the twin of
	// 198.18.0.0/15 above), AMT, AS112-v6, ORCHID/ORCHIDv2, DET, and the PCP
	// / TURN / DNS-SD anycast addresses. None of it is somewhere a COS object
	// lives, and one prefix is easier to keep true than eight. Same call the
	// v4 side already makes with 192.0.0.0/24. Documentation space is
	// 2001:db8::/32, which is outside this /23 and listed separately.
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"), // documentation
	netip.MustParsePrefix("3fff::/20"),     // documentation, RFC 9637
	netip.MustParsePrefix("5f00::/16"),     // SRv6 SIDs, RFC 9602 — routing labels, not hosts
	// Site-local. RFC 3879 deprecated it and IANA delisted it, which is why
	// no predicate and no registry row covers it — but the networks that
	// were numbered out of it before 2004 still route it internally, and it
	// is one line.
	netip.MustParsePrefix("fec0::/10"),
}

// translationPrefixes is TRANSLATION space, and no configuration reopens
// it. These addresses are not destinations, they are IPv4 destinations
// wearing an IPv6 costume: 64:ff9b:1::a9fe:a9fe is 169.254.169.254 the moment
// a NAT64 translator sees it, and 2002:7f00:1::1 is 127.0.0.1 through a 6to4
// relay.
//
// That costume is why the split exists. The early IsLoopback / IsPrivate /
// IsLinkLocalUnicast checks never fire on these — at that point they are
// IPv6 addresses, and every one of them reaches straight past a guard that
// only knows how to recognise an IPv4 address when it is written as one. This
// list is the ONLY thing standing between the guard and the address embedded
// inside. Letting the allowed prefixes override it would mean an allow-list
// of ::/0 — or, just as well, 2002::/16 — reaching the loopback and the metadata endpoint the guard exists to refuse,
// written in a spelling the operator never thought they were opening.
//
// So it is a hard block, and it costs nothing: no COS object, no proxy pool
// and no deployment lives in translation space, so there is no shape to
// accommodate here the way there is for a fake-IP proxy.
var translationPrefixes = []netip.Prefix{
	netip.MustParsePrefix("64:ff9b::/96"),   // NAT64, well-known prefix
	netip.MustParsePrefix("64:ff9b:1::/48"), // NAT64, local-use prefix (RFC 8215)
	netip.MustParsePrefix("2002::/16"),      // 6to4 — the last 112 bits open with an IPv4 address
}

// Policy answers whether one resolved address may be dialed. Production uses
// PublicOnly; tests substitute a policy that allows the loopback their own
// server is on, so what is under test is the guard's decision rather than the
// test harness's address.
type Policy func(netip.Addr) bool

// PublicOnly is the default policy: everything that is not routable public
// internet is refused, and nothing is reopened.
func PublicOnly(a netip.Addr) bool { return PublicAddr(a, nil) }

// PublicAddr reports whether a is routable public internet. allowed reopens
// ranges an operator declared theirs, but only inside reservedPrefixes (a
// fake-IP proxy pool is the case this exists for): loopback, private,
// link-local and translation space are refused whatever allowed says.
func PublicAddr(a netip.Addr, allowed []netip.Prefix) bool {
	// An IPv4-mapped IPv6 address (::ffff:127.0.0.1) reports none of the IPv4
	// predicates until it is unmapped, which is the whole trick.
	a = a.Unmap()
	if !a.IsValid() {
		return false
	}
	if a.IsLoopback() || a.IsPrivate() || a.IsUnspecified() ||
		a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() ||
		a.IsInterfaceLocalMulticast() || a.IsMulticast() {
		return false
	}
	// Translation space first, and before the allow-list is consulted at all.
	// The address inside one of these is an IPv4 address the checks above
	// would have refused on sight; the only reason they did not fire is the
	// spelling. Nothing an operator configures reopens it.
	for _, p := range translationPrefixes {
		if p.Contains(a) {
			return false
		}
	}
	for _, p := range reservedPrefixes {
		if p.Contains(a) {
			// Checked only for addresses the guard would otherwise refuse, so
			// an empty allow-list leaves the guard exactly as strict.
			for _, ok := range allowed {
				if ok.Contains(a) {
					return true
				}
			}
			return false
		}
	}
	return true
}

// Resolver is net.DefaultResolver's one method we need, so a test can hand
// back an address of its choosing for a name of its choosing.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// Dialer is the guarded dialer. The zero value is the production guard.
type Dialer struct {
	Allow    Policy                             // nil means PublicOnly
	Resolve  Resolver                           // nil means net.DefaultResolver
	OnRefuse func(host string, addr netip.Addr) // test hook; nil in production
}

// DialContext resolves the destination itself and connects to a checked
// address — never to the hostname, because resolving twice is the rebinding
// window.
//
// Every address is checked before any is dialed, and the connection is made
// to the literal that passed. A host that answers with a mix of public and
// internal addresses gets the public ones tried and the internal ones
// refused, rather than the whole name allowed on the strength of one good
// answer.
func (g Dialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("guarded dial: %w", err)
	}
	var resolver Resolver = net.DefaultResolver
	if g.Resolve != nil {
		resolver = g.Resolve
	}
	addrs, err := resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("guarded dial: resolve %s: %w", host, err)
	}
	allow := g.Allow
	if allow == nil {
		allow = PublicOnly
	}

	d := &net.Dialer{Timeout: dialTimeout}
	lastErr := error(ErrAddrBlocked)
	for _, a := range addrs {
		if !allow(a) {
			if g.OnRefuse != nil {
				g.OnRefuse(host, a)
			}
			continue
		}
		conn, dialErr := d.DialContext(ctx, network, net.JoinHostPort(a.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
	}
	return nil, lastErr
}

// NewHTTPClient builds a client that connects only through g.
//
// Two guards, and they cover different things. The dialer refuses a
// destination; CheckRedirect refuses a SCHEME, because a redirect to file://
// or gopher:// never reaches a dialer at all and the transport would happily
// hand it to a protocol handler. The proxy environment is ignored: a proxied
// fetch would send the request to an address the dial guard never sees.
// timeout bounds the whole request; zero means none.
func NewHTTPClient(g Dialer, timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = g.DialContext
	transport.Proxy = nil
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("guarded client: too many redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("guarded client: redirect to scheme %q refused", req.URL.Scheme)
			}
			return nil
		},
	}
}
