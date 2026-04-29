package service

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/alireza0/s-ui/database/model"
	"gorm.io/gorm"
)

const (
	wgForwardRuleCommentPrefix = "sui-wg-forward-ep-"
	wgForwardNftJumpComment    = "sui-wg-forward-jump"
	wgForwardIptChainName      = "SUI_WG_FORWARD"
	wgForwardNftChainName      = "SUI_WG_FORWARD"
	wgInternetRuleCommentPrefix = "sui-wg-internet-ep-"
	wgInternetNftJumpComment    = "sui-wg-internet-jump"
	wgInternetIptChainName      = "SUI_WG_INTERNET"
	wgInternetNftChainName      = "SUI_WG_INTERNET"
)

var wgInterfaceNameRe = regexp.MustCompile(`^[a-zA-Z0-9_.:-]{1,64}$`)

type wgForwardRuleSpec struct {
	EndpointID uint
	IIF        string
	OIF        string
	Comment    string
}

type wgInternetRuleSpec struct {
	EndpointID  uint
	IIF         string
	SourceCIDR  string
	FwdComment  string
	RetComment  string
	NatComment  string
}

func (s *EndpointService) ReconcileWireGuardForwardRules(db *gorm.DB) error {
	forwardSpecs, internetSpecs, err := loadWireGuardRuleSpecs(db)
	if err != nil {
		return err
	}
	backend := detectWGForwardFirewallBackend()
	switch backend {
	case "iptables":
		if err := applyWGForwardRulesIPTables(forwardSpecs); err != nil {
			return err
		}
		return applyWGInternetRulesIPTables(internetSpecs)
	case "nft":
		if err := applyWGForwardRulesNFT(forwardSpecs); err != nil {
			return err
		}
		return applyWGInternetRulesNFT(internetSpecs)
	default:
		return nil
	}
}

func loadWireGuardRuleSpecs(db *gorm.DB) ([]wgForwardRuleSpec, []wgInternetRuleSpec, error) {
	if db == nil {
		return nil, nil, nil
	}
	var endpoints []model.Endpoint
	if err := db.Model(model.Endpoint{}).Where("type IN ?", []string{wireGuardType, awgType}).Find(&endpoints).Error; err != nil {
		return nil, nil, err
	}
	specs := make([]wgForwardRuleSpec, 0, len(endpoints))
	internetSpecs := make([]wgInternetRuleSpec, 0, len(endpoints))
	for _, ep := range endpoints {
		spec, ok := parseWGForwardSpecFromEndpoint(&ep)
		if !ok {
			// no-op
		} else {
			specs = append(specs, spec)
		}
		internetSpec, ok := parseWGInternetSpecFromEndpoint(&ep)
		if ok {
			internetSpecs = append(internetSpecs, internetSpec)
		}
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].EndpointID < specs[j].EndpointID })
	sort.Slice(internetSpecs, func(i, j int) bool { return internetSpecs[i].EndpointID < internetSpecs[j].EndpointID })
	return specs, internetSpecs, nil
}

func parseWGForwardSpecFromEndpoint(ep *model.Endpoint) (wgForwardRuleSpec, bool) {
	if ep == nil || (ep.Type != wireGuardType && ep.Type != awgType) || len(ep.Options) == 0 {
		return wgForwardRuleSpec{}, false
	}
	var options map[string]interface{}
	if err := json.Unmarshal(ep.Options, &options); err != nil || options == nil {
		return wgForwardRuleSpec{}, false
	}
	if !boolFromAny(options["forward_allow"]) {
		return wgForwardRuleSpec{}, false
	}
	ifName := strings.TrimSpace(fmt.Sprint(options["name"]))
	if strings.EqualFold(ifName, "<nil>") {
		ifName = ""
	}
	if ifName != "" {
		if !wgInterfaceNameRe.MatchString(ifName) {
			return wgForwardRuleSpec{}, false
		}
		ifName = normalizeFirewallInterfacePattern(ifName)
	} else {
		if ep.Type == awgType {
			ifName = "awg*"
		} else {
			ifName = "wg*"
		}
	}
	comment := fmt.Sprintf("%s%d", wgForwardRuleCommentPrefix, ep.Id)
	return wgForwardRuleSpec{
		EndpointID: ep.Id,
		IIF:        ifName,
		OIF:        ifName,
		Comment:    comment,
	}, true
}

