package utils

import (
	"time"
)

// DDNSUpdateNow performs a single immediate deSEC DDNS update using the
// current config. It is safe to call concurrently and is used by the settings
// API (configapi) and the installer right after configuring DDNS. Returns true
// if an update was performed, false if DDNS is not configured/enabled.
func DDNSUpdateNow() bool {
	config := GetMainConfig()
	if !config.DDNS.Enabled || config.DDNS.FQDN == "" || config.DDNS.Token == "" {
		return false
	}

	publicIP, err := GetPublicIPv4()
	if err != nil {
		Warn("DDNSUpdateNow: public IP lookup failed: " + err.Error())
		return false
	}

	if publicIP == config.DDNS.LastKnownIP {
		// IP unchanged; still refresh the UPnP lease opportunistically.
		refreshUPnPMappingSafely()
		return true
	}

	if err := DesecDynDNSUpdate(config.DDNS.FQDN, config.DDNS.Token, publicIP, ""); err != nil {
		Error("DDNSUpdateNow: update failed for "+config.DDNS.FQDN+": "+err.Error(), nil)
		return false
	}

	cfg := ReadConfigFromFile()
	cfg.DDNS.LastKnownIP = publicIP
	cfg.DDNS.LastUpdate = time.Now()
	SetBaseMainConfig(cfg)

	Log("DDNSUpdateNow: updated " + config.DDNS.FQDN + " -> " + publicIP)

	refreshUPnPMappingSafely()
	return true
}

// refreshUPnPMappingSafely re-asserts the UDP 4242 mapping if Constellation is
// enabled; failures are logged but not fatal (some routers don't support UPnP).
func refreshUPnPMappingSafely() {
	config := GetMainConfig()
	if !config.ConstellationConfig.Enabled {
		return
	}
	if err := AddUPnPPortMapping(); err != nil {
		Warn("DDNS: UPnP port mapping refresh failed: " + err.Error())
	}
}