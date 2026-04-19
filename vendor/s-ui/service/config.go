package service

import (
	"encoding/json"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alireza0/s-ui/core"
	"github.com/alireza0/s-ui/database"
	"github.com/alireza0/s-ui/database/model"
	"github.com/alireza0/s-ui/logger"
	"github.com/alireza0/s-ui/util/common"
)

var (
	LastUpdate          int64
	corePtr             *core.Core
	startCoreMu         sync.Mutex
	startCoreInProgress bool
	lastStartFailTime   time.Time
	startCooldown       = 15 * time.Second
)

type ConfigService struct {
	GroupService
	ClientService
	TlsService
	SettingService
	InboundService
	OutboundService
	ServicesService
	EndpointService
}

type SingBoxConfig struct {
	Log          json.RawMessage   `json:"log"`
	Dns          json.RawMessage   `json:"dns"`
	Ntp          json.RawMessage   `json:"ntp"`
	Inbounds     []json.RawMessage `json:"inbounds"`
	Outbounds    []json.RawMessage `json:"outbounds"`
	Services     []json.RawMessage `json:"services"`
	Endpoints    []json.RawMessage `json:"endpoints"`
	Route        json.RawMessage   `json:"route"`
	Experimental json.RawMessage   `json:"experimental"`
}

func NewConfigService(core *core.Core) *ConfigService {
	corePtr = core
	db := database.GetDB()
	if db != nil {
		tx := db.Begin()
		if err := (&ClientService{}).MigrateL3RouterIdentities(tx); err != nil {
			tx.Rollback()
			logger.Warning("l3router identity migration failed: ", err)
		} else {
			tx.Commit()
		}
		if db != nil {
			tx2 := db.Begin()
			if err := PersistL3RouterRouteRules(tx2); err != nil {
				tx2.Rollback()
				logger.Warning("l3router route persist on startup: ", err)
			} else {
				tx2.Commit()
			}
		}
	}
	return &ConfigService{}
}

func (s *ConfigService) GetConfig(data string) (*[]byte, error) {
	var err error
	if len(data) == 0 {
		data, err = s.SettingService.GetConfig()
		if err != nil {
			return nil, err
		}
	}
	singboxConfig := SingBoxConfig{}
	err = json.Unmarshal([]byte(data), &singboxConfig)
	if err != nil {
		return nil, err
	}

	singboxConfig.Inbounds, err = s.InboundService.GetAllConfig(database.GetDB())
	if err != nil {
		return nil, err
	}
	singboxConfig.Outbounds, err = s.OutboundService.GetAllConfig(database.GetDB())
	if err != nil {
		return nil, err
	}
	singboxConfig.Services, err = s.ServicesService.GetAllConfig(database.GetDB())
	if err != nil {
		return nil, err
	}
	singboxConfig.Endpoints, err = s.EndpointService.GetAllConfig(database.GetDB())
	if err != nil {
		return nil, err
	}
	rawConfig, err := json.MarshalIndent(singboxConfig, "", "  ")
	if err != nil {
		return nil, err
	}
	expanded, err := ExpandSUIFieldsInSingboxConfig(database.GetDB(), rawConfig)
	if err != nil {
		return nil, err
	}
	return &expanded, nil
}

func isValidRoutableCIDR(cidr string) bool {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(cidr))
	if err != nil {
		return false
	}
	if prefix.Addr().IsLoopback() || prefix.Addr().IsLinkLocalUnicast() {
		return false
	}
	return true
}

func (s *ConfigService) StartCore() error {
	if corePtr.IsRunning() {
		return nil
	}
	startCoreMu.Lock()
	if startCoreInProgress {
		startCoreMu.Unlock()
		return nil
	}
	if time.Since(lastStartFailTime) < startCooldown {
		logger.Info("start core cooldown ", startCooldown/time.Second, " seconds")
		startCoreMu.Unlock()
		return nil
	}
	startCoreInProgress = true
	startCoreMu.Unlock()
	defer func() {
		startCoreMu.Lock()
		startCoreInProgress = false
		startCoreMu.Unlock()
	}()

	logger.Info("starting core")
	rawConfig, err := s.GetConfig("")
	if err != nil {
		return err
	}
	err = corePtr.Start(*rawConfig)
	if err != nil {
		startCoreMu.Lock()
		lastStartFailTime = time.Now()
		startCoreMu.Unlock()
		logger.Error("start sing-box err:", err.Error())
		return err
	}
	logger.Info("sing-box started")
	return nil
}

