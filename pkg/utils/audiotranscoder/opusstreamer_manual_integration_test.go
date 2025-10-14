//go:build manual_integration

package audiotranscoder_test

import (
	"context"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"testing"

	"github.com/deepgram/deepgram-go-sdk/v3/pkg/audio/microphone"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3/pkg/media/oggwriter"
	"github.com/stretchr/testify/require"

	"github.com/dTelecom/sdk-ai-bot/pkg/utils/audiotranscoder"
	"github.com/dTelecom/sdk-ai-bot/pkg/utils/int16reader"
)

const (
	sampleRate = 48000
	channels   = 1
)

func Test_SaveToOgg(t *testing.T) {
	microphone.Initialize()

	mic, err := microphone.New(microphone.AudioConfig{
		InputChannels: channels,
		SamplingRate:  sampleRate,
	})
	require.NoError(t, err)

	err = mic.Start()
	require.NoError(t, err)

	oggWriter, err := oggwriter.New("./test.ogg", sampleRate, channels)
	require.NoError(t, err)

	opusStreamer, err := audiotranscoder.NewOpusStreamer(int16reader.New(mic), sampleRate, channels, nil)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go opusStreamer.Run(ctx)

	go func() {
		var (
			seq  uint16 = uint16(rand.Uint32())
			ts   uint32 = rand.Uint32()
			ssrc uint32 = rand.Uint32()
			step uint32 = 960 // 20 мс * 48 кГц
		)
		for pkt := range opusStreamer.Packets() {
			rtpPkt := &rtp.Packet{
				Header: rtp.Header{
					Version:        2,
					PayloadType:    111, // динамический тип Opus
					SequenceNumber: seq,
					Timestamp:      ts,
					SSRC:           ssrc,
				},
				Payload: pkt,
			}
			if err := oggWriter.WriteRTP(rtpPkt); err != nil {
				require.NoError(t, err)
			}

			seq++
			ts += step
		}
	}()

	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	cancel()

	_ = mic.Stop()
	microphone.Teardown()
}
