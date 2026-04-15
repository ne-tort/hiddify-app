package l3routerendpoint

import (
	"fmt"
	"net/netip"
	"strings"

	rt "github.com/sagernet/sing-box/experimental/l3router"
	"github.com/sagernet/sing-box/option"
	E "github.com/sagernet/sing/common/exceptions"
)

func ParseRouteOptions(ro option.L3RouterRouteOptions) (rt.Route, error) {
	var r rt.Route
	r.ID = rt.RouteID(ro.ID)
	r.Owner = ro.Owner
	var err error
	r.AllowedSrc, err = ParsePrefixes(ro.AllowedSrc)
	if err != nil {
		return rt.Route{}, fmt.Errorf("allowed_src: %w", err)
	}
	r.AllowedDst, err = ParsePrefixes(ro.AllowedDst)
	if err != nil {
		return rt.Route{}, fmt.Errorf("allowed_dst: %w", err)
	}
	r.ExportedPrefixes, err = ParsePrefixes(ro.ExportedPrefixes)
	if err != nil {
		return rt.Route{}, fmt.Errorf("exported_prefixes: %w", err)
	}
	return r, nil
}

func ParsePrefixes(items []string) ([]netip.Prefix, error) {
	if len(items) == 0 {
		return nil, nil
	}
	result := make([]netip.Prefix, 0, len(items))
	seen := make(map[netip.Prefix]struct{}, len(items))
	for index, item := range items {
		prefix, err := netip.ParsePrefix(item)
		if err != nil {
			return nil, fmt.Errorf("item[%d]=%q: %w", index, item, err)
		}
		prefix = prefix.Masked()
		if _, exists := seen[prefix]; exists {
			continue
		}
		seen[prefix] = struct{}{}
		result = append(result, prefix)
	}
	return result, nil
}

func ValidateRoute(r rt.Route) error {
	if r.ID == 0 {
		return E.New("route id must be non-zero")
	}
	if strings.TrimSpace(r.Owner) == "" {
		return E.New("route owner must be non-empty")
	}
	if len(r.ExportedPrefixes) == 0 {
		return E.New("route exported_prefixes must not be empty")
	}
	return nil
}
