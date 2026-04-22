package service

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alireza0/s-ui/database"
	"github.com/alireza0/s-ui/database/model"
	"github.com/alireza0/s-ui/logger"
	"github.com/alireza0/s-ui/util"
	"github.com/alireza0/s-ui/util/common"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"gorm.io/gorm"
)

type ClientService struct{}

// clientSavePayload embeds Client; group_ids is not a DB column (see model.Client.GroupIds gorm:"-").
type clientSavePayload struct {
	model.Client
}

// parseClientSaveGroupIDsField reports whether JSON contains a top-level "group_ids" key.
// If the key is absent, callers should not replace memberships (edit flows without the field).
// If present (including null or []), ids is the new membership list (possibly empty).
func parseClientSaveGroupIDsField(data json.RawMessage) (present bool, ids []uint, err error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return false, nil, err
	}
	raw, ok := m["group_ids"]
	if !ok {
		return false, nil, nil
	}
	present = true
	if len(raw) == 0 || string(raw) == "null" {
		return true, []uint{}, nil
	}
	if err := json.Unmarshal(raw, &ids); err != nil {
		return true, nil, err
	}
	return true, ids, nil
}

var l3RouterPeerIDMu sync.Mutex

func (s *ClientService) Get(id string) (*[]model.Client, error) {
	if id == "" {
		return s.GetAll()
	}
	return s.getById(id)
}

func (s *ClientService) getById(id string) (*[]model.Client, error) {
	db := database.GetDB()
	var client []model.Client
	err := db.Model(model.Client{}).Where("id in ?", strings.Split(id, ",")).Scan(&client).Error
	if err != nil {
		return nil, err
	}
	gs := GroupService{}
	gs.FillClientGroupIDs(db, &client)
	return &client, nil
}

func (s *ClientService) GetAll() (*[]model.Client, error) {
	db := database.GetDB()
	var clients []model.Client
	err := db.Model(model.Client{}).
		Select("`id`, `enable`, `name`, `desc`, `inbounds`, `up`, `down`, `volume`, `expiry`").
		Scan(&clients).Error
	if err != nil {
		return nil, err
	}
	gs := GroupService{}
	gs.FillClientGroupIDs(db, &clients)
	return &clients, nil
}

