package sub

import (
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/alireza0/s-ui/database"
	"github.com/alireza0/s-ui/logger"
	"github.com/alireza0/s-ui/service"

	"github.com/gin-gonic/gin"
)

type SubHandler struct {
	service.SettingService
	service.RuleSetService
	service.GeoDatService
	SubService
	JsonService
	ClashService
}

func NewSubHandler(g *gin.RouterGroup) {
	a := &SubHandler{}
	a.initRouter(g)
}

// subscriptionRequestHost is the hostname clients use to reach the subscription endpoint
// (no port), for filling WireGuard "server" when subDomain/subURI omit a host.
func subscriptionRequestHost(c *gin.Context) string {
	h := strings.TrimSpace(c.GetHeader("X-Forwarded-Host"))
	if h != "" {
		if i := strings.IndexByte(h, ','); i >= 0 {
			h = strings.TrimSpace(h[:i])
		}
	} else {
		h = strings.TrimSpace(c.Request.Host)
	}
	if h == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(h); err == nil {
		return host
	}
	return h
}

func (s *SubHandler) initRouter(g *gin.RouterGroup) {
	g.GET("/geodat/:kind", s.geoDatFile)
	g.GET("/ruleset/:kind/:tag", s.ruleSetFile)
	g.GET("/:subid", s.subs)
	g.HEAD("/:subid", s.subHeaders)
}

func (s *SubHandler) subs(c *gin.Context) {
	var headers []string
	var result *string
	var err error
	subId := c.Param("subid")
	format, isFormat := c.GetQuery("format")
	if isFormat {
		switch format {
		case "json":
			result, headers, err = s.JsonService.GetJson(subId, subscriptionRequestHost(c))
		case "json-rule":
			result, headers, err = s.JsonService.GetJsonRule(subId, subscriptionRequestHost(c))
		case "happ":
			result, headers, err = s.JsonService.GetJsonHapp(subId, subscriptionRequestHost(c))
		case "json-l3router":
			result, headers, err = s.JsonService.GetJsonL3Router(subId, subscriptionRequestHost(c))
		case "json-wg":
			result, headers, err = s.JsonService.GetJsonWG(subId, subscriptionRequestHost(c))
		case "json-masque":
			result, headers, err = s.JsonService.GetJsonMasque(subId, subscriptionRequestHost(c))
		case "clash":
			result, headers, err = s.ClashService.GetClash(subId)
		}
		if err != nil || result == nil {
			logger.Error(err)
			c.String(400, "Error!")
			return
		}
	} else {
		result, headers, err = s.SubService.GetSubs(subId)
		if err != nil || result == nil {
			logger.Error(err)
			c.String(400, "Error!")
			return
		}
	}

	s.addHeaders(c, headers)

	c.String(200, *result)
}

func (s *SubHandler) geoDatFile(c *gin.Context) {
	kindWithExt := strings.TrimSpace(c.Param("kind"))
	if !strings.HasSuffix(strings.ToLower(kindWithExt), ".dat") {
		c.String(http.StatusBadRequest, "invalid geodat extension")
		return
	}
	kind := strings.TrimSuffix(kindWithExt, ".dat")
	result, err := s.GeoDatService.BuildGeoDat(database.GetDB(), kind)
	if err != nil {
		if errors.Is(err, service.ErrGeoDatInvalidKind) {
			c.String(http.StatusBadRequest, "invalid geodat kind")
			return
		}
		if errors.Is(err, service.ErrGeoDatNotFound) {
			c.String(http.StatusNotFound, "Not Found")
			return
		}
		c.String(http.StatusServiceUnavailable, "Geodat unavailable")
		return
	}
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Cache-Control", "public, max-age=3600")
	c.Header("ETag", result.ETag)
	if inm := strings.TrimSpace(c.GetHeader("If-None-Match")); inm != "" && inm == result.ETag {
		c.Status(http.StatusNotModified)
		return
	}
	c.Data(http.StatusOK, "application/octet-stream", result.Bytes)
}

func (s *SubHandler) ruleSetFile(c *gin.Context) {
	kind := c.Param("kind")
	tagWithExt := strings.TrimSpace(c.Param("tag"))
	if !strings.HasSuffix(strings.ToLower(tagWithExt), ".srs") {
		c.String(http.StatusBadRequest, "invalid ruleset extension")
		return
	}
	tag := strings.TrimSuffix(tagWithExt, ".srs")
	result, err := s.RuleSetService.BuildRuleSetSRS(database.GetDB(), kind, tag)
	if err != nil {
		if errors.Is(err, service.ErrRuleSetNotFound) {
			c.String(http.StatusNotFound, "Not Found")
			return
		}
		logger.Warning("ruleset build failed: ", err)
		c.String(http.StatusServiceUnavailable, "Ruleset unavailable")
		return
	}
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Cache-Control", "public, max-age=3600")
	c.Header("ETag", result.ETag)
	if inm := strings.TrimSpace(c.GetHeader("If-None-Match")); inm != "" && inm == result.ETag {
		c.Status(http.StatusNotModified)
		return
	}
	c.Data(http.StatusOK, "application/octet-stream", result.Bytes)
}

func (s *SubHandler) subHeaders(c *gin.Context) {
	subId := c.Param("subid")
	client, err := s.SubService.getClientBySubId(subId)
	if err != nil {
		logger.Error(err)
		c.String(400, "Error!")
		return
	}

	headers := s.SubService.getClientHeaders(client)
	s.addHeaders(c, headers)

	c.Status(200)
}

func (s *SubHandler) addHeaders(c *gin.Context, headers []string) {
	c.Writer.Header().Set("Subscription-Userinfo", headers[0])
	c.Writer.Header().Set("Profile-Update-Interval", headers[1])
	c.Writer.Header().Set("Profile-Title", headers[2])
}