func parseWGInternetSpecFromEndpoint(ep *model.Endpoint) (wgInternetRuleSpec, bool) {
	if ep == nil || (ep.Type != wireGuardType && ep.Type != awgType) || len(ep.Options) == 0 {
		return wgInternetRuleSpec{}, false
	}
	var options map[string]interface{}
	if err := json.Unmarshal(ep.Options, &options); err != nil || options == nil {
		return wgInternetRuleSpec{}, false
	}
	if !boolFromAnyDefaultTrue(options["internet_allow"]) {
		return wgInternetRuleSpec{}, false
	}
	ifName := strings.TrimSpace(fmt.Sprint(options["name"]))
	if strings.EqualFold(ifName, "<nil>") {
		ifName = ""
	}
	if ifName != "" {
		if !wgInterfaceNameRe.MatchString(ifName) {
			return wgInternetRuleSpec{}, false
		}
		ifName = normalizeFirewallInterfacePattern(ifName)
	} else {
		if ep.Type == awgType {
			ifName = "awg*"
		} else {
			ifName = "wg*"
		}
	}
	sourceCIDR := firstIPv4EndpointCIDR(options["address"])
	// Hub client mode usually keeps local tunnel address as /32.
	// For internet egress NAT we need client subnet from peer allowed_ips (e.g. 10.5.0.0/24).
	if boolFromAny(options["hub_client_mode"]) {
		if peerCIDR := firstIPv4PeerAllowedCIDR(options["peers"]); peerCIDR != "" {
			sourceCIDR = peerCIDR
		}
	}
	if sourceCIDR == "" {
		return wgInternetRuleSpec{}, false
	}
	return wgInternetRuleSpec{
		EndpointID: ep.Id,
		IIF:        ifName,
		SourceCIDR: sourceCIDR,
		FwdComment: fmt.Sprintf("%s%d-fwd", wgInternetRuleCommentPrefix, ep.Id),
		RetComment: fmt.Sprintf("%s%d-ret", wgInternetRuleCommentPrefix, ep.Id),
		NatComment: fmt.Sprintf("%s%d-nat", wgInternetRuleCommentPrefix, ep.Id),
	}, true
}

func firstIPv4PeerAllowedCIDR(raw interface{}) string {
	peers, ok := raw.([]interface{})
	if !ok {
		return ""
	}
	for _, peerRaw := range peers {
		peerMap, ok := peerRaw.(map[string]interface{})
		if !ok || peerMap == nil {
			continue
		}
		for _, value := range toStringSlice(peerMap["allowed_ips"]) {
			value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
			if value == "" {
				continue
			}
			prefix, err := netip.ParsePrefix(value)
			if err != nil || !prefix.Addr().Is4() {
				continue
			}
			// Skip full defaults; we need concrete VPN source range for NAT.
			if prefix.Bits() == 0 {
				continue
			}
			return prefix.Masked().String()
		}
	}
	return ""
}

func boolFromAny(v interface{}) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		s := strings.TrimSpace(strings.ToLower(x))
		return s == "true" || s == "1" || s == "yes" || s == "on"
	case float64:
		return x != 0
	case int:
		return x != 0
	default:
		return false
	}
}

func boolFromAnyDefaultTrue(v interface{}) bool {
	if v == nil {
		return true
	}
	return boolFromAny(v)
}

func firstIPv4EndpointCIDR(raw interface{}) string {
	for _, value := range toStringSlice(raw) {
		value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
		if value == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(value)
		if err != nil || !prefix.Addr().Is4() {
			continue
		}
		return prefix.Masked().String()
	}
	return ""
}

