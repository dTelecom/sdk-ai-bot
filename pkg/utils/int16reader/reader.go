package int16reader

import (
	"encoding/binary"
	"io"
)

// Интерфейс вашего исходного объекта.
//
//go:generate ../../../bin/mockgen -source $GOFILE -destination=mocks/mocks.go -package mocks
type Int16Source interface {
	// Возвращает очередную порцию сэмплов.
	// При достижении конца должен вернуть io.EOF.
	Read() ([]int16, error)
}

// Int16ToByteReader удовлетворяет io.Reader.
type Int16ToByteReader struct {
	src    Int16Source // оригинальный источник
	cache  []byte      // невыданные ранее байты
	closed bool        // получили финальное EOF от src
}

// New создаёт обёртку.
func New(src Int16Source) *Int16ToByteReader {
	return &Int16ToByteReader{src: src}
}

// Read реализует io.Reader.
func (r *Int16ToByteReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
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

		// Читаем новую порцию int16.
		ints, err := r.src.Read()
		if err != nil && err != io.EOF {
			// Ошибка источника: передаём её дальше,
			// но сначала выдаём то, что успели записать.
			if nWritten > 0 {
				return nWritten, nil
			}
			return 0, err
		}
		if err == io.EOF {
			r.closed = true // помним, что дальше данных не будет
		}
		if len(ints) == 0 {
			// Источник вернул EOF без данных.
			if nWritten > 0 {
				return nWritten, nil
			}
			return 0, io.EOF
		}

		// Конвертация int16 → []byte (Little-Endian).
		r.cache = make([]byte, 2*len(ints))
		for i, v := range ints {
			binary.LittleEndian.PutUint16(r.cache[i*2:], uint16(v))
		}
		// Цикл продолжится и скопирует cache в p.
	}

	return nWritten, nil
}
