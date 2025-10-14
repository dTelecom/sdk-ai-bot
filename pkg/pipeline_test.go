package pkg_test

import (
	"context"
	"io"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	"github.com/dTelecom/sdk-ai-bot/pkg"
	"github.com/dTelecom/sdk-ai-bot/pkg/mocks"
	"github.com/dTelecom/sdk-ai-bot/pkg/utils/chanbytereader"
)

func TestBot(t *testing.T) {
	tests := []struct {
		name               string
		inputData          []byte
		userName           string
		setupMocks         func(*mocks.MockSpeechToText, *mocks.MockTextProcessor, *mocks.MockTextToSpeech)
		startErr           string
		addParticipantErr  string
		expectedOutputData []byte
	}{
		{
			name:               "happy",
			inputData:          []byte("input audio data"),
			userName:           "noname",
			expectedOutputData: []byte("output audio data"),
			setupMocks: func(stt *mocks.MockSpeechToText, tp *mocks.MockTextProcessor, tts *mocks.MockTextToSpeech) {
				stt.EXPECT().
					Transcribe(gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, r <-chan pkg.AudioChunk) (<-chan pkg.SpeechChunk, error) {
						speechTokens := make(chan pkg.SpeechChunk, 7)
						go func() {
							defer close(speechTokens)

							// collect all audio from channel and assert
							var got []byte
							for ch := range r {
								got = append(got, ch...)
							}
							assert.Equal(t, []byte("input audio data"), got)
							speechTokens <- &pkg.SpeechControlChunk{pkg.SpeechControlSpeechStarted}
							speechTokens <- &pkg.SpeechTextChunk{"Hello."}
							speechTokens <- &pkg.SpeechControlChunk{pkg.SpeechControlSpeechEnded}
							speechTokens <- &pkg.SpeechControlChunk{pkg.SpeechControlSpeechStarted}
							speechTokens <- &pkg.SpeechTextChunk{"Who"}
							speechTokens <- &pkg.SpeechTextChunk{"are"}
							speechTokens <- &pkg.SpeechTextChunk{"you?"}
						}()

						return speechTokens, nil
					})
				tp.EXPECT().
					Process(gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, questions <-chan pkg.TextChunk) (<-chan pkg.TextChunk, error) {
						answers := make(chan pkg.TextChunk, 1)
						go func() {
							defer close(answers)

							assert.Equal(t, &pkg.TextControlChunk{pkg.TextControlClear, "noname"}, <-questions)
							assert.Equal(t, &pkg.TextContentChunk{"Hello.", "noname"}, <-questions)
							assert.Equal(t, &pkg.TextControlChunk{pkg.TextControlClear, "noname"}, <-questions)
							assert.Equal(t, &pkg.TextContentChunk{"Who are you?", "noname"}, <-questions)
							answers <- &pkg.TextContentChunk{"Hello. I am a helpful assistant.", ""}
						}()

						return answers, nil
					})
				tts.EXPECT().
					Synthesize(gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, text <-chan pkg.TextChunk) (<-chan pkg.AudioChunk, error) {
						out := make(chan pkg.AudioChunk, 2)
						go func() {
							defer close(out)
							assert.Equal(t, &pkg.TextContentChunk{"Hello. I am a helpful assistant.", ""}, <-text)
							// no more text
							_, ok := <-text
							assert.False(t, ok)
							out <- []byte("output audio data")
						}()
						return out, nil
					})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockSTT := mocks.NewMockSpeechToText(ctrl)
			mockTP := mocks.NewMockTextProcessor(ctrl)
			mockTTS := mocks.NewMockTextToSpeech(ctrl)

			tt.setupMocks(mockSTT, mockTP, mockTTS)

			bot := pkg.NewPipeline(mockSTT, mockTP, mockTTS)

			result, err := bot.Start(context.Background())

			if tt.startErr != "" {
				assert.Contains(t, err.Error(), tt.startErr)
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, result)

			// prepare audio chunks channel to feed into AddParticipant
			audioCh := make(chan pkg.AudioChunk, 1)
			audioCh <- tt.inputData
			close(audioCh)
			err = bot.AddParticipant(context.Background(), tt.userName, audioCh)

			if tt.addParticipantErr != "" {
				assert.Contains(t, err.Error(), tt.addParticipantErr)
				return
			}

			assert.NoError(t, err)

			// convert channel to io.Reader for convenience
			output, err := io.ReadAll(chanbytereader.New(result))
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedOutputData, output)
		})
	}
}
