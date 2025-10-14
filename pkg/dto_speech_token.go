package pkg

type SpeechChunkType int

const (
	SpeechChunkTypeText SpeechChunkType = iota
	SpeechChunkTypeControl
)

const (
	SpeechControlSpeechStarted = "SpeechStarted"
	SpeechControlSpeechEnded   = "SpeechEnded"
)

type SpeechChunk interface {
	Type() SpeechChunkType
}

type SpeechTextChunk struct {
	Text string
}

func (c *SpeechTextChunk) Type() SpeechChunkType {
	return SpeechChunkTypeText
}

type SpeechControlChunk struct {
	Code string
}

func (c *SpeechControlChunk) Type() SpeechChunkType {
	return SpeechChunkTypeControl
}
