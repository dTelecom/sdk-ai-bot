package agent

import (
	"context"
	"fmt"
	"io"
	"time"

	lksdk "github.com/dtelecom/server-sdk-go"
	"github.com/livekit/protocol/livekit"
	"github.com/pion/webrtc/v3"
	"go.uber.org/zap"

	"github.com/dTelecom/sdk-ai-bot/pkg"
	"github.com/dTelecom/sdk-ai-bot/pkg/utils/opussampleprovider"
)

func (a *Agent) Connect(url, token string) error {
	roomCallback := lksdk.NewRoomCallback()
	roomCallback.OnTrackPublished = a.onTrackPublished
	roomCallback.OnTrackSubscribed = a.onTrackSubscribed

	var err error
	a.room, err = lksdk.ConnectToRoomWithToken(url, token, roomCallback, lksdk.WithAutoSubscribe(false))
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	track, _ := lksdk.NewLocalSampleTrack(webrtc.RTPCodecCapability{
		MimeType:  webrtc.MimeTypeOpus,
		ClockRate: sampleRate,
	})

	_, err = a.room.LocalParticipant.PublishTrack(track, &lksdk.TrackPublicationOptions{
		Name:   "AI agent",
		Source: livekit.TrackSource_MICROPHONE,
	})
	if err != nil {
		a.room.Disconnect()
		return fmt.Errorf("failed to publish local track: %w", err)
	}

	botAudioChunks, err := a.pipeline.Start(context.Background())
	if err != nil {
		a.room.Disconnect()
		return fmt.Errorf("failed to start ai: %w", err)
	}

	sampleProvider := opussampleprovider.NewFromOpusChunkChan(botAudioChunks, frameDuration)
	err = track.StartWrite(sampleProvider, nil)
	if err != nil {
		return fmt.Errorf("failed to start write from sample provider: %w", err)
	}

	return nil
}

func (a *Agent) onTrackPublished(publication *lksdk.RemoteTrackPublication, _ *lksdk.RemoteParticipant) {
	if publication.Kind() == lksdk.TrackKindAudio && publication.Source() == livekit.TrackSource_MICROPHONE {
		if err := publication.SetSubscribed(true); err != nil {
			fmt.Printf("failed to subscribe: %s", err.Error())
		}
	}
}

func (a *Agent) onTrackSubscribed(track *webrtc.TrackRemote, _ *lksdk.RemoteTrackPublication, rp *lksdk.RemoteParticipant) {
	chunks := make(chan pkg.AudioChunk)

	err := a.pipeline.AddParticipant(context.Background(), rp.Name(), chunks)
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
