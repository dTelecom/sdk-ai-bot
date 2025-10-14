package muxer_test

import (
	"io"
	"os"
	"testing"

	"github.com/pion/webrtc/v3/pkg/media/oggreader"
	"github.com/stretchr/testify/require"

	"github.com/dTelecom/sdk-ai-bot/pkg"
	"github.com/dTelecom/sdk-ai-bot/pkg/speech_to_text/muxer"
)

func TestOgg(t *testing.T) {
	file, err := os.Open("./test_data/speech.ogg")
	require.NoError(t, err)

	oggReader, oggHeader, err := oggreader.NewWith(file)
	require.NoError(t, err)

	src := make(chan pkg.AudioChunk)

	muxerReader := muxer.NewOgg(src, 1000, oggHeader.SampleRate, uint16(oggHeader.Channels))

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

	newFile, err := os.Create("./test_data/new-speech.ogg")
	require.NoError(t, err)

	//goland:noinspection GoUnhandledErrorResult
	defer newFile.Close()

	buffer := make([]byte, 4096)
	for {
		n, err := muxerReader.Read(buffer)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)

		_, err = newFile.Write(buffer[:n])
		require.NoError(t, err)
	}
}
