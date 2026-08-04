package client

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
)

type Notification struct {
	Method string
	Params json.RawMessage
}
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message) }

type envelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}
type pendingResult struct {
	result json.RawMessage
	err    error
}

type Client struct {
	conn          net.Conn
	scanner       *bufio.Scanner
	encoder       *json.Encoder
	writeMu       sync.Mutex
	pendingMu     sync.Mutex
	pending       map[string]chan pendingResult
	nextID        atomic.Uint64
	notifications chan Notification
	done          chan struct{}
	closeOnce     sync.Once
}

func DialUnix(path string) (*Client, error) {
	conn, err := net.Dial("unix", path)
	if err != nil {
		return nil, err
	}
	client := &Client{conn: conn, scanner: bufio.NewScanner(conn), encoder: json.NewEncoder(conn), pending: make(map[string]chan pendingResult), notifications: make(chan Notification, 256), done: make(chan struct{})}
	client.scanner.Buffer(make([]byte, 64<<10), 4<<20)
	go client.readLoop()
	return client, nil
}
func (c *Client) Notifications() <-chan Notification { return c.notifications }
func (c *Client) Done() <-chan struct{}              { return c.done }
func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	id := c.nextID.Add(1)
	key := fmt.Sprintf("%d", id)
	response := make(chan pendingResult, 1)
	c.pendingMu.Lock()
	c.pending[key] = response
	c.pendingMu.Unlock()
	request := map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
	c.writeMu.Lock()
	err := c.encoder.Encode(request)
	c.writeMu.Unlock()
	if err != nil {
		c.removePending(key)
		return err
	}
	select {
	case item := <-response:
		if item.err != nil {
			return item.err
		}
		if result == nil || len(item.result) == 0 {
			return nil
		}
		return json.Unmarshal(item.result, result)
	case <-ctx.Done():
		c.removePending(key)
		return ctx.Err()
	case <-c.done:
		return errors.New("client closed")
	}
}
func (c *Client) Close() error {
	var err error
	c.closeOnce.Do(func() { err = c.conn.Close(); close(c.done) })
	return err
}
func (c *Client) readLoop() {
	defer c.Close()
	defer close(c.notifications)
	for c.scanner.Scan() {
		var message envelope
		if json.Unmarshal(c.scanner.Bytes(), &message) != nil {
			continue
		}
		if message.Method != "" {
			select {
			case c.notifications <- Notification{Method: message.Method, Params: message.Params}:
			default:
			}
			continue
		}
		key := string(message.ID)
		c.pendingMu.Lock()
		response := c.pending[key]
		delete(c.pending, key)
		c.pendingMu.Unlock()
		if response != nil {
			if message.Error != nil {
				response <- pendingResult{err: message.Error}
			} else {
				response <- pendingResult{result: message.Result}
			}
		}
	}
	c.failPending(errors.New("connection closed"))
}
func (c *Client) removePending(key string) {
	c.pendingMu.Lock()
	delete(c.pending, key)
	c.pendingMu.Unlock()
}
func (c *Client) failPending(err error) {
	c.pendingMu.Lock()
	pending := c.pending
	c.pending = make(map[string]chan pendingResult)
	c.pendingMu.Unlock()
	for _, response := range pending {
		response <- pendingResult{err: err}
	}
}
