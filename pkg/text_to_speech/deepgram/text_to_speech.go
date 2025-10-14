package deepgram

import (
	"context"
	"errors"
	"fmt"

	interfacesv1 "github.com/deepgram/deepgram-go-sdk/v3/pkg/api/speak/v1/websocket/interfaces"
	clientinterfaces "github.com/deepgram/deepgram-go-sdk/v3/pkg/client/interfaces"
	"github.com/deepgram/deepgram-go-sdk/v3/pkg/client/speak"
	"go.uber.org/zap"

	"github.com/dTelecom/sdk-ai-bot/pkg"
	"github.com/dTelecom/sdk-ai-bot/pkg/text_to_speech"
	"github.com/dTelecom/sdk-ai-bot/pkg/text_to_speech/transcoder"
)

// Config содержит конфигурацию для TextToSpeech
type Config struct {
	APIKey     string
	Model      string
	Encoding   string
	SampleRate int

	ClientFactory func(ctx context.Context,
		APIKey string,
		cOptions *clientinterfaces.ClientOptions,
		sOptions *clientinterfaces.WSSpeakOptions,
		cb interfacesv1.SpeakMessageCallback,
	) (Client, error)

	TranscoderFactory text_to_speech.CreateTranscoderFn
}

type TextToSpeech struct {
	config Config
	log    *zap.Logger
}

func DefaultConfig(APIKey string) Config {
	return Config{
		APIKey:     APIKey,
		Model:      "aura-asteria-en",
		Encoding:   "linear16",
		SampleRate: 48000,

		ClientFactory: func(
			ctx context.Context,
			APIKey string,
			cOptions *clientinterfaces.ClientOptions,
			sOptions *clientinterfaces.WSSpeakOptions,
			cb interfacesv1.SpeakMessageCallback,
		) (Client, error) {
			return speak.NewWSUsingCallback(ctx, APIKey, cOptions, sOptions, cb)
		},

		TranscoderFactory: transcoder.NewPassThrough,
	}
}

func New(APIKey string, log *zap.Logger) *TextToSpeech {
	return NewWithConfig(DefaultConfig(APIKey), log)
}

func NewWithConfig(config Config, log *zap.Logger) *TextToSpeech {
	return &TextToSpeech{
		config: config,
		log:    log,
	}
}

func (t *TextToSpeech) Config() Config {
	return t.config
}

func (t *TextToSpeech) Synthesize(ctx context.Context, text <-chan pkg.TextChunk) (<-chan pkg.AudioChunk, error) {
	if t.config.APIKey == "" {
		return nil, errors.New("API key is empty")
	}

	cOptions := &clientinterfaces.ClientOptions{}
	sOptions := &clientinterfaces.WSSpeakOptions{
		Model:      t.config.Model,
		Encoding:   t.config.Encoding,
		SampleRate: t.config.SampleRate,
	}

	cb, callbackController := newTTSCallback(t.log)

	ws, err := t.config.ClientFactory(ctx, t.config.APIKey, cOptions, sOptions, cb)
	if err != nil {
		return nil, fmt.Errorf("failed to create ws client: %w", err)
	}
	if !ws.Connect() {
		return nil, errors.New("failed to connect to Deepgram TTS WS")
	}

	transc := t.config.TranscoderFactory(cb.audioCh)

	go func() {
		defer func() {
			callbackController.OnFlush(func() {
				ws.Stop()
			})
			_ = ws.Flush()
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case chunk, ok := <-text:
				if !ok {
					return
				}
				switch chunk.Type() {
				case pkg.TextChunkTypeText:
					if stringToken, ok := chunk.(*pkg.TextContentChunk); ok {
						if err := ws.SpeakWithText(stringToken.Text); err != nil {
							_ = ws.ProcessError(fmt.Errorf("failed to send text: %w", err))
							return
						}
					}
				case pkg.TextChunkTypeControl:
					if controlToken, ok := chunk.(*pkg.TextControlChunk); ok && controlToken.Code == pkg.TextControlClear {
						callbackController.Clear()
						transc.Clear()
						if err := ws.Clear(); err != nil {
							_ = ws.ProcessError(fmt.Errorf("failed to clear: %w", err))
							return
						}
						t.log.Debug("Cleared")
					}
				}
			}
		}
	}()

	return transc.Chunks(), nil
}
