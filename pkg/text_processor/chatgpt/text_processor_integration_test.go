//go:build integration

package chatgpt_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/dTelecom/sdk-ai-bot/pkg"
	"github.com/dTelecom/sdk-ai-bot/pkg/text_processor/chatgpt"
)

func Test_Answer(t *testing.T) {
	err := godotenv.Load("../../../../.env")
	require.NoError(t, err)

	config := chatgpt.DefaultConfig(os.Getenv("CHATGPT_API_KEY"))
	config.SystemPrompt = "Answer as short as possible"

	processor := chatgpt.NewWithConfig(config, zap.Must(zap.NewDevelopment()))

	questionCh := make(chan pkg.TextChunk, 3)
	questionCh <- pkg.NewTextContentChunk("What is the capital of Germany?", "noname")
	questionCh <- pkg.NewTextContentChunk("Define Newton’s second law.", "noname")
	questionCh <- pkg.NewTextContentChunk("Who wrote ‘1984’?", "noname")
	close(questionCh)

	answerCh, err := processor.Process(context.Background(), questionCh)
	require.NoError(t, err)

	var answers []string
	for answer := range answerCh {
		require.Equal(t, pkg.TextChunkTypeText, answer.Type())
		stringToken, ok := answer.(*pkg.TextContentChunk)
		require.True(t, ok)
		answers = append(answers, stringToken.Text)
	}

	require.Len(t, answers, 3)
	require.Contains(t, answers[0], "Berlin")
	require.Contains(t, strings.ReplaceAll(answers[1], " ", ""), "F=ma")
	require.Contains(t, answers[2], "George Orwell")
}
