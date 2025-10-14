//go:build manual_integration

package deepgram_test

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"os"
	"reflect"
	"testing"
	"time"
	"unsafe"

	"github.com/deepgram/deepgram-go-sdk/v3/pkg/audio/microphone"
	"github.com/hraban/opus"
	"github.com/joho/godotenv"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3/pkg/media/oggwriter"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dTelecom/sdk-ai-bot/pkg"
	"github.com/dTelecom/sdk-ai-bot/pkg/speech_to_text"
	"github.com/dTelecom/sdk-ai-bot/pkg/speech_to_text/deepgram"
	"github.com/dTelecom/sdk-ai-bot/pkg/utils/ringbuffer"
)

const (
	sampleRate    = 48_000
	channels      = 1
	frameDuration = 20 * time.Millisecond
)

func Test_TranscribeFromMic_linear(t *testing.T) {
	err := godotenv.Load("../../../../.env")
	require.NoError(t, err)

	microphone.Initialize()

	mic, err := microphone.New(microphone.AudioConfig{
		InputChannels: channels,
		SamplingRate:  sampleRate,
	})
	require.NoError(t, err)

	config := deepgram.DefaultConfig(os.Getenv("DEEPGRAM_API_KEY"))
	config.Channels = channels
	config.SampleRate = sampleRate
	config.Encoding = "linear16"
	config.Language = "en-US"

	dg := deepgram.NewWithConfig(config, zap.Must(zap.NewDevelopment()))

	src := make(chan pkg.AudioChunk)

	chunks, err := dg.Transcribe(context.Background(), src)
	require.NoError(t, err)

	go func() {
		defer close(src)

		for {
			data, err := mic.Read()
			if err != nil {
				if err == io.EOF {
					break
				}
				require.NoError(t, err)
			}
			src <- int16sToBytes(data)
		}
	}()

	err = mic.Start()
	require.NoError(t, err)

	for chunk := range chunks {
		fmt.Println(chunk)
	}

	_ = mic.Stop()
	microphone.Teardown()
}

func Test_TranscribeFromMic_opus(t *testing.T) {
	err := godotenv.Load("../../../../.env")
	require.NoError(t, err)

	microphone.Initialize()

	mic, err := microphone.New(microphone.AudioConfig{
		InputChannels: channels,
		SamplingRate:  sampleRate,
	})
	require.NoError(t, err)

	config := deepgram.DefaultConfig(os.Getenv("DEEPGRAM_API_KEY"))
	config.Channels = 0
	config.SampleRate = 0
	config.Encoding = ""
	config.Language = "en-US"
	config.MuxerFactory = linear16ToOgg

	dg := deepgram.NewWithConfig(config, zap.Must(zap.NewDevelopment()))

	src := make(chan pkg.AudioChunk)

	chunks, err := dg.Transcribe(context.Background(), src)
	require.NoError(t, err)

	go func() {
		defer close(src)

		for {
			data, err := mic.Read()
			if err != nil {
				if err == io.EOF {
					break
				}
				require.NoError(t, err)
			}
			src <- int16sToBytes(data)
		}
	}()

	err = mic.Start()
	require.NoError(t, err)

	for chunk := range chunks {
		fmt.Println(chunk)
	}

	_ = mic.Stop()
	microphone.Teardown()
}

func Test_SaveMicToFile(t *testing.T) {
	microphone.Initialize()

	mic, err := microphone.New(microphone.AudioConfig{
		InputChannels: channels,
		SamplingRate:  sampleRate,
	})
	require.NoError(t, err)

	src := make(chan pkg.AudioChunk)
	go func() {
		defer close(src)

		for {
			data, err := mic.Read()
			if err != nil {
				if err == io.EOF {
					break
				}
				require.NoError(t, err)
			}
			src <- int16sToBytes(data)
		}
	}()

	err = mic.Start()
	require.NoError(t, err)

	defer func() {
		_ = mic.Stop()
		microphone.Teardown()
	}()

	oggReader := linear16ToOgg(src)

	outFile, err := os.Create("./output.ogg")
	require.NoError(t, err)

	//goland:noinspection ALL
	defer outFile.Close()

	_, err = io.Copy(outFile, oggReader)
	require.NoError(t, err)
}

func int16sToBytes(p []int16) []byte {
	hdr := (*reflect.SliceHeader)(unsafe.Pointer(&p))
	hdr.Len *= 2
	hdr.Cap *= 2
	return *(*[]byte)(unsafe.Pointer(hdr))
}

func linear16ToOgg(chunks <-chan pkg.AudioChunk) speech_to_text.Muxer {
	enc, err := opus.NewEncoder(sampleRate, channels, opus.AppVoIP)
	if err != nil {
		panic(err)
	}

	samplesPerFrame := uint32(sampleRate * frameDuration / time.Second)
	pcmFrame := make([]int16, samplesPerFrame)
	encBuf := make([]byte, 4000) // comfortably big

	pipeReader, pipeWriter := io.Pipe()

	linear16RingBuffer := ringbuffer.New(uint64(samplesPerFrame * 10) /*real size will be bigger*/)
	go func() {
		oggWriter, err := oggwriter.NewWith(pipeWriter, sampleRate, channels)
		if err != nil {
			panic(err)
		}

		//goland:noinspection GoUnhandledErrorResult
		defer oggWriter.Close()

		var (
			seq  = uint16(rand.Uint32())
			ts   = rand.Uint32()
			ssrc = rand.Uint32()
		)
		for chunk := range chunks {
			for len(chunk) > 0 {
				n := linear16RingBuffer.Write(chunk)
				chunk = chunk[n:]

				for linear16RingBuffer.Available() > int(samplesPerFrame*2) {
					read := linear16RingBuffer.Read(int16sToBytes(pcmFrame))
					if read != int(samplesPerFrame*2) {
						panic("frame size too small")
					}
					bufLen, err := enc.Encode(pcmFrame, encBuf)
					if err != nil {
						panic(err)
					}

					rtpPkt := &rtp.Packet{
						Header: rtp.Header{
							Version:        2,
							PayloadType:    111,
							SequenceNumber: seq,
							Timestamp:      ts,
							SSRC:           ssrc,
						},
						Payload: encBuf[:bufLen],
					}
					if err := oggWriter.WriteRTP(rtpPkt); err != nil {
						panic(err)
					}

					seq++
					ts += samplesPerFrame
				}
			}
		}
	}()

	return pipeReader
}
