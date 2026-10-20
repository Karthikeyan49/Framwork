package tools

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"syscall"
	"time"
)

// Network policy (ADR100 decision 8). Every address a connector host resolves
// to is classified before any connection is opened; the connection is made to
// the validated literal address only, so a second DNS answer (rebinding) can
// never change the destination.

type addressClass int

const (
	addressPublic   addressClass = iota // routable public unicast
	addressInternal                     // permitted only for an internal endpoint in an opted-in process
	addressNever                        // refused even when internal networks are allowed
)

func mustPrefixes(values ...string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(values))
	for _, v := range values {
		out = append(out, netip.MustParsePrefix(v))
	}
	return out
}

var (
	// Cloud instance metadata/credential endpoints stay refused even for
	// internal endpoints: AWS/GCP/Azure/OpenStack IMDS, AWS IMDS IPv6, ECS
	// task metadata, EKS pod identity, Alibaba Cloud and Oracle Cloud.
	metadataAddresses = map[netip.Addr]bool{
		netip.MustParseAddr("169.254.169.254"): true,
		netip.MustParseAddr("169.254.170.2"):   true,
		netip.MustParseAddr("169.254.170.23"):  true,
		netip.MustParseAddr("100.100.100.200"): true,
		netip.MustParseAddr("192.0.0.192"):     true,
		netip.MustParseAddr("fd00:ec2::254"):   true,
		netip.MustParseAddr("fd00:ec2::23"):    true,
	}
	// 0/8 "this network" (unspecified), 224/4 multicast, 240/4 reserved
	// including the limited broadcast 255.255.255.255, and the whole IPv4
	// link-local block 169.254.0.0/16 (AWS, GCP, Azure, ECS, EKS, Tencent and any
	// provider that follows the convention keep their metadata services there)
	// plus the Azure WireServer 168.63.129.16 (gateway review F8).
	neverV4 = mustPrefixes("0.0.0.0/8", "169.254.0.0/16", "168.63.129.16/32", "224.0.0.0/4", "240.0.0.0/4")
	// AWS's IPv6 service block (instance metadata, DNS, time) is never allowed.
	neverV6 = netip.MustParsePrefix("fd00:ec2::/32")
	// Loopback, RFC1918, CGNAT, IETF protocol assignments,
	// documentation, benchmarking and the deprecated 6to4 relay anycast.
	internalV4 = mustPrefixes(
		"10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8",
		"172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24",
		"192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24",
	)
	v6Loopback      = netip.IPv6Loopback()
	v6Reserved      = netip.MustParsePrefix("::/8") // unspecified, IPv4-compatible and unassigned
	v6NAT64         = netip.MustParsePrefix("64:ff9b::/96")
	v6NAT64Local    = netip.MustParsePrefix("64:ff9b:1::/48")
	v6Discard       = netip.MustParsePrefix("100::/64")
	v6Teredo        = netip.MustParsePrefix("2001::/32")
	v66to4          = netip.MustParsePrefix("2002::/16")
	v6GlobalUnicast = netip.MustParsePrefix("2000::/3")
	// IETF protocol assignments (incl. 2001:2::/48 benchmarking), the
	// documentation prefixes and SRv6 SIDs inside 2000::/3.
	internalV6Global = mustPrefixes("2001::/23", "2001:db8::/32", "3fff::/20", "5f00::/16")
	// Unique local, link-local and deprecated site-local.
	internalV6Local = mustPrefixes("fc00::/7", "fe80::/10", "fec0::/10")
)

func inAny(ip netip.Addr, prefixes []netip.Prefix) bool {
	for _, p := range prefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

func classifyAddress(ip netip.Addr) addressClass {
	if !ip.IsValid() {
		return addressNever
	}
	ip = ip.WithZone("")
	if ip.Is4In6() {
		// IPv4-mapped IPv6 reaches the embedded IPv4 address.
		ip = ip.Unmap()
	}
	if metadataAddresses[ip] {
		return addressNever
	}
	if ip.Is4() {
		switch {
		case inAny(ip, neverV4):
			return addressNever
		case inAny(ip, internalV4):
			return addressInternal
		}
		return addressPublic
	}
	// ISATAP interface identifiers (0000:5efe or 0200:5efe followed by an IPv4 address)
	// reach the embedded IPv4 whatever the prefix: a non-public one is never allowed.
	if b := ip.As16(); b[8]&0xfd == 0 && b[9] == 0 && b[10] == 0x5e && b[11] == 0xfe {
		if classifyAddress(netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]})) != addressPublic {
			return addressNever
		}
	}
	switch {
	case neverV6.Contains(ip):
		return addressNever
	case ip == v6Loopback:
		return addressInternal
	// NAT64 prefixes begin with a zero byte, so they must be matched before
	// the ::/8 reserved block that would otherwise swallow them.
	case v6NAT64.Contains(ip):
		return embeddedClass(ip, 12)
	case v6NAT64Local.Contains(ip):
		return addressInternal
	case v6Reserved.Contains(ip):
		// ::, deprecated IPv4-compatible ::a.b.c.d and unassigned space.
		return addressNever
	case v6Discard.Contains(ip), v6Teredo.Contains(ip):
		return addressNever
	case v66to4.Contains(ip):
		return embeddedClass(ip, 2)
	case inAny(ip, internalV6Global):
		return addressInternal
	case v6GlobalUnicast.Contains(ip):
		return addressPublic
	case inAny(ip, internalV6Local):
		return addressInternal
	}
	// ff00::/8 multicast and every unassigned or reserved block.
	return addressNever
}

