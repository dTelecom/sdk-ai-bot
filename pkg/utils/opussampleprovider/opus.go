package opussampleprovider

import (
	"errors"
	"time"

	"github.com/pion/webrtc/v3/pkg/media"

	"github.com/dTelecom/sdk-ai-bot/pkg"
)

var ErrNoSamples = errors.New("no samples")

type OpusChunkProvider struct {
	chunks   <-chan pkg.AudioChunk
	frameDur time.Duration
}

func NewFromOpusChunkChan(chunks <-chan pkg.AudioChunk, frameDur time.Duration) *OpusChunkProvider {
	return &OpusChunkProvider{
		chunks:   chunks,
		frameDur: frameDur,
	}
}

func (p *OpusChunkProvider) NextSample() (media.Sample, error) {
	chunk, ok := <-p.chunks
	if !ok {
		return media.Sample{}, ErrNoSamples
	}

	return media.Sample{
		Data:     chunk,
		Duration: p.frameDur,
		// For audio, Timestamp is optional; SDK will set it.
	}, nil
}

func (p *OpusChunkProvider) OnBind() error {
	return nil
}

func (p *OpusChunkProvider) OnUnbind() error {
	return nil
}

func (p *OpusChunkProvider) Close() error {
	return nil
}
