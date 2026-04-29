package device

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"net/netip"
	"sync/atomic"
	"testing"
)

const benchmarkPacketSize = 1280

// wgSyntheticTransportProfile mirrors l3router synthetic transport profile API.
type wgSyntheticTransportProfile interface {
	Name() string
	Encode(payload []byte, packetID uint64) ([]byte, error)
	Decode(frame []byte, packetID uint64) ([]byte, error)
}

type wgPlainProfile struct{}

func (p *wgPlainProfile) Name() string { return "plain_l3router_baseline" }
func (p *wgPlainProfile) Encode(payload []byte, _ uint64) ([]byte, error) {
	return payload, nil
}
func (p *wgPlainProfile) Decode(frame []byte, _ uint64) ([]byte, error) {
	return frame, nil
}

type wgVLESSRealityVisionSynthetic struct {
	aead cipher.AEAD
}

func newWGVLESSRealityVisionSynthetic() (*wgVLESSRealityVisionSynthetic, error) {
	key := []byte("0123456789abcdef0123456789abcdef")
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &wgVLESSRealityVisionSynthetic{aead: aead}, nil
}

func (p *wgVLESSRealityVisionSynthetic) Name() string { return "vless_reality_vision_synthetic" }
func (p *wgVLESSRealityVisionSynthetic) Encode(payload []byte, packetID uint64) ([]byte, error) {
	nonce := make([]byte, p.aead.NonceSize())
	binary.BigEndian.PutUint64(nonce[len(nonce)-8:], packetID)
	frame := make([]byte, 8)
	binary.BigEndian.PutUint32(frame[:4], 0x5649534e)
	binary.BigEndian.PutUint32(frame[4:8], uint32(len(payload)))
	frame = p.aead.Seal(frame, nonce, payload, frame[:8])
	return frame, nil
}
func (p *wgVLESSRealityVisionSynthetic) Decode(frame []byte, packetID uint64) ([]byte, error) {
	if len(frame) < 8+p.aead.Overhead() {
		return nil, fmt.Errorf("short frame")
	}
	header := frame[:8]
	nonce := make([]byte, p.aead.NonceSize())
	binary.BigEndian.PutUint64(nonce[len(nonce)-8:], packetID)
	payload, err := p.aead.Open(nil, nonce, frame[8:], header)
	if err != nil {
		return nil, err
	}
	return payload, nil
}

type wgHysteria2Synthetic struct {
	aead cipher.AEAD
}

func newWGHysteria2Synthetic() (*wgHysteria2Synthetic, error) {
	key := []byte("hy2-hy2-hy2-hy2-hy2-hy2-hy2-hy2-")
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &wgHysteria2Synthetic{aead: aead}, nil
}

func (p *wgHysteria2Synthetic) Name() string { return "hy2_synthetic" }
func (p *wgHysteria2Synthetic) Encode(payload []byte, packetID uint64) ([]byte, error) {
	nonce := make([]byte, p.aead.NonceSize())
	binary.BigEndian.PutUint64(nonce[len(nonce)-8:], packetID)
	frame := make([]byte, 12)
	binary.BigEndian.PutUint32(frame[:4], 0x48593230)
	binary.BigEndian.PutUint32(frame[4:8], uint32(len(payload)))
	binary.BigEndian.PutUint32(frame[8:12], crc32.ChecksumIEEE(payload))
	frame = p.aead.Seal(frame, nonce, payload, frame[:12])
	return frame, nil
}
func (p *wgHysteria2Synthetic) Decode(frame []byte, packetID uint64) ([]byte, error) {
	if len(frame) < 12+p.aead.Overhead() {
		return nil, fmt.Errorf("short hy2 frame")
	}
	header := frame[:12]
	nonce := make([]byte, p.aead.NonceSize())
	binary.BigEndian.PutUint64(nonce[len(nonce)-8:], packetID)
	payload, err := p.aead.Open(nil, nonce, frame[12:], header)
	if err != nil {
		return nil, err
	}
	if crc32.ChecksumIEEE(payload) != binary.BigEndian.Uint32(header[8:12]) {
		return nil, fmt.Errorf("hy2 crc mismatch")
	}
	return payload, nil
}

