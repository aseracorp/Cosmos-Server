package docker

import (
	"net"
	"sync"
	"time"

	"github.com/azukaar/cosmos-server/src/utils"
	"github.com/docker/docker/api/types"
)

// localSubnetsTTL bounds staleness; the cache is also dropped on network create/destroy events.
const localSubnetsTTL = 30 * time.Second

var (
	localSubnetsMu      sync.Mutex
	localSubnets        []*net.IPNet
	localSubnetsExpires time.Time
)

// ForgetLocalNetworkSubnets drops the cached subnet list (called from the docker event loop).
func ForgetLocalNetworkSubnets() {
	localSubnetsMu.Lock()
	defer localSubnetsMu.Unlock()
	localSubnets = nil
	localSubnetsExpires = time.Time{}
}

// localNetworkSubnets returns the CIDRs of host-local bridge networks; on docker error the previous answer is kept.
func localNetworkSubnets() []*net.IPNet {
	localSubnetsMu.Lock()
	defer localSubnetsMu.Unlock()

	if time.Now().Before(localSubnetsExpires) {
		return localSubnets
	}

	if err := Connect(); err != nil {
		return localSubnets
	}

	networks, err := DockerClient.NetworkList(DockerContext, types.NetworkListOptions{})
	if err != nil {
		utils.Error("Docker - Cannot list networks for the local subnet cache", err)
		return localSubnets
	}

	subnets := []*net.IPNet{}
	for _, network := range networks {
		// only a host-local bridge is all-local containers; macvlan/overlay subnets include remote hosts
		if network.Driver != "bridge" || network.Scope != "local" {
			continue
		}
		for _, conf := range network.IPAM.Config {
			if conf.Subnet == "" {
				continue
			}
			if _, cidr, err := net.ParseCIDR(conf.Subnet); err == nil {
				subnets = append(subnets, cidr)
			}
		}
	}

	localSubnets = subnets
	localSubnetsExpires = time.Now().Add(localSubnetsTTL)
	return localSubnets
}

// IsLocalDockerIP reports whether ip sits on a network of this docker host; wired into utils.IsLocalDockerIP at startup.
func IsLocalDockerIP(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}

	for _, subnet := range localNetworkSubnets() {
		if subnet.Contains(parsed) {
			return true
		}
	}
	return false
}