// embeddedClass classifies a translation form (NAT64, 6to4) by the IPv4
// address it carries. Translated paths may only reach public IPv4; internal
// services are addressed directly, never through a translator.
func embeddedClass(ip netip.Addr, offset int) addressClass {
	b := ip.As16()
	v4 := netip.AddrFrom4([4]byte{b[offset], b[offset+1], b[offset+2], b[offset+3]})
	if classifyAddress(v4) == addressPublic {
		return addressPublic
	}
	return addressNever
}

// DeniedAddress reports whether a resolved address must not be dialed.
// internalAllowed is true only for an endpoint declared network "internal" in
// a process that enabled internal networks. Even then unspecified, multicast,
// broadcast, reserved and cloud metadata addresses stay denied, as do
// translation forms (NAT64, 6to4, IPv4-compatible) of non-public IPv4.
// IPv4-mapped IPv6 is classified as its embedded IPv4 address.
func DeniedAddress(ip netip.Addr, internalAllowed bool) bool {
	switch classifyAddress(ip) {
	case addressPublic:
		return false
	case addressInternal:
		return !internalAllowed
	}
	return true
}

// HostResolver resolves an endpoint host. *net.Resolver implements it. A
// custom resolver cannot bypass policy: every returned address is validated.
type HostResolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

var (
	errNetworkPolicy = errors.New("tool network policy denied the destination")
	errResolve       = errors.New("tool host resolution failed")
	errDial          = errors.New("tool connection failed")
)

const maxResolvedAddresses = 32

type attemptKey struct{}

func attemptFrom(ctx context.Context) *attempt {
	a, _ := ctx.Value(attemptKey{}).(*attempt)
	return a
}

// dialContext is the transport's only dialer. The transport detaches dial
// contexts from request cancellation but keeps values, so the per-request
// attempt (host, port, network policy, deadline) is always present.
func (g *Gateway) dialContext(ctx context.Context, _, addr string) (net.Conn, error) {
	a := attemptFrom(ctx)
	if a == nil {
		return nil, errNetworkPolicy
	}
	if addr != a.dialAddr {
		a.denied.Store(true)
		return nil, errNetworkPolicy
	}
	ctx, cancel := context.WithDeadline(ctx, a.deadline)
	defer cancel()
	addrs, err := g.lookup(ctx, a.host)
	if err != nil || len(addrs) == 0 {
		return nil, errResolve
	}
	if len(addrs) > maxResolvedAddresses {
		a.denied.Store(true)
		return nil, errNetworkPolicy
	}
	// Every answer is validated; one denied answer refuses the destination,
	// so a mixed public/private answer cannot be used to reach an internal
	// address on a later attempt.
	for _, ip := range addrs {
		if DeniedAddress(ip, a.internal) {
			a.denied.Store(true)
			return nil, errNetworkPolicy
		}
	}
	for _, ip := range addrs {
		target := netip.AddrPortFrom(ip.Unmap(), uint16(a.port))
		conn, err := g.dialer.DialContext(ctx, "tcp", target.String())
		if err == nil {
			return conn, nil
		}
		if a.denied.Load() || ctx.Err() != nil {
			break
		}
	}
	if a.denied.Load() {
		return nil, errNetworkPolicy
	}
	return nil, errDial
}

// lookup runs the resolver outside the dial goroutine's stack so a resolver
// that panics or ignores its context can neither crash the process (this code
// runs on a transport goroutine, beyond Execute's recover) nor outlive the
// attempt's deadline.
func (g *Gateway) lookup(ctx context.Context, host string) ([]netip.Addr, error) {
	type answer struct {
		addrs []netip.Addr
		err   error
	}
	ch := make(chan answer, 1)
	go func() {
		defer func() {
			if recover() != nil {
				ch <- answer{err: errResolve}
			}
		}()
		addrs, err := g.resolver.LookupNetIP(ctx, "ip", host)
		ch <- answer{addrs, err}
	}()
	select {
	case r := <-ch:
		return r.addrs, r.err
	case <-ctx.Done():
		return nil, errResolve
	}
}

// controlContext re-validates the literal socket address immediately before
// connect(2), independently of how the address was chosen.
func controlContext(ctx context.Context, _, address string, _ syscall.RawConn) error {
	a := attemptFrom(ctx)
	target, err := netip.ParseAddrPort(address)
	if a == nil || err != nil || DeniedAddress(target.Addr(), a.internal) || int(target.Port()) != a.port {
		if a != nil {
			a.denied.Store(true)
		}
		return errNetworkPolicy
	}
	return nil
}

func newDialer() net.Dialer {
	return net.Dialer{Timeout: MaxTimeoutMS * time.Millisecond, KeepAlive: -1, ControlContext: controlContext}
}

func endpointAddr(e Endpoint) string {
	return net.JoinHostPort(e.Host, strconv.Itoa(e.Port))
}

// AddressGuard returns a net.Dialer ControlContext that refuses, immediately before
// connect(2), every literal address for which denied reports true and every port other
// than port. Packages outside tools that dial a pinned endpoint (the AI provider
// adapter) use it so the last-moment re-validation is the same code as the tool
// gateway's, without importing syscall themselves.
func AddressGuard(port int, denied func(netip.Addr) bool) func(context.Context, string, string, syscall.RawConn) error {
	return func(_ context.Context, _, address string, _ syscall.RawConn) error {
		target, err := netip.ParseAddrPort(address)
		if err != nil || denied(target.Addr()) || int(target.Port()) != port {
			return errNetworkPolicy
		}
		return nil
	}
}
