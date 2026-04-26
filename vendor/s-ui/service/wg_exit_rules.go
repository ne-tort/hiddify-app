package service

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/alireza0/s-ui/database/model"
	"gorm.io/gorm"
)

const (
	wgExitRuleCommentPrefix = "sui-wg-exit-ep-"
	wgExitIptChainName      = "SUI_WG_EXIT"
	wgExitNftChainName      = "SUI_WG_EXIT"
	wgExitNftJumpComment    = "sui-wg-exit-jump"
	wgExitTableBase         = 30000
	wgExitMarkBase          = 0x530000
	wgExitRulePriority      = 10900
)

type wgExitRuleSpec struct {
	EndpointID uint
	IIF        string
	SourceCIDR string
	Comment    string
	Mark       uint32
	Table      int
}

func (s *EndpointService) ReconcileWireGuardExitRules(db *gorm.DB) error {
	specs, err := loadWireGuardExitSpecs(db)
	if err != nil {
		return err
	}
	backend := detectWGForwardFirewallBackend()
	switch backend {
	case "iptables":
		if err := applyWGExitRulesIPTables(specs); err != nil {
			return err
		}
	case "nft":
		if err := applyWGExitRulesNFT(specs); err != nil {
			return err
		}
	default:
		// no firewall backend, but still cleanup stale policy routing
	}
	return reconcileWGExitPolicyRouting(specs)
}

func loadWireGuardExitSpecs(db *gorm.DB) ([]wgExitRuleSpec, error) {
	if db == nil {
		return nil, nil
	}
	var endpoints []model.Endpoint
	if err := db.Model(model.Endpoint{}).Where("type IN ?", []string{wireGuardType, awgType}).Find(&endpoints).Error; err != nil {
		return nil, err
	}
	specs := make([]wgExitRuleSpec, 0, len(endpoints))
	for _, ep := range endpoints {
		spec, ok := parseWGExitSpecFromEndpoint(&ep)
		if !ok {
			continue
		}
		specs = append(specs, spec)
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].EndpointID < specs[j].EndpointID })
	return specs, nil
}

