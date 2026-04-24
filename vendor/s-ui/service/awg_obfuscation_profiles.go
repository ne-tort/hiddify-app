package service

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/alireza0/s-ui/database/model"
	"github.com/alireza0/s-ui/logger"
	"github.com/alireza0/s-ui/util/common"
	"gorm.io/gorm"
)

type AwgObfuscationProfilesService struct{}

type awgObfuscationProfileDTO struct {
	Id              uint    `json:"id"`
	Name            string  `json:"name"`
	Desc            string  `json:"desc"`
	Enabled         bool    `json:"enabled"`
	Jc              *int    `json:"jc"`
	Jmin            *int    `json:"jmin"`
	Jmax            *int    `json:"jmax"`
	S1              *int    `json:"s1"`
	S2              *int    `json:"s2"`
	S3              *int    `json:"s3"`
	S4              *int    `json:"s4"`
	H1              *string `json:"h1"`
	H2              *string `json:"h2"`
	H3              *string `json:"h3"`
	H4              *string `json:"h4"`
	I1              *string `json:"i1"`
	I2              *string `json:"i2"`
	I3              *string `json:"i3"`
	I4              *string `json:"i4"`
	I5              *string `json:"i5"`
	LastValidateErr string          `json:"last_validate_error"`
	GeneratorSpec   json.RawMessage `json:"generator_spec,omitempty"`
	ClientIds       []uint          `json:"client_ids,omitempty"`
	GroupIds        []uint          `json:"group_ids,omitempty"`
}

func (s *AwgObfuscationProfilesService) dtoFromRow(tx *gorm.DB, r model.AwgObfuscationProfile) (awgObfuscationProfileDTO, error) {
	cids, gids, err := s.getMembership(tx, r.Id)
	if err != nil {
		return awgObfuscationProfileDTO{}, err
	}
	return awgObfuscationProfileDTO{
		Id:              r.Id,
		Name:            r.Name,
		Desc:            r.Desc,
		Enabled:         r.Enabled,
		Jc:              r.Jc,
		Jmin:            r.Jmin,
		Jmax:            r.Jmax,
		S1:              r.S1,
		S2:              r.S2,
		S3:              r.S3,
		S4:              r.S4,
		H1:              r.H1,
		H2:              r.H2,
		H3:              r.H3,
		H4:              r.H4,
		I1:              r.I1,
		I2:              r.I2,
		I3:              r.I3,
		I4:              r.I4,
		I5:              r.I5,
		LastValidateErr: r.LastValidateErr,
		GeneratorSpec:   generatorSpecJSON(r.GeneratorSpec),
		ClientIds:       cids,
		GroupIds:        gids,
	}, nil
}

func generatorSpecJSON(s string) json.RawMessage {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if !json.Valid([]byte(s)) {
		return nil
	}
	return json.RawMessage(s)
}

func saveAwgValidationError(tx *gorm.DB, id uint, errText string) {
	if tx == nil || id == 0 {
		return
	}
	_ = tx.Model(&model.AwgObfuscationProfile{}).Where("id = ?", id).Update("last_validate_err", strings.TrimSpace(errText)).Error
}

