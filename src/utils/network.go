package utils

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/huin/goupnp"
	"github.com/huin/goupnp/dcps/internetgateway1"
	"github.com/huin/goupnp/dcps/internetgateway2"
)

// Well-known public-IP echo services (plain text, IPv4-first). We try them in
// order and fall back to the next on any failure.
var publicIPEchoServices = []string{
	"https://api.ipify.org",
	"https://ifconfig.me/ip",
	"https://ipv4.icanhazip.com",
	"https://checkip.amazonaws.com",
}

// NetworkInfo describes the WAN-facing state of this host.
type NetworkInfo struct {
	PublicIP         string `json:"publicIp"`
	IPv6             string `json:"ipv6,omitempty"`
	IsCGNAT          bool   `json:"isCGNAT"`
	RouterExternalIP string `json:"routerExternalIp,omitempty"`
	RouterVendor     string `json:"routerVendor,omitempty"`
	RouterModel      string `json:"routerModel,omitempty"`
	RouterName       string `json:"routerName,omitempty"`
	UPnPAvailable    bool   `json:"upnpAvailable"`
	UPnPError        string `json:"upnpError,omitempty"`
	LANIP            string `json:"lanIp,omitempty"`
}

func fetchPlainText(url string, timeout time.Duration) (string, error) {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// GetPublicIPv4 returns this host's public IPv4 address. If none of the echo
// services respond, returns an error.
func GetPublicIPv4() (string, error) {
	var lastErr error
	for _, svc := range publicIPEchoServices {
		ip, err := fetchPlainText(svc, 10*time.Second)
		if err == nil && net.ParseIP(ip) != nil && strings.Contains(ip, ".") {
			return ip, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("no public IP echo service reachable")
	}
	return "", lastErr
}

// resolveFirstIPv4 returns the first A record for a hostname, or "".
func resolveFirstIPv4(host string) string {
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

// isCGNATRange reports whether ip is in 100.64.0.0/10 (RFC 6598).
func isCGNATRange(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	v4 := parsed.To4()
	return v4 != nil && v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127
}

// GetLocalIP returns the first non-loopback private IPv4 on this host.
func GetLocalIP() string {
	ips, _ := ListIps(false)
	for _, ip := range ips {
		if net.ParseIP(ip) != nil && !strings.HasPrefix(ip, "127.") && !strings.HasPrefix(ip, "169.254.") {
			return ip
		}
	}
	return ""
}

// DetectNetwork gathers public-IP / CGNAT / router-vendor info.
func DetectNetwork() NetworkInfo {
	info := NetworkInfo{}

	pub, err := GetPublicIPv4()
	if err != nil {
		Warn("DetectNetwork: public IP lookup failed: " + err.Error())
	} else {
		info.PublicIP = pub
	}

	info.LANIP = GetLocalIP()

	// Try UPnP IGD discovery for external IP + vendor.
	if res, err := discoverIGD(); err == nil {
		info.UPnPAvailable = true
		if ext, err := res.client.GetExternalIPAddress(); err == nil {
			info.RouterExternalIP = ext
		}
		if res.root != nil {
			info.RouterVendor = res.root.Device.Manufacturer
			info.RouterModel = res.root.Device.ModelName
			info.RouterName = res.root.Device.FriendlyName
		}
	} else {
		info.UPnPError = err.Error()
	}

	// CGNAT heuristic: public IP in RFC6598 range => definitely CGNAT.
	if info.PublicIP != "" && isCGNATRange(info.PublicIP) {
		info.IsCGNAT = true
	}

	return info
}

// ── UPnP helpers ────────────────────────────────────────────────────────────

// igdClient abstracts both WANIPConnection v1 and v2 so callers can treat them
// uniformly for the operations we need.
type igdClient interface {
	AddPortMapping(newRemoteHost string, newExternalPort uint16, newProtocol string, newInternalPort uint16, newInternalClient string, newEnabled bool, newPortMappingDescription string, newLeaseDuration uint32) (err error)
	DeletePortMapping(newRemoteHost string, newExternalPort uint16, newProtocol string) (err error)
	GetExternalIPAddress() (newExternalIPAddress string, err error)
	GetSpecificPortMappingEntry(newRemoteHost string, newExternalPort uint16, newProtocol string) (newInternalPort uint16, newInternalClient string, newEnabled bool, newPortMappingDescription string, newLeaseDuration uint32, err error)
}

type igdResult struct {
	client igdClient
	root   *goupnp.RootDevice
}

func discoverIGD() (*igdResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	// Discover all IGD devices (v2 preferred, fall back to v1).
	devices, err := goupnp.DiscoverDevicesCtx(ctx, "urn:schemas-upnp-org:device:InternetGatewayDevice:2")
	if err == nil && len(devices) > 0 {
		for _, d := range devices {
			if d.Err != nil || d.Root == nil {
				continue
			}
			clients, err := internetgateway2.NewWANIPConnection2ClientsFromRootDevice(d.Root, &d.Root.URLBase)
			if err == nil && len(clients) > 0 {
				return &igdResult{clients[0], d.Root}, nil
			}
			clients1, err1 := internetgateway1.NewWANIPConnection1ClientsFromRootDevice(d.Root, &d.Root.URLBase)
			if err1 == nil && len(clients1) > 0 {
				return &igdResult{clients1[0], d.Root}, nil
			}
		}
	}

	// Fall back to v1 discovery.
	devices1, err1 := goupnp.DiscoverDevicesCtx(ctx, "urn:schemas-upnp-org:device:InternetGatewayDevice:1")
	if err1 == nil {
		for _, d := range devices1 {
			if d.Err != nil || d.Root == nil {
				continue
			}
			clients1, errC := internetgateway1.NewWANIPConnection1ClientsFromRootDevice(d.Root, &d.Root.URLBase)
			if errC == nil && len(clients1) > 0 {
				return &igdResult{clients1[0], d.Root}, nil
			}
		}
	}

	return nil, errors.New("no UPnP IGD found")
}

// AddUPnPPortMapping adds a UDP port mapping for Constellation (4242) to this
// host's LAN IP. Returns an error if discovery or mapping fails.
func AddUPnPPortMapping() error {
	res, err := discoverIGD()
	if err != nil {
		return err
	}
	lanIP := GetLocalIP()
	if lanIP == "" {
		return errors.New("no local IP to map")
	}
	if err := res.client.AddPortMapping("", 4242, "UDP", 4242, lanIP, true, "Cosmos Constellation", 0); err == nil {
		return nil
	} else {
		// Some routers reject lease 0; retry with a long lease.
		if err2 := res.client.AddPortMapping("", 4242, "UDP", 4242, lanIP, true, "Cosmos Constellation", 86400); err2 == nil {
			return nil
		} else {
			return fmt.Errorf("UPnP AddPortMapping failed: %v (lease0: %v)", err2, err)
		}
	}
}

// RemoveUPnPPortMapping removes a previously-added UDP 4242 mapping.
func RemoveUPnPPortMapping() error {
	res, err := discoverIGD()
	if err != nil {
		return err
	}
	if err := res.client.DeletePortMapping("", 4242, "UDP"); err != nil {
		return fmt.Errorf("UPnP DeletePortMapping failed: %w", err)
	}
	return nil
}

// HasUPnPPortMapping checks whether the UDP 4242 mapping exists.
func HasUPnPPortMapping() bool {
	res, err := discoverIGD()
	if err != nil {
		return false
	}
	_, _, _, _, _, err = res.client.GetSpecificPortMappingEntry("", 4242, "UDP")
	return err == nil
}