func (s *ClientService) Save(tx *gorm.DB, act string, data json.RawMessage, hostname string) ([]uint, bool, error) {
	var err error
	var inboundIds []uint

	switch act {
	case "new", "edit":
		var cw clientSavePayload
		err = json.Unmarshal(data, &cw)
		if err != nil {
			return nil, false, err
		}
		client := cw.Client
		if act == "edit" {
			inboundIds, err = s.findInboundsChanges(tx, &client, false)
			if err != nil {
				return nil, false, err
			}
		} else {
			err = json.Unmarshal(client.Inbounds, &inboundIds)
			if err != nil {
				return nil, false, err
			}
		}
		err = tx.Save(&client).Error
		if err != nil {
			return nil, false, err
		}
		err = s.ensureL3RouterIdentity(tx, &client)
		if err != nil {
			return nil, false, err
		}
		err = s.updateLinksWithFixedInbounds(tx, []*model.Client{&client}, hostname)
		if err != nil {
			return nil, false, err
		}
		err = tx.Save(&client).Error
		if err != nil {
			return nil, false, err
		}
		present, gids, perr := parseClientSaveGroupIDsField(data)
		if perr != nil {
			return nil, false, perr
		}
		if present {
			gs := GroupService{}
			if err = gs.SyncClientGroupMemberships(tx, client.Id, gids); err != nil {
				return nil, false, err
			}
		}
		inboundIds, err = MergePolicyReconcile(tx, inboundIds, []uint{client.Id}, hostname)
		if err != nil {
			return nil, false, err
		}
	case "addbulk":
		var rawItems []json.RawMessage
		err = json.Unmarshal(data, &rawItems)
		if err != nil {
			return nil, false, err
		}
		payloads := make([]clientSavePayload, len(rawItems))
		for i := range rawItems {
			err = json.Unmarshal(rawItems[i], &payloads[i])
			if err != nil {
				return nil, false, err
			}
		}
		clients := make([]*model.Client, len(payloads))
		for i := range payloads {
			clients[i] = &payloads[i].Client
		}
		err = json.Unmarshal(clients[0].Inbounds, &inboundIds)
		if err != nil {
			return nil, false, err
		}
		err = tx.Save(clients).Error
		if err != nil {
			return nil, false, err
		}
		for _, client := range clients {
			err = s.ensureL3RouterIdentity(tx, client)
			if err != nil {
				return nil, false, err
			}
		}
		err = s.updateLinksWithFixedInbounds(tx, clients, hostname)
		if err != nil {
			return nil, false, err
		}
		err = tx.Save(clients).Error
		if err != nil {
			return nil, false, err
		}
		gs := GroupService{}
		for i := range payloads {
			present, gids, perr := parseClientSaveGroupIDsField(rawItems[i])
			if perr != nil {
				return nil, false, perr
			}
			if present {
				if err = gs.SyncClientGroupMemberships(tx, clients[i].Id, gids); err != nil {
					return nil, false, err
				}
			}
		}
		cids := make([]uint, len(clients))
		for i, c := range clients {
			cids[i] = c.Id
		}
		inboundIds, err = MergePolicyReconcile(tx, inboundIds, cids, hostname)
		if err != nil {
			return nil, false, err
		}
	case "editbulk":
		var rawItems []json.RawMessage
		err = json.Unmarshal(data, &rawItems)
		if err != nil {
			return nil, false, err
		}
		payloads := make([]clientSavePayload, len(rawItems))
		for i := range rawItems {
			err = json.Unmarshal(rawItems[i], &payloads[i])
			if err != nil {
				return nil, false, err
			}
		}
		clients := make([]*model.Client, len(payloads))
		for i := range payloads {
			clients[i] = &payloads[i].Client
		}
		for _, client := range clients {
			changedInboundIds, err := s.findInboundsChanges(tx, client, true)
			if err != nil {
				return nil, false, err
			}
			if len(changedInboundIds) > 0 {
				inboundIds = common.UnionUintArray(inboundIds, changedInboundIds)
			}
		}
		err = tx.Save(clients).Error
		if err != nil {
			return nil, false, err
		}
		for _, client := range clients {
			err = s.ensureL3RouterIdentity(tx, client)
			if err != nil {
				return nil, false, err
			}
		}
		if len(inboundIds) > 0 {
			err = s.updateLinksWithFixedInbounds(tx, clients, hostname)
			if err != nil {
				return nil, false, err
			}
		}
		err = tx.Save(clients).Error
		if err != nil {
			return nil, false, err
		}
		gs := GroupService{}
		for i := range payloads {
			present, gids, perr := parseClientSaveGroupIDsField(rawItems[i])
			if perr != nil {
				return nil, false, perr
			}
			if present {
				if err = gs.SyncClientGroupMemberships(tx, clients[i].Id, gids); err != nil {
					return nil, false, err
				}
			}
		}
		cids := make([]uint, len(clients))
		for i, c := range clients {
			cids[i] = c.Id
		}
		inboundIds, err = MergePolicyReconcile(tx, inboundIds, cids, hostname)
		if err != nil {
			return nil, false, err
		}
	case "delbulk":
		var ids []uint
		err = json.Unmarshal(data, &ids)
		if err != nil {
			return nil, false, err
		}
		inboundIds, err = MergePolicyReconcile(tx, inboundIds, ids, hostname)
		if err != nil {
			return nil, false, err
		}
		for _, id := range ids {
			var client model.Client
			err = tx.Where("id = ?", id).First(&client).Error
			if err != nil {
				return nil, false, err
			}
			var clientInbounds []uint
			err = json.Unmarshal(client.Inbounds, &clientInbounds)
			if err != nil {
				return nil, false, err
			}
			inboundIds = common.UnionUintArray(inboundIds, clientInbounds)
		}
		if err = tx.Where("client_id in ?", ids).Delete(model.ClientGroupMember{}).Error; err != nil {
			return nil, false, err
		}
		err = tx.Where("id in ?", ids).Delete(model.Client{}).Error
		if err != nil {
			return nil, false, err
		}
	case "del":
		var id uint
		err = json.Unmarshal(data, &id)
		if err != nil {
			return nil, false, err
		}
		inboundIds, err = MergePolicyReconcile(tx, inboundIds, []uint{id}, hostname)
		if err != nil {
			return nil, false, err
		}
		var client model.Client
		err = tx.Where("id = ?", id).First(&client).Error
		if err != nil {
			return nil, false, err
		}
		var fromClient []uint
		err = json.Unmarshal(client.Inbounds, &fromClient)
		if err != nil {
			return nil, false, err
		}
		inboundIds = common.UnionUintArray(inboundIds, fromClient)
		if err = tx.Where("client_id = ?", id).Delete(model.ClientGroupMember{}).Error; err != nil {
			return nil, false, err
		}
		err = tx.Where("id = ?", id).Delete(model.Client{}).Error
		if err != nil {
			return nil, false, err
		}
	default:
		return nil, false, common.NewErrorf("unknown action: %s", act)
	}

	l3PeersChanged, err := (&EndpointService{}).SyncAllL3RouterPeers(tx)
	if err != nil {
		return nil, false, err
	}
	wgPeersChanged, err := (&EndpointService{}).SyncAllWireGuardPeers(tx)
	if err != nil {
		return nil, false, err
	}
	if err := PersistL3RouterRouteRules(tx); err != nil {
		return nil, false, err
	}

	return inboundIds, l3PeersChanged || wgPeersChanged, nil
}

