// (c) go2rtc

package core

import (
	"io"
	"os"
	"sync"

	"github.com/mattn/go-isatty"
	"github.com/rs/zerolog"
)

type circularBuffer struct {
	chunks [][]byte
	r, w   int
	mu     sync.Mutex
}

const (
	chunkCount = 16
	chunkSize  = 1 << 16
)

var MemoryLog = newBuffer()
var Logger zerolog.Logger

func InitLogger() zerolog.Logger {
	var writer io.Writer
	writer = os.Stdout

	console := &zerolog.ConsoleWriter{Out: writer}
	console.NoColor = !isatty.IsTerminal(writer.(*os.File).Fd())
	console.TimeFormat = "15:04:05.000"

	writer = console
	writer = zerolog.MultiLevelWriter(writer, MemoryLog)

	lvl, _ := zerolog.ParseLevel("trace")
	Logger = zerolog.New(writer).Level(lvl)

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnixMs
	Logger = Logger.With().Timestamp().Logger()

	return Logger
}

func newBuffer() *circularBuffer {
	b := &circularBuffer{chunks: make([][]byte, 0, chunkCount)}
	// create first chunk
	b.chunks = append(b.chunks, make([]byte, 0, chunkSize))
	return b
}

func (b *circularBuffer) Write(p []byte) (n int, err error) {
	n = len(p)

	b.mu.Lock()
	// check if chunk has size
	if len(b.chunks[b.w])+n > chunkSize {
		// increase write chunk index
		if b.w++; b.w == chunkCount {
			b.w = 0
		}
		// check overflow
		if b.r == b.w {
			// increase read chunk index
			if b.r++; b.r == chunkCount {
				b.r = 0
			}
		}
		// check if current chunk exists
		if b.w == len(b.chunks) {
			// allocate new chunk
			b.chunks = append(b.chunks, make([]byte, 0, chunkSize))
		} else {
			// reset len of current chunk
			b.chunks[b.w] = b.chunks[b.w][:0]
		}
	}

	b.chunks[b.w] = append(b.chunks[b.w], p...)
	b.mu.Unlock()
	return
}

func (b *circularBuffer) WriteTo(w io.Writer) (n int64, err error) {
	buf := make([]byte, 0, chunkCount*chunkSize)

	// use temp buffer inside mutex because w.Write can take some time
	b.mu.Lock()
	for i := b.r; ; {
		buf = append(buf, b.chunks[i]...)
		if i == b.w {
			break
		}
		if i++; i == chunkCount {
			i = 0
		}
	}
	b.mu.Unlock()

	nn, err := w.Write(buf)
	return int64(nn), err
}

func (b *circularBuffer) Reset() {
	b.mu.Lock()
	b.chunks[0] = b.chunks[0][:0]
	b.r = 0
	b.w = 0
	b.mu.Unlock()
}
