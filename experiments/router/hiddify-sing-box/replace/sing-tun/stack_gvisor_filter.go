//go:build with_gvisor

package tun

import (
	"net/netip"

	"github.com/sagernet/gvisor/pkg/tcpip"
	"github.com/sagernet/gvisor/pkg/tcpip/header"
	"github.com/sagernet/gvisor/pkg/tcpip/stack"
)

var _ stack.LinkEndpoint = (*LinkEndpointFilter)(nil)

type LinkEndpointFilter struct {
	stack.LinkEndpoint
	BroadcastAddress    netip.Addr
	Writer              GVisorTun
	L3OverlayPrefixes   []netip.Prefix
	L3OverlaySend       func([]byte) error
}

func (w *LinkEndpointFilter) Attach(dispatcher stack.NetworkDispatcher) {
	w.LinkEndpoint.Attach(&networkDispatcherFilter{
		NetworkDispatcher: dispatcher,
		broadcastAddress:  w.BroadcastAddress,
		writer:            w.Writer,
		l3Prefixes:        w.L3OverlayPrefixes,
		l3Send:            w.L3OverlaySend,
	})
}

var _ stack.NetworkDispatcher = (*networkDispatcherFilter)(nil)

type networkDispatcherFilter struct {
	stack.NetworkDispatcher
	broadcastAddress netip.Addr
	writer           GVisorTun
	l3Prefixes       []netip.Prefix
	l3Send           func([]byte) error
}

func (w *networkDispatcherFilter) DeliverNetworkPacket(protocol tcpip.NetworkProtocolNumber, pkt *stack.PacketBuffer) {
	var network header.Network
	if protocol == header.IPv4ProtocolNumber {
		if headerPackets, loaded := pkt.Data().PullUp(header.IPv4MinimumSize); loaded {
			network = header.IPv4(headerPackets)
		}
	} else {
		if headerPackets, loaded := pkt.Data().PullUp(header.IPv6MinimumSize); loaded {
			network = header.IPv6(headerPackets)
		}
	}
	if network == nil {
		w.NetworkDispatcher.DeliverNetworkPacket(protocol, pkt)
		return
	}
	destination := AddrFromAddress(network.DestinationAddress())
	if w.l3Send != nil && len(w.l3Prefixes) > 0 && prefixListContains(w.l3Prefixes, destination) {
		packetSlice := append([]byte(nil), pkt.Data().AsRange().ToSlice()...)
		_ = w.l3Send(packetSlice)
		pkt.DecRef()
		return
	}
	if destination == w.broadcastAddress || !destination.IsGlobalUnicast() {
		w.writer.WritePacket(pkt)
		return
	}
	w.NetworkDispatcher.DeliverNetworkPacket(protocol, pkt)
}
