package model

import "encoding/json"

// L3RouterPeer stores per-client L3 settings for an l3router endpoint (source of truth for allowed_ips and filters).
// peer_id is not stored; it is read from client.Config at materialization time.
type L3RouterPeer struct {
	Id                   uint            `json:"id" gorm:"primaryKey;autoIncrement"`
	EndpointId           uint            `json:"endpoint_id" gorm:"not null;uniqueIndex:ux_l3_peer_ep_client"`
	ClientId             uint            `json:"client_id" gorm:"not null;uniqueIndex:ux_l3_peer_ep_client"`
	AllowedCIDRs         json.RawMessage `json:"allowed_cidrs" gorm:"type:text"` // JSON array of strings, e.g. ["10.0.0.1/32"]
	FilterSourceIPs      json.RawMessage `json:"filter_source_ips" gorm:"type:text"`
	FilterDestinationIPs json.RawMessage `json:"filter_destination_ips" gorm:"type:text"`
	GroupID              uint            `json:"group_id" gorm:"default:0"`
	// PeerSerial: monotonic per endpoint (max existing + 1 on insert). Ordering for UI/API.
	PeerSerial uint `json:"peer_serial" gorm:"column:peer_serial;not null;default:0"`
}