type wgTUICSynthetic struct {
	aead cipher.AEAD
	mac  []byte
}

func newWGTUICSynthetic() (*wgTUICSynthetic, error) {
	key := []byte("0123456789abcdef0123456789abcdef")
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &wgTUICSynthetic{aead: aead, mac: []byte("tuic-mac-key")}, nil
}

func (p *wgTUICSynthetic) Name() string { return "tuic_synthetic" }
func (p *wgTUICSynthetic) Encode(payload []byte, packetID uint64) ([]byte, error) {
	nonce := make([]byte, p.aead.NonceSize())
	binary.BigEndian.PutUint64(nonce[len(nonce)-8:], packetID)
	tokenRaw := make([]byte, 8)
	binary.BigEndian.PutUint64(tokenRaw, packetID^0xa5a5a5a5a5a5a5a5)
	token := base64.RawURLEncoding.EncodeToString(tokenRaw)
	if len(token) < 11 {
		return nil, fmt.Errorf("short tuic token")
	}
	header := make([]byte, 15)
	copy(header[:11], token[:11])
	binary.BigEndian.PutUint32(header[11:15], uint32(len(payload)))
	frame := p.aead.Seal(append([]byte{}, header...), nonce, payload, header)
	h := hmac.New(sha256.New, p.mac)
	h.Write(frame)
	frame = append(frame, h.Sum(nil)[:8]...)
	return frame, nil
}
func (p *wgTUICSynthetic) Decode(frame []byte, packetID uint64) ([]byte, error) {
	if len(frame) < 15+p.aead.Overhead()+8 {
		return nil, fmt.Errorf("short tuic frame")
	}
	h := hmac.New(sha256.New, p.mac)
	h.Write(frame[:len(frame)-8])
	if !hmac.Equal(h.Sum(nil)[:8], frame[len(frame)-8:]) {
		return nil, fmt.Errorf("tuic tag mismatch")
	}
	body := frame[:len(frame)-8]
	header := body[:15]
	nonce := make([]byte, p.aead.NonceSize())
	binary.BigEndian.PutUint64(nonce[len(nonce)-8:], packetID)
	payload, err := p.aead.Open(nil, nonce, body[15:], header)
	if err != nil {
		return nil, err
	}
	return payload, nil
}

type wgMieruSynthetic struct {
	aead cipher.AEAD
}

func newWGMieruSynthetic() (*wgMieruSynthetic, error) {
	key := []byte("0123456789abcdef0123456789abcdef")
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &wgMieruSynthetic{aead: aead}, nil
}

func (p *wgMieruSynthetic) Name() string { return "mieru_synthetic" }
func (p *wgMieruSynthetic) Encode(payload []byte, packetID uint64) ([]byte, error) {
	nonce := make([]byte, p.aead.NonceSize())
	binary.BigEndian.PutUint64(nonce[len(nonce)-8:], packetID)
	frame := make([]byte, 16)
	copy(frame[:4], []byte("MI53"))
	binary.BigEndian.PutUint32(frame[4:8], uint32(len(payload)))
	binary.BigEndian.PutUint64(frame[8:16], packetID)
	frame = p.aead.Seal(frame, nonce, payload, frame[:16])
	return frame, nil
}
func (p *wgMieruSynthetic) Decode(frame []byte, packetID uint64) ([]byte, error) {
	if len(frame) < 16+p.aead.Overhead() {
		return nil, fmt.Errorf("short mieru frame")
	}
	header := frame[:16]
	nonce := make([]byte, p.aead.NonceSize())
	binary.BigEndian.PutUint64(nonce[len(nonce)-8:], packetID)
	payload, err := p.aead.Open(nil, nonce, frame[16:], header)
	if err != nil {
		return nil, err
	}
	return payload, nil
}

