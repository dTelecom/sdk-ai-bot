package chatgpt

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sashabaranov/go-openai"
	"go.uber.org/zap"

	"github.com/dTelecom/sdk-ai-bot/pkg"
)

const logPrefix = "tp.chatgpt "

// Config конфигурация для ChatGPTProcessor
type Config struct {
	APIKey       string
	Model        string
	MaxTurns     int
	SystemPrompt string

	NewClient func(APIKey string) APIClient
}

// DefaultConfig возвращает конфигурацию по умолчанию
func DefaultConfig(APIKey string) Config {
	return Config{
		APIKey:       APIKey,
		Model:        openai.GPT4Turbo,
		MaxTurns:     20,
		SystemPrompt: "You are a helpful assistant.",

		NewClient: func(APIKey string) APIClient {
			return openai.NewClient(APIKey)
		},
	}
}

type Processor struct {
	config  Config
	client  APIClient
	history []openai.ChatCompletionMessage
	log     *zap.Logger
}

// New создаёт процессор с дефолтами
func New(apiKey string, log *zap.Logger) *Processor {
	config := DefaultConfig(apiKey)
	return NewWithConfig(config, log)
}

// NewWithConfig создаёт процессор с указанной конфигурацией
func NewWithConfig(config Config, log *zap.Logger) *Processor {
	return &Processor{
		config: config,
		client: config.NewClient(config.APIKey),
		log:    log,
	}
}

func (p *Processor) Process(
	ctx context.Context,
	question <-chan pkg.TextChunk,
) (<-chan pkg.TextChunk, error) {
	if p.config.APIKey == "" {
		return nil, errors.New("API key is empty")
	}

	out := make(chan pkg.TextChunk)

	go func() {
		defer close(out)

		for {
			select {
			case <-ctx.Done():
				return
			case q, ok := <-question:
				if !ok {
					return
				}

				switch q.Type() {
				case pkg.TextChunkTypeText:
					if stringToken, ok := q.(*pkg.TextContentChunk); ok {
						askStart := time.Now()
						ans, err := p.askWithContext(ctx, stringToken.Text, stringToken.Source)
						if err != nil {
							p.log.Error(logPrefix+"Answer", zap.Error(err), zap.Duration("Duration", time.Now().Sub(askStart)))
							return
						}

						p.log.Debug(logPrefix+"Answer", zap.String("Val", ans), zap.Duration("Duration", time.Now().Sub(askStart)))

						select {
						case <-ctx.Done():
							return
						case out <- pkg.NewTextContentChunk(ans, ""):
						}
					}
				case pkg.TextChunkTypeControl:
					if controlToken, ok := q.(*pkg.TextControlChunk); ok && controlToken.Code == pkg.TextControlClear {
						select {
						case <-ctx.Done():
							return
						case out <- q:
						}
					}
				}

			}
		}
	}()

	return out, nil
}

func (p *Processor) askWithContext(ctx context.Context, question, src string) (string, error) {
	// 1. формируем последовательность: system + history + новый вопрос
	msgs := make([]openai.ChatCompletionMessage, 0, 2+len(p.history))
	msgs = append(msgs, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleSystem, Content: p.config.SystemPrompt})
	msgs = append(msgs, p.history...)
	msgs = append(msgs, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleSystem, Content: fmt.Sprintf("User '%s' talks", src)})
	msgs = append(msgs, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: question})

	resp, err := p.client.CreateChatCompletion(ctx,
		openai.ChatCompletionRequest{
			Model:    p.config.Model,
			Messages: msgs,
		},
	)
	if err != nil {
		return "", fmt.Errorf("create chat completion err: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", errors.New("len(resp.Choices) == 0")
	}

	// 2. сохраняем «вопрос-ответ» в history
	p.history = append(p.history,
		openai.ChatCompletionMessage{Role: openai.ChatMessageRoleSystem, Content: fmt.Sprintf("User '%s' talks", src)},
		openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: question},
		resp.Choices[0].Message,
	)
	// 3. если превысили лимит — срезаем самые старые реплики
	if p.config.MaxTurns > 0 && len(p.history) > p.config.MaxTurns*2 {
		// оставляем последние MaxTurns*2 сообщений
		p.history = p.history[len(p.history)-p.config.MaxTurns*2:]
	}

	return resp.Choices[0].Message.Content + "\n", nil
}
