package transcoder

import (
	"github.com/dTelecom/sdk-ai-bot/pkg"
	"github.com/dTelecom/sdk-ai-bot/pkg/text_to_speech"
)

type PassThrough struct {
	chunks <-chan pkg.AudioChunk
}

func NewPassThrough(chunks <-chan pkg.AudioChunk) text_to_speech.Transcoder {
	return &PassThrough{
		chunks: chunks,
	}
}

func (t *PassThrough) Chunks() <-chan pkg.AudioChunk {
	return t.chunks
}

func (t *PassThrough) Clear() {}
