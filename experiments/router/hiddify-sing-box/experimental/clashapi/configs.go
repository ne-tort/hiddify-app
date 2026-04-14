package clashapi

import (
	"net/http"

	"github.com/sagernet/sing-box/log"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

func configRouter(server *Server, logFactory log.Factory) http.Handler {
	r := chi.NewRouter()
	r.Get("/", getConfigs(server, logFactory))
	r.Put("/", updateConfigs)
	r.Patch("/", patchConfigs(server))
	return r
}

type configSchema struct {
	Port        int    `json:"port"`
	SocksPort   int    `json:"socks-port"`
	RedirPort   int    `json:"redir-port"`
	TProxyPort  int    `json:"tproxy-port"`
	MixedPort   int    `json:"mixed-port"`
	AllowLan    bool   `json:"allow-lan"`
	BindAddress string `json:"bind-address"`
	Mode        string `json:"mode"`
	// sing-box added
	ModeList []string       `json:"mode-list"`
	LogLevel string         `json:"log-level"`
	IPv6     bool           `json:"ipv6"`
	Tun      map[string]any `json:"tun"`
	L3Router map[string]any `json:"l3router,omitempty"`
}

func getConfigs(server *Server, logFactory log.Factory) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		logLevel := logFactory.Level()
		if logLevel == log.LevelTrace {
			logLevel = log.LevelDebug
		} else if logLevel < log.LevelError {
			logLevel = log.LevelError
		}
		l3Endpoints, l3Totals := collectL3RouterMetrics(server.endpoint.Endpoints())
		render.JSON(w, r, &configSchema{
			Mode:        server.mode,
			ModeList:    server.modeList,
			BindAddress: "*",
			LogLevel:    log.FormatLevel(logLevel),
			L3Router: map[string]any{
				"endpoints": l3Endpoints,
				"totals":    l3Totals,
			},
		})
	}
}

func patchConfigs(server *Server) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var newConfig configSchema
		err := render.DecodeJSON(r.Body, &newConfig)
		if err != nil {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, ErrBadRequest)
			return
		}
		if newConfig.Mode != "" {
			server.SetMode(newConfig.Mode)
		}
		render.NoContent(w, r)
	}
}

func updateConfigs(w http.ResponseWriter, r *http.Request) {
	render.NoContent(w, r)
}
