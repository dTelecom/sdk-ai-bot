// Package opussampleprovider adapts a Linear16 io.Reader to LiveKit's lksdk.SampleProvider
// interface, emitting Opus packets in real‑time using audiotranscoder.OpusStreamer.
//
// The provider is deliberately opinionated — it assumes 48 kHz, mono Linear16 on
// input and produces 20‑ms Opus frames (typical for WebRTC/LiveKit). Feel free
// to extend the constructor to accept a sample‑rate/channels/options struct if
// you need something else.
//
//	src := getLinear16Reader()
//	provider := opussampleprovider.NewFromLinear16Reader(src)
//	// pass *provider to lksdk.NewLocalSampleTrack(provider, lksdk.Opus)
package opussampleprovider

import (
	"context"
	"io"
	"time"

	"github.com/pion/webrtc/v3/pkg/media"

	"github.com/dTelecom/sdk-ai-bot/pkg/utils/audiotranscoder"
)

// NOTE: the methods use *ByteReaderProvider receivers so that internal mutable state is
// preserved. (The stub code you supplied had value receivers, which would copy
// the struct and break statefulness.)

type ByteReaderProvider struct {
	r io.Reader

	streamer *audiotranscoder.OpusStreamer
	packets  <-chan []byte
	errCh    <-chan error

	ctx    context.Context
	cancel context.CancelFunc

	frameDur time.Duration // cached for media.Sample.Duration
}

// NewFromLinear16Reader wires a ByteReaderProvider with default OpusStreamer settings
// (48 kHz, mono, 20‑ms frames).
func NewFromLinear16Reader(r io.Reader) *ByteReaderProvider {
	return &ByteReaderProvider{r: r}
}

// ensureInit lazily creates and starts the OpusStreamer on first bind.
func (p *ByteReaderProvider) ensureInit() error {
	if p.streamer != nil {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	streamer, err := audiotranscoder.NewOpusStreamer(p.r, 48_000, 1, &audiotranscoder.Options{BufferMillis: 30_000})
	if err != nil {
		cancel()
		return err
	}
	p.ctx = ctx
	p.cancel = cancel
	p.streamer = streamer
	p.packets = streamer.Packets()
	p.errCh = streamer.Err()
	p.frameDur = 20 * time.Millisecond // matches default in OpusStreamer

	go streamer.Run(ctx)
	return nil
}

// NextSample blocks until the next Opus packet is available or context/error.
func (p *ByteReaderProvider) NextSample(ctx context.Context) (media.Sample, error) {
	if err := p.ensureInit(); err != nil {
		return media.Sample{}, err
	}

	for {
		select {
		case pkt, ok := <-p.packets:
			if !ok {
				// Streamer finished; check for error.
				select {
				case err := <-p.errCh:
					if err == nil {
						err = io.EOF
					}
					return media.Sample{}, err
				default:
					return media.Sample{}, io.EOF
				}
			}
			return media.Sample{
				Data:     pkt,
				Duration: p.frameDur,
				// For audio, Timestamp is optional; SDK will set it.
			}, nil

		case err := <-p.errCh:
			if err == nil {
				err = io.EOF
			}
			return media.Sample{}, err

		case <-ctx.Done():
			return media.Sample{}, ctx.Err()
		}
	}
}

// OnBind is called by LiveKit when the track is added to a PeerConnection.
func (p *ByteReaderProvider) OnBind() error {
	return p.ensureInit()
}

// OnUnbind stops the internal pipeline but keeps ByteReaderProvider reusable.
func (p *ByteReaderProvider) OnUnbind() error {
	if p.cancel != nil {
		p.cancel()
	}
	p.streamer = nil
	p.packets = nil
	p.errCh = nil
	return nil
}

// Close fully releases resources; after this the ByteReaderProvider should not be used.
func (p *ByteReaderProvider) Close() error {
	if p.cancel != nil {
		p.cancel()
	}
	// Drain potential error to avoid goroutine leak.
	select {
	case <-p.errCh:
	default:
	}
	p.streamer = nil
	return nil
}
