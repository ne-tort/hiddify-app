package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/alireza0/s-ui/database"
	"github.com/alireza0/s-ui/logger"
	"github.com/alireza0/s-ui/sub"
	"github.com/alireza0/s-ui/service"
	"github.com/alireza0/s-ui/util"
	"github.com/alireza0/s-ui/util/common"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func parseUIntQuery(c *gin.Context, key string) uint {
	raw := c.Query(key)
	if raw == "" {
		return 0
	}
	n, err := strconv.ParseUint(raw, 10, 32)
	if err != nil {
		return 0
	}
	return uint(n)
}

type ApiService struct {
	service.SettingService
	service.UserService
	service.ConfigService
	service.GroupService
	service.ClientService
	service.TlsService
	service.InboundService
	service.OutboundService
	service.EndpointService
	service.ServicesService
	service.GeoCatalogService
	service.RoutingProfilesService
	service.AwgObfuscationProfilesService
	service.PanelService
	service.StatsService
	service.ServerService
}

func (a *ApiService) LoadData(c *gin.Context) {
	data, err := a.getData(c)
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	jsonObj(c, data, nil)
}

func (a *ApiService) getData(c *gin.Context) (interface{}, error) {
	data := make(map[string]interface{}, 0)
	lu := c.Query("lu")
	isUpdated, err := a.ConfigService.CheckChanges(lu)
	if err != nil {
		return "", err
	}
	onlines, err := a.StatsService.GetOnlines()

	sysInfo := a.ServerService.GetSingboxInfo()
	if sysInfo["running"] == false {
		logs := a.ServerService.GetLogs("1", "debug")
		if len(logs) > 0 {
			data["lastLog"] = logs[0]
		}
	}

	if err != nil {
		return "", err
	}
	if isUpdated {
		config, err := a.SettingService.GetConfig()
		if err != nil {
			return "", err
		}
		clients, err := a.ClientService.GetAll()
		if err != nil {
			return "", err
		}
		tlsConfigs, err := a.TlsService.GetAll()
		if err != nil {
			return "", err
		}
		inbounds, err := a.InboundService.GetAll()
		if err != nil {
			return "", err
		}
		outbounds, err := a.OutboundService.GetAll()
		if err != nil {
			return "", err
		}
		endpoints, err := a.EndpointService.GetAll()
		if err != nil {
			return "", err
		}
		services, err := a.ServicesService.GetAll()
		if err != nil {
			return "", err
		}
		geoCatalog, err := a.GeoCatalogService.GetAll(database.GetDB(), 0)
		if err != nil {
			return "", err
		}
		routingProfiles, err := a.RoutingProfilesService.GetAll(database.GetDB())
		if err != nil {
			return "", err
		}
		awgObfuscationProfiles, err := a.AwgObfuscationProfilesService.GetAll(database.GetDB())
		if err != nil {
			return "", err
		}
		subURI, err := a.SettingService.GetFinalSubURI(getHostname(c))
		if err != nil {
			return "", err
		}
		trafficAge, err := a.SettingService.GetTrafficAge()
		if err != nil {
			return "", err
		}
		data["config"] = json.RawMessage(config)
		data["clients"] = clients
		data["tls"] = tlsConfigs
		data["inbounds"] = inbounds
		data["outbounds"] = outbounds
		data["endpoints"] = endpoints
		data["services"] = services
		data["geoCatalog"] = geoCatalog
		data["routingProfiles"] = routingProfiles
		data["awgObfuscationProfiles"] = awgObfuscationProfiles
		data["subURI"] = subURI
		data["enableTraffic"] = trafficAge > 0
		data["onlines"] = onlines
		userGroups, err := a.GroupService.GetAllGroups(database.GetDB())
		if err != nil {
			return "", err
		}
		data["userGroups"] = userGroups
	} else {
		data["onlines"] = onlines
	}

	return data, nil
}

