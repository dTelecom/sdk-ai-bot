package deepgram

import (
	"errors"
	"strings"
	"sync/atomic"

	interfaces "github.com/deepgram/deepgram-go-sdk/v3/pkg/api/listen/v1/websocket/interfaces"
	"go.uber.org/zap"

	"github.com/dTelecom/sdk-ai-bot/pkg"
)

const logPrefix = "stt.deepgram "

var fullChannelErr = errors.New("token channel full")

type callback struct {
	tokenCh       chan pkg.SpeechChunk
	speechStarted atomic.Bool
	sb            strings.Builder
	log           *zap.Logger
}

func newCallback(log *zap.Logger) *callback {
	return &callback{
		tokenCh: make(chan pkg.SpeechChunk, 1024),
		log:     log,
	}
}

func (c *callback) Open(*interfaces.OpenResponse) error {
	c.log.Debug(logPrefix + "Open")

	return nil
}

func (c *callback) Message(mr *interfaces.MessageResponse) error {
	c.log.Debug(logPrefix + "Message")

	var sentence string
	if len(mr.Channel.Alternatives) > 0 {
		sentence = strings.TrimSpace(mr.Channel.Alternatives[0].Transcript)
	}

	if !c.speechStarted.Load() && len(sentence) > 0 {
		select {
		case c.tokenCh <- &pkg.SpeechControlChunk{Code: pkg.SpeechControlSpeechStarted}:
			c.log.Debug(logPrefix + "SpeechControlChunk(SpeechStarted)")
			c.speechStarted.Store(true)
		default:
			return fullChannelErr
		}
	}

	if mr.IsFinal && len(sentence) > 0 {
		if c.sb.Len() > 0 {
			c.sb.WriteString(" ")
		}
		c.sb.WriteString(sentence)
	}

	if mr.SpeechFinal {
		if err := c.endSpeech(); err != nil {
			return err
		}
	}

	return nil
}

func (c *callback) Metadata(metadata *interfaces.MetadataResponse) error {
	c.log.Debug(logPrefix + "Metadata")

	return nil
}

func (c *callback) SpeechStarted(*interfaces.SpeechStartedResponse) error {
	c.log.Debug(logPrefix + "SpeechStarted")

	return nil
}

func (c *callback) UtteranceEnd(*interfaces.UtteranceEndResponse) error {
	c.log.Debug(logPrefix + "UtteranceEnd")

	return c.endSpeech()
}

func (c *callback) Close(*interfaces.CloseResponse) error {
	c.log.Debug(logPrefix + "Close")

	defer close(c.tokenCh)

	return c.endSpeech()
}

func (c *callback) Error(r *interfaces.ErrorResponse) error {
	c.log.Debug(logPrefix+"Error", zap.String("ErrMsg", r.ErrMsg), zap.String("Description", r.Description))

	return nil
}

func (c *callback) UnhandledEvent([]byte) error {
	c.log.Debug(logPrefix + "UnhandledEvent")

	return nil
}

func (c *callback) endSpeech() error {
	if c.sb.Len() > 0 {
		select {
		case c.tokenCh <- &pkg.SpeechTextChunk{Text: c.sb.String()}:
			c.log.Debug(logPrefix+"SpeechTextChunk", zap.String("Text", c.sb.String()))
			c.sb.Reset()
		default:
			return fullChannelErr
		}
	}

	if c.speechStarted.Load() {
		select {
		case c.tokenCh <- &pkg.SpeechControlChunk{Code: pkg.SpeechControlSpeechEnded}:
			c.log.Debug(logPrefix + "SpeechControlChunk(SpeechEnded)")
			c.speechStarted.Store(false)
		default:
			return fullChannelErr
		}
	}

	return nil
}
