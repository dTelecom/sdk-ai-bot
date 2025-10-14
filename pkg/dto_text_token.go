package pkg

type TextChunkType int

const (
	TextChunkTypeText TextChunkType = iota
	TextChunkTypeControl
)

const (
	TextControlClear = "Clear"
)

type TextChunk interface {
	Type() TextChunkType
}

type TextContentChunk struct {
	Text   string
	Source string
}

func (c *TextContentChunk) Type() TextChunkType {
	return TextChunkTypeText
}

func NewTextContentChunk(text, source string) *TextContentChunk {
	return &TextContentChunk{text, source}
}

type TextControlChunk struct {
	Code   string
	Source string
}

func (c *TextControlChunk) Type() TextChunkType {
	return TextChunkTypeControl
}
