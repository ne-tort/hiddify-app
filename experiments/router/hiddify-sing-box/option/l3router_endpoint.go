package option

// L3RouterRouteOptions is one peer-like static route in l3router JSON config.
type L3RouterRouteOptions struct {
	ID               uint64   `json:"id"`
	Owner            string   `json:"owner,omitempty"`
	AllowedSrc       []string `json:"allowed_src,omitempty"`
	AllowedDst       []string `json:"allowed_dst,omitempty"`
	ExportedPrefixes []string `json:"exported_prefixes,omitempty"`
}

// L3RouterEndpointOptions configures the L3 Router endpoint data-plane and static routes.
type L3RouterEndpointOptions struct {
	// Routes are registered into MemEngine at startup (static bootstrap path).
	Routes []L3RouterRouteOptions `json:"routes,omitempty"`
	// OverlayDestination is the UDP destination used when writing forwarded raw IP packets to a peer session
	// (must match what clients use for the IP-in-UDP tunnel, e.g. 198.18.0.1:33333).
	OverlayDestination string `json:"overlay_destination,omitempty"`
	// ACLEnabled toggles AllowedSrc/AllowedDst enforcement in dataplane.
	// Default false for minimal routing path.
	ACLEnabled bool `json:"acl_enabled,omitempty"`
	// FragmentPolicy controls IPv4 fragment handling: allow|drop.
	FragmentPolicy string `json:"fragment_policy,omitempty"`
	// OverflowPolicy controls egress queue overflow behavior: drop_new|drop_oldest.
	OverflowPolicy string `json:"overflow_policy,omitempty"`
	// TelemetryLevel controls per-packet metric overhead: off|minimal|default|forensic.
	TelemetryLevel string `json:"telemetry_level,omitempty"`
}
