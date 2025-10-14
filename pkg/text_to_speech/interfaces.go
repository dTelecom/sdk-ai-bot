package text_to_speech

import (
	"github.com/dTelecom/sdk-ai-bot/pkg"
)

type Transcoder interface {
	Chunks() <-chan pkg.AudioChunk
	Clear()
}

type CreateTranscoderFn func(<-chan pkg.AudioChunk) Transcoder
