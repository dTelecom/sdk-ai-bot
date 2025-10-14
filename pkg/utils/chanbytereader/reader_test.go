package chanbytereader

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestReader_Read_Table(t *testing.T) {
	tests := []struct {
		name        string
		inputChunks [][]byte
		bufSize     int
		expected    []byte
		description string
	}{
		{
			name:        "empty channel",
			inputChunks: [][]byte{},
			bufSize:     8,
			expected:    []byte{},
			description: "чтение из пустого канала должно вернуть 0 байт и EOF",
		},
		{
			name:        "single chunk smaller than buffer",
			inputChunks: [][]byte{{1, 2, 3}},
			bufSize:     10,
			expected:    []byte{1, 2, 3},
			description: "чтение одного чанка меньше буфера",
		},
		{
			name:        "single chunk larger than buffer",
			inputChunks: [][]byte{{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}},
			bufSize:     3,
			expected:    []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
			description: "чтение большого чанка по частям",
		},
		{
			name:        "multiple chunks",
			inputChunks: [][]byte{{1, 2, 3}, {4, 5, 6}, {7, 8, 9}},
			bufSize:     2,
			expected:    []byte{1, 2, 3, 4, 5, 6, 7, 8, 9},
			description: "чтение нескольких чанков по 2 байта",
		},
		{
			name:        "empty chunks in sequence",
			inputChunks: [][]byte{{}, {1, 2, 3}, {}, {4, 5, 6}},
			bufSize:     4,
			expected:    []byte{1, 2, 3, 4, 5, 6},
			description: "обработка пустых чанков в последовательности",
		},
		{
			name:        "read after close",
			inputChunks: [][]byte{{1, 2, 3}},
			bufSize:     1,
			expected:    []byte{1, 2, 3},
			description: "чтение после закрытия канала",
		},
		{
			name:        "partial reads across chunks",
			inputChunks: [][]byte{{1, 2, 3, 4}, {5, 6, 7, 8}},
			bufSize:     3,
			expected:    []byte{1, 2, 3, 4, 5, 6, 7, 8},
			description: "частичное чтение через границы чанков",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := make(chan []byte, len(tt.inputChunks))
			go func() {
				defer close(ch)
				for _, chunk := range tt.inputChunks {
					ch <- chunk
					time.Sleep(1 * time.Millisecond)
				}
			}()

			reader := New(ch)
			var result bytes.Buffer
			buf := make([]byte, tt.bufSize)
			for {
				n, err := reader.Read(buf)
				if n > 0 {
					result.Write(buf[:n])
				}
				if err == io.EOF {
					break
				}
				assert.NoError(t, err, "неожиданная ошибка при чтении")
			}

			// Для пустого канала используем assert.Empty, для остальных - assert.Equal
			if len(tt.expected) == 0 {
				assert.Empty(t, result.Bytes(), "прочитанные данные должны быть пустыми")
			} else {
				assert.Equal(t, tt.expected, result.Bytes(), "прочитанные данные должны совпадать с ожидаемыми")
			}
		})
	}
}

func TestReader_Read_Stress(t *testing.T) {
	t.Run("stress test with large data", func(t *testing.T) {
		ch := make(chan []byte, 100)
		reader := New(ch)

		// Создаем большой объем данных
		expectedData := make([]byte, 10000)
		for i := range expectedData {
			expectedData[i] = byte(i % 256)
		}

		// Отправляем данные чанками
		go func() {
			defer close(ch)
			chunkSize := 100
			for i := 0; i < len(expectedData); i += chunkSize {
				end := i + chunkSize
				if end > len(expectedData) {
					end = len(expectedData)
				}
				ch <- expectedData[i:end]
				time.Sleep(1 * time.Millisecond)
			}
		}()

		// Читаем данные
		var result bytes.Buffer
		buffer := make([]byte, 50) // Маленький буфер для тестирования

		for {
			n, err := reader.Read(buffer)
			if n > 0 {
				result.Write(buffer[:n])
			}
			if err == io.EOF {
				break
			}
			assert.NoError(t, err, "неожиданная ошибка при чтении")
		}

		assert.Equal(t, expectedData, result.Bytes(), "прочитанные данные должны совпадать с отправленными")
	})
}

func TestReader_Read_NonBlocking(t *testing.T) {
	t.Run("non-blocking read from empty channel", func(t *testing.T) {
		ch := make(chan []byte)
		reader := New(ch)

		buffer := make([]byte, 10)
		n, err := reader.Read(buffer)

		// Неблокирующее чтение должно вернуть 0 байт без ошибки
		assert.Equal(t, 0, n, "должно вернуть 0 байт")
		assert.NoError(t, err, "не должно быть ошибки")
	})
}
