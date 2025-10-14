package ringbuffer

import (
	"sync"
)

// capacity должна быть степенью 2 — тогда индекс = pos & mask
type Ring struct {
	buf        []byte
	mask       uint64 // capacity-1
	head, tail uint64 // счётчики в байтах (не индексы!)

	mu sync.Mutex
}

// New возвращает кольцо заданного размера (степень 2).
func New(capacity uint64) *Ring {
	capacity = nextPowerOfTwo(capacity)
	return &Ring{
		buf:  make([]byte, capacity),
		mask: capacity - 1,
	}
}

// Write копирует как можно больше данных из src, возвращает n записанных байт.
func (r *Ring) Write(src []byte) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	free := len(r.buf) - int(r.head-r.tail)
	if free == 0 || len(src) == 0 {
		return 0
	}
	if len(src) > free {
		src = src[:free]
	}
	off := r.head & r.mask

	// первая (до конца буфера) и, при необходимости, вторая (c 0) копии
	n := copy(r.buf[off:], src)
	if n < len(src) {
		n += copy(r.buf, src[n:])
	}
	r.head += uint64(n)
	return n
}

// Read копирует до len(dst) байт в dst, возвращает n считанных байт.
func (r *Ring) Read(dst []byte) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	avail := int(r.head - r.tail)
	if avail == 0 || len(dst) == 0 {
		return 0
	}
	if len(dst) > avail {
		dst = dst[:avail]
	}
	off := r.tail & r.mask

	n := copy(dst, r.buf[off:])
	if n < len(dst) {
		n += copy(dst[n:], r.buf)
	}
	r.tail += uint64(n)
	return n
}

func (r *Ring) Available() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return int(r.head - r.tail)
}

func (r *Ring) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.head, r.tail = 0, 0
}

func nextPowerOfTwo(n uint64) uint64 {
	if n == 0 {
		return 1
	}
	n--
	n |= n >> 1
	n |= n >> 2
	n |= n >> 4
	n |= n >> 8
	n |= n >> 16
	n |= n >> 32
	return n + 1
}
