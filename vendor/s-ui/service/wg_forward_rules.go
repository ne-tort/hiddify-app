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
	wgForwardRuleCommentPrefix = "sui-wg-forward-ep-"
	wgForwardNftJumpComment    = "sui-wg-forward-jump"
	wgForwardIptChainName      = "SUI_WG_FORWARD"
	wgForwardNftChainName      = "SUI_WG_FORWARD"
)

var wgInterfaceNameRe = regexp.MustCompile(`^[a-zA-Z0-9_.:-]{1,64}$`)

type wgForwardRuleSpec struct {
	EndpointID uint
	IIF        string
	OIF        string
	Comment    string
}

func (s *EndpointService) ReconcileWireGuardForwardRules(db *gorm.DB) error {
	specs, err := loadWireGuardForwardRuleSpecs(db)
	if err != nil {
		return err
	}
	backend := detectWGForwardFirewallBackend()
	switch backend {
	case "iptables":
		return applyWGForwardRulesIPTables(specs)
	case "nft":
		return applyWGForwardRulesNFT(specs)
	default:
		return nil
	}
}

func loadWireGuardForwardRuleSpecs(db *gorm.DB) ([]wgForwardRuleSpec, error) {
	if db == nil {
		return nil, nil
	}
	var endpoints []model.Endpoint
	if err := db.Model(model.Endpoint{}).Where("type = ?", wireGuardType).Find(&endpoints).Error; err != nil {
		return nil, err
	}
	specs := make([]wgForwardRuleSpec, 0, len(endpoints))
	for _, ep := range endpoints {
		spec, ok := parseWGForwardSpecFromEndpoint(&ep)
		if !ok {
			continue
		}
		specs = append(specs, spec)
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].EndpointID < specs[j].EndpointID })
	return specs, nil
}

func parseWGForwardSpecFromEndpoint(ep *model.Endpoint) (wgForwardRuleSpec, bool) {
	if ep == nil || ep.Type != wireGuardType || len(ep.Options) == 0 {
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
	} else {
		ifName = "wg*"
	}
	comment := fmt.Sprintf("%s%d", wgForwardRuleCommentPrefix, ep.Id)
	return wgForwardRuleSpec{
		EndpointID: ep.Id,
		IIF:        ifName,
		OIF:        ifName,
		Comment:    comment,
	}, true
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

		_ = deleteAllNFTJumpRules(family, "DOCKER-USER", wgForwardNftChainName)
		_ = deleteAllNFTJumpRules(family, "FORWARD", wgForwardNftChainName)
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

func deleteAllNFTJumpRules(family, chain, targetChain string) error {
	out, err := exec.Command("nft", "-a", "list", "chain", family, "filter", chain).CombinedOutput()
	if err != nil {
		return err
	}
	handles := parseNFTJumpHandles(string(out), targetChain)
	sort.Slice(handles, func(i, j int) bool { return handles[i] > handles[j] })
	for _, h := range handles {
		if err := runFirewallCmd("nft", "delete", "rule", family, "filter", chain, "handle", strconv.Itoa(h)); err != nil {
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
		if inIf == "wg*" {
			inIf = "wg+"
		}
		if outIf == "wg*" {
			outIf = "wg+"
		}
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

func runFirewallCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %w: %s", name, args, err, strings.TrimSpace(string(out)))
	}
	return nil
}

