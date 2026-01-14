package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"
)

type SigType string

const (
	SigOffer           SigType = "offer"
	SigAnswer          SigType = "answer"
	SigIce             SigType = "iceCandidate"
	SigConfig          SigType = "config"
	SigPing            SigType = "ping"
	SigPong            SigType = "pong"
	SigListStreamers   SigType = "listStreamers"
	SigStreamerList    SigType = "streamerList"
	SigSubscribe       SigType = "subscribe"
	SigLayerPreference SigType = "layerPreference"
)

type SubscribeMsg struct {
	Type       SigType `json:"type"`
	StreamerID string  `json:"streamerId"`
}

type StreamerListMsg struct {
	Type SigType  `json:"type"`
	IDs  []string `json:"ids"`
}

type Envelope struct {
	Type SigType `json:"type"`
	// The rest is decoded per-type
}

type ListStreamersMsg struct {
	Type SigType `json:"type"`
}

type PingPongMsg struct {
	Type SigType `json:"type"`
	Time int64   `json:"time"`
}

// IceCandidateData matches `iceCandidateData` message used inside `iceCandidate`.
type IceCandidateData struct {
	Candidate        string  `json:"candidate"`
	SDPMid           string  `json:"sdpMid"`
	SDPMLineIndex    int     `json:"sdpMLineIndex"`
	UsernameFragment *string `json:"usernameFragment,omitempty"`
}

type IceCandidateMsg struct {
	Type      SigType          `json:"type"` // "iceCandidate"
	Candidate IceCandidateData `json:"candidate"`
	PlayerID  *string          `json:"playerId,omitempty"`
}

type OfferAnswerMsg struct {
	Type          SigType `json:"type"` // "offer" or "answer"
	SDP           string  `json:"sdp"`
	PlayerID      *string `json:"playerId,omitempty"`
	MinBitrateBps *int    `json:"minBitrateBps,omitempty"`
	MaxBitrateBps *int    `json:"maxBitrateBps,omitempty"`
}

type MediaSource struct {
	audioRTP        chan *rtp.Packet
	videoRTP        chan *rtp.Packet
	videoCodec      webrtc.RTPCodecCapability
	videoCodecOnce  sync.Once
	videoCodecReady chan struct{}
	cancel          context.CancelFunc
	ws              *websocket.Conn
	pc              *webrtc.PeerConnection
}

func (s *MediaSource) Close() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.ws != nil {
		_ = s.ws.Close()
	}
	if s.pc != nil {
		_ = s.pc.Close()
	}
}

func (s *MediaSource) AudioRTPPackets() <-chan *rtp.Packet { return s.audioRTP }

func (s *MediaSource) VideoRTPPackets() <-chan *rtp.Packet { return s.videoRTP }

func (s *MediaSource) VideoCodec() (webrtc.RTPCodecCapability, bool) {
	select {
	case <-s.videoCodecReady:
		return s.videoCodec, true
	default:
		return webrtc.RTPCodecCapability{}, false
	}
}

func (s *MediaSource) WaitVideoCodec(ctx context.Context) (webrtc.RTPCodecCapability, error) {
	select {
	case <-s.videoCodecReady:
		return s.videoCodec, nil
	case <-ctx.Done():
		return webrtc.RTPCodecCapability{}, ctx.Err()
	}
}

type sourceConfig struct {
	stun          string
	playerID      string
	streamerID    string
	logSDP        bool
	minBitrateBps *int
	maxBitrateBps *int
}

type SourceOption func(*sourceConfig)

func WithSTUN(stun string) SourceOption     { return func(c *sourceConfig) { c.stun = stun } }
func WithPlayerID(id string) SourceOption   { return func(c *sourceConfig) { c.playerID = id } }
func WithStreamerID(id string) SourceOption { return func(c *sourceConfig) { c.streamerID = id } }
func WithLogSDP(v bool) SourceOption        { return func(c *sourceConfig) { c.logSDP = v } }
func WithBitrateRange(minBps, maxBps *int) SourceOption {
	return func(c *sourceConfig) { c.minBitrateBps, c.maxBitrateBps = minBps, maxBps }
}