func (a *ApiService) LoadPartialData(c *gin.Context, objs []string) error {
	data := make(map[string]interface{}, 0)
	id := c.Query("id")

	for _, obj := range objs {
		switch obj {
		case "inbounds":
			inbounds, err := a.InboundService.Get(id)
			if err != nil {
				return err
			}
			data[obj] = inbounds
		case "outbounds":
			outbounds, err := a.OutboundService.GetAll()
			if err != nil {
				return err
			}
			data[obj] = outbounds
		case "endpoints":
			endpoints, err := a.EndpointService.GetAll()
			if err != nil {
				return err
			}
			data[obj] = endpoints
		case "services":
			services, err := a.ServicesService.GetAll()
			if err != nil {
				return err
			}
			data[obj] = services
		case "tls":
			tlsConfigs, err := a.TlsService.GetAll()
			if err != nil {
				return err
			}
			data[obj] = tlsConfigs
		case "clients":
			clients, err := a.ClientService.Get(id)
			if err != nil {
				return err
			}
			data[obj] = clients
		case "config":
			config, err := a.SettingService.GetConfig()
			if err != nil {
				return err
			}
			data[obj] = json.RawMessage(config)
		case "settings":
			settings, err := a.SettingService.GetAllSetting()
			if err != nil {
				return err
			}
			data[obj] = settings
		case "groups":
			groups, err := a.GroupService.GetAllGroups(database.GetDB())
			if err != nil {
				return err
			}
			data[obj] = groups
		case "geo_catalog":
			tagID := parseUIntQuery(c, "tag_id")
			geoCatalog, err := a.GeoCatalogService.GetAll(database.GetDB(), tagID)
			if err != nil {
				return err
			}
			data[obj] = geoCatalog
		case "routing_profiles":
			routingProfiles, err := a.RoutingProfilesService.GetAll(database.GetDB())
			if err != nil {
				return err
			}
			data[obj] = routingProfiles
		case "awg_obfuscation_profiles":
			rows, err := a.AwgObfuscationProfilesService.GetAll(database.GetDB())
			if err != nil {
				return err
			}
			data[obj] = rows
		}
	}

	jsonObj(c, data, nil)
	return nil
}

func (a *ApiService) GetUsers(c *gin.Context) {
	users, err := a.UserService.GetUsers()
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	jsonObj(c, *users, nil)
}

func (a *ApiService) GetSettings(c *gin.Context) {
	data, err := a.SettingService.GetAllSetting()
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	jsonObj(c, data, err)
}

func (a *ApiService) GetStats(c *gin.Context) {
	resource := c.Query("resource")
	tag := c.Query("tag")
	limit, err := strconv.Atoi(c.Query("limit"))
	if err != nil {
		limit = 100
	}
	data, err := a.StatsService.GetStats(resource, tag, limit)
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	jsonObj(c, data, err)
}

func (a *ApiService) GetStatus(c *gin.Context) {
	request := c.Query("r")
	result := a.ServerService.GetStatus(request)
	jsonObj(c, result, nil)
}

func (a *ApiService) GetOnlines(c *gin.Context) {
	onlines, err := a.StatsService.GetOnlines()
	jsonObj(c, onlines, err)
}

func (a *ApiService) GetLogs(c *gin.Context) {
	count := c.Query("c")
	level := c.Query("l")
	logs := a.ServerService.GetLogs(count, level)
	jsonObj(c, logs, nil)
}

func (a *ApiService) CheckChanges(c *gin.Context) {
	actor := c.Query("a")
	chngKey := c.Query("k")
	count := c.Query("c")
	changes := a.ConfigService.GetChanges(actor, chngKey, count)
	jsonObj(c, changes, nil)
}

func (a *ApiService) GetKeypairs(c *gin.Context) {
	kType := c.Query("k")
	options := c.Query("o")
	keypair := a.ServerService.GenKeypair(kType, options)
	jsonObj(c, keypair, nil)
}

func (a *ApiService) GetNextL3PrivateSubnet(c *gin.Context) {
	excludeStr := c.DefaultQuery("excludeId", "0")
	var excludeID uint
	if n, err := strconv.ParseUint(excludeStr, 10, 32); err == nil {
		excludeID = uint(n)
	}
	s, err := service.SuggestNextL3PrivateSubnet(database.GetDB(), excludeID)
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	jsonObj(c, s, nil)
}

