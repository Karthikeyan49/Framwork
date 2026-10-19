package tools

import (
	"net/netip"
	"testing"
)

func TestDeniedAddressCorpus(t *testing.T) {
	type want struct{ public, internal bool } // denied for a public endpoint / for an opted-in internal endpoint
	allowed := want{false, false}
	internalOnly := want{true, false}
	never := want{true, true}
	corpus := map[string]want{
		// Public unicast, including range edges just outside denied blocks.
		"8.8.8.8": allowed, "1.1.1.1": allowed, "93.184.216.34": allowed, "9.255.255.255": allowed,
		"11.0.0.0": allowed, "172.15.255.255": allowed, "172.32.0.0": allowed, "192.167.255.255": allowed,
		"192.169.0.0": allowed, "100.63.255.255": allowed, "100.128.0.0": allowed, "169.253.255.255": allowed,
		"169.255.0.0": allowed, "126.255.255.255": allowed, "128.0.0.0": allowed, "198.17.255.255": allowed,
		"198.20.0.0": allowed, "223.255.255.255": allowed, "1.0.0.0": allowed,
		"2606:4700:4700::1111": allowed, "2001:4860:4860::8888": allowed, "2a00:1450:4001::1": allowed,
		// The Azure WireServer is a single address; its neighbours are ordinary public ones.
		"168.63.129.15": allowed, "168.63.129.17": allowed, "168.63.0.1": allowed,
		// ISATAP with a public embedded IPv4 is just a public address.
		"2a00:1450::5efe:808:808": allowed, "2a00:1450::200:5efe:101:101": allowed,
		// IPv4-mapped and translation forms of public IPv4.
		"::ffff:8.8.8.8": allowed, "64:ff9b::808:808": allowed, "2002:808:808::1": allowed,

		// Loopback.
		"127.0.0.1": internalOnly, "127.255.255.254": internalOnly, "::1": internalOnly,
		// RFC1918.
		"10.0.0.1": internalOnly, "10.255.255.255": internalOnly, "172.16.0.1": internalOnly,
		"172.31.255.255": internalOnly, "192.168.0.1": internalOnly, "192.168.255.255": internalOnly,
		// IPv6 link-local and CGNAT. (IPv4 link-local 169.254.0.0/16 is where clouds put
		// their metadata services; the whole block is never allowed, see below.)
		"fe80::1": internalOnly, "fe80::1%eth0": internalOnly, "febf::1": internalOnly,
		"100.64.0.1": internalOnly, "100.127.255.255": internalOnly,
		// Documentation, benchmarking, IETF assignments, deprecated relay.
		"192.0.2.1": internalOnly, "198.51.100.7": internalOnly, "203.0.113.9": internalOnly,
		"198.18.0.1": internalOnly, "198.19.255.255": internalOnly, "192.0.0.1": internalOnly,
		"192.88.99.1": internalOnly, "2001:db8::1": internalOnly, "2001:2::1": internalOnly, "3fff::1": internalOnly,
		// IPv6 unique local, deprecated site-local, local-use NAT64.
		"fc00::1": internalOnly, "fd12:3456:789a::1": internalOnly, "fec0::1": internalOnly, "64:ff9b:1::a00:1": internalOnly,
		// IPv4-mapped forms are classified as the embedded IPv4.
		"::ffff:127.0.0.1": internalOnly, "::ffff:10.1.2.3": internalOnly, "::ffff:192.168.1.1": internalOnly,
		"::ffff:100.64.0.1": internalOnly,
		// Neighbours of the never-allowed blocks below stay ordinary addresses.
		"fd00:ec1::1": internalOnly, "fd00:ec3::1": internalOnly,

		// Unspecified ("this network").
		"0.0.0.0": never, "0.1.2.3": never, "::": never,
		// Multicast, reserved and broadcast.
		"224.0.0.1": never, "239.255.255.255": never, "240.0.0.1": never, "255.255.255.255": never,
		"ff02::1": never, "ff05::2": never,
		// Cloud metadata, even for internal endpoints. The listed addresses are the known
		// ones (AWS, GCP, Azure, ECS, EKS, Alibaba, Oracle); the whole IPv4 link-local
		// block 169.254.0.0/16 and AWS's fd00:ec2::/32 are refused as well, so the GKE
		// (169.254.169.252) and Tencent (169.254.0.23) services and any provider that
		// follows the same convention are covered, and so is Azure's WireServer.
		"169.254.169.254": never, "169.254.170.2": never, "169.254.170.23": never, "100.100.100.200": never,
		"192.0.0.192": never, "fd00:ec2::254": never, "fd00:ec2::23": never,
		"169.254.169.252": never, "169.254.0.23": never, "169.254.0.1": never, "169.254.169.253": never,
		"169.254.255.254": never, "fd00:ec2::1": never, "fd00:ec2:ffff::1": never, "168.63.129.16": never,
		"::ffff:169.254.169.254": never, "::ffff:169.254.1.1": never, "::ffff:168.63.129.16": never,
		"::ffff:0.0.0.0": never, "::ffff:224.0.0.1": never, "::ffff:255.255.255.255": never,
		// ISATAP interface identifiers (0:5efe: or 200:5efe: followed by an IPv4) reach the
		// embedded IPv4 address: a non-public one is never allowed, whatever the prefix.
		"2a00:1450::5efe:7f00:1": never, "2a00:1450::200:5efe:a00:1": never, "fe80::5efe:c0a8:101": never,
		"2606:4700::5efe:a9fe:a9fe": never,
		// Deprecated IPv4-compatible forms and other ::/8 space.
		"::127.0.0.1": never, "::10.0.0.1": never, "::169.254.169.254": never, "::8.8.8.8": never, "::1:0:0:1": never,
		// NAT64 and 6to4 embedding non-public IPv4.
		"64:ff9b::7f00:1": never, "64:ff9b::a9fe:a9fe": never, "64:ff9b::a00:1": never, "64:ff9b::c0a8:101": never,
		"2002:7f00:1::": never, "2002:a9fe:a9fe::1": never, "2002:a00:1::": never, "2002:c0a8:101::1": never,
		// Teredo, discard-only and unassigned space.
		"2001::1": never, "2001:0:4136:e378:8000:63bf:3fff:fdd2": never, "100::1": never, "4000::1": never, "e000::1": never,
	}
	for literal, expected := range corpus {
		ip, err := netip.ParseAddr(literal)
		if err != nil {
			t.Fatalf("corpus entry %q: %v", literal, err)
		}
		if got := DeniedAddress(ip, false); got != expected.public {
			t.Errorf("DeniedAddress(%s, false) = %v", literal, got)
		}
		if got := DeniedAddress(ip, true); got != expected.internal {
			t.Errorf("DeniedAddress(%s, true) = %v", literal, got)
		}
	}
	if !DeniedAddress(netip.Addr{}, true) {
		t.Error("invalid address allowed")
	}
}