func NewMediaSource(parent context.Context, wsURL string, opts ...SourceOption) (*MediaSource, error) {
	cfg := &sourceConfig{stun: "stun:stun.l.google.com:19302"}
	for _, o := range opts {
		o(cfg)
	}

	ctx, cancel := context.WithCancel(parent)

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		cancel()
		return nil, err
	}

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{ICEServers: []webrtc.ICEServer{{URLs: []string{cfg.stun}}}})
	if err != nil {
		_ = ws.Close()
		cancel()
		return nil, err
	}

	if _, err = pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
		_ = ws.Close()
		_ = pc.Close()
		cancel()
		return nil, err
	}
	if _, err = pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
		_ = ws.Close()
		_ = pc.Close()
		cancel()
		return nil, err
	}

	src := &MediaSource{
		audioRTP:        make(chan *rtp.Packet, 1024),
		videoRTP:        make(chan *rtp.Packet, 4096),
		videoCodecReady: make(chan struct{}),
		cancel:          cancel,
		ws:              ws,
		pc:              pc,
	}

	// ICE → WS
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		j := c.ToJSON()
		msg := IceCandidateMsg{Type: SigIce, Candidate: IceCandidateData{Candidate: j.Candidate, SDPMid: dval(j.SDPMid), SDPMLineIndex: ival(j.SDPMLineIndex)}}
		if cfg.playerID != "" {
			pid := cfg.playerID
			msg.PlayerID = &pid
		}
		_ = writeJSON(ws, msg)
	})

	// OnTrack → push RTP packets to channels
	pc.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		log.Printf("MediaSource OnTrack: kind=%s mime=%s pt=%d ssrc=%d", track.Kind(), track.Codec().MimeType, track.PayloadType(), track.SSRC())
		if track.Kind() == webrtc.RTPCodecTypeVideo {
			_ = pc.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(track.SSRC())}})
			go func() {
				ticker := time.NewTicker(3 * time.Second)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						_ = pc.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(track.SSRC())}})
					}
				}
			}()
		}

		for {
			rtpPkt, _, readErr := track.ReadRTP()
			if readErr != nil {
				if errors.Is(readErr, context.Canceled) {
					return
				}
				return
			}

			mime := track.Codec().MimeType
			switch {
			case strings.EqualFold(mime, webrtc.MimeTypeOpus):
				select {
				case src.audioRTP <- rtpPkt:
				case <-ctx.Done():
					return
				default:
					// drop to avoid latency buildup
				}
			case strings.EqualFold(mime, webrtc.MimeTypeH264):
				// capture codec parameters once for downstream sender SDP
				s := track.Codec()
				src.videoCodecOnce.Do(func() {
					src.videoCodec = s.RTPCodecCapability
					close(src.videoCodecReady)
				})
				// Для видео не дропаем RTP: потеря одного фрагмента ломает кадр
				select {
				case src.videoRTP <- rtpPkt:
				case <-ctx.Done():
					return
				}
			default:
				log.Printf("MediaSource OnTrack: unsupported codec mime=%s (kind=%s)", mime, track.Kind())
			}
		}
	})

	// Incoming WS reader → channel
	incoming := make(chan json.RawMessage, 128)
	go func() {
		defer close(incoming)
		for {
			_, data, err := ws.ReadMessage()
			if err != nil {
				return
			}
			incoming <- json.RawMessage(data)
		}
	}()

	// Kick off: request streamer list
	if err := writeJSON(ws, ListStreamersMsg{Type: SigListStreamers}); err != nil {
		_ = ws.Close()
		_ = pc.Close()
		cancel()
		return nil, err
	}

	// Periodic ping
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case t := <-ticker.C:
				_ = writeJSON(ws, PingPongMsg{Type: SigPing, Time: t.UnixMilli()})
			}
		}
	}()

	// Signalling loop
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case raw, ok := <-incoming:
				if !ok {
					return
				}
				var env Envelope
				if err := json.Unmarshal(raw, &env); err != nil {
					continue
				}
				switch env.Type {
				case SigPing:
					var p PingPongMsg
					_ = json.Unmarshal(raw, &p)
					_ = writeJSON(ws, PingPongMsg{Type: SigPong, Time: p.Time})
				case SigStreamerList:
					var m StreamerListMsg
					if err := json.Unmarshal(raw, &m); err != nil {
						continue
					}
					if len(m.IDs) == 0 {
						continue
					}
					chosen := m.IDs[0]
					if cfg.streamerID != "" {
						for _, id := range m.IDs {
							if id == cfg.streamerID {
								chosen = id
								break
							}
						}
					}
					_ = writeJSON(ws, SubscribeMsg{Type: SigSubscribe, StreamerID: chosen})
				case SigOffer:
					var m OfferAnswerMsg
					if err := json.Unmarshal(raw, &m); err != nil {
						continue
					}
					if cfg.logSDP {
						log.Printf("remote offer SDP %s", m.SDP)
					}
					if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: m.SDP}); err != nil {
						log.Printf("SetRemote(offer): %v", err)
						continue
					}
					a, err := pc.CreateAnswer(nil)
					if err != nil {
						log.Printf("CreateAnswer: %v", err)
						continue
					}
					if err := pc.SetLocalDescription(a); err != nil {
						log.Printf("SetLocal(answer): %v", err)
						continue
					}
					if cfg.logSDP {
						log.Printf("local answer SDP %s", a.SDP)
					}
					resp := OfferAnswerMsg{Type: SigAnswer, SDP: a.SDP, MinBitrateBps: cfg.minBitrateBps, MaxBitrateBps: cfg.maxBitrateBps}
					if cfg.playerID != "" {
						pid := cfg.playerID
						resp.PlayerID = &pid
					}
					_ = writeJSON(ws, resp)
				case SigAnswer:
					var m OfferAnswerMsg
					if err := json.Unmarshal(raw, &m); err != nil {
						continue
					}
					_ = pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: m.SDP})
				case SigIce:
					var m IceCandidateMsg
					if err := json.Unmarshal(raw, &m); err != nil {
						continue
					}
					cand := webrtc.ICECandidateInit{Candidate: m.Candidate.Candidate, SDPMid: &m.Candidate.SDPMid}
					idx := uint16(m.Candidate.SDPMLineIndex)
					cand.SDPMLineIndex = &idx
					if m.Candidate.UsernameFragment != nil {
						cand.UsernameFragment = m.Candidate.UsernameFragment
					}
					_ = pc.AddICECandidate(cand)
				default:
					// ignore
				}
			}
		}
	}()

	return src, nil
}

func dval(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
func ival(p *uint16) int {
	if p == nil {
		return 0
	}
	return int(*p)
}

var wsWriteMu sync.Mutex

// writeJSON logs WS outbound message type (without payload) and writes JSON.
func writeJSON(ws *websocket.Conn, v interface{}) error {
	if t, ok := extractSigType(v); ok {
		log.Printf("WS OUT: %s", t)
	} else {
		log.Printf("WS OUT: <unknown>")
	}
	wsWriteMu.Lock()
	defer wsWriteMu.Unlock()
	return ws.WriteJSON(v)
}

// extractSigType tries to read exported field `Type` from a struct or pointer to struct.
func extractSigType(v interface{}) (SigType, bool) {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return "", false
	}
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return "", false
	}
	f := rv.FieldByName("Type")
	if !f.IsValid() {
		return "", false
	}
	// Field must be exported to be Interfaceable
	if f.Kind() == reflect.String && f.CanInterface() {
		iv := f.Interface()
		switch t := iv.(type) {
		case SigType:
			return t, true
		case string:
			return SigType(t), true
		default:
			return SigType(fmt.Sprint(iv)), true
		}
	}
	return "", false
}