func (a *ApiService) GetDb(c *gin.Context) {
	db, err := database.GetDb("")
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", "attachment; filename=s-ui_"+time.Now().Format("20060102-150405")+".db")
	c.Writer.Write(db)
}

func (a *ApiService) postActions(c *gin.Context) (string, json.RawMessage, error) {
	var data map[string]json.RawMessage
	err := c.ShouldBind(&data)
	if err != nil {
		return "", nil, err
	}
	return string(data["action"]), data["data"], nil
}

func (a *ApiService) Login(c *gin.Context) {
	remoteIP := getRemoteIp(c)
	loginUser, err := a.UserService.Login(c.Request.FormValue("user"), c.Request.FormValue("pass"), remoteIP)
	if err != nil {
		jsonMsg(c, "", err)
		return
	}

	sessionMaxAge, err := a.SettingService.GetSessionMaxAge()
	if err != nil {
		logger.Infof("Unable to get session's max age from DB")
	}

	err = SetLoginUser(c, loginUser, sessionMaxAge)
	if err == nil {
		logger.Info("user ", loginUser, " login success")
	} else {
		logger.Warning("login failed: ", err)
	}

	jsonMsg(c, "", nil)
}

func (a *ApiService) ChangePass(c *gin.Context) {
	id := c.Request.FormValue("id")
	oldPass := c.Request.FormValue("oldPass")
	newUsername := c.Request.FormValue("newUsername")
	newPass := c.Request.FormValue("newPass")
	err := a.UserService.ChangePass(id, oldPass, newUsername, newPass)
	if err == nil {
		logger.Info("change user credentials success")
		jsonMsg(c, "save", nil)
	} else {
		logger.Warning("change user credentials failed:", err)
		jsonMsg(c, "", err)
	}
}

func (a *ApiService) Save(c *gin.Context, loginUser string) {
	hostname := getHostname(c)
	obj := c.Request.FormValue("object")
	act := c.Request.FormValue("action")
	data := c.Request.FormValue("data")
	initUsers := c.Request.FormValue("initUsers")
	inboundInit := c.Request.FormValue("inboundInit")
	objs, err := a.ConfigService.Save(obj, act, json.RawMessage(data), initUsers, inboundInit, loginUser, hostname)
	if err != nil {
		jsonMsg(c, "save", err)
		return
	}
	err = a.LoadPartialData(c, objs)
	if err != nil {
		jsonMsg(c, obj, err)
	}
}

func (a *ApiService) RestartApp(c *gin.Context) {
	err := a.PanelService.RestartPanel(3)
	jsonMsg(c, "restartApp", err)
}

func (a *ApiService) RestartSb(c *gin.Context) {
	err := a.ConfigService.RestartCore()
	jsonMsg(c, "restartSb", err)
}

func (a *ApiService) LinkConvert(c *gin.Context) {
	link := c.Request.FormValue("link")
	result, _, err := util.GetOutbound(link, 0)
	jsonObj(c, result, err)
}

func (a *ApiService) SubConvert(c *gin.Context) {
	link := c.Request.FormValue("link")
	result, err := util.GetExternalSub(link)
	jsonObj(c, result, err)
}

func (a *ApiService) ImportDb(c *gin.Context) {
	file, _, err := c.Request.FormFile("db")
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	defer file.Close()
	err = database.ImportDB(file)
	jsonMsg(c, "", err)
}

func (a *ApiService) Logout(c *gin.Context) {
	loginUser := GetLoginUser(c)
	if loginUser != "" {
		logger.Infof("user %s logout", loginUser)
	}
	ClearSession(c)
	jsonMsg(c, "", nil)
}

func (a *ApiService) LoadTokens() ([]byte, error) {
	return a.UserService.LoadTokens()
}

