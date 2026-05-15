package core

import (
	"github.com/sagernet/sing-box/adapter/endpoint"
	masqueproto "github.com/sagernet/sing-box/protocol/masque"
)

func registerMasqueEndpoints(registry *endpoint.Registry) {
	masqueproto.RegisterEndpoint(registry)
}
