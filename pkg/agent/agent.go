package agent

import (
	"errors"
	"fmt"
	"os"
	"time"

	lksdk "github.com/dtelecom/server-sdk-go"
	"go.uber.org/zap"

	"github.com/dTelecom/sdk-ai-bot/pkg"
	"github.com/dTelecom/sdk-ai-bot/pkg/speech_to_text"
	deepgramSpeechToText "github.com/dTelecom/sdk-ai-bot/pkg/speech_to_text/deepgram"
	"github.com/dTelecom/sdk-ai-bot/pkg/speech_to_text/muxer"
	"github.com/dTelecom/sdk-ai-bot/pkg/text_processor/chatgpt"
	deepgramTextToSpeech "github.com/dTelecom/sdk-ai-bot/pkg/text_to_speech/deepgram"
	"github.com/dTelecom/sdk-ai-bot/pkg/text_to_speech/transcoder"
)

const (
	sampleRate    = 48000
	frameDuration = 20 * time.Millisecond
	channels      = 1
)

type config struct {
	speechToText  pkg.SpeechToText
	textProcessor pkg.TextProcessor
	textToSpeech  pkg.TextToSpeech
}

type Agent struct {
	logger   *zap.Logger
	room     *lksdk.Room
	pipeline *pkg.Pipeline
}

func New(logger *zap.Logger, options ...Option) (*Agent, error) {
	var cfg config
	for _, o := range options {
		o(&cfg)
	}

	var err error

	if cfg.speechToText == nil {
		cfg.speechToText, err = buildDeepgramSTT(logger)
		if err != nil {
			return nil, fmt.Errorf("failed to build default deepgram stt: %w", err)
		}
	}

	if cfg.textProcessor == nil {
		cfg.textProcessor, err = buildChatGPTTextProcessor(logger)
		if err != nil {
			return nil, fmt.Errorf("failed to build default chatGPT text processor: %w", err)
		}
	}

	if cfg.textToSpeech == nil {
		cfg.textToSpeech, err = buildDeepgramTTS(logger)
		if err != nil {
			return nil, fmt.Errorf("failed to build default deepgram tts: %w", err)
		}
	}

	return &Agent{
		logger:   logger,
		pipeline: pkg.NewPipeline(cfg.speechToText, cfg.textProcessor, cfg.textToSpeech),
	}, nil
}

func buildDeepgramSTT(logger *zap.Logger) (pkg.SpeechToText, error) {
	deepgramApiKey := os.Getenv("DEEPGRAM_API_KEY")
	if deepgramApiKey == "" {
		return nil, errors.New("DEEPGRAM_API_KEY required")
	}

	// opus will be detected automatically
	sttConfig := deepgramSpeechToText.DefaultConfig(deepgramApiKey)
	sttConfig.Channels = 0
	sttConfig.SampleRate = 0
	sttConfig.Encoding = ""
	sttConfig.MuxerFactory = func(chunks <-chan pkg.AudioChunk) speech_to_text.Muxer {
		return muxer.NewOgg(chunks, uint32(frameDuration/time.Millisecond), sampleRate, channels)
	}

	return deepgramSpeechToText.NewWithConfig(sttConfig, logger), nil
}

func buildChatGPTTextProcessor(logger *zap.Logger) (pkg.TextProcessor, error) {
	chatgptApiKey := os.Getenv("CHATGPT_API_KEY")
	if chatgptApiKey == "" {
		return nil, errors.New("CHATGPT_API_KEY required")
	}

	return chatgpt.New(chatgptApiKey, logger), nil
}

func buildDeepgramTTS(logger *zap.Logger) (pkg.TextToSpeech, error) {
	deepgramApiKey := os.Getenv("DEEPGRAM_API_KEY")
	if deepgramApiKey == "" {
		return nil, errors.New("DEEPGRAM_API_KEY required")
	}

	opusTranscoder, err := transcoder.NewLinear16ToOpusBuilder(
		sampleRate,
		channels,
		&transcoder.Linear16ToOpusOptions{FrameDuration: frameDuration},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create opus transcoder builder: %w", err)
	}

	ttsConfig := deepgramTextToSpeech.DefaultConfig(deepgramApiKey)
	ttsConfig.Encoding = "linear16"
	ttsConfig.SampleRate = sampleRate
	ttsConfig.TranscoderFactory = opusTranscoder.BuildAndRunTranscoder

	return deepgramTextToSpeech.NewWithConfig(ttsConfig, logger), nil
}
