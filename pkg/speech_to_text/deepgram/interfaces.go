package deepgram

import (
	"io"
)

//go:generate ../../../bin/mockgen -source $GOFILE -destination=mocks/mocks.go -package mocks

type DeepgramClient interface {
	Connect() bool
	Stream(reader io.Reader) error
	Write(p []byte) (int, error)
}
