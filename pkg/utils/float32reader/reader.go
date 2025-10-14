package float32reader

import (
	"encoding/binary"
	"io"
	"math"
)

// Интерфейс вашего исходного объекта.
type Float32Source interface {
	// Возвращает очередную порцию сэмплов.
	// При достижении конца должен вернуть io.EOF.
	Read() ([]float32, error)
}

// Int16ToByteReader удовлетворяет io.Reader.
type Float32ToByteReader struct {
	src    Float32Source // оригинальный источник
	cache  []byte        // невыданные ранее байты
	closed bool          // получили финальное EOF от src
}

// NewInt16ToByteReader создаёт обёртку.
func New(src Float32Source) *Float32ToByteReader {
	return &Float32ToByteReader{src: src}
}

// Read реализует io.Reader.
func (r *Float32ToByteReader) Read(p []byte) (int, error) {
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
		floats, err := r.src.Read()
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
		if len(floats) == 0 {
			// Источник вернул EOF без данных.
			if nWritten > 0 {
				return nWritten, nil
			}
			return 0, io.EOF
		}

		// Конвертация float32 → []byte (Little-Endian).
		r.cache = make([]byte, 4*len(floats))
		for i, v := range floats {
			u := math.Float32bits(v)
			binary.LittleEndian.PutUint32(r.cache[i*4:], u)
		}
		// Цикл продолжится и скопирует cache в p.
	}

	return nWritten, nil
}
