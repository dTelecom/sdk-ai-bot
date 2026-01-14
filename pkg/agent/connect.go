package agent

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	lksdk "github.com/dtelecom/server-sdk-go"
	"github.com/gorilla/websocket"
	"github.com/livekit/protocol/livekit"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media/samplebuilder"
	"go.uber.org/zap"

	"github.com/dTelecom/sdk-ai-bot/pkg"
	"github.com/dTelecom/sdk-ai-bot/pkg/speech_to_text/muxer"
)

type DisconnectFunc func()

func (a *Agent) Connect(url, token string, callback *Callback) (DisconnectFunc, error) {
	ctx, cancel := context.WithCancel(context.Background())

	var (
		agentConnected  atomic.Bool
		pipelineStarted atomic.Bool
		cleanupOnce     sync.Once
		ms              *MediaSource
	)

	cleanup := func() {
		cleanupOnce.Do(func() {
			cancel()

			if ms != nil {
				ms.Close()
			}

			if pipelineStarted.Load() {
				if err := a.pipeline.Stop(); err != nil {
					a.logger.Warn("failed to stop pipeline", zap.Error(err))
				}
			}

			if agentConnected.Load() && callback != nil && callback.OnDisconnected != nil {
				callback.OnDisconnected()
			}
		})
	}

	mediaSourceURL := os.Getenv("MEDIA_SOURCE_URL")
	if mediaSourceURL == "" {
		cancel()
		return nil, fmt.Errorf("MEDIA_SOURCE_URL required")
	}

	ingestURL := os.Getenv("MEDIA_INGEST_URL")
	if ingestURL == "" {
		cancel()
		return nil, fmt.Errorf("MEDIA_INGEST_URL required")
	}

	botAudioChunks, err := a.pipeline.Start(ctx)
	if err != nil {
		a.room.Disconnect()
		cleanup()
		return nil, fmt.Errorf("failed to start ai: %w", err)
	}

	pipelineStarted.Store(true)

	go a.forwardBotAudioToIngest(botAudioChunks, ingestURL)

	roomCallback := lksdk.NewRoomCallback()
	roomCallback.OnTrackPublished = a.onTrackPublished
	roomCallback.OnTrackSubscribed = a.onTrackSubscribed(ctx)
	roomCallback.OnDisconnected = cleanup

	a.room, err = lksdk.ConnectToRoomWithToken(url, token, roomCallback, lksdk.WithAutoSubscribe(false))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	audioRTPTrack, _ := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{
		MimeType:  webrtc.MimeTypeOpus,
		ClockRate: sampleRate,
	}, "ai-audio", "ai")

	minBps := 300_000
	if envMin, ok := os.LookupEnv("MEDIA_MIN_BPS"); ok {
		if v, convErr := strconv.Atoi(envMin); convErr == nil && v > 0 {
			minBps = v
		}
	}
	maxBps := 1_000_000
	if envMax, ok := os.LookupEnv("MEDIA_MAX_BPS"); ok {
		if v, convErr := strconv.Atoi(envMax); convErr == nil && v > 0 {
			maxBps = v
		}
	}

	ms, err = NewMediaSource(ctx, mediaSourceURL, WithBitrateRange(&minBps, &maxBps))
	if err != nil {
		a.room.Disconnect()
		cleanup()
		return nil, fmt.Errorf("failed to create media source: %w", err)
	}

	_, err = a.room.LocalParticipant.PublishTrack(audioRTPTrack, &lksdk.TrackPublicationOptions{
		Name:   "AI agent audio",
		Source: livekit.TrackSource_MICROPHONE,
	})
	if err != nil {
		a.room.Disconnect()
		cleanup()
		return nil, fmt.Errorf("failed to publish local audio track: %w", err)
	}

	go a.forwardAudio(ms, audioRTPTrack)

	vctx, vcancel := context.WithTimeout(ctx, 5*time.Second)
	defer vcancel()
	vcap, vErr := ms.WaitVideoCodec(vctx)
	if vErr != nil {
		a.room.Disconnect()
		cleanup()
		return nil, fmt.Errorf("failed to wait video codec: %w", vErr)
	}

	videoTrack, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{
		MimeType:     vcap.MimeType,
		ClockRate:    vcap.ClockRate,
		Channels:     vcap.Channels,
		SDPFmtpLine:  vcap.SDPFmtpLine,
		RTCPFeedback: vcap.RTCPFeedback,
	}, "ai-video", "ai")
	if err != nil {
		a.room.Disconnect()
		cleanup()
		return nil, fmt.Errorf("failed to create local static RTP track: %w", err)
	}

	_, err = a.room.LocalParticipant.PublishTrack(videoTrack, &lksdk.TrackPublicationOptions{
		Name:   "AI agent video",
		Source: livekit.TrackSource_CAMERA,
	})
	if err != nil {
		a.room.Disconnect()
		cleanup()
		return nil, fmt.Errorf("failed to publish local video track: %w", err)
	}

	go a.forwardVideo(ms, videoTrack)

	agentConnected.Store(true)

	return func() {
		a.room.Disconnect()
		cleanup()
	}, nil
}

