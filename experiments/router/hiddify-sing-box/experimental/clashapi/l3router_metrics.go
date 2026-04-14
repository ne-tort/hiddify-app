package clashapi

import (
	"net/http"

	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
	l3routerendpoint "github.com/sagernet/sing-box/protocol/l3router"

	"github.com/go-chi/render"
)

type l3RouterEndpointMetrics struct {
	Type    string                   `json:"type"`
	Tag     string                   `json:"tag"`
	Metrics l3routerendpoint.Metrics `json:"metrics"`
}

type l3RouterMetricsResponse struct {
	Endpoints []l3RouterEndpointMetrics `json:"endpoints"`
	Totals    l3routerendpoint.Metrics  `json:"totals"`
}

func l3RouterMetrics(server *Server) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		endpoints, totals := collectL3RouterMetrics(server.endpoint.Endpoints())
		render.JSON(w, r, l3RouterMetricsResponse{
			Endpoints: endpoints,
			Totals:    totals,
		})
	}
}

func collectL3RouterMetrics(endpoints []adapter.Endpoint) ([]l3RouterEndpointMetrics, l3routerendpoint.Metrics) {
	result := make([]l3RouterEndpointMetrics, 0, len(endpoints))
	var totals l3routerendpoint.Metrics
	for _, ep := range endpoints {
		if ep.Type() != C.TypeL3Router {
			continue
		}
		snapshotter, ok := ep.(interface {
			SnapshotMetrics() l3routerendpoint.Metrics
		})
		if !ok {
			continue
		}
		metrics := snapshotter.SnapshotMetrics()
		result = append(result, l3RouterEndpointMetrics{
			Type:    ep.Type(),
			Tag:     ep.Tag(),
			Metrics: metrics,
		})
		totals.IngressPackets += metrics.IngressPackets
		totals.ForwardPackets += metrics.ForwardPackets
		totals.DropPackets += metrics.DropPackets
		totals.EgressWriteFail += metrics.EgressWriteFail
		totals.ControlUpsertOK += metrics.ControlUpsertOK
		totals.ControlRemoveOK += metrics.ControlRemoveOK
		totals.ControlErrors += metrics.ControlErrors
	}
	return result, totals
}
