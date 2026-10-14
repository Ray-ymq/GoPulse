package control

import (
	"net"
	"strings"
	"testing"
)

func TestAllocateLifecycleSubnetsAvoidsExistingNetworks(t *testing.T) {
	_, first, _ := net.ParseCIDR("10.240.0.0/24")
	_, second, _ := net.ParseCIDR("10.240.4.0/24")
	allocated, err := allocateLifecycleSubnets(strings.Repeat("a", 64), []*net.IPNet{first, second}, []string{"business", "edge", "observability"})
	if err != nil {
		t.Fatal(err)
	}
	if len(allocated) != 3 {
		t.Fatalf("allocated %d networks, want 3", len(allocated))
	}
	seen := map[string]bool{}
	for name, raw := range allocated {
		if !validLifecycleSubnet(raw) {
			t.Fatalf("%s received invalid subnet %q", name, raw)
		}
		if seen[raw] || raw == first.String() || raw == second.String() {
			t.Fatalf("subnet %q was reused", raw)
		}
		seen[raw] = true
	}
}

func TestValidLifecycleSubnetRejectsOutsidePool(t *testing.T) {
	for _, subnet := range []string{"172.30.0.0/24", "10.240.1.0/16", "10.240.1.1/24", "10.240.1.0/25"} {
		if validLifecycleSubnet(subnet) {
			t.Fatalf("accepted invalid lifecycle subnet %q", subnet)
		}
	}
	if !validLifecycleSubnet("10.240.1.0/24") {
		t.Fatal("rejected valid lifecycle subnet")
	}
}
