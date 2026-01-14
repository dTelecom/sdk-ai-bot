package main

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	deepgram "github.com/deepgram/deepgram-go-sdk/v3/pkg/common"
	"github.com/joho/godotenv"
	"go.uber.org/zap"

	"github.com/dTelecom/sdk-ai-bot/pkg"
	"github.com/dTelecom/sdk-ai-bot/pkg/agent"
	"github.com/dTelecom/sdk-ai-bot/pkg/text_processor/chatgpt"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		fmt.Println(err.Error())

		return
	}

	deepgram.Init(deepgram.InitLib{
		LogLevel: deepgram.LogLevelFull,
	})

	logger := zap.Must(zap.NewDevelopment())

	textProcessor, err := buildTextProcessor(logger)
	if err != nil {
		logger.Fatal("failed to create text processor", zap.Error(err))
	}

	a, err := agent.New(logger, agent.WithTextProcessor(textProcessor))
	if err != nil {
		logger.Fatal("failed to create agent", zap.Error(err))
	}

	roomToken := os.Getenv("ROOM_TOKEN")
	if roomToken == "" {
		logger.Fatal("ROOM_TOKEN required")
	}

	url := os.Getenv("DTELECOM_URL")
	if url == "" {
		logger.Fatal("DTELECOM_URL required")
	}

	disconnectedCh := make(chan interface{})
	callback := agent.NewCallback()
	callback.OnDisconnected = func() {
		logger.Info("agent disconnected")
		close(disconnectedCh)
	}

	disconnect, err := a.Connect(url, roomToken, callback)
	if err != nil {
		logger.Error("failed to connect", zap.Error(err))
	}

	sigs := make(chan os.Signal, 1)
	defer close(sigs)

	go func() {
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
		sig, ok := <-sigs
		logger.Info("received OS signal", zap.String("name", sig.String()))
		if ok {
			disconnect()
		}
	}()

	<-disconnectedCh
}

func buildTextProcessor(logger *zap.Logger) (pkg.TextProcessor, error) {
	chatgptApiKey := os.Getenv("CHATGPT_API_KEY")
	if chatgptApiKey == "" {
		return nil, errors.New("CHATGPT_API_KEY required")
	}

	config := chatgpt.DefaultConfig(chatgptApiKey)
	config.SystemPrompt = "You are a cheerful and playful assistant. You always try to answer with a sense of humor."

	return chatgpt.NewWithConfig(config, logger), nil
}
