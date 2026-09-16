package constellation

import (
	"net"
	"strings"
	"sync"
	"time"

	"github.com/azukaar/cosmos-server/src/utils"
)

// WatchdogInterval is how often we re-resolve lighthouse public hostnames.
const WatchdogInterval = 2 * time.Minute

// WatchdogIPChangeRestartDelay throttles nebula restarts after an IP change so
// a flapping WAN IP can't cause a restart storm.
const WatchdogIPChangeRestartDelay = 1 * time.Minute

// resolveHostnameIPv4 returns the first A record for host, or "" if unresolvable.
func resolveHostnameIPv4(host string) string {
	host = strings.TrimSpace(host)
	if host == "" || net.ParseIP(host) != nil {
		return host
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return ""
	}
	for _, ip := range ips {
		if ip.To4() != nil {
			return ip.String()
		}
	}
	return ""
}

// lastWatchdogIPs remembers, per lighthouse public hostname, the IP we last
// baked into nebula-temp.yml, so we can detect a change and restart.
var (
	watchdogIPsMux  sync.Mutex
	lastWatchdogIPs = map[string]string{}
)

// WatchLighthouseIPs periodically resolves the public hostnames of lighthouses
// (and relays) and restarts nebula when an address changed. Nebula itself
// accepts DNS names in static_host_map / lighthouse.hosts, but it does not
// re-connect when the A record changes — a restart is required to pick up the
// new endpoint.
func WatchLighthouseIPs() {
	if !utils.GetMainConfig().ConstellationConfig.Enabled {
		return
	}

	devices, err := GetAllDevicesEvenBlocked()
	if err != nil {
		utils.Warn("WatchLighthouseIPs: cannot list devices: " + err.Error())
		return
	}

	changed := false
	nowIPs := map[string]string{}

	for _, dev := range devices {
		if dev.Blocked || dev.IP == "" {
			continue
		}
		if !dev.IsLighthouse && !dev.IsRelay {
			continue
		}
		hostname := dev.PublicHostname
		if hostname == "" {
			continue
		}
		for _, h := range strings.Split(hostname, ",") {
			h = strings.TrimSpace(h)
			if h == "" {
				continue
			}
			ip := resolveHostnameIPv4(h)
			if ip == "" {
				continue
			}
			key := h
			nowIPs[key] = ip
			if prev, ok := lastWatchdogIPs[key]; ok && prev != ip && prev != "" {
				utils.Log("WatchLighthouseIPs: " + key + " changed " + prev + " -> " + ip)
				changed = true
			}
		}
	}

	watchdogIPsMux.Lock()
	lastWatchdogIPs = nowIPs
	watchdogIPsMux.Unlock()

	if changed {
		utils.Log("WatchLighthouseIPs: public IP of a lighthouse/relay changed, restarting nebula to reconnect")
		// Wait a beat to let the new A record settle, then restart.
		time.Sleep(WatchdogIPChangeRestartDelay)
		RestartNebula()
	}
}

// RunLighthouseIPWatchdog is the long-running goroutine started from Init().
func RunLighthouseIPWatchdog() {
	for {
		time.Sleep(WatchdogInterval)
		WatchLighthouseIPs()
	}
}
