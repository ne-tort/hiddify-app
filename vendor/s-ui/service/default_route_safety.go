package service

import (
	"os/exec"
	"strings"

	"github.com/alireza0/s-ui/util/common"
)

// tunnelIfNamePrefixes are interface name prefixes that indicate a likely VPN/tunnel default route
// (denylist). Physical uplinks like eth0, ens3, enp0s1, wlan0, bond0 are not listed here.
var tunnelIfNamePrefixes = []string{
	"wg", "awg", "tun", "tap", "utun", "nlmesh", "sing", "l2tp", "ppp", "ipsec",
	"gretap", "erspan", "xfrm", "veth",
}

// CheckDefaultRouteNotThroughTunnel returns an error if the host's IPv4 (and when present, IPv6)
// default route uses an interface that looks like a WireGuard/AWG/tun class device.
// Used before enabling "exit node" on a WireGuard/AWG hub to avoid a known-bad host routing state.
func CheckDefaultRouteNotThroughTunnel() error {
	if _, err := exec.LookPath("ip"); err != nil {
		return nil
	}
	if err := checkIPRouteDefault("ip", "-4", "route", "show", "default"); err != nil {
		return err
	}
	// IPv6: if there is no default, `ip -6` may print nothing — ignore.
	if err := checkIPRouteDefault("ip", "-6", "route", "show", "default"); err != nil {
		return err
	}
	return nil
}

func checkIPRouteDefault(ipArgs ...string) error {
	out, err := exec.Command(ipArgs[0], ipArgs[1:]...).CombinedOutput()
	if err != nil {
		// No default route (e.g. v6) is not fatal
		if len(strings.TrimSpace(string(out))) == 0 {
			return nil
		}
		return common.NewErrorf("default route check failed: %v: %s", err, strings.TrimSpace(string(out)))
	}
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		ifName := defaultRouteLineDev(line)
		if ifName == "" {
			continue
		}
		if isTunnelLikeInterface(ifName) {
			return common.NewErrorf(
				"default route uses tunnel-like interface %q; fix host routing so default is via a physical uplink (e.g. eth0) before enabling exit node",
				ifName,
			)
		}
	}
	return nil
}

// defaultRouteLineDev extracts the interface name after "dev" in an `ip route` line.
func defaultRouteLineDev(line string) string {
	parts := strings.Fields(line)
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "dev" {
			return parts[i+1]
		}
	}
	return ""
}

func isTunnelLikeInterface(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return false
	}
	// "ifb*" and similar are sometimes used; treat as non-physical for safety in exit context.
	if strings.HasPrefix(n, "ifb") {
		return true
	}
	for _, p := range tunnelIfNamePrefixes {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}

// ParseDefaultRouteDevsForTest exposes parsed dev names from output (for unit tests).
func parseDefaultRouteDevsForTest(output string) []string {
	var out []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if d := defaultRouteLineDev(line); d != "" {
			out = append(out, d)
		}
	}
	return out
}
