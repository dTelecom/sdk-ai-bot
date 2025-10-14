package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"reflect"
	"syscall"
	"time"
	"unsafe"

	"github.com/deepgram/deepgram-go-sdk/v3/pkg/audio/microphone"
	"github.com/deepgram/deepgram-go-sdk/v3/pkg/common"
	"github.com/hajimehoshi/oto/v2"
	"github.com/joho/godotenv"
	"go.uber.org/zap"

	"github.com/dTelecom/sdk-ai-bot/pkg"
	deepgramSpeechToText "github.com/dTelecom/sdk-ai-bot/pkg/speech_to_text/deepgram"
	"github.com/dTelecom/sdk-ai-bot/pkg/text_processor/chatgpt"
	deepgramTextToSpeech "github.com/dTelecom/sdk-ai-bot/pkg/text_to_speech/deepgram"
	"github.com/dTelecom/sdk-ai-bot/pkg/utils/chanbytereader"
)

const (
	sampleRate = 48000
	channels   = 1
)

func main() {
	err := godotenv.Load()
	if err != nil {
		fmt.Println(err.Error())

		return
	}

	deepgramApiKey := os.Getenv("DEEPGRAM_API_KEY")
	if deepgramApiKey == "" {
		fmt.Println("DEEPGRAM_API_KEY required")

		return
	}

	chatgptApiKey := os.Getenv("CHATGPT_API_KEY")
	if chatgptApiKey == "" {
		fmt.Println("CHATGPT_API_KEY required")

		return
	}

	common.Init(common.InitLib{
		LogLevel: common.LogLevelFull,
	})

	logger := zap.Must(zap.NewDevelopment())

	sttConfig := deepgramSpeechToText.DefaultConfig(deepgramApiKey)
	sttConfig.Channels = channels
	sttConfig.SampleRate = sampleRate
	sttConfig.Encoding = "linear16"
	speechToText := deepgramSpeechToText.NewWithConfig(sttConfig, logger)

	textProcessor := chatgpt.New(chatgptApiKey, logger)

	ttsConfig := deepgramTextToSpeech.DefaultConfig(deepgramApiKey)
	ttsConfig.Encoding = "linear16"
	textToSpeech := deepgramTextToSpeech.NewWithConfig(ttsConfig, logger)

	bot := pkg.NewPipeline(speechToText, textProcessor, textToSpeech)

	micChunks, teardownMic, err := getMicrophoneChunks()
	if err != nil {
		fmt.Println("failed to init mic: ", err.Error())

		return
	}
	defer teardownMic()

	botChunks, err := bot.Start(context.Background())
	if err != nil {
		fmt.Println("failed to start bot: ", err.Error())

		return
	}

	err = bot.AddParticipant(context.Background(), "developer", micChunks)
	if err != nil {
		fmt.Println("failed to add participant: ", err.Error())

		return
	}

	closeAudioPlayer, err := playAudio(botChunks)
	if err != nil {
		fmt.Println("failed to init audio player: ", err.Error())

		return
	}
	defer closeAudioPlayer()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
}

func getMicrophoneChunks() (<-chan pkg.AudioChunk, func(), error) {
	microphone.Initialize()

	mic, err := microphone.New(microphone.AudioConfig{
		InputChannels: channels,
		SamplingRate:  sampleRate,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create microphone: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	chunks := make(chan pkg.AudioChunk)
	go func() {
		for {
			samples, err := mic.Read()
			if err != nil {
				return
			}
			select {
			case chunks <- dstAsBytes(samples):
			case <-time.After(time.Second * time.Duration(len(samples)) / time.Duration(sampleRate)):
			case <-ctx.Done():
				return
			}
		}
	}()

	err = mic.Start()
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("failed tot start mic: %w", err)
	}

	teardown := func() {
		cancel()
		_ = mic.Stop()
		microphone.Teardown()
	}

	return chunks, teardown, nil
}

func playAudio(audioChunks <-chan pkg.AudioChunk) (func(), error) {
	otoCtx, ready, err := oto.NewContext(sampleRate, channels, oto.FormatSignedInt16LE)
	if err != nil {
		return nil, err
	}
	<-ready

	player := otoCtx.NewPlayer(chanbytereader.New(audioChunks))
	player.Play()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				fmt.Printf("UnplayedBufferSize: %d\n", player.UnplayedBufferSize())
			case <-ctx.Done():
				return
			}
		}
	}()

	playerClose := func() {
		cancel()
		_ = player.Close()
	}

	return playerClose, nil
}

func dstAsBytes(p []int16) []byte {
	hdr := (*reflect.SliceHeader)(unsafe.Pointer(&p))
	hdr.Len *= 2
	hdr.Cap *= 2
	return *(*[]byte)(unsafe.Pointer(hdr))
}
