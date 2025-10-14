package pkg

//go:generate ../bin/mockgen -source $GOFILE -destination=mocks/mocks.go -package mocks

import (
	"context"
)

type SpeechToText interface {
	Transcribe(ctx context.Context, r <-chan AudioChunk) (<-chan SpeechChunk, error)
}

type TextProcessor interface {
	Process(ctx context.Context, question <-chan TextChunk) (<-chan TextChunk, error)
}

type TextToSpeech interface {
	Synthesize(ctx context.Context, text <-chan TextChunk) (<-chan AudioChunk, error)
}