func (s *AwgObfuscationProfilesService) GetAll(tx *gorm.DB) ([]awgObfuscationProfileDTO, error) {
	var rows []model.AwgObfuscationProfile
	if err := tx.Model(model.AwgObfuscationProfile{}).Order("name asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]awgObfuscationProfileDTO, 0, len(rows))
	for _, r := range rows {
		dto, err := s.dtoFromRow(tx, r)
		if err != nil {
			return nil, err
		}
		out = append(out, dto)
	}
	return out, nil
}

func (s *AwgObfuscationProfilesService) GetByID(tx *gorm.DB, id uint) (*model.AwgObfuscationProfile, error) {
	var row model.AwgObfuscationProfile
	if err := tx.Model(model.AwgObfuscationProfile{}).Where("id = ?", id).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *AwgObfuscationProfilesService) Save(tx *gorm.DB, act string, data json.RawMessage) error {
	switch act {
	case "new":
		var p awgObfuscationProfileDTO
		if err := json.Unmarshal(data, &p); err != nil {
			return err
		}
		return s.create(tx, p)
	case "edit":
		var p awgObfuscationProfileDTO
		if err := json.Unmarshal(data, &p); err != nil {
			return err
		}
		return s.update(tx, p)
	case "del":
		var id uint
		if err := json.Unmarshal(data, &id); err != nil {
			return err
		}
		if err := tx.Where("profile_id = ?", id).Delete(model.AwgObfuscationProfileClientMember{}).Error; err != nil {
			return err
		}
		if err := tx.Where("profile_id = ?", id).Delete(model.AwgObfuscationProfileGroupMember{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", id).Delete(model.AwgObfuscationProfile{}).Error
	case "setMembers":
		var payload struct {
			ProfileId uint   `json:"profile_id"`
			ClientIds []uint `json:"client_ids"`
			GroupIds  []uint `json:"group_ids"`
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			return err
		}
		if payload.ProfileId == 0 {
			return common.NewErrorf("profile_id required")
		}
		return s.setMembership(tx, payload.ProfileId, payload.ClientIds, payload.GroupIds)
	default:
		return common.NewErrorf("unknown awg_obfuscation_profiles action: %s", act)
	}
}

func (s *AwgObfuscationProfilesService) rowFromDTO(p awgObfuscationProfileDTO) model.AwgObfuscationProfile {
	row := model.AwgObfuscationProfile{
		Name:    strings.TrimSpace(p.Name),
		Desc:    strings.TrimSpace(p.Desc),
		Enabled: p.Enabled,
		Jc: p.Jc, Jmin: p.Jmin, Jmax: p.Jmax,
		S1: p.S1, S2: p.S2, S3: p.S3, S4: p.S4,
		H1: trimStrPtr(p.H1), H2: trimStrPtr(p.H2), H3: trimStrPtr(p.H3), H4: trimStrPtr(p.H4),
		I1: trimStrPtr(p.I1), I2: trimStrPtr(p.I2), I3: trimStrPtr(p.I3), I4: trimStrPtr(p.I4), I5: trimStrPtr(p.I5),
	}
	if len(p.GeneratorSpec) > 0 {
		row.GeneratorSpec = strings.TrimSpace(string(p.GeneratorSpec))
	}
	return row
}

func trimStrPtr(p *string) *string {
	if p == nil {
		return nil
	}
	t := strings.TrimSpace(*p)
	if t == "" {
		return nil
	}
	return &t
}

func (s *AwgObfuscationProfilesService) create(tx *gorm.DB, p awgObfuscationProfileDTO) error {
	if strings.TrimSpace(p.Name) == "" {
		return common.NewErrorf("profile name required")
	}
	row := s.rowFromDTO(p)
	if err := ValidateAwg20ObfuscationFields(&row); err != nil {
		row.LastValidateErr = strings.TrimSpace(err.Error())
		return common.NewErrorf("%v", err)
	}
	row.LastValidateErr = ""
	if err := tx.Create(&row).Error; err != nil {
		return err
	}
	// GORM omits false bool on Create (zero value); force disabled when requested.
	if !p.Enabled {
		if err := tx.Model(&model.AwgObfuscationProfile{}).Where("id = ?", row.Id).Update("enabled", false).Error; err != nil {
			return err
		}
	}
	return s.setMembership(tx, row.Id, p.ClientIds, p.GroupIds)
}

func (s *AwgObfuscationProfilesService) update(tx *gorm.DB, p awgObfuscationProfileDTO) error {
	if p.Id == 0 {
		return common.NewErrorf("profile id required")
	}
	if strings.TrimSpace(p.Name) == "" {
		return common.NewErrorf("profile name required")
	}
	row := s.rowFromDTO(p)
	if err := ValidateAwg20ObfuscationFields(&row); err != nil {
		saveAwgValidationError(tx, p.Id, err.Error())
		return common.NewErrorf("%v", err)
	}
	row.LastValidateErr = ""
	if err := tx.Model(model.AwgObfuscationProfile{}).Where("id = ?", p.Id).Updates(map[string]interface{}{
		"name":    row.Name,
		"desc":    row.Desc,
		"enabled": row.Enabled,
		"jc":      row.Jc, "jmin": row.Jmin, "jmax": row.Jmax,
		"s1": row.S1, "s2": row.S2, "s3": row.S3, "s4": row.S4,
		"h1": row.H1, "h2": row.H2, "h3": row.H3, "h4": row.H4,
		"i1": row.I1, "i2": row.I2, "i3": row.I3, "i4": row.I4, "i5": row.I5,
		"generator_spec":    row.GeneratorSpec,
		"last_validate_err": row.LastValidateErr,
	}).Error; err != nil {
		return err
	}
	return s.setMembership(tx, p.Id, p.ClientIds, p.GroupIds)
}

func (s *AwgObfuscationProfilesService) setMembership(tx *gorm.DB, profileID uint, clientIDs []uint, groupIDs []uint) error {
	clientIDs = uniqUint(clientIDs)
	groupIDs = uniqUint(groupIDs)
	if err := tx.Where("profile_id = ?", profileID).Delete(model.AwgObfuscationProfileClientMember{}).Error; err != nil {
		return err
	}
	if err := tx.Where("profile_id = ?", profileID).Delete(model.AwgObfuscationProfileGroupMember{}).Error; err != nil {
		return err
	}
	for _, cid := range clientIDs {
		var n int64
		if err := tx.Model(model.Client{}).Where("id = ?", cid).Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			return common.NewErrorf("unknown client id: %d", cid)
		}
		if err := tx.Create(&model.AwgObfuscationProfileClientMember{ProfileId: profileID, ClientId: cid}).Error; err != nil {
			return err
		}
	}
	for _, gid := range groupIDs {
		var n int64
		if err := tx.Model(model.UserGroup{}).Where("id = ?", gid).Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			return common.NewErrorf("unknown group id: %d", gid)
		}
		if err := tx.Create(&model.AwgObfuscationProfileGroupMember{ProfileId: profileID, GroupId: gid}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *AwgObfuscationProfilesService) getMembership(tx *gorm.DB, profileID uint) ([]uint, []uint, error) {
	var clients []uint
	if err := tx.Model(model.AwgObfuscationProfileClientMember{}).Where("profile_id = ?", profileID).Pluck("client_id", &clients).Error; err != nil {
		return nil, nil, err
	}
	var groups []uint
	if err := tx.Model(model.AwgObfuscationProfileGroupMember{}).Where("profile_id = ?", profileID).Pluck("group_id", &groups).Error; err != nil {
		return nil, nil, err
	}
	return uniqUint(clients), uniqUint(groups), nil
}

// ResolveObfuscationProfileForClient returns the first enabled profile assigned to the client (direct or via groups),
// ordered by profile id ascending when multiple match. Nil if none.
func (s *AwgObfuscationProfilesService) ResolveObfuscationProfileForClient(tx *gorm.DB, clientID uint) (*model.AwgObfuscationProfile, error) {
	if clientID == 0 {
		return nil, common.NewErrorf("client id required")
	}
	idsSet := map[uint]struct{}{}
	var directIDs []uint
	if err := tx.Model(model.AwgObfuscationProfileClientMember{}).Where("client_id = ?", clientID).Pluck("profile_id", &directIDs).Error; err != nil {
		return nil, err
	}
	for _, id := range directIDs {
		idsSet[id] = struct{}{}
	}
	var groupIDs []uint
	if err := tx.Model(model.ClientGroupMember{}).Where("client_id = ?", clientID).Pluck("group_id", &groupIDs).Error; err != nil {
		return nil, err
	}
	gs := GroupService{}
	expandedGroups := make([]uint, 0, len(groupIDs))
	for _, gid := range uniqUint(groupIDs) {
		desc, err := gs.DescendantGroupIDs(tx, gid)
		if err != nil {
			return nil, err
		}
		expandedGroups = append(expandedGroups, desc...)
	}
	expandedGroups = uniqUint(expandedGroups)
	if len(expandedGroups) > 0 {
		var groupProfileIDs []uint
		if err := tx.Model(model.AwgObfuscationProfileGroupMember{}).Where("group_id in ?", expandedGroups).Pluck("profile_id", &groupProfileIDs).Error; err != nil {
			return nil, err
		}
		for _, id := range groupProfileIDs {
			idsSet[id] = struct{}{}
		}
	}
	ids := make([]uint, 0, len(idsSet))
	for id := range idsSet {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []model.AwgObfuscationProfile
	if err := tx.Model(model.AwgObfuscationProfile{}).Where("id in ? AND enabled = ?", ids, true).Order("id asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	if len(rows) > 1 {
		logger.Warningf("awg obfuscation: client %d matches %d profiles; using id=%d", clientID, len(rows), rows[0].Id)
	}
	out := rows[0]
	return &out, nil
}

// AutofillObfuscation is deprecated: obfuscation is generated client-side (AmneziaWG-Architect genCfg in the panel UI).
func (s *AwgObfuscationProfilesService) AutofillObfuscation(_ *gorm.DB, profileID uint) (map[string]interface{}, error) {
	_ = profileID
	return map[string]interface{}{
		"status":  "client_side_only",
		"message": "use the profile editor «Generate» button (AmneziaWG-Architect logic in the browser)",
		"values":  map[string]interface{}{},
	}, nil
}

// MergeProfileIntoMap writes AmneziaWG obfuscation keys into dst (non-nil profile fields only).
// Endpoint inline options should be merged after this call to override.
// GeneratorSpec on the model is UI metadata only and is never written into sing-box JSON here.
func MergeAwgProfileIntoMap(dst map[string]interface{}, profile *model.AwgObfuscationProfile) {
	if profile == nil {
		return
	}
	putInt := func(key string, v *int) {
		if v != nil {
			dst[key] = *v
		}
	}
	putStr := func(key string, v *string) {
		if v != nil && strings.TrimSpace(*v) != "" {
			dst[key] = strings.TrimSpace(*v)
		}
	}
	putInt("jc", profile.Jc)
	putInt("jmin", profile.Jmin)
	putInt("jmax", profile.Jmax)
	putInt("s1", profile.S1)
	putInt("s2", profile.S2)
	putInt("s3", profile.S3)
	putInt("s4", profile.S4)
	putStr("h1", profile.H1)
	putStr("h2", profile.H2)
	putStr("h3", profile.H3)
	putStr("h4", profile.H4)
	putStr("i1", profile.I1)
	putStr("i2", profile.I2)
	putStr("i3", profile.I3)
	putStr("i4", profile.I4)
	putStr("i5", profile.I5)
}