func (s *ConfigService) RestartCore() error {
	err := s.StopCore()
	if err != nil {
		return err
	}
	return s.StartCore()
}

func (s *ConfigService) restartCoreWithConfig(config json.RawMessage) error {
	startCoreMu.Lock()
	if startCoreInProgress {
		startCoreMu.Unlock()
		return nil
	}
	startCoreInProgress = true
	startCoreMu.Unlock()
	defer func() {
		startCoreMu.Lock()
		startCoreInProgress = false
		startCoreMu.Unlock()
	}()

	if corePtr.IsRunning() {
		if err := corePtr.Stop(); err != nil {
			logger.Error("restart sing-box err (stop):", err.Error())
			return err
		}
	}
	rawConfig, err := s.GetConfig(string(config))
	if err != nil {
		logger.Error("restart sing-box err (get config):", err.Error())
		return err
	}
	if err := corePtr.Start(*rawConfig); err != nil {
		logger.Error("restart sing-box err (start):", err.Error())
		return err
	}
	logger.Info("sing-box restarted with new config")
	return nil
}

func (s *ConfigService) StopCore() error {
	err := corePtr.Stop()
	if err != nil {
		return err
	}
	logger.Info("sing-box stopped")
	return nil
}

func (s *ConfigService) CheckOutbound(tag string, link string) core.CheckOutboundResult {
	if tag == "" {
		return core.CheckOutboundResult{Error: "missing query parameter: tag"}
	}
	if corePtr == nil || !corePtr.IsRunning() {
		return core.CheckOutboundResult{Error: "core not running"}
	}
	return core.CheckOutbound(corePtr.GetCtx(), tag, link)
}