func wgSyntheticTransportProfiles(b *testing.B) []wgSyntheticTransportProfile {
	vless, err := newWGVLESSRealityVisionSynthetic()
	if err != nil {
		b.Fatalf("create vless profile: %v", err)
	}
	hy2, err := newWGHysteria2Synthetic()
	if err != nil {
		b.Fatalf("create hy2 profile: %v", err)
	}
	tuic, err := newWGTUICSynthetic()
	if err != nil {
		b.Fatalf("create tuic profile: %v", err)
	}
	mieru, err := newWGMieruSynthetic()
	if err != nil {
		b.Fatalf("create mieru profile: %v", err)
	}
	return []wgSyntheticTransportProfile{
		&wgPlainProfile{},
		vless,
		hy2,
		tuic,
		mieru,
	}
}

// BenchmarkWireGuardAllowedIPsEndToEndSyntheticTransport mirrors
// BenchmarkL3RouterEndToEndSyntheticTransport with AllowedIPs-based routing.
func BenchmarkWireGuardAllowedIPsEndToEndSyntheticTransport(b *testing.B) {
	profiles := wgSyntheticTransportProfiles(b)
	for _, p := range profiles {
		profile := p
		b.Run(profile.Name(), func(b *testing.B) {
			benchWireGuardAllowedIPsEndToEndForProfile(b, profile)
		})
	}
}

func BenchmarkWireGuardAllowedIPsEndToEndSyntheticTransportParallel(b *testing.B) {
	profiles := wgSyntheticTransportProfiles(b)
	for _, p := range profiles {
		profile := p
		b.Run(profile.Name(), func(b *testing.B) {
			benchWireGuardAllowedIPsEndToEndParallelForProfile(b, profile)
		})
	}
}

