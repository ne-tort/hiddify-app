// Package l3router defines the public L3 Router contract: peer-like routes, ACL, FIB lookup,
// and ingress (packet + session identity) without embedding any specific inbound protocol.
package l3router

import (
	"net/netip"
)

// SessionKey identifies an authenticated inbound session. It is produced by the sing-box protocol
// layer (user name, account id, or other stable session label) and consumed as an opaque key by Router.
type SessionKey string

// RouteID identifies a logical Route (control-plane object), analogous to a WireGuard peer id
// but without cryptographic meaning.
type RouteID uint64

// Route describes one logical route for control-plane registration (policies, prefixes, ownership).
type Route struct {
	ID RouteID
	// Owner is an optional policy label (user, group, or policy id string).
	Owner string
	// AllowedSrc limits acceptable source addresses on ingress (anti-spoof / AllowedIPs-style).
	AllowedSrc []netip.Prefix
	// AllowedDst limits acceptable destinations for this route when used as ingress (optional policy).
	AllowedDst []netip.Prefix
	// ExportedPrefixes are prefixes announced into the FIB for longest-prefix egress selection.
	ExportedPrefixes []netip.Prefix
}

// Action is the data-plane disposition for one packet.
type Action uint8

const (
	ActionDrop Action = iota
	ActionForward
)

// Decision is the outcome of processing one ingress IP datagram. Protocol details stay outside:
// only SessionKey and optional egress hint are visible at this boundary.
type Decision struct {
	Action Action
	// EgressSession, when Action == ActionForward, selects the target delivery session.
	EgressSession SessionKey
	DropReason    DropReason
}

type DropReason uint8

const (
	DropUnknown DropReason = iota
	DropMalformedPacket
	DropNoIngressRoute
	DropACLSource
	DropACLDestination
	DropNoEgressRoute
)

// Engine is the Router data plane: ACL + FIB. Implementations must not depend on inbound protocol types.
type Engine interface {
	HandleIngress(packet []byte, ingress SessionKey) Decision
}

// RouteStore holds control-plane Route definitions (create/update/remove). Runtime may combine this
// with session binding (which SessionKey maps to which RouteID) in a separate registry.
type RouteStore interface {
	UpsertRoute(r Route)
	RemoveRoute(id RouteID)
}

// SessionBinding maps authenticated inbound sessions to a Route (ingress identity) and registers
// which SessionKey receives forwarded traffic for each Route (egress delivery target).
type SessionBinding interface {
	SetIngressSession(routeID RouteID, session SessionKey)
	ClearIngressSession(session SessionKey)
	SetEgressSession(routeID RouteID, session SessionKey)
	ClearEgressSession(routeID RouteID)
}
