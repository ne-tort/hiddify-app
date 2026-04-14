package option

// L3RouterRouteOptions is one control-plane route (MemEngine UpsertRoute + owner for session binding).
type L3RouterRouteOptions struct {
	ID               uint64   `json:"id"`
	Owner            string   `json:"owner,omitempty"`
	AllowedSrc       []string `json:"allowed_src,omitempty"`
	AllowedDst       []string `json:"allowed_dst,omitempty"`
	ExportedPrefixes []string `json:"exported_prefixes,omitempty"`
}

// L3RouterEndpointOptions configures the L3 Router endpoint data-plane and static routes.
type L3RouterEndpointOptions struct {
	// Routes are registered into MemEngine at startup (UpsertRoute).
	Routes []L3RouterRouteOptions `json:"routes,omitempty"`
	// OverlayDestination is the UDP destination used when writing forwarded raw IP packets to a peer session
	// (must match what clients use for the IP-in-UDP tunnel, e.g. 198.18.0.1:33333).
	OverlayDestination string `json:"overlay_destination,omitempty"`
}
