package muxer

import (
	"io"

	"github.com/dTelecom/sdk-ai-bot/pkg"
	"github.com/dTelecom/sdk-ai-bot/pkg/speech_to_text"
)

type Linear struct {
	src    <-chan pkg.AudioChunk
	cache  []byte
	closed bool
}

func NewLinear(src <-chan pkg.AudioChunk) speech_to_text.Muxer {
	return &Linear{
		src: src,
	}
}

func (l *Linear) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	nWritten := 0
	for len(p) > 0 {
		// Если есть байты из прошлых вызовов – отдаём их сначала.
		if len(l.cache) > 0 {
			copied := copy(p, l.cache)
			p = p[copied:]
			l.cache = l.cache[copied:]
			nWritten += copied
			continue
		}

		// Если кэш пуст, а источник уже вернул EOF раньше, то
		// либо сразу сообщаем о конце, либо отдаём накопленное.
		if l.closed {
			if nWritten > 0 {
				return nWritten, nil
			}
			return 0, io.EOF
		}

		// Читаем новую порцию []byte.
		select {
		case bytes, ok := <-l.src:
			if !ok {
				l.closed = true
				if nWritten > 0 {
					return nWritten, nil
				}
				return 0, io.EOF
			}
			l.cache = bytes
		default:
			return nWritten, nil
		}
	}

	return nWritten, nil
}
