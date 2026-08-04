package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/example/agent-shell-tool/internal/orchestrator"
	"github.com/example/agent-shell-tool/pkg/client"
)

type console struct {
	client *client.Client
	writer io.Writer
	mu     sync.Mutex
}

func main() {
	socket := flag.String("socket", "/run/agent/orchestrator.sock", "orchestrator Unix socket")
	flag.Parse()
	rpc, err := client.DialUnix(*socket)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect %s: %v\n", *socket, err)
		os.Exit(1)
	}
	defer rpc.Close()
	terminal := &console{client: rpc, writer: os.Stdout}
	go terminal.printNotifications()
	fmt.Printf("Agent Orchestrator console connected to %s\n", *socket)
	fmt.Println("Enter help for commands; enter quit to exit.")
	if err := terminal.repl(os.Stdin); err != nil && !errors.Is(err, io.EOF) {
		fmt.Fprintf(os.Stderr, "console error: %v\n", err)
		os.Exit(1)
	}
}
func (c *console) repl(reader io.Reader) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), 16<<20)
	for {
		c.printf("orchestrator> ")
		if !scanner.Scan() {
			return scanner.Err()
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		quit, err := c.executeLine(line)
		if err != nil {
			c.printf("error: %v\n", err)
		}
		if quit {
			return nil
		}
	}
}
func (c *console) executeLine(line string) (bool, error) {
	command, remainder := cutToken(line)
	switch command {
	case "help", "?":
		c.printHelp()
	case "quit", "exit":
		return true, nil
	case "tools":
		return false, c.callAndPrint("orchestrator.tools", map[string]bool{"discover": remainder == "discover"})
	case "health":
		return false, c.callAndPrint("orchestrator.health", map[string]any{})
	case "status":
		return false, c.callAndPrint("system.health", map[string]any{})
	case "reload":
		return false, c.callAndPrint("orchestrator.reload", map[string]any{})
	case "route":
		if remainder == "" {
			return false, errors.New("usage: route <method>")
		}
		return false, c.callAndPrint("orchestrator.route", map[string]string{"method": remainder})
	case "call":
		return false, c.callFriendly(remainder)
	case "call-json":
		var params orchestrator.CallParams
		if err := parseJSON(remainder, &params); err != nil {
			return false, err
		}
		return false, c.callAndPrint("orchestrator.call", params)
	case "batch-json":
		var params orchestrator.BatchParams
		if err := parseJSON(remainder, &params); err != nil {
			return false, err
		}
		return false, c.callAndPrint("orchestrator.batch", params)
	case "rpc":
		method, raw := cutToken(remainder)
		if method == "" {
			return false, errors.New("usage: rpc <method> [json]")
		}
		params := map[string]any{}
		if raw != "" {
			if err := json.Unmarshal([]byte(raw), &params); err != nil {
				return false, err
			}
		}
		return false, c.callAndPrint(method, params)
	default:
		return false, fmt.Errorf("unknown command %q; use help", command)
	}
	return false, nil
}
func (c *console) callFriendly(raw string) error {
	tool, rest := cutToken(raw)
	method, paramsRaw := cutToken(rest)
	if tool == "" || method == "" {
		return errors.New("usage: call <tool|auto> <method> [json params]")
	}
	params := json.RawMessage("{}")
	if paramsRaw != "" {
		if !json.Valid([]byte(paramsRaw)) {
			return errors.New("invalid JSON parameters")
		}
		params = json.RawMessage(paramsRaw)
	}
	if tool == "auto" {
		tool = ""
	}
	return c.callAndPrint("orchestrator.call", orchestrator.CallParams{Tool: tool, Method: method, Params: params})
}
func (c *console) callAndPrint(method string, params any) error {
	var result any
	if err := c.client.Call(context.Background(), method, params, &result); err != nil {
		return err
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	c.printf("%s\n", data)
	return nil
}
func (c *console) printNotifications() {
	for notification := range c.client.Notifications() {
		if notification.Method != "orchestrator.tool_event" {
			continue
		}
		var event orchestrator.ToolEvent
		if json.Unmarshal(notification.Params, &event) != nil {
			continue
		}
		data, _ := json.Marshal(event.Params)
		c.printf("\n[event %s %s] %s\n", event.Tool, event.Method, data)
	}
}
func (c *console) printf(format string, args ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fmt.Fprintf(c.writer, format, args...)
}
func (c *console) printHelp() {
	c.printf(`Commands:
  tools [discover]
  health
  status
  reload
  route <method>
  call <tool|auto> <method> [json params]
  call-json <json>
  batch-json <json>
  rpc <orchestrator-method> [json params]
  quit

Examples:
  tools discover
  route read.lines
  call auto read.lines {"path":"README.md","start_line":1,"end_line":5}
  call search-text search_text.search {"path":".","query":"TODO","limit":10}
  call shell exec.start {"request_id":"demo","command":"pwd","cwd":"/workspace"}
  batch-json {"parallel":true,"calls":[{"method":"system.health","tool":"read-file"},{"method":"system.health","tool":"search-text"}]}
`)
}
func parseJSON(raw string, target any) error {
	if strings.TrimSpace(raw) == "" {
		return errors.New("JSON parameters are required")
	}
	if err := json.Unmarshal([]byte(raw), target); err != nil {
		return fmt.Errorf("invalid JSON parameters: %w", err)
	}
	return nil
}
func cutToken(value string) (string, string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ""
	}
	index := strings.IndexAny(value, " \t")
	if index < 0 {
		return value, ""
	}
	return value[:index], strings.TrimSpace(value[index+1:])
}
