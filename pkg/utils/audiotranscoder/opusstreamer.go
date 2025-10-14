// Package audiotranscoder provides a real‑time Linear16 → Opus transcoder
// that keeps wall‑clock time. Even if the Linear16 source delivers no bytes
// during pauses, the transcoder still outputs Opus packets that represent
// silence, so that the downstream consumer continues to receive a constant
// stream.
//
// Build‑time requirement: system libopus (cgo) and the Go package
//
//	go get github.com/hraban/opus
//
// Example usage:
//
//	src := getYourLinear16Reader()
//	ctx := context.Background()
//	s, _ := audiotranscoder.NewOpusStreamer(src, 48_000, 1, nil) // 48 kHz, mono
//	go s.Run(ctx)
//	for pkt := range s.Packets() {
//	    sendOverWebRTC(pkt)
//	}
//	if err := <-s.Err(); err != nil && !errors.Is(err, context.Canceled) {
//	    log.Fatal(err)
//	}
package audiotranscoder

import (
	"context"
	"errors"
	"io"
	"reflect"
	"time"
	"unsafe"

	"github.com/hraban/opus"

	"github.com/dTelecom/sdk-ai-bot/pkg/utils/ringbuffer"
)

// Options tweaks behaviour. Zero values are sensible.
//   - FrameDuration — Opus frame size (2.5‑60 ms). 20 ms is good for VoIP.
//   - BufferMillis  — depth of the internal jitter buffer in milliseconds.
//   - Application   — opus.AppVoIP (default), opus.AppAudio, opus.AppRestrictedLowDelay.
type Options struct {
	FrameDuration time.Duration
	BufferMillis  int
	Application   opus.Application
}

// OpusStreamer converts Linear16 (little‑endian 16‑bit PCM) to Opus in real time.
// Pointers returned by Packets() remain valid only until the next read — copy if
// you need to keep them.
type OpusStreamer struct {
	src        io.Reader
	enc        *opus.Encoder
	sampleRate int
	channels   int
	frameSize  int // samples per channel

	ring *ringbuffer.Ring

	out   chan []byte
	errCh chan error

	opts Options
}

// NewOpusStreamer builds a ready‑to‑run transcoder.
func NewOpusStreamer(src io.Reader, sampleRate, channels int, opts *Options) (*OpusStreamer, error) {
	if channels != 1 && channels != 2 {
		return nil, errors.New("opus: only mono or stereo supported")
	}
	switch sampleRate {
	case 8_000, 12_000, 16_000, 24_000, 48_000:
	default:
		return nil, errors.New("opus: unsupported sample rate")
	}

	var o Options
	if opts != nil {
		o = *opts
	}
	if o.FrameDuration == 0 {
		o.FrameDuration = 20 * time.Millisecond
	}
	if o.BufferMillis == 0 {
		o.BufferMillis = 200
	}
	if o.Application == 0 {
		o.Application = opus.AppVoIP
	}

	frameSize := sampleRate * int(o.FrameDuration/time.Millisecond) / 1000

	enc, err := opus.NewEncoder(sampleRate, channels, o.Application)
	if err != nil {
		return nil, err
	}

	s := &OpusStreamer{
		src:        src,
		enc:        enc,
		sampleRate: sampleRate,
		channels:   channels,
		frameSize:  frameSize,
		ring:       ringbuffer.New(uint64((sampleRate * channels * 2 * o.BufferMillis) / 1000)),
		out:        make(chan []byte),
		errCh:      make(chan error, 1),
		opts:       o,
	}
	return s, nil
}

// Packets yields encoded Opus packets. Close when Run() returns.
func (s *OpusStreamer) Packets() <-chan []byte { return s.out }

// Err returns a channel with the first terminal error.
func (s *OpusStreamer) Err() <-chan error { return s.errCh }

// Run starts transcoding and blocks until ctx.Done() or a fatal error.
func (s *OpusStreamer) Run(ctx context.Context) {
	go s.ingest(ctx)

	ticker := time.NewTicker(s.opts.FrameDuration)
	defer ticker.Stop()
	defer close(s.out)

	pcmFrame := make([]int16, s.frameSize*s.channels)
	encBuf := make([]byte, 4000) // comfortably big

	for {
		select {
		case <-ctx.Done():
			s.errCh <- ctx.Err()
			return
		case <-ticker.C:
			s.pullPCM(pcmFrame)
			n, err := s.enc.Encode(pcmFrame, encBuf)
			if err != nil {
				s.errCh <- err
				return
			}
			pkt := make([]byte, n)
			copy(pkt, encBuf[:n])
			select {
			case s.out <- pkt:
			case <-ctx.Done():
				s.errCh <- ctx.Err()
				return
			}
		}
	}
}

func (s *OpusStreamer) Clear() {
	s.ring.Clear()
}

// ingest continuously copies bytes from src into the ring buffer.
func (s *OpusStreamer) ingest(ctx context.Context) {
	buf := make([]byte, 4096)
	for {
		n, err := s.src.Read(buf)
		buf2Write := buf[:n]
		for {
			w := s.ring.Write(buf2Write)
			buf2Write = buf2Write[w:]
			if len(buf2Write) == 0 {
				break
			}
			select {
			case <-time.After(time.Millisecond * time.Duration(s.opts.BufferMillis/2)):
			case <-ctx.Done():
				return
			}
		}
		if err != nil {
			if err == io.EOF {
				// Treat EOF as "no data right now"; throttle reads.
				select {
				case <-time.After(10 * time.Millisecond):
				case <-ctx.Done():
					return
				}
				continue
			}
			s.errCh <- err
			return
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

// pullPCM fills dst with len(dst) samples, padding with silence if necessary.
func (s *OpusStreamer) pullPCM(dst []int16) {
	bytesNeeded := len(dst) * 2
	take := s.ring.Read(dstAsBytes(dst))
	if take < bytesNeeded {
		for i := take / 2; i < len(dst); i++ {
			dst[i] = 0
		}
	}
}

// dstAsBytes reinterprets a []int16 as []byte without allocation.
func dstAsBytes(p []int16) []byte {
	hdr := (*reflect.SliceHeader)(unsafe.Pointer(&p))
	hdr.Len *= 2
	hdr.Cap *= 2
	return *(*[]byte)(unsafe.Pointer(hdr))
}
