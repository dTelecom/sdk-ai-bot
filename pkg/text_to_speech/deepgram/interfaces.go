package deepgram

//go:generate ../../../bin/mockgen -source $GOFILE -destination=mocks/mocks.go -package mocks

type Client interface {
	Connect() bool
	Stop()
	SpeakWithText(text string) error
	Clear() error
	ProcessError(err error) error
	Flush() error
}
