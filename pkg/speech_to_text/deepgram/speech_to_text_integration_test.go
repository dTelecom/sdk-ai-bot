//go:build integration

package deepgram_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/joho/godotenv"
	"github.com/pion/webrtc/v3/pkg/media/oggreader"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dTelecom/sdk-ai-bot/pkg"
	"github.com/dTelecom/sdk-ai-bot/pkg/speech_to_text"
	"github.com/dTelecom/sdk-ai-bot/pkg/speech_to_text/deepgram"
	"github.com/dTelecom/sdk-ai-bot/pkg/speech_to_text/muxer"
)

func Test_TranscribeFromFile(t *testing.T) {
	err := godotenv.Load("../../../../.env")
	require.NoError(t, err)

	file, err := os.Open("./test_data/speech.opus")
	require.NoError(t, err)

	oggReader, oggHeader, err := oggreader.NewWith(file)
	require.NoError(t, err)

	config := deepgram.DefaultConfig(os.Getenv("DEEPGRAM_API_KEY"))
	// params will be detected from container header
	config.Channels = 0
	config.SampleRate = 0
	config.Encoding = ""
	// ---------------------------------------------
	config.Language = "en-US"
	config.MuxerFactory = func(chunks <-chan pkg.AudioChunk) speech_to_text.Muxer {
		return muxer.NewOgg(chunks, 1000, oggHeader.SampleRate, uint16(oggHeader.Channels))
	}

	att := deepgram.NewWithConfig(config, zap.Must(zap.NewDevelopment()))

	src := make(chan pkg.AudioChunk)

	chunks, err := att.Transcribe(context.Background(), src)
	require.NoError(t, err)

	go func() {
		defer close(src)

		for {
			pagePayload, pageHeader, err := oggReader.ParseNextPage()
			if err == io.EOF {
				break
			}
			require.NoError(t, err)

			if pageHeader.GranulePosition == 0 {
				continue
			}

			src <- pagePayload
		}
	}()

	// var transctiption string
	for chunk := range chunks {
		if chunk.Type() == pkg.SpeechChunkTypeText {
			if stringToken, ok := chunk.(*pkg.SpeechTextChunk); ok {
				// transctiption += stringToken.Text
				fmt.Println(stringToken.Text)
			}
		}
	}

	// require.Contains(t, transctiption, "Yep")
	// require.Contains(t, transctiption, "said it before")
	// require.Contains(t, transctiption, "say it again")
	// require.Contains(t, transctiption, "moves pretty fast")
	// require.Contains(t, transctiption, "stop and look around once in a while")
}
