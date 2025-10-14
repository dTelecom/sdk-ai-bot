package muxer

import (
	"io"
	"math/rand"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3/pkg/media/oggwriter"

	"github.com/dTelecom/sdk-ai-bot/pkg"
	"github.com/dTelecom/sdk-ai-bot/pkg/speech_to_text"
)

const (
	rtpVersion     = 2
	rtpPayloadType = 111
)

func NewOgg(src <-chan pkg.AudioChunk, sampleDurationInMillis uint32, sampleRate uint32, channelCount uint16) speech_to_text.Muxer {
	pipeReader, pipeWriter := io.Pipe()

	go func() {
		//goland:noinspection GoUnhandledErrorResult
		defer pipeWriter.Close()

		oggWriter, err := oggwriter.NewWith(pipeWriter, sampleRate, channelCount)
		if err != nil {
			return
		}

		//goland:noinspection GoUnhandledErrorResult
		defer oggWriter.Close()

		var (
			seq  = uint16(rand.Uint32())
			ts   = rand.Uint32()
			ssrc = rand.Uint32()
			step = sampleDurationInMillis * sampleRate / 1000
		)
		for chunk := range src {
			rtpPkt := &rtp.Packet{
				Header: rtp.Header{
					Version:        rtpVersion,
					PayloadType:    rtpPayloadType,
					SequenceNumber: seq,
					Timestamp:      ts,
					SSRC:           ssrc,
				},
				Payload: chunk,
			}
			if err := oggWriter.WriteRTP(rtpPkt); err != nil {
				return
			}

			seq++
			ts += step
		}
	}()

	return pipeReader
}
