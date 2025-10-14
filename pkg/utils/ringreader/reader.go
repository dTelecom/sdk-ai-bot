// Package ringreader позволяет читать из io.Reader через циклический буфер.
package ringreader

import (
	"io"
	"sync"
)

// Reader читает src в фоне и отдаёт наружу через Read().
type Reader struct {
	src io.Reader

	buf       []byte // кольцевой буфер
	capacity  int    // len(buf)
	r, w      int    // индексы чтения и записи
	avail     int    // сколько байт доступно
	err       error  // первый полученный от src Read() error (включая io.EOF)
	closed    bool   // фоновой горутины больше нет
	mu        sync.Mutex
	dataReady *sync.Cond
}

// New создаёт Reader с буфером size байт и запускает фонового
// «дренажёра», который читает src пока не получит ошибку.
func New(src io.Reader, size int) *Reader {
	if size < 1 {
		size = 32 * 1024
	}
	rr := &Reader{
		src:      src,
		buf:      make([]byte, size),
		capacity: size,
	}
	rr.dataReady = sync.NewCond(&rr.mu)

	go rr.drain()
	return rr
}

// Read реализует io.Reader, блокируя, пока нет данных и источник не закрыт.
func (r *Reader) Read(p []byte) (n int, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for r.avail == 0 && !r.closed {
		r.dataReady.Wait()
	}

	if r.avail == 0 && r.closed {
		return 0, r.err
	}

	// Сколько реально можем отдать сейчас
	toCopy := len(p)
	if toCopy > r.avail {
		toCopy = r.avail
	}

	// Может потребоваться две копии из-за кольца
	first := r.capacity - r.r
	if first > toCopy {
		first = toCopy
	}
	copy(p, r.buf[r.r:r.r+first])
	second := toCopy - first
	if second > 0 {
		copy(p[first:], r.buf[:second])
	}

	// Обновляем индексы
	r.r = (r.r + toCopy) % r.capacity
	r.avail -= toCopy
	return toCopy, nil
}

// drain читает src и кладёт данные в кольцевой буфер.
func (r *Reader) drain() {
	tmp := make([]byte, 4096)
	for {
		n, er := r.src.Read(tmp)
		if n > 0 {
			r.write(tmp[:n])
		}
		if er != nil {
			r.mu.Lock()
			r.err = er
			r.closed = true
			r.mu.Unlock()
			r.dataReady.Broadcast()
			return
		}
	}
}

// write вставляет data в кольцо, перезаписывая старые данные при переполнении.
func (r *Reader) write(data []byte) {
	r.mu.Lock()
	defer func() {
		r.mu.Unlock()
		r.dataReady.Signal()
	}()

	for _, b := range data {
		if r.avail == r.capacity { // кольцо полно — вытесняем старый байт
			r.r = (r.r + 1) % r.capacity
			r.avail--
		}
		r.buf[r.w] = b
		r.w = (r.w + 1) % r.capacity
		r.avail++
	}
}
