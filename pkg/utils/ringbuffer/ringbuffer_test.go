package ringbuffer_test

import (
	"bytes"
	"testing"

	"github.com/dTelecom/sdk-ai-bot/pkg/utils/ringbuffer"
)

type step struct {
	write            []byte
	expectedWriteLen int
	readCapacity     int
	expectedRead     []byte
}

func TestRing_ReadWrite(t *testing.T) {
	cases := []struct {
		name     string
		capacity uint64
		steps    []step
	}{
		{
			name:     "simple",
			capacity: 4,
			steps: []step{
				{
					write:            []byte("ab"),
					expectedWriteLen: 2,
				},
				{
					write:            []byte("cd"),
					expectedWriteLen: 2,
				},
				{
					readCapacity: 3,
					expectedRead: []byte("abc"),
				},
				{
					readCapacity: 1,
					expectedRead: []byte("d"),
				},
			},
		}, {
			name:     "write more than capacity",
			capacity: 3, // real capacity will be 4
			steps: []step{
				{
					write:            []byte("ab"),
					expectedWriteLen: 2,
				},
				{
					write:            []byte("cdef"),
					expectedWriteLen: 2,
				},
				{
					readCapacity: 2,
					expectedRead: []byte("ab"),
				},
				{
					write:            []byte("ef"),
					expectedWriteLen: 2,
				},
				{
					readCapacity: 2,
					expectedRead: []byte("cd"),
				},
				{
					readCapacity: 2,
					expectedRead: []byte("ef"),
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rb := ringbuffer.New(tc.capacity)
			for _, step := range tc.steps {
				if step.write != nil {
					if n := rb.Write(step.write); n != step.expectedWriteLen {
						t.Fatalf("Write() = %d, want %d", n, step.expectedWriteLen)
					}
				}
				if step.expectedRead != nil {
					dst := make([]byte, step.readCapacity)
					if n := rb.Read(dst); n != len(step.expectedRead) {
						t.Fatalf("Read() = %d, want %d", n, len(step.expectedRead))
					}
					if !bytes.Equal(dst, step.expectedRead) {
						t.Fatalf("got %q, want %q", dst, step.expectedRead)
					}
				}
			}
			if n := rb.Read(make([]byte, 1)); n != 0 {
				t.Fatalf("unexpected data after expected reads: %d byte(s)", n)
			}
		})
	}
}