func (a *Agent) forwardVideo(ms *MediaSource, videoTrack *webrtc.TrackLocalStaticSample) {
	const clockRate = 90000

	h264SampleBuilder := samplebuilder.New(1200, &codecs.H264Packet{}, clockRate)
	for pkt := range ms.VideoRTPPackets() {
		h264SampleBuilder.Push(pkt)
		sample := h264SampleBuilder.Pop()
		if sample == nil {
			continue
		}
		if sample.PrevDroppedPackets > 0 {
			a.logger.Error("PrevDroppedPackets > 0")
		}

		if writeErr := videoTrack.WriteSample(*sample); writeErr != nil {
			// non-recoverable in most cases; stop the loop
			break
		}
	}
}

func (a *Agent) forwardAudio(ms *MediaSource, audioRTPTrack *webrtc.TrackLocalStaticRTP) {
	for pkt := range ms.AudioRTPPackets() {
		if writeErr := audioRTPTrack.WriteRTP(pkt); writeErr != nil {
			// non-recoverable in most cases; stop the loop
			break
		}
	}
}

func (a *Agent) forwardBotAudioToIngest(botAudioChunks <-chan pkg.AudioChunk, ingestURL string) {
	const idleTimeout = 30 * time.Second

	// Оборачиваем opus-чанк-и в OGG поток
	oggReader := muxer.NewOgg(botAudioChunks, uint32(frameDuration/time.Millisecond), sampleRate, channels)

	// Буферизуем байты OGG в канал, чтобы уметь детектить отсутствие данных
	oggBytes := make(chan []byte, 64)
	go func() {
		defer close(oggBytes)
		buf := make([]byte, 4096)
		for {
			n, rErr := oggReader.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				oggBytes <- chunk
			}
			if rErr != nil {
				if rErr != io.EOF {
					a.logger.Warn("ogg reader error", zap.Error(rErr))
				}
				return
			}
		}
	}()

	for {
		// Ждём первого чанка, чтобы открыть WS только при наличии данных
		firstChunk, ok := <-oggBytes
		if !ok {
			return
		}

		wsConn, _, wsErr := websocket.DefaultDialer.Dial(ingestURL, nil)
		if wsErr != nil {
			a.logger.Warn("failed to connect to ingest", zap.Error(wsErr))
			// Подождём немного и попробуем снова при следующих данных
			time.Sleep(time.Second)
			continue
		}

		func() {
			defer wsConn.Close()
			// Параметры keepalive
			const (
				pingPeriod = 10 * time.Second
				pongWait   = 30 * time.Second
			)

			var writeMu sync.Mutex

			// Настраиваем дедлайны и обработчики ping/pong
			_ = wsConn.SetReadDeadline(time.Now().Add(pongWait))
			wsConn.SetPongHandler(func(string) error {
				// Продлеваем срок ожидания чтения при получении pong
				_ = wsConn.SetReadDeadline(time.Now().Add(pongWait))
				return nil
			})
			wsConn.SetPingHandler(func(appData string) error {
				// Логируем входящий ping и отвечаем pong, защищая запись mutex'ом
				a.logger.Debug("ping received from ingest")
				writeMu.Lock()
				defer writeMu.Unlock()
				deadline := time.Now().Add(5 * time.Second)
				if err := wsConn.WriteControl(websocket.PongMessage, []byte(appData), deadline); err != nil {
					a.logger.Warn("failed to send pong to ingest", zap.Error(err))
					return err
				}
				return nil
			})

			// Читаем входящие сообщения (включая control-фреймы), чтобы обрабатывать ping/pong
			readDone := make(chan struct{})
			go func() {
				defer close(readDone)
				for {
					if _, _, rErr := wsConn.ReadMessage(); rErr != nil {
						return
					}
				}
			}()

			// Тикер для периодических ping
			pingTicker := time.NewTicker(pingPeriod)
			defer pingTicker.Stop()

			// Отправляем старт
			writeMu.Lock()
			if err := wsConn.WriteMessage(websocket.TextMessage, []byte("start")); err != nil {
				writeMu.Unlock()
				a.logger.Warn("failed to send start to ingest", zap.Error(err))
				return
			}
			writeMu.Unlock()

			// Отправляем первый чанк сразу
			writeMu.Lock()
			if wErr := wsConn.WriteMessage(websocket.BinaryMessage, firstChunk); wErr != nil {
				writeMu.Unlock()
				a.logger.Warn("failed to write ogg chunk to ingest", zap.Error(wErr))
				writeMu.Lock()
				_ = wsConn.WriteMessage(websocket.TextMessage, []byte("stop"))
				writeMu.Unlock()
				return
			}
			writeMu.Unlock()

			for {
				select {
				case chunk, ok := <-oggBytes:
					if !ok {
						writeMu.Lock()
						_ = wsConn.WriteMessage(websocket.TextMessage, []byte("stop"))
						writeMu.Unlock()
						return
					}
					writeMu.Lock()
					if wErr := wsConn.WriteMessage(websocket.BinaryMessage, chunk); wErr != nil {
						writeMu.Unlock()
						a.logger.Warn("failed to write ogg chunk to ingest", zap.Error(wErr))
						writeMu.Lock()
						_ = wsConn.WriteMessage(websocket.TextMessage, []byte("stop"))
						writeMu.Unlock()
						return
					}
					writeMu.Unlock()
				case <-time.After(idleTimeout):
					// Тишина более idleTimeout — закрываем WS и ждём новых данных для перезапуска
					writeMu.Lock()
					_ = wsConn.WriteMessage(websocket.TextMessage, []byte("stop"))
					writeMu.Unlock()
					return
				case <-pingTicker.C:
					// Периодически отправляем ping для keepalive
					writeMu.Lock()
					deadline := time.Now().Add(5 * time.Second)
					if err := wsConn.WriteControl(websocket.PingMessage, []byte("ping"), deadline); err != nil {
						writeMu.Unlock()
						a.logger.Warn("failed to send ping to ingest", zap.Error(err))
						// Закрываем соединение, перезапустим при следующих данных
						return
					}
					writeMu.Unlock()
				case <-readDone:
					// Чтение завершилось (соединение закрыто) — выходим
					return
				}
			}
		}()
		// Цикл продолжится: ждём следующий первый чанк, чтобы открыть новый WS
	}
}