func (s *ClientService) updateLinksWithFixedInbounds(tx *gorm.DB, clients []*model.Client, hostname string) error {
	var err error
	var inbounds []model.Inbound
	var inboundIds []uint

	err = json.Unmarshal(clients[0].Inbounds, &inboundIds)
	if err != nil {
		return err
	}

	// Zero inbounds means removing local links only
	if len(inboundIds) > 0 {
		err = tx.Model(model.Inbound{}).Preload("Tls").Where("id in ? and type in ?", inboundIds, util.InboundTypeWithLink).Find(&inbounds).Error
		if err != nil {
			return err
		}
	}
	for index, client := range clients {
		var clientLinks []map[string]string
		err = json.Unmarshal(client.Links, &clientLinks)
		if err != nil {
			return err
		}

		newClientLinks := []map[string]string{}
		for _, inbound := range inbounds {
			newLinks := util.LinkGenerator(client.Config, &inbound, hostname)
			for _, newLink := range newLinks {
				newClientLinks = append(newClientLinks, map[string]string{
					"remark": inbound.Tag,
					"type":   "local",
					"uri":    newLink,
				})
			}
		}

		// Add non local links
		for _, clientLink := range clientLinks {
			if clientLink["type"] != "local" {
				newClientLinks = append(newClientLinks, clientLink)
			}
		}

		clients[index].Links, err = json.MarshalIndent(newClientLinks, "", "  ")
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *ClientService) UpdateClientsOnInboundAdd(tx *gorm.DB, initIds string, inboundId uint, hostname string) error {
	clientIds := strings.Split(initIds, ",")
	var clients []model.Client
	err := tx.Model(model.Client{}).Where("id in ?", clientIds).Find(&clients).Error
	if err != nil {
		return err
	}
	var inbound model.Inbound
	err = tx.Model(model.Inbound{}).Preload("Tls").Where("id = ?", inboundId).Find(&inbound).Error
	if err != nil {
		return err
	}
	for _, client := range clients {
		// Add inbounds
		var clientInbounds []uint
		json.Unmarshal(client.Inbounds, &clientInbounds)
		clientInbounds = append(clientInbounds, inboundId)
		client.Inbounds, err = json.MarshalIndent(clientInbounds, "", "  ")
		if err != nil {
			return err
		}
		// Add links
		var clientLinks, newClientLinks []map[string]string
		json.Unmarshal(client.Links, &clientLinks)
		newLinks := util.LinkGenerator(client.Config, &inbound, hostname)
		for _, newLink := range newLinks {
			newClientLinks = append(newClientLinks, map[string]string{
				"remark": inbound.Tag,
				"type":   "local",
				"uri":    newLink,
			})
		}
		for _, clientLink := range clientLinks {
			if clientLink["remark"] != inbound.Tag {
				newClientLinks = append(newClientLinks, clientLink)
			}
		}

		client.Links, err = json.MarshalIndent(newClientLinks, "", "  ")
		if err != nil {
			return err
		}
		err = tx.Save(&client).Error
		if err != nil {
			return err
		}
	}
	return nil
}

// RemoveInboundFromClient removes one inbound id from the client's inbounds JSON and drops local links for tag.
func (s *ClientService) RemoveInboundFromClient(tx *gorm.DB, client *model.Client, inboundId uint, tag string) error {
	var err error
	var clientInbounds, newClientInbounds []uint
	_ = json.Unmarshal(client.Inbounds, &clientInbounds)
	for _, iid := range clientInbounds {
		if iid != inboundId {
			newClientInbounds = append(newClientInbounds, iid)
		}
	}
	client.Inbounds, err = json.MarshalIndent(newClientInbounds, "", "  ")
	if err != nil {
		return err
	}
	var clientLinks, newClientLinks []map[string]string
	_ = json.Unmarshal(client.Links, &clientLinks)
	for _, cl := range clientLinks {
		if cl["remark"] != tag {
			newClientLinks = append(newClientLinks, cl)
		}
	}
	client.Links, err = json.MarshalIndent(newClientLinks, "", "  ")
	if err != nil {
		return err
	}
	return tx.Save(client).Error
}

func (s *ClientService) UpdateClientsOnInboundDelete(tx *gorm.DB, id uint, tag string) error {
	var clientIds []uint
	err := tx.Raw("SELECT clients.id FROM clients, json_each(clients.inbounds) AS je WHERE je.value = ?", id).Scan(&clientIds).Error
	if err != nil {
		return err
	}
	if len(clientIds) == 0 {
		return nil
	}
	var clients []model.Client
	err = tx.Model(model.Client{}).Where("id IN ?", clientIds).Find(&clients).Error
	if err != nil {
		return err
	}
	for _, client := range clients {
		// Delete inbounds
		var clientInbounds, newClientInbounds []uint
		json.Unmarshal(client.Inbounds, &clientInbounds)
		for _, clientInbound := range clientInbounds {
			if clientInbound != id {
				newClientInbounds = append(newClientInbounds, clientInbound)
			}
		}
		client.Inbounds, err = json.MarshalIndent(newClientInbounds, "", "  ")
		if err != nil {
			return err
		}
		// Delete links
		var clientLinks, newClientLinks []map[string]string
		json.Unmarshal(client.Links, &clientLinks)
		for _, clientLink := range clientLinks {
			if clientLink["remark"] != tag {
				newClientLinks = append(newClientLinks, clientLink)
			}
		}
		client.Links, err = json.MarshalIndent(newClientLinks, "", "  ")
		if err != nil {
			return err
		}
		err = tx.Save(&client).Error
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *ClientService) UpdateLinksByInboundChange(tx *gorm.DB, inbounds *[]model.Inbound, hostname string, oldTag string) error {
	var err error
	for _, inbound := range *inbounds {
		var clientIds []uint
		err = tx.Raw("SELECT clients.id FROM clients, json_each(clients.inbounds) AS je WHERE je.value = ?", inbound.Id).Scan(&clientIds).Error
		if err != nil {
			return err
		}
		if len(clientIds) == 0 {
			continue
		}
		var clients []model.Client
		err = tx.Model(model.Client{}).Where("id IN ?", clientIds).Find(&clients).Error
		if err != nil {
			return err
		}
		for _, client := range clients {
			var clientLinks, newClientLinks []map[string]string
			json.Unmarshal(client.Links, &clientLinks)
			newLinks := util.LinkGenerator(client.Config, &inbound, hostname)
			for _, newLink := range newLinks {
				newClientLinks = append(newClientLinks, map[string]string{
					"remark": inbound.Tag,
					"type":   "local",
					"uri":    newLink,
				})
			}
			for _, clientLink := range clientLinks {
				if clientLink["type"] != "local" || (clientLink["remark"] != inbound.Tag && clientLink["remark"] != oldTag) {
					newClientLinks = append(newClientLinks, clientLink)
				}
			}

			client.Links, err = json.MarshalIndent(newClientLinks, "", "  ")
			if err != nil {
				return err
			}
			err = tx.Save(&client).Error
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *ClientService) DepleteClients() ([]uint, error) {
	var err error
	var clients []model.Client
	var changes []model.Changes
	var users []string
	var inboundIds []uint

	dt := time.Now().Unix()
	db := database.GetDB()

	tx := db.Begin()
	defer func() {
		if err == nil {
			tx.Commit()
			if err1 := db.Exec("PRAGMA wal_checkpoint(FULL)").Error; err1 != nil {
				logger.Error("Error checkpointing WAL: ", err1.Error())
			}
		} else {
			tx.Rollback()
		}
	}()

	// Reset clients
	inboundIds, err = s.ResetClients(tx, dt)
	if err != nil {
		return nil, err
	}

	// Deplete clients
	err = tx.Model(model.Client{}).Where("enable = true AND ((volume >0 AND up+down > volume) OR (expiry > 0 AND expiry < ?))", dt).Scan(&clients).Error
	if err != nil {
		return nil, err
	}

	for _, client := range clients {
		logger.Debug("Client ", client.Name, " is going to be disabled")
		users = append(users, client.Name)
		var userInbounds []uint
		json.Unmarshal(client.Inbounds, &userInbounds)
		// Find changed inbounds
		inboundIds = common.UnionUintArray(inboundIds, userInbounds)
		changes = append(changes, model.Changes{
			DateTime: dt,
			Actor:    "DepleteJob",
			Key:      "clients",
			Action:   "disable",
			Obj:      json.RawMessage("\"" + client.Name + "\""),
		})
	}

	// Save changes
	if len(changes) > 0 {
		err = tx.Model(model.Client{}).Where("enable = true AND ((volume >0 AND up+down > volume) OR (expiry > 0 AND expiry < ?))", dt).Update("enable", false).Error
		if err != nil {
			return nil, err
		}
		err = tx.Model(model.Changes{}).Create(&changes).Error
		if err != nil {
			return nil, err
		}
		LastUpdate = dt
	}

	return inboundIds, nil
}

func (s *ClientService) ResetClients(tx *gorm.DB, dt int64) ([]uint, error) {
	var err error
	var resetClients, allClients []*model.Client
	var changes []model.Changes
	var inboundIds []uint
	// Set delay start without periodic reset
	err = tx.Model(model.Client{}).
		Where("enable = true AND delay_start = true AND auto_reset = false AND (Up + Down) > 0").Find(&resetClients).Error
	if err != nil {
		return nil, err
	}
	for _, client := range resetClients {
		client.Expiry = dt + (int64(client.ResetDays) * 86400)
		client.DelayStart = false
		changes = append(changes, model.Changes{
			DateTime: dt,
			Actor:    "ResetJob",
			Key:      "clients",
			Action:   "reset",
			Obj:      json.RawMessage("\"" + client.Name + "\""),
		})
	}
	allClients = append(allClients, resetClients...)

	// Set delay start with periodic reset
	err = tx.Model(model.Client{}).
		Where("enable = true AND delay_start = true AND auto_reset = true AND (Up + Down) > 0").Find(&resetClients).Error
	if err != nil {
		return nil, err
	}
	for _, client := range resetClients {
		client.NextReset = dt + (int64(client.ResetDays) * 86400)
		client.DelayStart = false
		changes = append(changes, model.Changes{
			DateTime: dt,
			Actor:    "ResetJob",
			Key:      "clients",
			Action:   "reset",
			Obj:      json.RawMessage("\"" + client.Name + "\""),
		})
	}
	allClients = append(allClients, resetClients...)

	// Set periodic reset
	err = tx.Model(model.Client{}).
		Where("delay_start = false AND auto_reset = true AND next_reset < ?", dt).Find(&resetClients).Error
	if err != nil {
		return nil, err
	}
	for _, client := range resetClients {
		client.NextReset = dt + (int64(client.ResetDays) * 86400)
		client.TotalUp += client.Up
		client.TotalDown += client.Down
		client.Up = 0
		client.Down = 0
		if !client.Enable {
			client.Enable = true
			var clientInboundIds []uint
			json.Unmarshal(client.Inbounds, &clientInboundIds)
			inboundIds = common.UnionUintArray(inboundIds, clientInboundIds)
		}
	}
	allClients = append(allClients, resetClients...)

	// Save clients
	if len(allClients) > 0 {
		err = tx.Save(allClients).Error
		if err != nil {
			return nil, err
		}
	}

	// Save changes
	if len(changes) > 0 {
		err = tx.Model(model.Changes{}).Create(&changes).Error
		if err != nil {
			return nil, err
		}
		LastUpdate = dt
	}
	return inboundIds, nil
}

func (s *ClientService) findInboundsChanges(tx *gorm.DB, client *model.Client, fillOmitted bool) ([]uint, error) {
	var err error
	var oldClient model.Client
	var oldInboundIds, newInboundIds []uint
	err = tx.Model(model.Client{}).Where("id = ?", client.Id).First(&oldClient).Error
	if err != nil {
		return nil, err
	}
	if fillOmitted {
		client.Links = oldClient.Links
		client.Config = oldClient.Config
	}
	err = json.Unmarshal(oldClient.Inbounds, &oldInboundIds)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(client.Inbounds, &newInboundIds)
	if err != nil {
		return nil, err
	}

	// Check client.Config changes
	if !bytes.Equal(oldClient.Config, client.Config) ||
		oldClient.Name != client.Name ||
		oldClient.Enable != client.Enable {
		return common.UnionUintArray(oldInboundIds, newInboundIds), nil
	}

	// Check client.Inbounds changes
	diffInbounds := common.DiffUintArray(oldInboundIds, newInboundIds)

	return diffInbounds, nil
}

func (s *ClientService) MigrateL3RouterIdentities(tx *gorm.DB) error {
	var clients []model.Client
	if err := tx.Model(model.Client{}).Find(&clients).Error; err != nil {
		return err
	}
	for i := range clients {
		changed, err := s.ensureL3RouterIdentityWithResult(tx, &clients[i])
		if err != nil {
			return err
		}
		if changed {
			if err := tx.Model(model.Client{}).Where("id = ?", clients[i].Id).Update("config", clients[i].Config).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *ClientService) ensureL3RouterIdentity(tx *gorm.DB, client *model.Client) error {
	_, err := s.ensureL3RouterIdentityWithResult(tx, client)
	return err
}

func (s *ClientService) ensureL3RouterIdentityWithResult(tx *gorm.DB, client *model.Client) (bool, error) {
	if client.Id == 0 {
		return false, nil
	}
	gs := GroupService{}
	should, err := gs.ClientShouldHaveL3Router(tx, client.Id)
	if err != nil {
		return false, err
	}

	var configs map[string]map[string]interface{}
	if len(client.Config) > 0 {
		if err := json.Unmarshal(client.Config, &configs); err != nil {
			return false, err
		}
	} else {
		configs = make(map[string]map[string]interface{})
	}

	if configs == nil {
		configs = make(map[string]map[string]interface{})
	}

	if !should {
		if _, ok := configs["l3router"]; ok {
			delete(configs, "l3router")
			updatedConfig, err := json.MarshalIndent(configs, "", " ")
			if err != nil {
				return false, err
			}
			client.Config = updatedConfig
			return true, nil
		}
		return false, nil
	}

	l3Config, ok := configs["l3router"]
	if !ok || l3Config == nil {
		l3Config = make(map[string]interface{})
		configs["l3router"] = l3Config
	}

	changed := false
	peerID, hasPeerID, parseErr := parsePeerID(l3Config["peer_id"])
	if parseErr != nil {
		return false, parseErr
	}
	if !hasPeerID {
		l3RouterPeerIDMu.Lock()
		var err error
		peerID, err = s.allocateL3RouterPeerID(tx, client.Id)
		l3RouterPeerIDMu.Unlock()
		if err != nil {
			return false, err
		}
		l3Config["peer_id"] = peerID
		changed = true
	}

	userName, _ := l3Config["user"].(string)
	if userName != client.Name {
		l3Config["user"] = client.Name
		changed = true
	}

	if name, _ := l3Config["name"].(string); name != client.Name {
		l3Config["name"] = client.Name
		changed = true
	}

	if !changed {
		return false, nil
	}

	updatedConfig, err := json.MarshalIndent(configs, "", " ")
	if err != nil {
		return false, err
	}
	client.Config = updatedConfig
	return true, nil
}

func (s *ClientService) allocateL3RouterPeerID(tx *gorm.DB, excludeClientID uint) (uint64, error) {
	const maxSafeInteger uint64 = 9007199254740991
	used := make(map[uint64]struct{})
	var clients []model.Client
	if err := tx.Model(model.Client{}).Select("id, config").Find(&clients).Error; err != nil {
		return 0, err
	}
	for _, client := range clients {
		if excludeClientID != 0 && client.Id == excludeClientID {
			continue
		}
		var configs map[string]map[string]interface{}
		if len(client.Config) == 0 {
			continue
		}
		if err := json.Unmarshal(client.Config, &configs); err != nil {
			continue
		}
		if l3Config, ok := configs["l3router"]; ok {
			if id, ok, _ := parsePeerID(l3Config["peer_id"]); ok {
				used[id] = struct{}{}
			}
		}
	}

	for i := 0; i < 64; i++ {
		candidate, err := randomL3RouterPeerID(maxSafeInteger)
		if err != nil {
			return 0, err
		}
		if _, exists := used[candidate]; !exists {
			return candidate, nil
		}
	}

	return 0, fmt.Errorf("failed to allocate unique l3router peer_id")
}

func randomL3RouterPeerID(max uint64) (uint64, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return 0, err
	}
	if max <= 1 {
		return 1, nil
	}
	return (binary.BigEndian.Uint64(buf[:]) % (max - 1)) + 1, nil
}

func parsePeerID(raw interface{}) (uint64, bool, error) {
	switch v := raw.(type) {
	case float64:
		if v <= 0 {
			return 0, false, nil
		}
		if math.Trunc(v) != v {
			return 0, false, fmt.Errorf("invalid l3router peer_id: fractional numbers are not allowed")
		}
		return uint64(v), true, nil
	case int:
		if v > 0 {
			return uint64(v), true, nil
		}
	case int64:
		if v > 0 {
			return uint64(v), true, nil
		}
	case uint64:
		if v > 0 {
			return v, true, nil
		}
	case string:
		if strings.TrimSpace(v) == "" {
			return 0, false, nil
		}
		id, err := strconv.ParseUint(strings.TrimSpace(v), 10, 64)
		if err != nil || id == 0 {
			return 0, false, fmt.Errorf("invalid l3router peer_id string: %q", v)
		}
		return id, true, nil
	case json.Number:
		if strings.Contains(v.String(), ".") {
			return 0, false, fmt.Errorf("invalid l3router peer_id: fractional numbers are not allowed")
		}
		id, err := v.Int64()
		if err != nil || id <= 0 {
			return 0, false, fmt.Errorf("invalid l3router peer_id: %v", err)
		}
		return uint64(id), true, nil
	case nil:
		return 0, false, nil
	default:
		return 0, false, fmt.Errorf("invalid l3router peer_id type: %T", raw)
	}
	return 0, false, nil
}

func (s *ClientService) ensureWGIdentityWithResult(client *model.Client) (bool, error) {
	configs, err := decodeClientConfig(client.Config)
	if err != nil {
		return false, err
	}
	wgCfg, ok := configs["wireguard"]
	if !ok || wgCfg == nil {
		wgCfg = make(map[string]interface{})
		configs["wireguard"] = wgCfg
	}
	privateKey, _ := wgCfg["private_key"].(string)
	publicKey, _ := wgCfg["public_key"].(string)
	if strings.TrimSpace(privateKey) != "" && strings.TrimSpace(publicKey) != "" {
		return false, nil
	}
	keys, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return false, err
	}
	wgCfg["private_key"] = keys.String()
	wgCfg["public_key"] = keys.PublicKey().String()
	raw, err := json.MarshalIndent(configs, "", " ")
	if err != nil {
		return false, err
	}
	client.Config = raw
	return true, nil
}

func (s *ClientService) RotateWGIdentity(tx *gorm.DB, clientID uint) error {
	var client model.Client
	if err := tx.Model(model.Client{}).Where("id = ?", clientID).First(&client).Error; err != nil {
		return err
	}
	configs, err := decodeClientConfig(client.Config)
	if err != nil {
		return err
	}
	wgCfg, ok := configs["wireguard"]
	if !ok || wgCfg == nil {
		wgCfg = make(map[string]interface{})
		configs["wireguard"] = wgCfg
	}
	keys, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return err
	}
	wgCfg["private_key"] = keys.String()
	wgCfg["public_key"] = keys.PublicKey().String()
	raw, err := json.MarshalIndent(configs, "", " ")
	if err != nil {
		return err
	}
	if err := tx.Model(model.Client{}).Where("id = ?", clientID).Update("config", raw).Error; err != nil {
		return err
	}
	_, err = (&EndpointService{}).SyncAllWireGuardPeers(tx)
	return err
}

func decodeClientConfig(raw json.RawMessage) (map[string]map[string]interface{}, error) {
	if len(raw) == 0 {
		return map[string]map[string]interface{}{}, nil
	}
	var configs map[string]map[string]interface{}
	if err := json.Unmarshal(raw, &configs); err != nil {
		return nil, err
	}
	if configs == nil {
		configs = map[string]map[string]interface{}{}
	}
	return configs, nil
}