func (s *ConfigService) Save(obj string, act string, data json.RawMessage, initUsers string, inboundInit string, loginUser string, hostname string) ([]string, error) {
	var err error
	var objs []string = []string{obj}
	var endpointRuntimeAction *EndpointRuntimeAction
	needsCoreReload := false

	db := database.GetDB()
	tx := db.Begin()

	switch obj {
	case "clients":
		var inboundIds []uint
		var l3PeersChanged bool
		inboundIds, l3PeersChanged, err = s.ClientService.Save(tx, act, data, hostname)
		if err == nil && len(inboundIds) > 0 {
			objs = append(objs, "inbounds")
			err = s.InboundService.RestartInbounds(tx, inboundIds)
			if err != nil {
				return nil, common.NewErrorf("failed to update users for inbounds: %v", err)
			}
		}
		if l3PeersChanged {
			needsCoreReload = true
		}
	case "tls":
		err = s.TlsService.Save(tx, act, data, hostname)
		objs = append(objs, "clients", "inbounds")
	case "inbounds":
		err = s.InboundService.Save(tx, act, data, initUsers, inboundInit, hostname)
		if err == nil {
			err = PersistL3RouterRouteRules(tx)
		}
		if err == nil {
			var n int64
			tx.Model(model.Endpoint{}).Where("type = ?", l3RouterType).Count(&n)
			if n > 0 {
				needsCoreReload = true
			}
		}
		objs = append(objs, "clients")
	case "outbounds":
		err = s.OutboundService.Save(tx, act, data)
	case "services":
		err = s.ServicesService.Save(tx, act, data)
	case "endpoints":
		endpointRuntimeAction, err = s.EndpointService.Save(tx, act, data)
		if err == nil && endpointRuntimeAction != nil && endpointRuntimeAction.NeedsReload {
			needsCoreReload = true
		}
	case "l3router_peer":
		err = s.EndpointService.SaveL3RouterPeer(tx, data)
		if err == nil {
			if err = PersistL3RouterRouteRules(tx); err != nil {
				return nil, err
			}
			needsCoreReload = true
			objs = append(objs, "endpoints", "config")
		}
	case "config":
		err = s.SettingService.SaveConfig(tx, data)
		if err != nil {
			return nil, err
		}
		configData := make(json.RawMessage, len(data))
		copy(configData, data)
		go func() { _ = s.restartCoreWithConfig(configData) }()
	case "settings":
		err = s.SettingService.Save(tx, data)
	case "groups":
		err = s.GroupService.Save(tx, act, data)
		if err == nil {
			if err = (&ClientService{}).MigrateL3RouterIdentities(tx); err != nil {
				return nil, err
			}
			var l3PeersChanged bool
			l3PeersChanged, err = (&EndpointService{}).SyncAllL3RouterPeers(tx)
			if err != nil {
				return nil, err
			}
			wgPeersChanged, err := (&EndpointService{}).SyncAllWireGuardPeers(tx)
			if err != nil {
				return nil, err
			}
			if err = PersistL3RouterRouteRules(tx); err != nil {
				return nil, err
			}
			var polInIds []uint
			polInIds, err = ReconcileInboundPoliciesForGroupMembers(tx, hostname)
			if err != nil {
				return nil, err
			}
			if len(polInIds) > 0 {
				err = s.InboundService.RestartInbounds(tx, polInIds)
				if err != nil {
					return nil, common.NewErrorf("failed to update inbounds after group ACL reconcile: %v", err)
				}
				objs = append(objs, "inbounds")
			}
			if l3PeersChanged || wgPeersChanged {
				needsCoreReload = true
			}
			objs = append(objs, "clients", "endpoints", "config", "groups")
		}
	default:
		return nil, common.NewError("unknown object: ", obj)
	}
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	dt := time.Now().Unix()
	err = tx.Create(&model.Changes{
		DateTime: dt,
		Actor:    loginUser,
		Key:      obj,
		Action:   act,
		Obj:      data,
	}).Error
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	if err = tx.Commit().Error; err != nil {
		tx.Rollback()
		return nil, err
	}

	if needsCoreReload && corePtr.IsRunning() {
		if restartErr := s.RestartCore(); restartErr != nil {
			return nil, restartErr
		}
	} else if endpointRuntimeAction != nil && corePtr.IsRunning() {
		if runtimeErr := s.EndpointService.ApplyRuntimeAction(endpointRuntimeAction); runtimeErr != nil {
			return nil, runtimeErr
		}
	}
	// Try to start core if it is not running
	if !corePtr.IsRunning() {
		s.StartCore()
	}

	LastUpdate = time.Now().Unix()

	return objs, nil
}

func (s *ConfigService) CheckChanges(lu string) (bool, error) {
	if lu == "" {
		return true, nil
	}
	if LastUpdate == 0 {
		db := database.GetDB()
		var count int64
		err := db.Model(model.Changes{}).Where("date_time > " + lu).Count(&count).Error
		if err == nil {
			LastUpdate = time.Now().Unix()
		}
		return count > 0, err
	} else {
		intLu, err := strconv.ParseInt(lu, 10, 64)
		return LastUpdate > intLu, err
	}
}

func (s *ConfigService) GetChanges(actor string, chngKey string, count string) []model.Changes {
	c, _ := strconv.Atoi(count)
	whereString := "`id`>0"
	if len(actor) > 0 {
		whereString += " and `actor`='" + actor + "'"
	}
	if len(chngKey) > 0 {
		whereString += " and `key`='" + chngKey + "'"
	}
	db := database.GetDB()
	var chngs []model.Changes
	err := db.Model(model.Changes{}).Where(whereString).Order("`id` desc").Limit(c).Scan(&chngs).Error
	if err != nil {
		logger.Warning(err)
	}
	return chngs
}
