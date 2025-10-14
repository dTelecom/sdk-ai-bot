package deepgram_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/golang/mock/gomock"
	"go.uber.org/zap"

	"github.com/dTelecom/sdk-ai-bot/pkg"
	deepgram2 "github.com/dTelecom/sdk-ai-bot/pkg/speech_to_text/deepgram"
	"github.com/dTelecom/sdk-ai-bot/pkg/speech_to_text/deepgram/mocks"
	"github.com/dTelecom/sdk-ai-bot/pkg/speech_to_text/muxer"

	wsinterfaces "github.com/deepgram/deepgram-go-sdk/v3/pkg/api/listen/v1/websocket/interfaces"
	clientinterfaces "github.com/deepgram/deepgram-go-sdk/v3/pkg/client/interfaces"
	"github.com/stretchr/testify/assert"
)

func TestAudioToText_Transcribe(t *testing.T) {
	src := make(chan pkg.AudioChunk, 1)
	src <- []byte{0, 0, 0, 0}
	close(src)

	type fields struct {
		ctx    context.Context
		config deepgram2.Config
	}
	tests := []struct {
		name    string
		fields  fields
		want    []pkg.SpeechChunk
		wantErr bool
		setup   func(*testing.T, *mocks.MockDeepgramClient, func() wsinterfaces.LiveMessageCallback)
	}{
		{
			name: "successful transcription",
			fields: fields{
				ctx: context.Background(),
				config: deepgram2.Config{
					APIKey: "test-key",
				},
			},
			setup: func(t *testing.T, m *mocks.MockDeepgramClient, callbackGetter func() wsinterfaces.LiveMessageCallback) {
				m.EXPECT().Connect().Return(true)
				m.EXPECT().Stream(gomock.Any()).DoAndReturn(func(_ io.Reader) error {
					cb := callbackGetter()

					// SpeechStarted event
					err := cb.SpeechStarted(nil)
					assert.NoError(t, err)

					// Interim result (not final)
					err = cb.Message(&wsinterfaces.MessageResponse{
						Channel: wsinterfaces.Channel{Alternatives: []wsinterfaces.Alternative{{Transcript: "Hi"}}},
						IsFinal: false,
					})
					assert.NoError(t, err)

					// Final result but not speech final
					err = cb.Message(&wsinterfaces.MessageResponse{
						Channel:     wsinterfaces.Channel{Alternatives: []wsinterfaces.Alternative{{Transcript: "Hello"}}},
						IsFinal:     true,
						SpeechFinal: false,
					})
					assert.NoError(t, err)

					// Final result with speech final - this should send the token
					err = cb.Message(&wsinterfaces.MessageResponse{
						Channel:     wsinterfaces.Channel{Alternatives: []wsinterfaces.Alternative{{Transcript: "World."}}},
						IsFinal:     true,
						SpeechFinal: true,
					})
					assert.NoError(t, err)

					// Another final result with speech final
					err = cb.Message(&wsinterfaces.MessageResponse{
						Channel:     wsinterfaces.Channel{Alternatives: []wsinterfaces.Alternative{{Transcript: "Be Cool"}}},
						IsFinal:     true,
						SpeechFinal: true,
					})
					assert.NoError(t, err)

					// Close event
					err = cb.Close(&wsinterfaces.CloseResponse{})
					assert.NoError(t, err)

					return nil
				})
				m.EXPECT().Write(nil)
			},
			wantErr: false,
			want: []pkg.SpeechChunk{
				&pkg.SpeechControlChunk{Code: pkg.SpeechControlSpeechStarted},
				&pkg.SpeechTextChunk{Text: "Hello World."},
				&pkg.SpeechControlChunk{Code: pkg.SpeechControlSpeechEnded},
				&pkg.SpeechControlChunk{Code: pkg.SpeechControlSpeechStarted},
				&pkg.SpeechTextChunk{Text: "Be Cool"},
				&pkg.SpeechControlChunk{Code: pkg.SpeechControlSpeechEnded},
			},
		},
		{
			name: "connection error",
			fields: fields{
				ctx: context.Background(),
				config: deepgram2.Config{
					APIKey: "test-key",
				},
			},
			wantErr: true,
			setup: func(t *testing.T, m *mocks.MockDeepgramClient, callbackGetter func() wsinterfaces.LiveMessageCallback) {
				m.EXPECT().Connect().Return(false)
			},
		},
		{
			name: "stream error",
			fields: fields{
				ctx: context.Background(),
				config: deepgram2.Config{
					APIKey: "test-key",
				},
			},
			wantErr: false,
			setup: func(t *testing.T, m *mocks.MockDeepgramClient, callbackGetter func() wsinterfaces.LiveMessageCallback) {
				m.EXPECT().Connect().Return(true)
				m.EXPECT().Stream(gomock.Any()).DoAndReturn(func(_ io.Reader) error {
					cb := callbackGetter()
					err := cb.Close(&wsinterfaces.CloseResponse{})
					assert.NoError(t, err)

					return errors.New("stream error")
				})
				m.EXPECT().Write(nil)
			},
			want: nil,
		},
		{
			name: "empty transcriptions",
			fields: fields{
				ctx: context.Background(),
				config: deepgram2.Config{
					APIKey: "test-key",
				},
			},
			setup: func(t *testing.T, m *mocks.MockDeepgramClient, callbackGetter func() wsinterfaces.LiveMessageCallback) {
				m.EXPECT().Connect().Return(true)
				m.EXPECT().Stream(gomock.Any()).DoAndReturn(func(_ io.Reader) error {
					cb := callbackGetter()

					// SpeechStarted event
					err := cb.SpeechStarted(nil)
					assert.NoError(t, err)

					// Empty transcript
					err = cb.Message(&wsinterfaces.MessageResponse{
						Channel:     wsinterfaces.Channel{Alternatives: []wsinterfaces.Alternative{{Transcript: ""}}},
						IsFinal:     true,
						SpeechFinal: true,
					})
					assert.NoError(t, err)

					// Close event
					err = cb.Close(&wsinterfaces.CloseResponse{})
					assert.NoError(t, err)

					return nil
				})
				m.EXPECT().Write(nil)
			},
			wantErr: false,
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockClient := mocks.NewMockDeepgramClient(ctrl)
			var callback wsinterfaces.LiveMessageCallback

			tt.setup(t, mockClient, func() wsinterfaces.LiveMessageCallback { return callback })

			tt.fields.config.ClientFactory = func(ctx context.Context, apiKey string, cOpts *clientinterfaces.ClientOptions, tOpts *clientinterfaces.LiveTranscriptionOptions, cb wsinterfaces.LiveMessageCallback) (deepgram2.DeepgramClient, error) {
				callback = cb
				return mockClient, nil
			}

			// ensure MuxerFactory is set for tests
			if tt.fields.config.MuxerFactory == nil {
				tt.fields.config.MuxerFactory = muxer.NewLinear
			}
			a := deepgram2.NewWithConfig(tt.fields.config, zap.NewNop())
			got, err := a.Transcribe(tt.fields.ctx, src)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, got)

			var actual []pkg.SpeechChunk
			for r := range got {
				actual = append(actual, r)
			}

			assert.Equal(t, tt.want, actual)
		})
	}
}
