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

	disconnectedCh := make(chan interface{})
	callback := agent.NewCallback()
	callback.OnDisconnected = func() {
		logger.Info("agent disconnected")
		close(disconnectedCh)
	}

	disconnect, err := a.Connect(url, roomToken, callback)
	if err != nil {
		logger.Fatal("failed to connect", zap.Error(err))
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
