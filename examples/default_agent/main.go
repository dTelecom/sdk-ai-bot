package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	deepgram "github.com/deepgram/deepgram-go-sdk/v3/pkg/common"
	"github.com/joho/godotenv"
	"go.uber.org/zap"

	"github.com/dTelecom/sdk-ai-bot/pkg/agent"
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

	a, err := agent.New(logger)
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

	err = a.Connect(url, roomToken)
	if err != nil {
		logger.Error("failed to connect", zap.Error(err))
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
}
