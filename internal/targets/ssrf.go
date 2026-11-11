package targets

import (
	"fmt"
	"net/netip"
	"syscall"
)

// NetworkPolicy configures the SSRF guard. The zero value denies cloud metadata
// and link-local ranges and allows everything else.
type NetworkPolicy struct {
	// AllowCIDRs, when non-empty, restricts targets to these ranges (allow-list mode).
	// They also exempt addresses from BlockPrivate. They never override the built-in
	// metadata / link-local denials.
	AllowCIDRs []string `json:"allow_cidrs,omitempty"`
	// DenyCIDRs are extra denied ranges.
	DenyCIDRs []string `json:"deny_cidrs,omitempty"`
	// BlockPrivate also denies loopback, RFC1918, CGNAT, ULA and unspecified addresses.
	BlockPrivate bool `json:"block_private,omitempty"`
}

// SSRFError is returned (wrapped by the dialer) when a destination is denied.
type SSRFError struct {
	Addr   netip.Addr
	Reason string
}

func (e *SSRFError) Error() string {
	return fmt.Sprintf("ssrf guard: destination %s denied (%s)", e.Addr, e.Reason)
}

var alwaysDenied = mustPrefixes(
	"169.254.0.0/16",     // link-local incl. AWS/GCP/Azure metadata 169.254.169.254
	"fe80::/10",          // IPv6 link-local
	"fd00:ec2::/32",      // AWS IPv6 metadata fd00:ec2::254
	"100.100.100.200/32", // Alibaba metadata
	"192.0.0.192/32",     // Oracle metadata
)

var privateRanges = mustPrefixes(
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
	"::/128", "::1/128", "fc00::/7",
)

func mustPrefixes(s ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(s))
	for i, v := range s {
		out[i] = netip.MustParsePrefix(v)
	}
	return out
}

func parsePrefixes(s []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(s))
	for _, v := range s {
		p, err := netip.ParsePrefix(v)
		if err != nil {
			a, aerr := netip.ParseAddr(v)
			if aerr != nil {
				return nil, fmt.Errorf("invalid CIDR %q", v)
			}
			p = netip.PrefixFrom(a, a.BitLen())
		}
		out = append(out, p.Masked())
	}
	return out, nil
}

// SSRFGuard validates resolved destination addresses at dial time, so DNS
// rebinding and redirects cannot bypass it.
type SSRFGuard struct {
	allow, deny  []netip.Prefix
	blockPrivate bool
}

// NewSSRFGuard builds a guard from a policy.
func NewSSRFGuard(p NetworkPolicy) (*SSRFGuard, error) {
	allow, err := parsePrefixes(p.AllowCIDRs)
	if err != nil {
		return nil, err
	}
	deny, err := parsePrefixes(p.DenyCIDRs)
	if err != nil {
		return nil, err
	}
	return &SSRFGuard{allow: allow, deny: deny, blockPrivate: p.BlockPrivate}, nil
}

var defaultGuard = &SSRFGuard{}

func inPrefixes(ps []netip.Prefix, a netip.Addr) bool {
	for _, p := range ps {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// CheckIP returns an *SSRFError when ip may not be dialed. A nil guard applies the defaults.
func (g *SSRFGuard) CheckIP(ip netip.Addr) error {
	if g == nil {
		g = defaultGuard
	}
	ip = ip.Unmap().WithZone("")
	if inPrefixes(alwaysDenied, ip) {
		return &SSRFError{ip, "link-local / cloud metadata"}
	}
	if inPrefixes(g.deny, ip) {
		return &SSRFError{ip, "in deny list"}
	}
	allowed := inPrefixes(g.allow, ip)
	if len(g.allow) > 0 && !allowed {
		return &SSRFError{ip, "not in allow list"}
	}
	if g.blockPrivate && !allowed && (inPrefixes(privateRanges, ip) || ip.IsLoopback() || ip.IsUnspecified() || ip.IsPrivate()) {
		return &SSRFError{ip, "private address"}
	}
	return nil
}

// Control is a net.Dialer.Control hook; address is the already-resolved ip:port.
func (g *SSRFGuard) Control(network, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return &SSRFError{Reason: "unparsable address " + address}
	}
	return g.CheckIP(ap.Addr())
}
