package int16reader_test

import (
	"io"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/dTelecom/sdk-ai-bot/pkg/utils/int16reader"
	"github.com/dTelecom/sdk-ai-bot/pkg/utils/int16reader/mocks"
)

func TestInt16ToByteReader_Read(t *testing.T) {
	tests := []struct {
		name        string
		int16Chunks [][]int16
		bufferSize  int
		want        [][]byte
	}{
		{
			name:        "",
			int16Chunks: [][]int16{{1, 2, 3}, {4, 5, 6}},
			bufferSize:  2,
			want:        [][]byte{{1, 0}, {2, 0}, {3, 0}, {4, 0}, {5, 0}, {6, 0}},
		},
		{
			name:        "",
			int16Chunks: [][]int16{{1, 2, 3}, {4, 5, 6}},
			bufferSize:  8,
			want:        [][]byte{{1, 0, 2, 0, 3, 0, 4, 0}, {5, 0, 6, 0}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			int16Source := mocks.NewMockInt16Source(ctrl)
			for _, chunk := range tt.int16Chunks {
				int16Source.EXPECT().
					Read().
					Return(chunk, nil)
			}
			int16Source.EXPECT().
				Read().
				Return(nil, io.EOF)

			int16ToByteReader := int16reader.New(int16Source)
			buffer := make([]byte, tt.bufferSize)
			for _, expected := range tt.want {
				red, err := int16ToByteReader.Read(buffer)
				require.NoError(t, err)
				require.Equal(t, expected, buffer[:red])
			}
			_, err := int16ToByteReader.Read(buffer)
			require.ErrorIs(t, err, io.EOF)
		})
	}
}