func detectWGForwardFirewallBackend() string {
	// Prefer iptables when available: on iptables-nft systems this is the active FORWARD path.
	if _, err := exec.LookPath("iptables"); err == nil {
		if err := runFirewallCmd("iptables", "-S", "FORWARD"); err == nil {
			return "iptables"
		}
	}
	if _, err := exec.LookPath("nft"); err == nil {
		if err := runFirewallCmd("nft", "list", "tables"); err == nil {
			return "nft"
		}
	}
	return ""
}

func applyWGForwardRulesNFT(specs []wgForwardRuleSpec) error {
	// Cleanup legacy variants from older implementations (best-effort, ignore errors).
	_ = runFirewallCmd("nft", "delete", "table", "inet", "sui_wg_forward")
	_ = runFirewallCmd("nft", "flush", "chain", "inet", "filter", "sui_wg_forward")
	_ = runFirewallCmd("nft", "delete", "chain", "inet", "filter", "sui_wg_forward")

	applied := false
	for _, family := range []string{"ip", "ip6"} {
		anchor := "FORWARD"
		if runFirewallCmd("nft", "list", "chain", family, "filter", "DOCKER-USER") == nil {
			anchor = "DOCKER-USER"
		} else if runFirewallCmd("nft", "list", "chain", family, "filter", "FORWARD") != nil {
			continue
		}

		_ = runFirewallCmd("nft", "add", "chain", family, "filter", wgForwardNftChainName)
		if err := runFirewallCmd("nft", "flush", "chain", family, "filter", wgForwardNftChainName); err != nil {
			return err
		}

		_ = deleteAllNFTJumpRulesInTable(family, "filter", "DOCKER-USER", wgForwardNftChainName)
		_ = deleteAllNFTJumpRulesInTable(family, "filter", "FORWARD", wgForwardNftChainName)
		if err := runFirewallCmd(
			"nft", "insert", "rule", family, "filter", anchor,
			"jump", wgForwardNftChainName, "comment", wgForwardNftJumpComment,
		); err != nil {
			return err
		}

		for _, spec := range specs {
			if err := runFirewallCmd(
				"nft", "add", "rule", family, "filter", wgForwardNftChainName,
				"iifname", spec.IIF, "oifname", spec.OIF, "accept", "comment", spec.Comment,
			); err != nil {
				return err
			}
		}
		applied = true
	}
	if applied {
		return nil
	}

	// Fallback for nft-only setups without ip/ip6 filter tables.
	_ = runFirewallCmd("nft", "add", "table", "inet", "sui_wg_forward")
	_ = runFirewallCmd(
		"nft", "add", "chain", "inet", "sui_wg_forward", "forward",
		"{ type filter hook forward priority -310; policy accept; }",
	)
	if err := runFirewallCmd("nft", "flush", "chain", "inet", "sui_wg_forward", "forward"); err != nil {
		return err
	}
	for _, spec := range specs {
		if err := runFirewallCmd(
			"nft", "add", "rule", "inet", "sui_wg_forward", "forward",
			"iifname", spec.IIF, "oifname", spec.OIF, "accept", "comment", spec.Comment,
		); err != nil {
			return err
		}
	}
	return nil
}

func deleteAllNFTJumpRulesInTable(family, tableName, chain, targetChain string) error {
	out, err := exec.Command("nft", "-a", "list", "chain", family, tableName, chain).CombinedOutput()
	if err != nil {
		return err
	}
	handles := parseNFTJumpHandles(string(out), targetChain)
	sort.Slice(handles, func(i, j int) bool { return handles[i] > handles[j] })
	for _, h := range handles {
		if err := runFirewallCmd("nft", "delete", "rule", family, tableName, chain, "handle", strconv.Itoa(h)); err != nil {
			return err
		}
	}
	return nil
}

