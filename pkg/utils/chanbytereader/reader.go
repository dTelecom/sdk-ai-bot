package chanbytereader

import (
	"io"
	"sync/atomic"
)

type Reader struct {
	src    <-chan []byte
	muted  atomic.Bool
	cache  []byte
	closed bool
}

// New creates a new Reader for the given channel.
func New(ch <-chan []byte) *Reader {
	return &Reader{
		src: ch,
	}
}

// Read реализует io.Reader.
func (r *Reader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	if r.muted.Load() {
		if len(r.cache) > 0 {
			r.cache = r.cache[:0]
		}
		for len(r.src) > 0 {
			b := <-r.src
			if b == nil {
				r.muted.Store(false)
				break
			}
		}
		return 0, nil
	}

	nWritten := 0
	for len(p) > 0 {
		// Если есть байты из прошлых вызовов – отдаём их сначала.
		if len(r.cache) > 0 {
			copied := copy(p, r.cache)
			p = p[copied:]
			r.cache = r.cache[copied:]
			nWritten += copied
			continue
		}

		// Если кэш пуст, а источник уже вернул EOF раньше, то
		// либо сразу сообщаем о конце, либо отдаём накопленное.
		if r.closed {
			if nWritten > 0 {
				return nWritten, nil
			}
			return 0, io.EOF
		}

		// Читаем новую порцию []byte.
		select {
		case bytes, ok := <-r.src:
			if !ok {
				r.closed = true
				if nWritten > 0 {
					return nWritten, nil
				}
				return 0, io.EOF
			}
			r.cache = bytes
		default:
			return nWritten, nil
		}
	}

	return nWritten, nil
}

func (r *Reader) Clear() {
	r.muted.Store(true)
}
