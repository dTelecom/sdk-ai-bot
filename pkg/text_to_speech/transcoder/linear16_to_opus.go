package transcoder

import (
	"errors"
	"reflect"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/hraban/opus"

	"github.com/dTelecom/sdk-ai-bot/pkg"
	"github.com/dTelecom/sdk-ai-bot/pkg/text_to_speech"
	"github.com/dTelecom/sdk-ai-bot/pkg/utils/ringbuffer"
)

// Linear16ToOpusOptions tweaks behaviour. Zero values are sensible.
//   - FrameDuration — Opus frame size (2.5‑60 ms). 20 ms is good for VoIP.
//   - BufferMillis  — depth of the internal jitter buffer in milliseconds.
//   - Application   — opus.AppVoIP (default), opus.AppAudio, opus.AppRestrictedLowDelay.
type Linear16ToOpusOptions struct {
	FrameDuration time.Duration
	BufferMillis  int
	Application   opus.Application
}

type Linear16ToOpusBuilder struct {
	enc        *opus.Encoder
	sampleRate int
	channels   int
	frameSize  int // samples per channel

	opts Linear16ToOpusOptions
}

// NewLinear16ToOpusBuilder builds a transcoder builder.
func NewLinear16ToOpusBuilder(sampleRate, channels int, opts *Linear16ToOpusOptions) (*Linear16ToOpusBuilder, error) {
	if channels != 1 && channels != 2 {
		return nil, errors.New("opus: only mono or stereo supported")
	}
	switch sampleRate {
	case 8_000, 12_000, 16_000, 24_000, 48_000:
	default:
		return nil, errors.New("opus: unsupported sample rate")
	}

	var o Linear16ToOpusOptions
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

	s := &Linear16ToOpusBuilder{
		enc:        enc,
		sampleRate: sampleRate,
		channels:   channels,
		frameSize:  frameSize,
		opts:       o,
	}
	return s, nil
}

// Linear16ToOpus converts Linear16 (little‑endian 16‑bit PCM) to Opus in real time.
// Pointers returned by Packets() remain valid only until the next read — copy if
// you need to keep them.
type Linear16ToOpus struct {
	enc        *opus.Encoder
	sampleRate int
	channels   int
	frameSize  int // samples per channel

	ring *ringbuffer.Ring

	src   <-chan pkg.AudioChunk
	out   chan pkg.AudioChunk
	errCh chan error

	ended atomic.Bool

	opts Linear16ToOpusOptions
}

// BuildAndRunTranscoder starts transcoding and blocks until ctx.Done() or a fatal error.
func (b *Linear16ToOpusBuilder) BuildAndRunTranscoder(src <-chan pkg.AudioChunk) text_to_speech.Transcoder {
	t := &Linear16ToOpus{
		enc:        b.enc,
		sampleRate: b.sampleRate,
		channels:   b.channels,
		frameSize:  b.frameSize,

		ring: ringbuffer.New(uint64((b.sampleRate * b.channels * 2 * b.opts.BufferMillis) / 1000)),

		src:   src,
		out:   make(chan pkg.AudioChunk),
		errCh: make(chan error, 1),

		opts: b.opts,
	}
	go t.ingest()
	go t.produceOpusSample()

	return t
}

// Chunks yields encoded Opus packets. Close when Run() returns.
func (s *Linear16ToOpus) Chunks() <-chan pkg.AudioChunk { return s.out }

// Err returns a channel with the first terminal error.
func (s *Linear16ToOpus) Err() <-chan error { return s.errCh }

func (s *Linear16ToOpus) Clear() {
	s.ring.Clear()
}

// ingest continuously copies bytes from src into the ring buffer.
func (s *Linear16ToOpus) ingest() {
	defer s.ended.Store(true)

	for chunk := range s.src {
		for {
			w := s.ring.Write(chunk)
			chunk = chunk[w:]
			if len(chunk) == 0 {
				break
			}
			<-time.After(time.Millisecond * time.Duration(s.opts.BufferMillis/2))
		}
	}
}

func (s *Linear16ToOpus) produceOpusSample() {
	defer close(s.out)
	defer close(s.errCh)

	pcmFrame := make([]int16, s.frameSize*s.channels)
	encBuf := make([]byte, 4000) // comfortably big

	ticker := time.NewTicker(s.opts.FrameDuration)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			takeBytes := s.pullPCM(pcmFrame)
			if takeBytes == 0 && s.ended.Load() {
				return
			}
			n, err := s.enc.Encode(pcmFrame, encBuf)
			if err != nil {
				s.errCh <- err
				return
			}
			pkt := make([]byte, n)
			copy(pkt, encBuf[:n])

			s.out <- pkt
		}
	}
}

// pullPCM fills dst with len(dst) samples, padding with silence if necessary.
func (s *Linear16ToOpus) pullPCM(dst []int16) int {
	bytesNeeded := len(dst) * 2
	take := s.ring.Read(dstAsBytes(dst))
	if take < bytesNeeded {
		for i := take / 2; i < len(dst); i++ {
			dst[i] = 0
		}
	}
	return take
}

// dstAsBytes reinterprets a []int16 as []byte without allocation.
func dstAsBytes(p []int16) []byte {
	hdr := (*reflect.SliceHeader)(unsafe.Pointer(&p))
	hdr.Len *= 2
	hdr.Cap *= 2
	return *(*[]byte)(unsafe.Pointer(hdr))
}