func parseNFTJumpHandles(chainDump, targetChain string) []int {
	if strings.TrimSpace(chainDump) == "" || strings.TrimSpace(targetChain) == "" {
		return nil
	}
	lines := strings.Split(chainDump, "\n")
	handleRe := regexp.MustCompile(`# handle ([0-9]+)$`)
	seen := map[int]struct{}{}
	handles := make([]int, 0)
	for _, ln := range lines {
		line := strings.TrimSpace(ln)
		if line == "" || !strings.Contains(line, "jump "+targetChain) {
			continue
		}
		m := handleRe.FindStringSubmatch(line)
		if len(m) != 2 {
			continue
		}
		h, convErr := strconv.Atoi(m[1])
		if convErr != nil {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		handles = append(handles, h)
	}
	return handles
}

func normalizeFirewallInterfacePattern(ifName string) string {
	if strings.TrimSpace(ifName) == "" {
		return ifName
	}
	if strings.HasSuffix(ifName, "*") {
		return ifName
	}
	// sing-tun/amneziawg-go may auto-suffix interface names with an index
	// (e.g. "awg" -> "awg0"), so firewall matching should use a prefix pattern.
	return ifName + "*"
}

func iptablesInterfacePattern(ifName string) string {
	if strings.HasSuffix(ifName, "*") {
		return strings.TrimSuffix(ifName, "*") + "+"
	}
	return ifName
}

func applyWGForwardRulesIPTables(specs []wgForwardRuleSpec) error {
	anchorChain := "FORWARD"
	if runFirewallCmd("iptables", "-S", "DOCKER-USER") == nil {
		anchorChain = "DOCKER-USER"
	}
	_ = runFirewallCmd("iptables", "-N", wgForwardIptChainName)
	_ = runFirewallCmd("iptables", "-F", wgForwardIptChainName)
	_ = runFirewallCmd("iptables", "-D", "DOCKER-USER", "-j", wgForwardIptChainName)
	_ = runFirewallCmd("iptables", "-D", "FORWARD", "-j", wgForwardIptChainName)
	if err := runFirewallCmd("iptables", "-I", anchorChain, "1", "-j", wgForwardIptChainName); err != nil {
		return err
	}
	for _, spec := range specs {
		inIf := spec.IIF
		outIf := spec.OIF
		inIf = iptablesInterfacePattern(inIf)
		outIf = iptablesInterfacePattern(outIf)
		if err := runFirewallCmd(
			"iptables", "-A", wgForwardIptChainName,
			"-i", inIf, "-o", outIf,
			"-m", "comment", "--comment", spec.Comment,
			"-j", "ACCEPT",
		); err != nil {
			return err
		}
	}
	return nil
}

func applyWGInternetRulesIPTables(specs []wgInternetRuleSpec) error {
	anchorChain := "FORWARD"
	if runFirewallCmd("iptables", "-S", "DOCKER-USER") == nil {
		anchorChain = "DOCKER-USER"
	}
	_ = runFirewallCmd("iptables", "-N", wgInternetIptChainName)
	_ = runFirewallCmd("iptables", "-F", wgInternetIptChainName)
	_ = runFirewallCmd("iptables", "-D", "DOCKER-USER", "-j", wgInternetIptChainName)
	_ = runFirewallCmd("iptables", "-D", "FORWARD", "-j", wgInternetIptChainName)
	if err := runFirewallCmd("iptables", "-I", anchorChain, "1", "-j", wgInternetIptChainName); err != nil {
		return err
	}
	for _, spec := range specs {
		inIf := spec.IIF
		inIf = iptablesInterfacePattern(inIf)
		// WG/AWG clients to WAN.
		if err := runFirewallCmd(
			"iptables", "-A", wgInternetIptChainName,
			"-i", inIf, "!", "-o", inIf,
			"-m", "comment", "--comment", spec.FwdComment,
			"-j", "ACCEPT",
		); err != nil {
			return err
		}
		// Return path for established internet connections.
		if err := runFirewallCmd(
			"iptables", "-A", wgInternetIptChainName,
			"!", "-i", inIf, "-o", inIf,
			"-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED",
			"-m", "comment", "--comment", spec.RetComment,
			"-j", "ACCEPT",
		); err != nil {
			return err
		}
	}

	_ = runFirewallCmd("iptables", "-t", "nat", "-N", wgInternetIptChainName)
	_ = runFirewallCmd("iptables", "-t", "nat", "-F", wgInternetIptChainName)
	_ = runFirewallCmd("iptables", "-t", "nat", "-D", "POSTROUTING", "-j", wgInternetIptChainName)
	if err := runFirewallCmd("iptables", "-t", "nat", "-I", "POSTROUTING", "1", "-j", wgInternetIptChainName); err != nil {
		return err
	}
	for _, spec := range specs {
		outIf := spec.IIF
		outIf = iptablesInterfacePattern(outIf)
		if err := runFirewallCmd(
			"iptables", "-t", "nat", "-A", wgInternetIptChainName,
			"-s", spec.SourceCIDR, "!", "-o", outIf,
			"-m", "comment", "--comment", spec.NatComment,
			"-j", "MASQUERADE",
		); err != nil {
			return err
		}
	}
	return nil
}

func applyWGInternetRulesNFT(specs []wgInternetRuleSpec) error {
	_ = runFirewallCmd("nft", "delete", "table", "inet", "sui_wg_internet")
	_ = runFirewallCmd("nft", "flush", "chain", "inet", "filter", "sui_wg_internet")
	_ = runFirewallCmd("nft", "delete", "chain", "inet", "filter", "sui_wg_internet")
	_ = runFirewallCmd("nft", "flush", "chain", "ip", "nat", wgInternetNftChainName)
	_ = runFirewallCmd("nft", "delete", "chain", "ip", "nat", wgInternetNftChainName)

	applied := false
	for _, family := range []string{"ip", "ip6"} {
		anchor := "FORWARD"
		if runFirewallCmd("nft", "list", "chain", family, "filter", "DOCKER-USER") == nil {
			anchor = "DOCKER-USER"
		} else if runFirewallCmd("nft", "list", "chain", family, "filter", "FORWARD") != nil {
			continue
		}
		_ = runFirewallCmd("nft", "add", "chain", family, "filter", wgInternetNftChainName)
		if err := runFirewallCmd("nft", "flush", "chain", family, "filter", wgInternetNftChainName); err != nil {
			return err
		}
		_ = deleteAllNFTJumpRulesInTable(family, "filter", "DOCKER-USER", wgInternetNftChainName)
		_ = deleteAllNFTJumpRulesInTable(family, "filter", "FORWARD", wgInternetNftChainName)
		if err := runFirewallCmd(
			"nft", "insert", "rule", family, "filter", anchor,
			"jump", wgInternetNftChainName, "comment", wgInternetNftJumpComment,
		); err != nil {
			return err
		}
		for _, spec := range specs {
			if err := runFirewallCmd(
				"nft", "add", "rule", family, "filter", wgInternetNftChainName,
				"iifname", spec.IIF, "oifname", "!=", spec.IIF, "accept", "comment", spec.FwdComment,
			); err != nil {
				return err
			}
			if err := runFirewallCmd(
				"nft", "add", "rule", family, "filter", wgInternetNftChainName,
				"iifname", "!=", spec.IIF, "oifname", spec.IIF, "ct", "state", "related,established",
				"accept", "comment", spec.RetComment,
			); err != nil {
				return err
			}
		}
		applied = true
	}

	_ = runFirewallCmd("nft", "add", "chain", "ip", "nat", wgInternetNftChainName)
	_ = deleteAllNFTJumpRulesInTable("ip", "nat", "POSTROUTING", wgInternetNftChainName)
	if err := runFirewallCmd("nft", "insert", "rule", "ip", "nat", "POSTROUTING", "jump", wgInternetNftChainName); err != nil {
		return err
	}
	if err := runFirewallCmd("nft", "flush", "chain", "ip", "nat", wgInternetNftChainName); err != nil {
		return err
	}
	for _, spec := range specs {
		if err := runFirewallCmd(
			"nft", "add", "rule", "ip", "nat", wgInternetNftChainName,
			"ip", "saddr", spec.SourceCIDR, "oifname", "!=", spec.IIF, "masquerade", "comment", spec.NatComment,
		); err != nil {
			return err
		}
	}

	if applied {
		return nil
	}
	return nil
}

func runFirewallCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %w: %s", name, args, err, strings.TrimSpace(string(out)))
	}
	return nil
}

