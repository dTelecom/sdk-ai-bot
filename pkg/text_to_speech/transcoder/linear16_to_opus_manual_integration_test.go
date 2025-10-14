//go:build manual_integration

package transcoder_test

import (
	"context"
	"encoding/binary"
	"io"
	"math/rand"
	"os"
	"path"
	"testing"
	"time"

	"github.com/deepgram/deepgram-go-sdk/v3/pkg/audio/microphone"
	"github.com/joho/godotenv"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3/pkg/media/oggwriter"
	"github.com/stretchr/testify/require"

	"github.com/dTelecom/sdk-ai-bot/pkg"
	"github.com/dTelecom/sdk-ai-bot/pkg/text_to_speech/transcoder"
)

const (
	sampleRate    = 48_000
	channels      = 1
	frameDuration = 20 * time.Millisecond
)

func TestLinear16ToOpus_SaveMicToOgg(t *testing.T) {
	err := godotenv.Load("../../../../.env")
	require.NoError(t, err)

	microphone.Initialize()
	defer microphone.Teardown()

	mic, err := microphone.New(microphone.AudioConfig{
		InputChannels: channels,
		SamplingRate:  sampleRate,
	})
	require.NoError(t, err)

	src := make(chan pkg.AudioChunk)

	transcoderBuilder, err := transcoder.NewLinear16ToOpusBuilder(sampleRate, channels, nil)
	require.NoError(t, err)

	opusTranscoder := transcoderBuilder.BuildAndRunTranscoder(src)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	go func() {
		defer close(src)

		type res struct {
			data []int16
			err  error
		}

		micReadCh := make(chan res)
		go func() {
			for {
				data, err := mic.Read()
				micReadCh <- res{data, err}
			}
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case micRes := <-micReadCh:
				if micRes.err == io.EOF {
					return
				}
				require.NoError(t, micRes.err)
				buf := make([]byte, len(micRes.data)*2)
				for i, v := range micRes.data {
					binary.LittleEndian.PutUint16(buf[i*2:], uint16(v))
				}
				select {
				case src <- buf:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	wait := make(chan interface{})
	go func() {
		defer close(wait)

		oggPath := path.Join(os.TempDir(), "mic.ogg")
		t.Logf("oggPath = %s", oggPath)

		oggWriter, err := oggwriter.New(oggPath, sampleRate, channels)
		require.NoError(t, err)

		//goland:noinspection GoUnhandledErrorResult
		defer oggWriter.Close()

		var (
			seq             = uint16(rand.Uint32())
			ts              = rand.Uint32()
			ssrc            = rand.Uint32()
			samplesPerFrame = uint32(sampleRate * frameDuration / time.Second)
		)
		for chunk := range opusTranscoder.Chunks() {
			rtpPkt := &rtp.Packet{
				Header: rtp.Header{
					Version:        2,
					PayloadType:    111,
					SequenceNumber: seq,
					Timestamp:      ts,
					SSRC:           ssrc,
				},
				Payload: chunk,
			}

			err := oggWriter.WriteRTP(rtpPkt)
			require.NoError(t, err)

			seq++
			ts += samplesPerFrame
		}
	}()

	err = mic.Start()
	require.NoError(t, err)

	t.Log("Say something. Test will be ended in 5 seconds.")
	<-ctx.Done()

	err = mic.Stop()
	require.NoError(t, err)

	<-wait
}
