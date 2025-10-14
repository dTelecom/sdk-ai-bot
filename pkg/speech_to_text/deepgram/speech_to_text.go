package deepgram

import (
	"context"
	"errors"
	"fmt"
	"io"

	wsinterfaces "github.com/deepgram/deepgram-go-sdk/v3/pkg/api/listen/v1/websocket/interfaces"
	clientinterfaces "github.com/deepgram/deepgram-go-sdk/v3/pkg/client/interfaces"
	client "github.com/deepgram/deepgram-go-sdk/v3/pkg/client/listen"
	"go.uber.org/zap"

	"github.com/dTelecom/sdk-ai-bot/pkg"
	"github.com/dTelecom/sdk-ai-bot/pkg/speech_to_text"
	"github.com/dTelecom/sdk-ai-bot/pkg/speech_to_text/muxer"
)

// Config содержит конфигурацию для SpeechToText
type Config struct {
	APIKey          string
	Model           string
	Language        string
	SampleRate      int
	Channels        int
	Encoding        string
	EnableKeepAlive bool
	UtteranceEndMs  string

	ClientFactory func(context.Context, string, *clientinterfaces.ClientOptions, *clientinterfaces.LiveTranscriptionOptions, wsinterfaces.LiveMessageCallback) (DeepgramClient, error)

	MuxerFactory speech_to_text.CreateMuxerFn
}

// DefaultConfig возвращает конфигурацию по умолчанию
func DefaultConfig(APIKey string) Config {
	return Config{
		APIKey:          APIKey,
		Model:           "nova-3",
		Language:        "en-US",
		SampleRate:      16000,
		Channels:        1,
		EnableKeepAlive: true,
		UtteranceEndMs:  "1000",
		Encoding:        "opus",
		ClientFactory: func(
			ctx context.Context,
			apiKey string,
			cOptions *clientinterfaces.ClientOptions,
			tOptions *clientinterfaces.LiveTranscriptionOptions,
			callback wsinterfaces.LiveMessageCallback,
		) (DeepgramClient, error) {
			return client.NewWSUsingCallback(ctx, apiKey, cOptions, tOptions, callback)
		},
		MuxerFactory: muxer.NewLinear,
	}
}

type SpeechToText struct {
	config            Config
	clientOptions     *clientinterfaces.ClientOptions
	transcriptOptions *clientinterfaces.LiveTranscriptionOptions
	log               *zap.Logger
}

// New создает новый экземпляр SpeechToText с конфигурацией по умолчанию
func New(APIKey string, log *zap.Logger) *SpeechToText {
	return NewWithConfig(DefaultConfig(APIKey), log)
}

// NewWithConfig создает новый экземпляр SpeechToText с указанной конфигурацией
func NewWithConfig(config Config, log *zap.Logger) *SpeechToText {
	cOptions := &clientinterfaces.ClientOptions{
		EnableKeepAlive: config.EnableKeepAlive,
	}

	tOptions := &clientinterfaces.LiveTranscriptionOptions{
		Model:          config.Model,
		Keyterm:        []string{"deepgram"},
		Language:       config.Language,
		Punctuate:      true,
		Encoding:       config.Encoding,
		Channels:       config.Channels,
		SampleRate:     config.SampleRate,
		SmartFormat:    true,
		VadEvents:      true,
		InterimResults: true,
		UtteranceEndMs: config.UtteranceEndMs,
	}

	return &SpeechToText{
		config:            config,
		clientOptions:     cOptions,
		transcriptOptions: tOptions,
		log:               log,
	}
}

func (a *SpeechToText) Transcribe(ctx context.Context, r <-chan pkg.AudioChunk) (<-chan pkg.SpeechChunk, error) {
	cb := newCallback(a.log)

	dgClient, err := a.config.ClientFactory(ctx, a.config.APIKey, a.clientOptions, a.transcriptOptions, cb)
	if err != nil {
		return nil, fmt.Errorf("ERROR creating LiveTranscription connection: %w", err)
	}

	bConnected := dgClient.Connect()
	if !bConnected {
		return nil, errors.New("failed to connect")
	}

	go func() {
		defer dgClient.Write(nil)

		err := dgClient.Stream(a.config.MuxerFactory(r))
		if err != nil && !errors.Is(err, io.EOF) {
			a.log.Error("Stream ended", zap.Error(err))
		}
	}()

	return cb.tokenCh, nil
}