func benchWireGuardAllowedIPsEndToEndForProfile(b *testing.B, profile wgSyntheticTransportProfile) {
	var srcACL AllowedIPs
	var dstRoutes AllowedIPs
	peerA := new(Peer)
	peerB := new(Peer)

	srcACL.Insert(netip.MustParsePrefix("10.10.1.0/24"), peerA)
	srcACL.Insert(netip.MustParsePrefix("10.10.2.0/24"), peerB)
	dstRoutes.Insert(netip.MustParsePrefix("10.10.1.0/24"), peerA)
	dstRoutes.Insert(netip.MustParsePrefix("10.10.2.0/24"), peerB)

	ipPacket := makeBenchmarkIPv4UDPPacket(benchmarkPacketSize, 10, 10, 1, 2, 10, 10, 2, 2)

	b.ReportAllocs()
	b.SetBytes(int64(len(ipPacket)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		packetID := uint64(i + 1)
		clientAFrame, err := profile.Encode(ipPacket, packetID)
		if err != nil {
			b.Fatalf("%s encode ingress: %v", profile.Name(), err)
		}
		ingressPayload, err := profile.Decode(clientAFrame, packetID)
		if err != nil {
			b.Fatalf("%s decode ingress: %v", profile.Name(), err)
		}
		src := ingressPayload[12:16]
		dst := ingressPayload[16:20]

		srcPeer := srcACL.Lookup(src)
		if srcPeer != peerA {
			b.Fatalf("unexpected src owner: %p", srcPeer)
		}
		dstPeer := dstRoutes.Lookup(dst)
		if dstPeer != peerB {
			b.Fatalf("unexpected dst owner: %p", dstPeer)
		}

		serverEgressFrame, err := profile.Encode(ingressPayload, packetID)
		if err != nil {
			b.Fatalf("%s encode egress: %v", profile.Name(), err)
		}
		clientBPayload, err := profile.Decode(serverEgressFrame, packetID)
		if err != nil {
			b.Fatalf("%s decode egress: %v", profile.Name(), err)
		}
		if len(clientBPayload) != len(ipPacket) {
			b.Fatalf("payload length mismatch: got %d want %d", len(clientBPayload), len(ipPacket))
		}
	}
}

func benchWireGuardAllowedIPsEndToEndParallelForProfile(b *testing.B, profile wgSyntheticTransportProfile) {
	var srcACL AllowedIPs
	var dstRoutes AllowedIPs
	peerA := new(Peer)
	peerB := new(Peer)

	srcACL.Insert(netip.MustParsePrefix("10.10.1.0/24"), peerA)
	srcACL.Insert(netip.MustParsePrefix("10.10.2.0/24"), peerB)
	dstRoutes.Insert(netip.MustParsePrefix("10.10.1.0/24"), peerA)
	dstRoutes.Insert(netip.MustParsePrefix("10.10.2.0/24"), peerB)

	ipPacket := makeBenchmarkIPv4UDPPacket(benchmarkPacketSize, 10, 10, 1, 2, 10, 10, 2, 2)

	var packetIndex uint64
	var errorCount uint64

	b.ReportAllocs()
	b.SetBytes(int64(len(ipPacket)))
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			packetID := atomic.AddUint64(&packetIndex, 1)
			clientAFrame, err := profile.Encode(ipPacket, packetID)
			if err != nil {
				atomic.AddUint64(&errorCount, 1)
				continue
			}
			ingressPayload, err := profile.Decode(clientAFrame, packetID)
			if err != nil {
				atomic.AddUint64(&errorCount, 1)
				continue
			}
			srcPeer := srcACL.Lookup(ingressPayload[12:16])
			if srcPeer != peerA {
				atomic.AddUint64(&errorCount, 1)
				continue
			}
			dstPeer := dstRoutes.Lookup(ingressPayload[16:20])
			if dstPeer != peerB {
				atomic.AddUint64(&errorCount, 1)
				continue
			}
			serverEgressFrame, err := profile.Encode(ingressPayload, packetID)
			if err != nil {
				atomic.AddUint64(&errorCount, 1)
				continue
			}
			clientBPayload, err := profile.Decode(serverEgressFrame, packetID)
			if err != nil || len(clientBPayload) != len(ipPacket) {
				atomic.AddUint64(&errorCount, 1)
			}
		}
	})
	b.StopTimer()

	b.ReportMetric(float64(errorCount), "errors")
	b.ReportMetric(float64(errorCount)/float64(b.N), "error/op")
}

func makeBenchmarkIPv4UDPPacket(totalLen int, srcA, srcB, srcC, srcD, dstA, dstB, dstC, dstD byte) []byte {
	if totalLen < 28 {
		totalLen = 28
	}
	pkt := make([]byte, totalLen)
	pkt[0] = 0x45
	pkt[1] = 0x00
	binary.BigEndian.PutUint16(pkt[2:4], uint16(totalLen))
	binary.BigEndian.PutUint16(pkt[4:6], 0x1234)
	binary.BigEndian.PutUint16(pkt[6:8], 0x0000)
	pkt[8] = 0x40
	pkt[9] = 0x11 // UDP
	pkt[10] = 0x00
	pkt[11] = 0x00
	pkt[12], pkt[13], pkt[14], pkt[15] = srcA, srcB, srcC, srcD
	pkt[16], pkt[17], pkt[18], pkt[19] = dstA, dstB, dstC, dstD
	binary.BigEndian.PutUint16(pkt[20:22], 53)
	binary.BigEndian.PutUint16(pkt[22:24], 33333)
	binary.BigEndian.PutUint16(pkt[24:26], uint16(totalLen-20))
	binary.BigEndian.PutUint16(pkt[26:28], 0)
	for i := 28; i < len(pkt); i++ {
		pkt[i] = byte(i)
	}
	return pkt
}

