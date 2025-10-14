package chatgpt_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/sashabaranov/go-openai"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"

	"github.com/dTelecom/sdk-ai-bot/pkg"
	chatgpt2 "github.com/dTelecom/sdk-ai-bot/pkg/text_processor/chatgpt"
	"github.com/dTelecom/sdk-ai-bot/pkg/text_processor/chatgpt/mocks"
)

const userName = "noname"

func TestProcessor_Answer(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	tests := []struct {
		name            string
		config          chatgpt2.Config
		setupMock       func(*mocks.MockAPIClient)
		questions       []string
		expectedError   bool
		expectedAnswers []string
	}{
		{
			name: "successful single question",
			config: chatgpt2.Config{
				APIKey:       "test-key",
				Model:        openai.GPT4o,
				MaxTurns:     20,
				SystemPrompt: "You are a helpful assistant.",
			},
			setupMock: func(mockClient *mocks.MockAPIClient) {
				expectedRequest := openai.ChatCompletionRequest{
					Model: openai.GPT4o,
					Messages: []openai.ChatCompletionMessage{
						{
							Role:    openai.ChatMessageRoleSystem,
							Content: "You are a helpful assistant.",
						},
						{
							Role:    openai.ChatMessageRoleSystem,
							Content: fmt.Sprintf("User '%s' talks", userName),
						},
						{
							Role:    openai.ChatMessageRoleUser,
							Content: "Hello",
						},
					},
				}

				mockClient.EXPECT().
					CreateChatCompletion(gomock.Any(), expectedRequest).
					Return(openai.ChatCompletionResponse{
						Choices: []openai.ChatCompletionChoice{
							{
								Message: openai.ChatCompletionMessage{
									Role:    openai.ChatMessageRoleAssistant,
									Content: "Hello! How can I help you?",
								},
							},
						},
					}, nil)
			},
			questions:       []string{"Hello"},
			expectedError:   false,
			expectedAnswers: []string{"Hello! How can I help you?\n"},
		},
		{
			name: "API error",
			config: chatgpt2.Config{
				APIKey:       "test-key",
				Model:        openai.GPT4o,
				MaxTurns:     20,
				SystemPrompt: "You are a helpful assistant.",
			},
			setupMock: func(mockClient *mocks.MockAPIClient) {
				mockClient.EXPECT().
					CreateChatCompletion(gomock.Any(), gomock.Any()).
					Return(openai.ChatCompletionResponse{}, errors.New("API error")).AnyTimes()
			},
			questions:       []string{"Hello"},
			expectedError:   false,      // ошибка обрабатывается внутри и процессор завершается
			expectedAnswers: []string{}, // при ошибке процессор не отправляет токен
		},
		{
			name: "empty response",
			config: chatgpt2.Config{
				APIKey:       "test-key",
				Model:        openai.GPT4o,
				MaxTurns:     20,
				SystemPrompt: "You are a helpful assistant.",
			},
			setupMock: func(mockClient *mocks.MockAPIClient) {
				mockClient.EXPECT().
					CreateChatCompletion(gomock.Any(), gomock.Any()).
					Return(openai.ChatCompletionResponse{
						Choices: []openai.ChatCompletionChoice{}, // пустой ответ
					}, nil).AnyTimes()
			},
			questions:       []string{"Hello"},
			expectedError:   false,
			expectedAnswers: []string{}, // при ошибке процессор не отправляет токен
		},
		{
			name: "multiple questions with history management",
			config: chatgpt2.Config{
				APIKey:       "test-key",
				Model:        openai.GPT4o,
				MaxTurns:     4, // ограничиваем историю
				SystemPrompt: "You are a helpful assistant.",
			},
			setupMock: func(mockClient *mocks.MockAPIClient) {
				// Первый вызов: system + Q1
				expectedRequest1 := openai.ChatCompletionRequest{
					Model: openai.GPT4o,
					Messages: []openai.ChatCompletionMessage{
						{
							Role:    openai.ChatMessageRoleSystem,
							Content: "You are a helpful assistant.",
						},
						{
							Role:    openai.ChatMessageRoleSystem,
							Content: fmt.Sprintf("User '%s' talks", userName),
						},
						{
							Role:    openai.ChatMessageRoleUser,
							Content: "Q1",
						},
					},
				}

				// Второй вызов: system + Q1 + Response A + Q2
				expectedRequest2 := openai.ChatCompletionRequest{
					Model: openai.GPT4o,
					Messages: []openai.ChatCompletionMessage{
						{
							Role:    openai.ChatMessageRoleSystem,
							Content: "You are a helpful assistant.",
						},
						{
							Role:    openai.ChatMessageRoleSystem,
							Content: fmt.Sprintf("User '%s' talks", userName),
						},
						{
							Role:    openai.ChatMessageRoleUser,
							Content: "Q1",
						},
						{
							Role:    openai.ChatMessageRoleAssistant,
							Content: "Response A",
						},
						{
							Role:    openai.ChatMessageRoleSystem,
							Content: fmt.Sprintf("User '%s' talks", userName),
						},
						{
							Role:    openai.ChatMessageRoleUser,
							Content: "Q2",
						},
					},
				}

				// Третий вызов: system + Q1 + Response A + Q2 + Response B + Q3
				expectedRequest3 := openai.ChatCompletionRequest{
					Model: openai.GPT4o,
					Messages: []openai.ChatCompletionMessage{
						{
							Role:    openai.ChatMessageRoleSystem,
							Content: "You are a helpful assistant.",
						},
						{
							Role:    openai.ChatMessageRoleSystem,
							Content: fmt.Sprintf("User '%s' talks", userName),
						},
						{
							Role:    openai.ChatMessageRoleUser,
							Content: "Q1",
						},
						{
							Role:    openai.ChatMessageRoleAssistant,
							Content: "Response A",
						},
						{
							Role:    openai.ChatMessageRoleSystem,
							Content: fmt.Sprintf("User '%s' talks", userName),
						},
						{
							Role:    openai.ChatMessageRoleUser,
							Content: "Q2",
						},
						{
							Role:    openai.ChatMessageRoleAssistant,
							Content: "Response B",
						},
						{
							Role:    openai.ChatMessageRoleSystem,
							Content: fmt.Sprintf("User '%s' talks", userName),
						},
						{
							Role:    openai.ChatMessageRoleUser,
							Content: "Q3",
						},
					},
				}

				// Ожидаем 3 вызова с конкретными запросами
				mockClient.EXPECT().
					CreateChatCompletion(gomock.Any(), expectedRequest1).
					Return(openai.ChatCompletionResponse{
						Choices: []openai.ChatCompletionChoice{
							{
								Message: openai.ChatCompletionMessage{
									Role:    openai.ChatMessageRoleAssistant,
									Content: "Response A",
								},
							},
						},
					}, nil)

				mockClient.EXPECT().
					CreateChatCompletion(gomock.Any(), expectedRequest2).
					Return(openai.ChatCompletionResponse{
						Choices: []openai.ChatCompletionChoice{
							{
								Message: openai.ChatCompletionMessage{
									Role:    openai.ChatMessageRoleAssistant,
									Content: "Response B",
								},
							},
						},
					}, nil)

				mockClient.EXPECT().
					CreateChatCompletion(gomock.Any(), expectedRequest3).
					Return(openai.ChatCompletionResponse{
						Choices: []openai.ChatCompletionChoice{
							{
								Message: openai.ChatCompletionMessage{
									Role:    openai.ChatMessageRoleAssistant,
									Content: "Response C",
								},
							},
						},
					}, nil)
			},
			questions:       []string{"Q1", "Q2", "Q3"},
			expectedError:   false,
			expectedAnswers: []string{"Response A\n", "Response B\n", "Response C\n"},
		},
		{
			name: "custom system prompt",
			config: chatgpt2.Config{
				APIKey:       "test-key",
				Model:        openai.GPT4o,
				MaxTurns:     20,
				SystemPrompt: "You are a specialized math tutor.",
			},
			setupMock: func(mockClient *mocks.MockAPIClient) {
				mockClient.EXPECT().
					CreateChatCompletion(gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
						// Проверяем, что первое сообщение - system с правильным промптом
						if len(req.Messages) == 0 {
							return openai.ChatCompletionResponse{}, errors.New("no messages")
						}
						if req.Messages[0].Role != openai.ChatMessageRoleSystem {
							return openai.ChatCompletionResponse{}, errors.New("first message is not system")
						}
						if req.Messages[0].Content != "You are a specialized math tutor." {
							return openai.ChatCompletionResponse{}, errors.New("wrong system prompt")
						}

						return openai.ChatCompletionResponse{
							Choices: []openai.ChatCompletionChoice{
								{
									Message: openai.ChatCompletionMessage{
										Role:    openai.ChatMessageRoleAssistant,
										Content: "I'm your math tutor!",
									},
								},
							},
						}, nil
					})
			},
			questions:       []string{"What is 2+2?"},
			expectedError:   false,
			expectedAnswers: []string{"I'm your math tutor!\n"},
		},
		{
			name: "empty API key",
			config: chatgpt2.Config{
				APIKey:       "", // пустой ключ
				Model:        openai.GPT4o,
				MaxTurns:     20,
				SystemPrompt: "You are a helpful assistant.",
			},
			setupMock: func(mockClient *mocks.MockAPIClient) {
				// Мок не должен вызываться
			},
			questions:       []string{"Hello"},
			expectedError:   true, // ожидаем ошибку при создании
			expectedAnswers: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			var processor *chatgpt2.Processor
			if tt.setupMock != nil {
				mockClient := mocks.NewMockAPIClient(ctrl)
				tt.setupMock(mockClient)
				config := tt.config
				config.NewClient = func(APIKey string) chatgpt2.APIClient {
					return mockClient
				}
				processor = chatgpt2.NewWithConfig(config, zap.Must(zap.NewDevelopment()))
			} else {
				processor = chatgpt2.NewWithConfig(tt.config, zap.Must(zap.NewDevelopment()))
			}

			// Создаем канал для вопросов
			questionCh := make(chan pkg.TextChunk, len(tt.questions))
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Запускаем Process
			answerCh, err := processor.Process(ctx, questionCh)

			if tt.expectedError {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, answerCh)

			// Отправляем вопросы
			for _, question := range tt.questions {
				questionCh <- pkg.NewTextContentChunk(question, userName)
			}
			close(questionCh) // закрываем канал после отправки всех вопросов

			// Получаем ответы
			answers := make([]string, 0, len(tt.expectedAnswers))
			for i := 0; i < len(tt.expectedAnswers); i++ {
				select {
				case answer := <-answerCh:
					if textToken, ok := answer.(*pkg.TextContentChunk); ok {
						answers = append(answers, textToken.Text)
					} else {
						t.Fatalf("Unexpected token type: %T", answer)
					}
				case <-ctx.Done():
					t.Fatal("Timeout waiting for answer")
				}
			}

			// Проверяем ответы
			assert.Equal(t, tt.expectedAnswers, answers)
		})
	}
}
