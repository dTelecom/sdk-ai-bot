package pkg

import (
	"context"
	"fmt"
	"strings"
)

type Pipeline struct {
	audioToText   SpeechToText
	textProcessor TextProcessor
	textToAudio   TextToSpeech
	questions     chan TextChunk
}

func NewPipeline(
	audioToText SpeechToText,
	textProcessor TextProcessor,
	textToAudio TextToSpeech,
) *Pipeline {
	return &Pipeline{
		audioToText:   audioToText,
		textProcessor: textProcessor,
		textToAudio:   textToAudio,
	}
}

func (b *Pipeline) Start(ctx context.Context) (<-chan AudioChunk, error) {
	b.questions = make(chan TextChunk)

	answerChunks, err := b.textProcessor.Process(ctx, b.questions)
	if err != nil {
		return nil, fmt.Errorf("failed to answer: %w", err)
	}

	chunks, err := b.textToAudio.Synthesize(ctx, answerChunks)
	if err != nil {
		return nil, fmt.Errorf("failed to synthesize: %w", err)
	}

	return chunks, nil
}

func (b *Pipeline) AddParticipant(ctx context.Context, name string, chunks <-chan AudioChunk) error {
	speechTokens, err := b.audioToText.Transcribe(ctx, chunks)
	if err != nil {
		return fmt.Errorf("failed to transcribe: %w", err)
	}

	// Запускаем горутину для обработки токенов речи
	go func() {
		var stringTokens []string
		for {
			select {
			case speechToken, ok := <-speechTokens:
				if !ok {
					// Канал закрыт, отправляем последний вопрос если есть
					if len(stringTokens) > 0 {
						question := strings.Join(stringTokens, " ")
						select {
						case b.questions <- &TextContentChunk{question, name}:
						case <-ctx.Done():
							return
						}
					}
					return
				}

				switch speechToken.Type() {
				case SpeechChunkTypeText:
					if token, ok := speechToken.(*SpeechTextChunk); ok {
						stringTokens = append(stringTokens, token.Text)
					}
				case SpeechChunkTypeControl:
					if controlToken, ok := speechToken.(*SpeechControlChunk); ok {
						switch controlToken.Code {
						case SpeechControlSpeechStarted:
							select {
							case b.questions <- &TextControlChunk{TextControlClear, name}:
							case <-ctx.Done():
								return
							}
						case SpeechControlSpeechEnded:
							if len(stringTokens) > 0 {
								question := strings.Join(stringTokens, " ")
								select {
								case b.questions <- &TextContentChunk{question, name}:
								case <-ctx.Done():
									return
								}
								stringTokens = []string{}
							}
						}
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return nil
}
