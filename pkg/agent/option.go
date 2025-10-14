package agent

import (
	"github.com/dTelecom/sdk-ai-bot/pkg"
)

type Option func(*config)

func WithSTT(stt pkg.SpeechToText) Option {
	return func(c *config) {
		c.speechToText = stt
	}
}

func WithTextProcessor(textProcessor pkg.TextProcessor) Option {
	return func(c *config) {
		c.textProcessor = textProcessor
	}
}

func WithTTS(tts pkg.TextToSpeech) Option {
	return func(c *config) {
		c.textToSpeech = tts
	}
}
