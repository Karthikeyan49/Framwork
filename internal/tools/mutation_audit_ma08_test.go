package tools

import (
	"context"
	"net/netip"
	"testing"
)

func TestAddressGuardRefusesDeniedAddressesOtherPortsAndGarbage(t *testing.T) {
	denied := netip.MustParseAddr("10.0.0.1")
	guard := AddressGuard(443, func(a netip.Addr) bool { return a == denied })
	for address, refused := range map[string]bool{
		"203.0.113.7:443": false, "10.0.0.1:443": true, "203.0.113.7:80": true, "[2001:db8::1]:443": false, "not an address": true, "203.0.113.7": true,
	} {
		if err := guard(context.Background(), "tcp", address, nil); (err != nil) != refused {
			t.Errorf("%s: err %v, refused should be %v", address, err, refused)
		}
	}
}
