package pkg

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

type Pipeline struct {
	audioToText   SpeechToText
	textProcessor TextProcessor
	textToAudio   TextToSpeech
	questions     chan TextChunk
	mu            sync.Mutex
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

func (p *Pipeline) Start(ctx context.Context) (<-chan AudioChunk, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.questions != nil {
		return nil, errors.New("pipeline has been started")
	}

	p.questions = make(chan TextChunk)

	answerChunks, err := p.textProcessor.Process(ctx, p.questions)
	if err != nil {
		return nil, fmt.Errorf("failed to answer: %w", err)
	}

	chunks, err := p.textToAudio.Synthesize(ctx, answerChunks)
	if err != nil {
		return nil, fmt.Errorf("failed to synthesize: %w", err)
	}

	return chunks, nil
}

func (p *Pipeline) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.questions == nil {
		return errors.New("pipeline has not been started")
	}

	close(p.questions)

	p.questions = nil

	return nil
}

func (p *Pipeline) AddParticipant(ctx context.Context, name string, chunks <-chan AudioChunk) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.questions == nil {
		return errors.New("pipeline has not been started")
	}

	speechTokens, err := p.audioToText.Transcribe(ctx, chunks)
	if err != nil {
		return fmt.Errorf("failed to transcribe: %w", err)
	}

	// Запускаем горутину для обработки токенов речи
	questions := p.questions
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
						case questions <- &TextContentChunk{question, name}:
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
							case questions <- &TextControlChunk{TextControlClear, name}:
							case <-ctx.Done():
								return
							}
						case SpeechControlSpeechEnded:
							if len(stringTokens) > 0 {
								question := strings.Join(stringTokens, " ")
								select {
								case questions <- &TextContentChunk{question, name}:
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
