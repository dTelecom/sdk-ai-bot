package deepgram

import (
	"errors"
	"sync/atomic"

	wsinterfaces "github.com/deepgram/deepgram-go-sdk/v3/pkg/api/speak/v1/websocket/interfaces"
	"go.uber.org/zap"

	"github.com/dTelecom/sdk-ai-bot/pkg"
)

const logPrefix = "tts.deepgram "

var fullChannelErr = errors.New("token channel full")

type ttsCallback struct {
	audioCh chan pkg.AudioChunk
	muted   atomic.Bool
	onFlush atomic.Pointer[func()]
	log     *zap.Logger
}

type ttsCallbackController struct {
	cb *ttsCallback
}

func (c *ttsCallbackController) Clear() {
	c.cb.muted.Store(true)
}

func (c *ttsCallbackController) OnFlush(callback func()) {
	c.cb.onFlush.Store(&callback)
}

func newTTSCallback(log *zap.Logger) (*ttsCallback, *ttsCallbackController) {
	cb := &ttsCallback{
		audioCh: make(chan pkg.AudioChunk, 1024),
		log:     log,
	}
	return cb, &ttsCallbackController{cb}
}

func (c *ttsCallback) Binary(data []byte) error {
	if c.muted.Load() || len(data) == 0 {
		return nil
	}

	select {
	case c.audioCh <- data:
		return nil
	default:
		return fullChannelErr
	}
}
func (c *ttsCallback) Open(*wsinterfaces.OpenResponse) error         { return nil }
func (c *ttsCallback) Metadata(*wsinterfaces.MetadataResponse) error { return nil }
func (c *ttsCallback) Flush(*wsinterfaces.FlushedResponse) error {
	c.log.Debug(logPrefix + "Flush")
	if fn := c.onFlush.Load(); fn != nil {
		(*fn)()
	}

	return nil
}
func (c *ttsCallback) Clear(*wsinterfaces.ClearedResponse) error {
	select {
	case c.audioCh <- nil:
		c.muted.Store(false)
		return nil
	default:
		return fullChannelErr
	}
}
func (c *ttsCallback) Close(*wsinterfaces.CloseResponse) error {
	c.log.Debug(logPrefix + "Close")
	close(c.audioCh)

	return nil
}
func (c *ttsCallback) Warning(r *wsinterfaces.WarningResponse) error {
	c.log.Debug(logPrefix+"Warning", zap.String("WarnMsg", r.WarnMsg), zap.String("Description", r.Description))

	return nil
}
func (c *ttsCallback) Error(r *wsinterfaces.ErrorResponse) error {
	c.log.Debug(logPrefix+"Error", zap.String("ErrMsg", r.ErrMsg), zap.String("Description", r.Description))

	return nil
}
func (c *ttsCallback) UnhandledEvent([]byte) error {
	c.log.Debug(logPrefix + "UnhandledEvent")

	return nil
}
