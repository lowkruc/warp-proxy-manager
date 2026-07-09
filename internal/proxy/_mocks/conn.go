package mocks

import (
	"io"
	"net"
	"time"
)

// Conn is a mock net.Conn backed by a pre-loaded input buffer.
// Server reads from In, writes to Out.
type Conn struct {
	In  []byte
	Out []byte
	pos int
}

// NewConn creates a Conn with pre-loaded input data.
func NewConn(input []byte) *Conn {
	return &Conn{In: input}
}

func (c *Conn) Read(b []byte) (int, error) {
	if c.pos >= len(c.In) {
		return 0, io.EOF
	}
	n := copy(b, c.In[c.pos:])
	c.pos += n
	return n, nil
}

func (c *Conn) Write(b []byte) (int, error) {
	c.Out = append(c.Out, b...)
	return len(b), nil
}

func (c *Conn) Close() error                       { return nil }
func (c *Conn) LocalAddr() net.Addr                { return &net.IPAddr{} }
func (c *Conn) RemoteAddr() net.Addr               { return &net.IPAddr{} }
func (c *Conn) SetDeadline(_ time.Time) error      { return nil }
func (c *Conn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *Conn) SetWriteDeadline(_ time.Time) error { return nil }
