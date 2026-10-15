package control

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"net"
	"sort"
	"strings"
)

var lifecycleNetworkPool = &net.IPNet{IP: net.IPv4(10, 240, 0, 0), Mask: net.CIDRMask(16, 32)}

func (c *Controller) ensureNetworkSubnets() error {
	networks, ok := c.doc["networks"].(map[string]any)
	if !ok || len(networks) == 0 {
		return fail(ManifestError, "network", "product networks are missing")
	}
	names := make([]string, 0, len(networks))
	for name := range networks {
		names = append(names, name)
	}
	sort.Strings(names)
	known := make(map[string]bool, len(names))
	for _, name := range names {
		known[name] = true
	}
	for name, subnet := range c.state.NetworkSubnets {
		if !known[name] || !validLifecycleSubnet(subnet) {
			return fail(Ownership, "network", "installation network allocation is invalid")
		}
	}
	missing := make([]string, 0, len(names))
	for _, name := range names {
		if !validLifecycleSubnet(c.state.NetworkSubnets[name]) {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		used, err := c.dockerNetworkSubnets()
		if err != nil {
			return fail(Daemon, "network", "cannot inspect Docker network address allocations")
		}
		for _, raw := range c.state.NetworkSubnets {
			_, subnet, _ := net.ParseCIDR(raw)
			if subnet != nil {
				used = append(used, subnet)
			}
		}
		allocated, err := allocateLifecycleSubnets(c.state.Token, used, missing)
		if err != nil {
			return fail(Capacity, "network", "no private lifecycle network subnets are available")
		}
		if c.state.NetworkSubnets == nil {
			c.state.NetworkSubnets = map[string]string{}
		}
		for _, name := range missing {
			c.state.NetworkSubnets[name] = allocated[name]
		}
		if err := c.save(); err != nil {
			return fail(Permission, "network", "cannot persist installation network allocation")
		}
	}
	for _, name := range names {
		raw, ok := networks[name].(map[string]any)
		if !ok {
			return fail(ManifestError, "network", "invalid product network")
		}
		raw["ipam"] = map[string]any{"config": []any{map[string]any{"subnet": c.state.NetworkSubnets[name]}}}
	}
	return nil
}

func (c *Controller) dockerNetworkSubnets() ([]*net.IPNet, error) {
	listing, err := c.docker("network", "ls", "-q")
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(string(listing))
	if len(ids) == 0 {
		return nil, nil
	}
	inspect, err := c.docker(append([]string{"network", "inspect"}, ids...)...)
	if err != nil {
		return nil, err
	}
	var documents []struct {
		IPAM struct {
			Config []struct {
				Subnet string `json:"Subnet"`
			}
		} `json:"IPAM"`
	}
	if json.Unmarshal(inspect, &documents) != nil {
		return nil, errInvalidNetworkInspect
	}
	var used []*net.IPNet
	for _, document := range documents {
		for _, config := range document.IPAM.Config {
			_, subnet, err := net.ParseCIDR(config.Subnet)
			if err == nil && subnet != nil {
				used = append(used, subnet)
			}
		}
	}
	return used, nil
}

var errInvalidNetworkInspect = networkInspectError{}

type networkInspectError struct{}

func (networkInspectError) Error() string { return "invalid Docker network inspection" }

func validLifecycleSubnet(value string) bool {
	ip, subnet, err := net.ParseCIDR(value)
	if err != nil || subnet == nil || ip.To4() == nil {
		return false
	}
	ones, bits := subnet.Mask.Size()
	return bits == 32 && ones == 24 && lifecycleNetworkPool.Contains(subnet.IP) && ip.Equal(subnet.IP)
}

func allocateLifecycleSubnets(token string, used []*net.IPNet, names []string) (map[string]string, error) {
	if len(names) == 0 || len(names) > 256 {
		return nil, networkInspectError{}
	}
	digest := sha256.Sum256([]byte(token))
	start := int(binary.BigEndian.Uint16(digest[:2]) % 64)
	for attempt := 0; attempt < 64; attempt++ {
		block := (start + attempt) % 64
		candidate := make([]*net.IPNet, len(names))
		available := true
		for index := range names {
			third := block*4 + index
			candidate[index] = &net.IPNet{IP: net.IPv4(10, 240, byte(third), 0), Mask: net.CIDRMask(24, 32)}
			for _, existing := range used {
				if cidrOverlap(candidate[index], existing) {
					available = false
					break
				}
			}
			if !available {
				break
			}
		}
		if !available {
			continue
		}
		result := make(map[string]string, len(names))
		for index, name := range names {
			result[name] = candidate[index].String()
		}
		return result, nil
	}
	return nil, networkInspectError{}
}

func cidrOverlap(left, right *net.IPNet) bool {
	return left.Contains(right.IP) || right.Contains(left.IP)
}
