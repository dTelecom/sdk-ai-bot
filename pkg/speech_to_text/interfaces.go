package speech_to_text

import (
	"io"

	"github.com/dTelecom/sdk-ai-bot/pkg"
)

type Muxer interface {
	io.Reader
}

type CreateMuxerFn func(<-chan pkg.AudioChunk) Muxer
