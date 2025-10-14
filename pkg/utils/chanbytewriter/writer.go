package chanbytewriter

type Writer struct {
	ch chan<- []byte
}

func New(ch chan<- []byte) *Writer {
	return &Writer{
		ch: ch,
	}
}

func (w *Writer) Write(p []byte) (int, error) {
	// Делаем копию, чтобы избежать проблем с переиспользованием p
	buf := make([]byte, len(p))
	copy(buf, p)

	w.ch <- buf
	return len(p), nil
}
