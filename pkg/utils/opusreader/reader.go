package opusreader

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/pion/webrtc/v3/pkg/media/oggreader"
)

type OpusReader struct {
	oggReader   *oggreader.OggReader
	oggHeader   *oggreader.OggHeader
	lastGranule uint64

	dataCh chan []byte
	cache  []byte
	closed bool
	once   sync.Once
}

func New(filePath string) (*OpusReader, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}

	oggReader, oggHeader, err := oggreader.NewWith(file)
	if err != nil {
		return nil, err
	}

	return &OpusReader{
		oggReader:   oggReader,
		oggHeader:   oggHeader,
		lastGranule: 0,
		dataCh:      make(chan []byte),
	}, nil
}

func (r *OpusReader) Channels() uint8 {
	return r.oggHeader.Channels
}

func (r *OpusReader) SampleRate() uint32 {
	return r.oggHeader.SampleRate
}

func (r *OpusReader) Read(p []byte) (int, error) {
	r.once.Do(func() {
		go r.drain()
	})

	if len(p) == 0 {
		return 0, nil
	}

	nWritten := 0
	for len(p) > 0 {
		if len(r.cache) > 0 {
			copied := copy(p, r.cache)
			p = p[copied:]
			r.cache = r.cache[copied:]
			nWritten += copied
			continue
		}

		if r.closed {
			if nWritten > 0 {
				return nWritten, nil
			}
			return 0, io.EOF
		}

		select {
		case data, ok := <-r.dataCh:
			if ok {
				if len(data) > 0 {
					r.cache = data
				}
			} else {
				r.closed = true
				if nWritten > 0 {
					return nWritten, nil
				}
				return 0, io.EOF
			}
		default:
			return nWritten, nil
		}
	}

	return nWritten, nil
}

func (r *OpusReader) drain() {
	ts := time.Now()
	for {
		pageData, pageHeader, err := r.oggReader.ParseNextPage()
		if err != nil {
			fmt.Println("oggReader err:", err.Error())
			close(r.dataCh)
			return
		}
		if pageHeader.GranulePosition == 0 {
			continue
		}
		r.dataCh <- pageData
		sampleCount := float64(pageHeader.GranulePosition - r.lastGranule)
		r.lastGranule = pageHeader.GranulePosition
		sampleDuration := time.Duration((sampleCount/48000)*1000) * time.Millisecond
		ts = ts.Add(sampleDuration)
		d := ts.Sub(time.Now())
		time.Sleep(d)
	}
}
