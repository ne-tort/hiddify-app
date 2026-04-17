package model

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Endpoint struct {
	Id      uint            `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	Type    string          `json:"type" form:"type"`
	Tag     string          `json:"tag" form:"tag" gorm:"unique"`
	Options json.RawMessage `json:"-" form:"-"`
	Ext     json.RawMessage `json:"ext" form:"ext"`
}

func (o *Endpoint) UnmarshalJSON(data []byte) error {
	var err error
	var raw map[string]interface{}
	if err = json.Unmarshal(data, &raw); err != nil {
		return err
	}

	// Extract fixed fields and store the rest in Options
	if val, exists := raw["id"].(float64); exists {
		o.Id = uint(val)
	}
	delete(raw, "id")
	endpointType, ok := raw["type"].(string)
	if !ok || strings.TrimSpace(endpointType) == "" {
		return fmt.Errorf("invalid endpoint payload: missing string field `type`")
	}
	o.Type = strings.TrimSpace(endpointType)
	delete(raw, "type")
	tag, ok := raw["tag"].(string)
	if !ok || strings.TrimSpace(tag) == "" {
		return fmt.Errorf("invalid endpoint payload: missing string field `tag`")
	}
	o.Tag = strings.TrimSpace(tag)
	delete(raw, "tag")
	if _, exists := raw["ext"]; exists {
		o.Ext, err = json.MarshalIndent(raw["ext"], "", "  ")
		if err != nil {
			return fmt.Errorf("invalid endpoint payload: failed to marshal `ext`: %w", err)
		}
	} else {
		o.Ext = nil
	}
	delete(raw, "ext")

	// Remaining fields
	o.Options, err = json.MarshalIndent(raw, "", "  ")
	return err
}

// MarshalJSON customizes marshalling
func (o Endpoint) MarshalJSON() ([]byte, error) {
	// Combine fixed fields and dynamic fields into one map
	combined := make(map[string]interface{})
	switch o.Type {
	case "warp":
		combined["type"] = "wireguard"
	default:
		combined["type"] = o.Type
	}
	combined["tag"] = o.Tag

	if o.Options != nil {
		var restFields map[string]json.RawMessage
		if err := json.Unmarshal(o.Options, &restFields); err != nil {
			return nil, err
		}

		for k, v := range restFields {
			combined[k] = v
		}
	}

	return json.Marshal(combined)
}