func (a *ApiService) GetTokens(c *gin.Context) {
	loginUser := GetLoginUser(c)
	tokens, err := a.UserService.GetUserTokens(loginUser)
	jsonObj(c, tokens, err)
}

func (a *ApiService) AddToken(c *gin.Context) {
	loginUser := GetLoginUser(c)
	expiry := c.Request.FormValue("expiry")
	expiryInt, err := strconv.ParseInt(expiry, 10, 64)
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	desc := c.Request.FormValue("desc")
	token, err := a.UserService.AddToken(loginUser, expiryInt, desc)
	jsonObj(c, token, err)
}

func (a *ApiService) DeleteToken(c *gin.Context) {
	tokenId := c.Request.FormValue("id")
	err := a.UserService.DeleteToken(tokenId)
	jsonMsg(c, "", err)
}

func (a *ApiService) GetSingboxConfig(c *gin.Context) {
	rawConfig, err := a.ConfigService.GetConfig("")
	if err != nil {
		c.Status(400)
		c.Writer.WriteString(err.Error())
		return
	}
	c.Header("Content-Type", "application/json")
	c.Header("Content-Disposition", "attachment; filename=config_"+time.Now().Format("20060102-150405")+".json")
	c.Writer.Write(*rawConfig)
}

func (a *ApiService) GetCheckOutbound(c *gin.Context) {
	tag := c.Query("tag")
	link := c.Query("link")
	result := a.ConfigService.CheckOutbound(tag, link)
	jsonObj(c, result, nil)
}

func (a *ApiService) GetRoutingProfileHapp(c *gin.Context) {
	id, err := service.ParseRoutingProfileID(c.Query("id"))
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	row, err := a.RoutingProfilesService.GetByID(database.GetDB(), id)
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	link, err := a.RoutingProfilesService.BuildHappRoutingLink(*row)
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	payload, err := a.RoutingProfilesService.BuildHappPayload(*row)
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	jsonObj(c, map[string]interface{}{
		"id":           row.Id,
		"name":         row.Name,
		"happ_link":    link,
		"happ_payload": json.RawMessage(payload),
	}, nil)
}

func (a *ApiService) GetAwgObfuscationProfileAutofill(c *gin.Context) {
	id64, err := strconv.ParseUint(strings.TrimSpace(c.Query("id")), 10, 32)
	if err != nil || id64 == 0 {
		jsonMsg(c, "", common.NewErrorf("invalid profile id"))
		return
	}
	payload, err := a.AwgObfuscationProfilesService.AutofillObfuscation(database.GetDB(), uint(id64))
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	jsonObj(c, payload, nil)
}

func (a *ApiService) GetClientAwgConfFiles(c *gin.Context) {
	clientID64, err := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 32)
	if err != nil || clientID64 == 0 {
		jsonMsg(c, "", common.NewErrorf("invalid client id"))
		return
	}
	confSvc := sub.AWGConfService{}
	files, err := confSvc.ListClientFiles(database.GetDB(), uint(clientID64))
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	for i := range files {
		files[i].DownloadURL = "api/client/" + strconv.FormatUint(clientID64, 10) + "/files/awg-conf/" + strconv.FormatUint(uint64(files[i].EndpointID), 10) + "/download"
	}
	jsonObj(c, map[string]interface{}{
		"files": files,
	}, nil)
}

func (a *ApiService) buildHappGeoBaseURLForHost(requestHost string) string {
	requestHost = strings.TrimSpace(requestHost)
	if finalURI, err := a.SettingService.GetFinalSubURI(requestHost); err == nil {
		finalURI = strings.TrimSpace(finalURI)
		if finalURI != "" {
			return strings.TrimRight(finalURI, "/")
		}
	}
	subURI, _ := a.SettingService.GetSubURI()
	subURI = strings.TrimSpace(subURI)
	if subURI != "" {
		return strings.TrimRight(subURI, "/")
	}
	if requestHost == "" {
		return ""
	}
	subPath, _ := a.SettingService.GetSubPath()
	subPath = strings.TrimSpace(subPath)
	if !strings.HasPrefix(subPath, "/") {
		subPath = "/" + subPath
	}
	return "https://" + requestHost + strings.TrimRight(subPath, "/")
}

