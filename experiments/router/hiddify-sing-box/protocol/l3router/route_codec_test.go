package l3routerendpoint

import (
	"strings"
	"testing"

	"github.com/sagernet/sing-box/option"
)

func TestParseRouteOptions(t *testing.T) {
	route, err := ParseRouteOptions(option.L3RouterRouteOptions{
		ID:               10,
		Owner:            "owner-a",
		AllowedSrc:       []string{"10.10.0.0/24"},
		AllowedDst:       []string{"10.20.0.0/24"},
		ExportedPrefixes: []string{"10.30.0.0/24"},
	})
	if err != nil {
		t.Fatalf("ParseRouteOptions: %v", err)
	}
	if route.ID != 10 || route.Owner != "owner-a" {
		t.Fatalf("unexpected parsed route: %+v", route)
	}
	if len(route.AllowedSrc) != 1 || len(route.AllowedDst) != 1 || len(route.ExportedPrefixes) != 1 {
		t.Fatalf("unexpected prefixes count: %+v", route)
	}
}

func TestParseRouteOptionsInvalidPrefix(t *testing.T) {
	_, err := ParseRouteOptions(option.L3RouterRouteOptions{
		ID:               11,
		ExportedPrefixes: []string{"not-a-prefix"},
	})
	if err == nil {
		t.Fatal("expected parse error for invalid prefix")
	}
	if !strings.Contains(err.Error(), "exported_prefixes") {
		t.Fatalf("expected contextual field name in error, got: %v", err)
	}
}

func TestParseRouteOptionsDeduplicatesAndMasks(t *testing.T) {
	route, err := ParseRouteOptions(option.L3RouterRouteOptions{
		ID:               12,
		Owner:            "owner-b",
		AllowedSrc:       []string{"10.30.0.0/24"},
		ExportedPrefixes: []string{"10.30.0.7/24", "10.30.0.0/24"},
	})
	if err != nil {
		t.Fatalf("ParseRouteOptions: %v", err)
	}
	if len(route.ExportedPrefixes) != 1 {
		t.Fatalf("expected duplicate prefixes to be deduplicated, got %d", len(route.ExportedPrefixes))
	}
	if route.ExportedPrefixes[0].String() != "10.30.0.0/24" {
		t.Fatalf("expected masked prefix, got %s", route.ExportedPrefixes[0].String())
	}
}

func TestValidateRouteRequiresOwner(t *testing.T) {
	route, err := ParseRouteOptions(option.L3RouterRouteOptions{
		ID:               13,
		ExportedPrefixes: []string{"10.40.0.0/24"},
	})
	if err != nil {
		t.Fatalf("ParseRouteOptions: %v", err)
	}
	if err := ValidateRoute(route); err == nil {
		t.Fatal("expected validation error for missing owner")
	}
}
