package protocol

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
)

const DefaultMaxMessageBytes = 4 << 20

var ErrMalformedRequest = errors.New("malformed JSON-RPC request")

type Conn struct {
	raw      io.ReadWriteCloser
	scanner  *bufio.Scanner
	encoder  *json.Encoder
	writeMu  sync.Mutex
	closeMu  sync.Once
	closed   chan struct{}
	identity string
}

func NewConn(raw io.ReadWriteCloser, maxMessageBytes int) *Conn {
	if maxMessageBytes <= 0 {
		maxMessageBytes = DefaultMaxMessageBytes
	}
	scanner := bufio.NewScanner(raw)
	scanner.Buffer(make([]byte, 64<<10), maxMessageBytes)
	return &Conn{raw: raw, scanner: scanner, encoder: json.NewEncoder(raw), closed: make(chan struct{}), identity: remoteIdentity(raw)}
}
func (c *Conn) Identity() string        { return c.identity }
func (c *Conn) Closed() <-chan struct{} { return c.closed }
func (c *Conn) ReadRequest() (Request, error) {
	if !c.scanner.Scan() {
		if err := c.scanner.Err(); err != nil {
			return Request{}, err
		}
		return Request{}, io.EOF
	}
	var request Request
	if err := json.Unmarshal(c.scanner.Bytes(), &request); err != nil {
		return Request{}, fmt.Errorf("%w: %v", ErrMalformedRequest, err)
	}
	if request.JSONRPC != "2.0" || request.Method == "" {
		return Request{}, ErrMalformedRequest
	}
	return request, nil
}
func (c *Conn) Respond(response Response) error { return c.write(response) }
func (c *Conn) Notify(method string, params any) error {
	return c.write(Notification{JSONRPC: "2.0", Method: method, Params: params})
}
func (c *Conn) write(value any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	select {
	case <-c.closed:
		return net.ErrClosed
	default:
	}
	return c.encoder.Encode(value)
}
func (c *Conn) Close() error {
	var err error
	c.closeMu.Do(func() { close(c.closed); err = c.raw.Close() })
	return err
}
func remoteIdentity(raw io.ReadWriteCloser) string {
	if conn, ok := raw.(net.Conn); ok {
		return conn.RemoteAddr().String()
	}
	return "stdio"
}