func (a *ApiService) buildClientRulesLinks(db *gorm.DB, clientID uint, geoBaseURL string) ([]map[string]interface{}, error) {
	profiles, err := a.RoutingProfilesService.ResolveProfilesForClient(db, clientID)
	if err != nil {
		return nil, err
	}
	if len(profiles) == 0 {
		return []map[string]interface{}{}, nil
	}
	happLink, err := a.RoutingProfilesService.BuildMergedHappRoutingLinkWithGeoBase(profiles, geoBaseURL)
	if err != nil {
		return nil, err
	}
	return []map[string]interface{}{
		{
			"type":   "happ",
			"remark": "Routing Rules",
			"uri":    happLink,
		},
	}, nil
}

func (a *ApiService) GetClientRulesLinks(c *gin.Context) {
	clientID64, err := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 32)
	if err != nil || clientID64 == 0 {
		jsonMsg(c, "", common.NewErrorf("invalid client id"))
		return
	}
	geoBaseURL := a.buildHappGeoBaseURLForHost(getHostname(c))
	links, err := a.buildClientRulesLinks(database.GetDB(), uint(clientID64), geoBaseURL)
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	jsonObj(c, map[string]interface{}{
		"rules_links": links,
	}, nil)
}

func (a *ApiService) DownloadClientAwgConf(c *gin.Context) {
	clientID64, err := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 32)
	if err != nil || clientID64 == 0 {
		c.String(http.StatusBadRequest, "invalid client id")
		return
	}
	endpointID64, err := strconv.ParseUint(strings.TrimSpace(c.Param("endpointId")), 10, 32)
	if err != nil || endpointID64 == 0 {
		c.String(http.StatusBadRequest, "invalid endpoint id")
		return
	}
	confSvc := sub.AWGConfService{}
	filename, payload, err := confSvc.BuildClientFile(database.GetDB(), uint(clientID64), uint(endpointID64), getHostname(c))
	if err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename="+filename)
	c.Writer.Write(payload)
}

func (a *ApiService) GetClientWgConfFiles(c *gin.Context) {
	clientID64, err := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 32)
	if err != nil || clientID64 == 0 {
		jsonMsg(c, "", common.NewErrorf("invalid client id"))
		return
	}
	confSvc := sub.WGConfService{}
	files, err := confSvc.ListClientFiles(database.GetDB(), uint(clientID64))
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	for i := range files {
		files[i].DownloadURL = "api/client/" + strconv.FormatUint(clientID64, 10) + "/files/wg-conf/" + strconv.FormatUint(uint64(files[i].EndpointID), 10) + "/download"
	}
	jsonObj(c, map[string]interface{}{
		"files": files,
	}, nil)
}

func (a *ApiService) DownloadClientWgConf(c *gin.Context) {
	clientID64, err := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 32)
	if err != nil || clientID64 == 0 {
		c.String(http.StatusBadRequest, "invalid client id")
		return
	}
	endpointID64, err := strconv.ParseUint(strings.TrimSpace(c.Param("endpointId")), 10, 32)
	if err != nil || endpointID64 == 0 {
		c.String(http.StatusBadRequest, "invalid endpoint id")
		return
	}
	confSvc := sub.WGConfService{}
	filename, payload, err := confSvc.BuildClientFile(database.GetDB(), uint(clientID64), uint(endpointID64), getHostname(c))
	if err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename="+filename)
	c.Writer.Write(payload)
}

func (a *ApiService) GetRoutingProfileSingbox(c *gin.Context) {
	id, err := service.ParseRoutingProfileID(c.Query("id"))
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	row, err := a.RoutingProfilesService.GetByID(database.GetDB(), id)
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	rules := a.RoutingProfilesService.BuildSingboxManagedRules(*row)
	jsonObj(c, map[string]interface{}{
		"id":            row.Id,
		"name":          row.Name,
		"singbox_rules": rules,
	}, nil)
}
