package mcp

import (
	"io"

	"github.com/ahmetvural79/tunr/internal/inspector"
)

// NewForTest — creates a Server for testing with custom io.Reader/Writer
// Production uses stdin/stdout, tests use buffers
func NewForTest(in io.Reader, out io.Writer) *Server {
	ins := inspector.New(100)
	s := New(ins, nil)
	s.in = in
	s.out = out
	return s
}