func (a *Agent) onTrackPublished(publication *lksdk.RemoteTrackPublication, _ *lksdk.RemoteParticipant) {
	if publication.Kind() == lksdk.TrackKindAudio && publication.Source() == livekit.TrackSource_MICROPHONE {
		if err := publication.SetSubscribed(true); err != nil {
			fmt.Printf("failed to subscribe: %s", err.Error())
		}
	}
}

func (a *Agent) onTrackSubscribed(ctx context.Context) func(*webrtc.TrackRemote, *lksdk.RemoteTrackPublication, *lksdk.RemoteParticipant) {
	return func(track *webrtc.TrackRemote, _ *lksdk.RemoteTrackPublication, rp *lksdk.RemoteParticipant) {
		chunks := make(chan pkg.AudioChunk)

		err := a.pipeline.AddParticipant(ctx, rp.Name(), chunks)
		if err != nil {
			a.logger.Error("failed to add participant to ai flow", zap.Error(err))

			return
		}

		go func() {
			defer close(chunks)

			for {
				rtpPacket, _, err := track.ReadRTP()
				if err != nil {
					if err != io.EOF {
						a.logger.Warn("failed to read rtp packet", zap.Error(err))
					}
					break
				}

				select {
				case chunks <- rtpPacket.Payload:
				case <-time.After(frameDuration):
					a.logger.Warn("failed to write chunk")
				}
			}
		}()
	}
}
