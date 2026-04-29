package sub

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/alireza0/s-ui/database/model"
	"github.com/alireza0/s-ui/util/common"
	"gorm.io/gorm"
)

type WGConfService struct{}

func (s *WGConfService) ListClientFiles(db *gorm.DB, clientID uint) ([]AWGConfFileMeta, error) {
	if db == nil {
		return nil, common.NewError("database is not available")
	}
	client, err := (&AWGConfService{}).loadClient(db, clientID)
	if err != nil {
		return nil, err
	}
	var endpoints []model.Endpoint
	if err := db.Where("type = ?", wgEndpointType).Order("id ASC").Find(&endpoints).Error; err != nil {
		return nil, err
	}
	files := make([]AWGConfFileMeta, 0)
	for _, ep := range endpoints {
		ok, err := s.clientCanUseWGEndpointConf(db, client, ep)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		files = append(files, AWGConfFileMeta{
			EndpointID:  ep.Id,
			EndpointTag: ep.Tag,
			Filename:    awgConfFilename(client.Name, ep.Tag),
		})
	}
	return files, nil
}

func (s *WGConfService) BuildClientFile(db *gorm.DB, clientID, endpointID uint, requestHost string) (string, []byte, error) {
	if db == nil {
		return "", nil, common.NewError("database is not available")
	}
	client, err := (&AWGConfService{}).loadClient(db, clientID)
	if err != nil {
		return "", nil, err
	}
	var ep model.Endpoint
	if err := db.Where("id = ? AND type = ?", endpointID, wgEndpointType).First(&ep).Error; err != nil {
		return "", nil, err
	}
	ok, payload, err := s.resolveClientEndpointWGConfParts(db, client, ep, requestHost)
	if err != nil {
		return "", nil, err
	}
	if !ok {
		return "", nil, common.NewErrorf("wireguard endpoint %d is not available for client %d", endpointID, clientID)
	}
	return awgConfFilename(client.Name, ep.Tag), []byte(payload), nil
}

func (s *WGConfService) clientCanUseWGEndpointConf(db *gorm.DB, client *model.Client, ep model.Endpoint) (bool, error) {
	var opt map[string]interface{}
	if len(ep.Options) > 0 {
		_ = json.Unmarshal(ep.Options, &opt)
	}
	if opt == nil || !clientHasWGStyleMemberAccess(db, opt, client.Id) {
		return false, nil
	}
	if intFromAny(opt["listen_port"]) <= 0 {
		return false, nil
	}
	if wireGuardEndpointPublicKey(ep, opt) == "" {
		return false, nil
	}
	clientPub := clientWireGuardPublicKey(client.Config)
	clientName := strings.TrimSpace(client.Name)
	for _, p := range normalizePeerMaps(opt["peers"]) {
		if !wireGuardPeerMatchesClient(p, client.Id, clientPub, clientName) {
			continue
		}
		privateKey := strings.TrimSpace(fmt.Sprint(p["private_key"]))
		if privateKey == "" || privateKey == "<nil>" {
			privateKey = clientWireGuardPrivateKey(client.Config)
		}
		if privateKey == "" || len(toStringSlice(p["allowed_ips"])) == 0 {
			return false, nil
		}
		return true, nil
	}
	return false, nil
}

func (s *WGConfService) resolveClientEndpointWGConfParts(db *gorm.DB, client *model.Client, ep model.Endpoint, requestHost string) (bool, string, error) {
	var opt map[string]interface{}
	if len(ep.Options) > 0 {
		_ = json.Unmarshal(ep.Options, &opt)
	}
	if opt == nil || !clientHasWGStyleMemberAccess(db, opt, client.Id) {
		return false, "", nil
	}
	listenPort := intFromAny(opt["listen_port"])
	if listenPort <= 0 {
		return false, "", nil
	}
	serverPublicKey := wireGuardEndpointPublicKey(ep, opt)
	if serverPublicKey == "" {
		return false, "", nil
	}
	clientPub := clientWireGuardPublicKey(client.Config)
	clientName := strings.TrimSpace(client.Name)
	var peer map[string]interface{}
	for _, p := range normalizePeerMaps(opt["peers"]) {
		if wireGuardPeerMatchesClient(p, client.Id, clientPub, clientName) {
			peer = p
			break
		}
	}
	if peer == nil {
		return false, "", nil
	}
	privateKey := strings.TrimSpace(fmt.Sprint(peer["private_key"]))
	if privateKey == "" || privateKey == "<nil>" {
		privateKey = clientWireGuardPrivateKey(client.Config)
	}
	if privateKey == "" {
		return false, "", nil
	}
	localAddrs := toStringSlice(peer["allowed_ips"])
	if len(localAddrs) == 0 {
		return false, "", nil
	}
	peerAllowedIPs := internetFullTunnelPeerRoutes()
	peerHost := strings.TrimSpace(requestHost)
	if peerHost == "" {
		peerHost = "127.0.0.1"
	}
	payload := renderWGConf(wgConfRenderInput{
		PrivateKey:          privateKey,
		LocalAddresses:      localAddrs,
		ServerPublicKey:     serverPublicKey,
		ServerHost:          peerHost,
		ServerPort:          listenPort,
		PeerAllowedIPs:      peerAllowedIPs,
		PersistentKeepalive: intFromAny(opt["persistent_keepalive_interval"]),
		PreSharedKey:        strings.TrimSpace(fmt.Sprint(peer["pre_shared_key"])),
	})
	return true, payload, nil
}

type wgConfRenderInput struct {
	PrivateKey          string
	LocalAddresses      []string
	ServerPublicKey     string
	ServerHost          string
	ServerPort          int
	PeerAllowedIPs      []string
	PersistentKeepalive int
	PreSharedKey        string
}

func renderWGConf(in wgConfRenderInput) string {
	b := &strings.Builder{}
	b.WriteString("[Interface]\n")
	b.WriteString("PrivateKey = " + strings.TrimSpace(in.PrivateKey) + "\n")
	b.WriteString("Address = " + strings.Join(in.LocalAddresses, ", ") + "\n")
	b.WriteString("\n[Peer]\n")
	b.WriteString("PublicKey = " + strings.TrimSpace(in.ServerPublicKey) + "\n")
	b.WriteString("AllowedIPs = " + strings.Join(in.PeerAllowedIPs, ", ") + "\n")
	if strings.TrimSpace(in.PreSharedKey) != "" && in.PreSharedKey != "<nil>" {
		b.WriteString("PresharedKey = " + strings.TrimSpace(in.PreSharedKey) + "\n")
	}
	if in.PersistentKeepalive > 0 {
		b.WriteString(fmt.Sprintf("PersistentKeepalive = %d\n", in.PersistentKeepalive))
	}
	b.WriteString(fmt.Sprintf("Endpoint = %s:%d\n", strings.TrimSpace(in.ServerHost), in.ServerPort))
	return b.String()
}
