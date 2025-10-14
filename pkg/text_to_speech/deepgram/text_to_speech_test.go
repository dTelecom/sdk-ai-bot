package deepgram_test

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	interfacesv1 "github.com/deepgram/deepgram-go-sdk/v3/pkg/api/speak/v1/websocket/interfaces"
	clientinterfaces "github.com/deepgram/deepgram-go-sdk/v3/pkg/client/interfaces"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"

	"github.com/dTelecom/sdk-ai-bot/pkg"
	"github.com/dTelecom/sdk-ai-bot/pkg/text_to_speech/deepgram"
	"github.com/dTelecom/sdk-ai-bot/pkg/text_to_speech/deepgram/mocks"
	"github.com/dTelecom/sdk-ai-bot/pkg/text_to_speech/transcoder"
	"github.com/dTelecom/sdk-ai-bot/pkg/utils/chanbytereader"
)

func TestTextToSpeech_Synthesize(t *testing.T) {
	tests := []struct {
		name          string
		apiKey        string
		textChunks    []string
		setupMocks    func(*mocks.MockClient)
		expectedError string
		expectedAudio []byte // ожидаемые аудио данные
	}{
		{
			name:       "empty API key",
			apiKey:     "",
			textChunks: []string{"Hello"},
			setupMocks: func(m *mocks.MockClient) {
				// Моки не нужны, так как функция вернет ошибку до их использования
			},
			expectedError: "API key is empty",
		},
		{
			name:       "successful synthesis",
			apiKey:     "test-key",
			textChunks: []string{"Hello", "World"},
			setupMocks: func(m *mocks.MockClient) {
				m.EXPECT().Connect().Return(true)
				m.EXPECT().SpeakWithText("Hello").Return(nil)
				m.EXPECT().SpeakWithText("World").Return(nil)
				m.EXPECT().Flush().Return(nil)
				m.EXPECT().Stop().AnyTimes()
			},
			expectedAudio: []byte{0x01, 0x02, 0x03, 0x04}, // тестовые аудио данные
		},
		{
			name:       "connection failed",
			apiKey:     "test-key",
			textChunks: []string{"Hello"},
			setupMocks: func(m *mocks.MockClient) {
				m.EXPECT().Connect().Return(false)
			},
			expectedError: "failed to connect to Deepgram TTS WS",
		},
		{
			name:       "client creation failed",
			apiKey:     "test-key",
			textChunks: []string{"Hello"},
			setupMocks: func(m *mocks.MockClient) {
				// Моки не нужны, так как ClientFactory вернет ошибку
			},
			expectedError: "failed to create ws client",
		},
		{
			name:       "speak with text error",
			apiKey:     "test-key",
			textChunks: []string{"Hello", "World"},
			setupMocks: func(m *mocks.MockClient) {
				m.EXPECT().Connect().Return(true)
				m.EXPECT().SpeakWithText("Hello").Return(nil)
				m.EXPECT().SpeakWithText("World").Return(errors.New("speak error"))
				m.EXPECT().ProcessError(gomock.Any()).Return(nil)
				m.EXPECT().Flush().Return(nil)
				m.EXPECT().Stop().AnyTimes()
			},
			expectedAudio: []byte{0x05, 0x06}, // тестовые аудио данные для первого чанка
		},
		{
			name:       "empty text chunks",
			apiKey:     "test-key",
			textChunks: []string{},
			setupMocks: func(m *mocks.MockClient) {
				m.EXPECT().Connect().Return(true)
				m.EXPECT().Flush().Return(nil)
				m.EXPECT().Stop().AnyTimes()
			},
			expectedAudio: []byte{},
		},
		{
			name:       "single text chunk",
			apiKey:     "test-key",
			textChunks: []string{"Single chunk"},
			setupMocks: func(m *mocks.MockClient) {
				m.EXPECT().Connect().Return(true)
				m.EXPECT().SpeakWithText("Single chunk").Return(nil)
				m.EXPECT().Flush().Return(nil)
				m.EXPECT().Stop().AnyTimes()
			},
			expectedAudio: []byte{0x07, 0x08, 0x09}, // тестовые аудио данные
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockClient := mocks.NewMockClient(ctrl)
			tt.setupMocks(mockClient)

			var capturedCallback interfacesv1.SpeakMessageCallback

			config := deepgram.Config{
				APIKey:            tt.apiKey,
				TranscoderFactory: transcoder.NewPassThrough,
				ClientFactory: func(ctx context.Context, APIKey string, cOptions *clientinterfaces.ClientOptions, sOptions *clientinterfaces.WSSpeakOptions, cb interfacesv1.SpeakMessageCallback) (deepgram.Client, error) {
					if tt.expectedError == "failed to create ws client" {
						return nil, errors.New("failed to create ws client")
					}
					capturedCallback = cb
					return mockClient, nil
				},
			}

			tts := deepgram.NewWithConfig(config, zap.Must(zap.NewDevelopment()))

			// Создаем канал с TextChunk объектами
			textCh := make(chan pkg.TextChunk, len(tt.textChunks))
			go func() {
				defer close(textCh)
				for _, chunk := range tt.textChunks {
					textCh <- pkg.NewTextContentChunk(chunk, "")
				}
			}()

			// Вызываем Synthesize
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			chunks, err := tts.Synthesize(ctx, textCh)

			// Проверяем результат
			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
				assert.Nil(t, chunks)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, chunks)

				assert.NotNil(t, capturedCallback)

				// Симулируем получение аудио данных через callback
				if capturedCallback != nil {
					err := capturedCallback.Binary(tt.expectedAudio)
					assert.NoError(t, err, "callback Binary должен работать без ошибок")

					err = capturedCallback.Flush(&interfacesv1.FlushedResponse{})
					assert.NoError(t, err, "callback Flush должен работать без ошибок")

					err = capturedCallback.Close(&interfacesv1.CloseResponse{})
					assert.NoError(t, err, "callback Close должен работать без ошибок")

					// Проверяем, что данные можно прочитать из reader
					audioData, readErr := io.ReadAll(chanbytereader.New(chunks))
					assert.NoError(t, readErr, "не должно быть ошибки при чтении")
					assert.Equal(t, tt.expectedAudio, audioData, "полученные аудио данные должны совпадать с ожидаемыми")
				}
			}

			// Даем время горутине завершиться, чтобы моки успели вызваться
			time.Sleep(200 * time.Millisecond)
		})
	}
}