func parseWGExitSpecFromEndpoint(ep *model.Endpoint) (wgExitRuleSpec, bool) {
	if ep == nil || (ep.Type != wireGuardType && ep.Type != awgType) || len(ep.Options) == 0 {
		return wgExitRuleSpec{}, false
	}
	var options map[string]interface{}
	if err := json.Unmarshal(ep.Options, &options); err != nil || options == nil {
		return wgExitRuleSpec{}, false
	}
	peers := normalizePeerMaps(options["peers"])
	hasExit := false
	for _, p := range peers {
		if boolFromAnyWG(p["peer_exit"]) {
			hasExit = true
			break
		}
	}
	if !hasExit {
		return wgExitRuleSpec{}, false
	}
	ifName := strings.TrimSpace(fmt.Sprint(options["name"]))
	if strings.EqualFold(ifName, "<nil>") {
		ifName = ""
	}
	if ifName != "" {
		if !wgInterfaceNameRe.MatchString(ifName) {
			return wgExitRuleSpec{}, false
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
	if sourceCIDR == "" {
		return wgExitRuleSpec{}, false
	}
	return wgExitRuleSpec{
		EndpointID: ep.Id,
		IIF:        ifName,
		SourceCIDR: sourceCIDR,
		Comment:    fmt.Sprintf("%s%d", wgExitRuleCommentPrefix, ep.Id),
		Mark:       uint32(wgExitMarkBase + int(ep.Id)),
		Table:      wgExitTableBase + int(ep.Id),
	}, true
}

func applyWGExitRulesIPTables(specs []wgExitRuleSpec) error {
	_ = runFirewallCmd("iptables", "-t", "mangle", "-N", wgExitIptChainName)
	_ = runFirewallCmd("iptables", "-t", "mangle", "-F", wgExitIptChainName)
	_ = runFirewallCmd("iptables", "-t", "mangle", "-D", "PREROUTING", "-j", wgExitIptChainName)
	if err := runFirewallCmd("iptables", "-t", "mangle", "-I", "PREROUTING", "1", "-j", wgExitIptChainName); err != nil {
		return err
	}
	for _, spec := range specs {
		inIf := iptablesInterfacePattern(spec.IIF)
		if err := runFirewallCmd(
			"iptables", "-t", "mangle", "-A", wgExitIptChainName,
			"-i", inIf,
			"-s", spec.SourceCIDR,
			"-m", "comment", "--comment", spec.Comment,
			"-j", "MARK", "--set-xmark", fmt.Sprintf("0x%x/0xffffffff", spec.Mark),
		); err != nil {
			return err
		}
	}
	return nil
}

func applyWGExitRulesNFT(specs []wgExitRuleSpec) error {
	for _, family := range []string{"ip", "ip6"} {
		_ = runFirewallCmd("nft", "delete", "chain", family, "mangle", wgExitNftChainName)
	}
	applied := false
	for _, family := range []string{"ip", "ip6"} {
		if runFirewallCmd("nft", "list", "chain", family, "mangle", "PREROUTING") != nil {
			continue
		}
		_ = runFirewallCmd("nft", "add", "chain", family, "mangle", wgExitNftChainName)
		if err := runFirewallCmd("nft", "flush", "chain", family, "mangle", wgExitNftChainName); err != nil {
			return err
		}
		_ = deleteAllNFTJumpRulesInTable(family, "mangle", "PREROUTING", wgExitNftChainName)
		if err := runFirewallCmd(
			"nft", "insert", "rule", family, "mangle", "PREROUTING",
			"jump", wgExitNftChainName, "comment", wgExitNftJumpComment,
		); err != nil {
			return err
		}
		for _, spec := range specs {
			if err := runFirewallCmd(
				"nft", "add", "rule", family, "mangle", wgExitNftChainName,
				"iifname", spec.IIF,
				"ip", "saddr", spec.SourceCIDR,
				"meta", "mark", "set", fmt.Sprintf("0x%x", spec.Mark),
				"comment", spec.Comment,
			); err != nil {
				return err
			}
		}
		applied = true
	}
	if applied {
		return nil
	}
	_ = runFirewallCmd("nft", "add", "table", "inet", "sui_wg_exit")
	_ = runFirewallCmd("nft", "add", "chain", "inet", "sui_wg_exit", "prerouting", "{ type filter hook prerouting priority mangle; policy accept; }")
	if err := runFirewallCmd("nft", "flush", "chain", "inet", "sui_wg_exit", "prerouting"); err != nil {
		return err
	}
	for _, spec := range specs {
		if err := runFirewallCmd(
			"nft", "add", "rule", "inet", "sui_wg_exit", "prerouting",
			"iifname", spec.IIF,
			"ip", "saddr", spec.SourceCIDR,
			"meta", "mark", "set", fmt.Sprintf("0x%x", spec.Mark),
			"comment", spec.Comment,
		); err != nil {
			return err
		}
	}
	return nil
}

func reconcileWGExitPolicyRouting(specs []wgExitRuleSpec) error {
	active := make(map[int]wgExitRuleSpec, len(specs))
	for _, spec := range specs {
		active[spec.Table] = spec
	}

	existingRules, err := listWGExitRules()
	if err != nil {
		return err
	}
	for _, r := range existingRules {
		if spec, ok := active[r.Table]; ok && spec.Mark == r.Mark {
			continue
		}
		if err := runIPCmd("rule", "del", "fwmark", fmt.Sprintf("0x%x", r.Mark), "table", strconv.Itoa(r.Table), "priority", strconv.Itoa(r.Priority)); err != nil {
			return err
		}
	}

	for _, spec := range specs {
		ifName, err := resolveInterfaceByPattern(spec.IIF)
		if err != nil {
			return err
		}
		if err := runIPCmd("route", "replace", "table", strconv.Itoa(spec.Table), "default", "dev", ifName); err != nil {
			return err
		}
		if !hasWGExitRule(existingRules, spec) {
			if err := runIPCmd("rule", "add", "fwmark", fmt.Sprintf("0x%x", spec.Mark), "table", strconv.Itoa(spec.Table), "priority", strconv.Itoa(wgExitRulePriority+int(spec.EndpointID))); err != nil {
				return err
			}
		}
	}
	return nil
}

type wgExitIPRule struct {
	Priority int
	Mark     uint32
	Table    int
}

func listWGExitRules() ([]wgExitIPRule, error) {
	out, err := exec.Command("ip", "rule", "show").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("ip [rule show]: %w: %s", err, strings.TrimSpace(string(out)))
	}
	var rules []wgExitIPRule
	re := regexp.MustCompile(`^([0-9]+):\s+from all fwmark (0x[0-9a-fA-F]+)\s+lookup ([0-9]+)$`)
	for _, ln := range strings.Split(string(out), "\n") {
		line := strings.TrimSpace(ln)
		m := re.FindStringSubmatch(line)
		if len(m) != 4 {
			continue
		}
		table, _ := strconv.Atoi(m[3])
		if table < wgExitTableBase {
			continue
		}
		prio, _ := strconv.Atoi(m[1])
		markVal, _ := strconv.ParseUint(strings.TrimPrefix(m[2], "0x"), 16, 32)
		rules = append(rules, wgExitIPRule{Priority: prio, Mark: uint32(markVal), Table: table})
	}
	return rules, nil
}

func hasWGExitRule(existing []wgExitIPRule, spec wgExitRuleSpec) bool {
	for _, r := range existing {
		if r.Table == spec.Table && r.Mark == spec.Mark {
			return true
		}
	}
	return false
}

func resolveInterfaceByPattern(pattern string) (string, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return "", fmt.Errorf("empty interface pattern")
	}
	prefix := strings.TrimSuffix(pattern, "*")
	out, err := exec.Command("ip", "-o", "link", "show").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ip [-o link show]: %w: %s", err, strings.TrimSpace(string(out)))
	}
	for _, ln := range strings.Split(string(out), "\n") {
		line := strings.TrimSpace(ln)
		if line == "" {
			continue
		}
		idx := strings.Index(line, ": ")
		if idx < 0 {
			continue
		}
		rest := line[idx+2:]
		colon := strings.Index(rest, ":")
		if colon < 0 {
			continue
		}
		name := strings.TrimSpace(rest[:colon])
		if strings.HasPrefix(name, prefix) {
			return name, nil
		}
	}
	return "", fmt.Errorf("no interface matches pattern %q", pattern)
}

func runIPCmd(args ...string) error {
	cmd := exec.Command("ip", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ip %v: %w: %s", args, err, strings.TrimSpace(string(out)))
	}
	return nil
}
