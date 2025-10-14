package chatgpt

import (
	"context"

	"github.com/sashabaranov/go-openai"
)

//go:generate ../../../bin/mockgen -source $GOFILE -destination=mocks/mocks.go -package mocks

// APIClient интерфейс для работы с ChatGPT API
type APIClient interface {
	CreateChatCompletion(ctx context.Context, request openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error)
}
