package mcp

import (
	"io"

	"github.com/tunr-dev/tunr/internal/inspector"
)

// NewForTest — test için Server oluşturur, custom io.Reader/Writer ile
// Production'da stdin/stdout kullanılır, testte buffer kullanılır
func NewForTest(in io.Reader, out io.Writer) *Server {
	ins := inspector.New(100)
	s := New(ins, nil)
	s.in = in
	s.out = out
	return s
}
