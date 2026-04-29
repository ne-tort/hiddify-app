package sub

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/alireza0/s-ui/database/model"
	"github.com/alireza0/s-ui/service"
	"github.com/alireza0/s-ui/util/common"
	"gorm.io/gorm"
)

type AWGConfFileMeta struct {
	EndpointID uint   `json:"endpoint_id"`
	EndpointTag string `json:"endpoint_tag"`
	Filename   string `json:"filename"`
	DownloadURL string `json:"download_url"`
}

type AWGConfService struct {
	service.AwgObfuscationProfilesService
}

func (s *AWGConfService) ListClientFiles(db *gorm.DB, clientID uint) ([]AWGConfFileMeta, error) {
	if db == nil {
		return nil, common.NewError("database is not available")
	}
	client, err := s.loadClient(db, clientID)
	if err != nil {
		return nil, err
	}
	_ = client

	var endpoints []model.Endpoint
	if err := db.Where("type = ?", awgEndpointType).Order("id ASC").Find(&endpoints).Error; err != nil {
		return nil, err
	}

	files := make([]AWGConfFileMeta, 0)
	for _, ep := range endpoints {
		ok, err := s.clientCanUseEndpointConf(db, client, ep)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		files = append(files, AWGConfFileMeta{
			EndpointID: ep.Id,
			EndpointTag: ep.Tag,
			Filename:   awgConfFilename(client.Name, ep.Tag),
		})
	}
	return files, nil
}

func (s *AWGConfService) clientCanUseEndpointConf(db *gorm.DB, client *model.Client, ep model.Endpoint) (bool, error) {
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
	peers := normalizePeerMaps(opt["peers"])
	for _, p := range peers {
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

func (s *AWGConfService) BuildClientFile(db *gorm.DB, clientID, endpointID uint, requestHost string) (string, []byte, error) {
	if db == nil {
		return "", nil, common.NewError("database is not available")
	}
	client, err := s.loadClient(db, clientID)
	if err != nil {
		return "", nil, err
	}
	var ep model.Endpoint
	if err := db.Where("id = ? AND type = ?", endpointID, awgEndpointType).First(&ep).Error; err != nil {
		return "", nil, err
	}
	ok, payload, err := s.resolveClientEndpointConfParts(db, client, ep, requestHost)
	if err != nil {
		return "", nil, err
	}
	if !ok {
		return "", nil, common.NewErrorf("awg endpoint %d is not available for client %d", endpointID, clientID)
	}
	return awgConfFilename(client.Name, ep.Tag), []byte(payload), nil
}

func (s *AWGConfService) loadClient(db *gorm.DB, clientID uint) (*model.Client, error) {
	if clientID == 0 {
		return nil, common.NewError("client id is required")
	}
	var client model.Client
	if err := db.Where("id = ?", clientID).First(&client).Error; err != nil {
		return nil, err
	}
	return &client, nil
}

func (s *AWGConfService) resolveClientEndpointConfParts(db *gorm.DB, client *model.Client, ep model.Endpoint, requestHost string) (bool, string, error) {
	if client == nil {
		return false, "", nil
	}
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
	peers := normalizePeerMaps(opt["peers"])
	var peer map[string]interface{}
	for _, p := range peers {
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
	peerHost := resolveWGServerHostWithSettings(service.SettingService{}, requestHost)
	if peerHost == "" {
		return false, "", nil
	}

	resolvedObfs, err := s.resolveAWGObfuscationMap(db, opt, client.Id)
	if err != nil {
		return false, "", err
	}
	payload := renderAWGConf(awgConfRenderInput{
		PrivateKey:          privateKey,
		LocalAddresses:      localAddrs,
		ServerPublicKey:     serverPublicKey,
		ServerHost:          peerHost,
		ServerPort:          listenPort,
		PeerAllowedIPs:      peerAllowedIPs,
		PersistentKeepalive: intFromAny(opt["persistent_keepalive_interval"]),
		PreSharedKey:        strings.TrimSpace(fmt.Sprint(peer["pre_shared_key"])),
		Obfuscation:         resolvedObfs,
	})
	return true, payload, nil
}

func (s *AWGConfService) resolveAWGObfuscationMap(db *gorm.DB, endpointOptions map[string]interface{}, clientID uint) (map[string]interface{}, error) {
	out, err := service.ResolveEffectiveAwgObfuscation(db, endpointOptions, clientID)
	if err != nil {
		return nil, err
	}
	normalizeAwgObfuscationIntsInMap(out)
	return out, nil
}

type awgConfRenderInput struct {
	PrivateKey          string
	LocalAddresses      []string
	ServerPublicKey     string
	ServerHost          string
	ServerPort          int
	PeerAllowedIPs      []string
	PersistentKeepalive int
	PreSharedKey        string
	Obfuscation         map[string]interface{}
}

func renderAWGConf(in awgConfRenderInput) string {
	b := &strings.Builder{}
	b.WriteString("[Interface]\n")
	b.WriteString("PrivateKey = " + strings.TrimSpace(in.PrivateKey) + "\n")
	b.WriteString("Address = " + strings.Join(in.LocalAddresses, ", ") + "\n")
	for _, key := range []string{"jc", "jmin", "jmax", "s1", "s2", "s3", "s4", "h1", "h2", "h3", "h4", "i1", "i2", "i3", "i4", "i5"} {
		v, ok := in.Obfuscation[key]
		if !ok || v == nil {
			continue
		}
		s := strings.TrimSpace(fmt.Sprint(v))
		if s == "" || s == "<nil>" {
			continue
		}
		b.WriteString(iniKeyCase(key) + " = " + s + "\n")
	}
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

func iniKeyCase(key string) string {
	switch key {
	case "jc":
		return "Jc"
	case "jmin":
		return "Jmin"
	case "jmax":
		return "Jmax"
	case "s1":
		return "S1"
	case "s2":
		return "S2"
	case "s3":
		return "S3"
	case "s4":
		return "S4"
	case "h1":
		return "H1"
	case "h2":
		return "H2"
	case "h3":
		return "H3"
	case "h4":
		return "H4"
	case "i1":
		return "I1"
	case "i2":
		return "I2"
	case "i3":
		return "I3"
	case "i4":
		return "I4"
	case "i5":
		return "I5"
	default:
		return key
	}
}

func awgConfFilename(clientName, endpointTag string) string {
	clean := strings.NewReplacer(" ", "_", "/", "_", "\\", "_", ":", "_").Replace(strings.TrimSpace(clientName + "-" + endpointTag))
	if clean == "-" || clean == "" {
		clean = "awg-client"
	}
	return clean + ".conf"
}
