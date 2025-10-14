//go:build manual_integration

package deepgram_test

import (
	"context"
	"math/rand"
	"os"
	"path"
	"testing"
	"time"

	"github.com/hajimehoshi/oto/v2"
	"github.com/joho/godotenv"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3/pkg/media/oggwriter"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dTelecom/sdk-ai-bot/pkg"
	"github.com/dTelecom/sdk-ai-bot/pkg/text_to_speech/deepgram"
	"github.com/dTelecom/sdk-ai-bot/pkg/text_to_speech/transcoder"
	"github.com/dTelecom/sdk-ai-bot/pkg/utils/chanbytereader"
)

const (
	sampleRate = 48000
	channels   = 1
)

func Test_Synthesize_play(t *testing.T) {
	err := godotenv.Load("../../../../.env")
	require.NoError(t, err)

	config := deepgram.DefaultConfig(os.Getenv("DEEPGRAM_API_KEY"))
	config.Encoding = "linear16"
	config.SampleRate = sampleRate

	dg := deepgram.New(os.Getenv("DEEPGRAM_API_KEY"), zap.Must(zap.NewDevelopment()))

	otoCtx, ready, err := oto.NewContext(config.SampleRate, 1, oto.FormatSignedInt16LE)
	require.NoError(t, err)
	<-ready

	textCh := make(chan pkg.TextChunk, 5)
	textCh <- pkg.NewTextContentChunk("Hello! How are you doing?", "user")
	textCh <- pkg.NewTextContentChunk("This is very long text.", "user")
	textCh <- pkg.NewTextContentChunk("I have to say something to make it longer.", "user")
	textCh <- pkg.NewTextContentChunk("Very longer.", "user")
	textCh <- pkg.NewTextContentChunk("\n", "user")
	close(textCh)

	linear16Chunks, err := dg.Synthesize(context.Background(), textCh)
	require.NoError(t, err)

	player := otoCtx.NewPlayer(chanbytereader.New(linear16Chunks))

	player.Play()

	for player.IsPlaying() {
		time.Sleep(time.Second)
	}

	err = player.Close()
	require.NoError(t, err)
}

func Test_Synthesize_saveToOgg(t *testing.T) {
	err := godotenv.Load("../../../../.env")
	require.NoError(t, err)

	opusTranscoderBuilder, err := transcoder.NewLinear16ToOpusBuilder(sampleRate, channels, nil)
	require.NoError(t, err)

	config := deepgram.DefaultConfig(os.Getenv("DEEPGRAM_API_KEY"))
	config.Encoding = "linear16"
	config.SampleRate = sampleRate
	config.TranscoderFactory = opusTranscoderBuilder.BuildAndRunTranscoder

	dg := deepgram.NewWithConfig(config, zap.Must(zap.NewDevelopment()))

	textCh := make(chan pkg.TextChunk, 5)
	textCh <- pkg.NewTextContentChunk("Hello! How are you doing?", "user")
	textCh <- pkg.NewTextContentChunk("This is very long text.", "user")
	textCh <- pkg.NewTextContentChunk("I have to say something to make it longer.", "user")
	textCh <- pkg.NewTextContentChunk("Very longer.", "user")
	textCh <- pkg.NewTextContentChunk("\n", "user")
	close(textCh)

	opusChunks, err := dg.Synthesize(context.Background(), textCh)
	require.NoError(t, err)

	oggPath := path.Join(os.TempDir(), "synthesize.ogg")
	t.Logf("oggPath = %s", oggPath)

	oggWriter, err := oggwriter.New(oggPath, uint32(config.SampleRate), channels)
	require.NoError(t, err)

	var (
		seq  uint16 = uint16(rand.Uint32())
		ts   uint32 = rand.Uint32()
		ssrc uint32 = rand.Uint32()
		step uint32 = 960 // 20 мс * 48 кГц
	)
	for pkt := range opusChunks {
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
}
