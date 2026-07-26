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

	"github.com/example/agent-shell-tool/internal/filewriter"
	"github.com/example/agent-shell-tool/pkg/client"
)

type console struct {
	client *client.Client
	writer io.Writer
}

func main() {
	socket := flag.String("socket", "/run/agent/write-file-tool.sock", "write-file-tool Unix socket")
	flag.Parse()
	rpc, err := client.DialUnix(*socket)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect %s: %v\n", *socket, err)
		os.Exit(1)
	}
	defer rpc.Close()
	terminal := &console{client: rpc, writer: os.Stdout}
	fmt.Printf("Agent Write File Tool console connected to %s\n", *socket)
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
		fmt.Fprint(c.writer, "write-file-tool> ")
		if !scanner.Scan() {
			return scanner.Err()
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		quit, err := c.executeLine(line)
		if err != nil {
			fmt.Fprintf(c.writer, "error: %v\n", err)
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
	case "health":
		return false, c.call("system.health", map[string]any{})
	case "capabilities":
		return false, c.call("system.capabilities", map[string]any{})
	case "create-json", "overwrite-json", "append-json", "write-at-json":
		var params filewriter.Operation
		if err := parseJSON(remainder, &params); err != nil {
			return false, err
		}
		method := "write_file." + strings.ReplaceAll(strings.TrimSuffix(command, "-json"), "-", "_")
		return false, c.call(method, params)
	case "batch-json", "preview-json":
		var params filewriter.BatchParams
		if err := parseJSON(remainder, &params); err != nil {
			return false, err
		}
		return false, c.call("write_file."+strings.TrimSuffix(command, "-json"), params)
	case "rollback":
		if remainder == "" {
			return false, errors.New("usage: rollback <transaction_id>")
		}
		return false, c.call("write_file.rollback", filewriter.RollbackParams{TransactionID: remainder})
	case "cancel":
		if remainder == "" {
			return false, errors.New("usage: cancel <write_id>")
		}
		return false, c.call("write_file.cancel", filewriter.CancelParams{WriteID: remainder})
	case "call":
		method, raw := cutToken(remainder)
		if method == "" {
			return false, errors.New("usage: call <method> [json params]")
		}
		params := map[string]any{}
		if raw != "" {
			if err := json.Unmarshal([]byte(raw), &params); err != nil {
				return false, err
			}
		}
		return false, c.call(method, params)
	default:
		return false, fmt.Errorf("unknown command %q; use help", command)
	}
	return false, nil
}
func (c *console) call(method string, params any) error {
	var result any
	if err := c.client.Call(context.Background(), method, params, &result); err != nil {
		return err
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(c.writer, string(data))
	return nil
}
func (c *console) printHelp() {
	fmt.Fprint(c.writer, `Commands:
  health
  capabilities
  create-json <json>
  overwrite-json <json>
  append-json <json>
  write-at-json <json>
  preview-json <json>
  batch-json <json>
  rollback <transaction_id>
  cancel <write_id>
  call <method> [json]
  quit

Examples:
  create-json {"path":"demo.txt","content":"hello\n","create_parents":true}
  overwrite-json {"path":"demo.txt","content":"updated\n","expected_sha256":"..."}
  append-json {"path":"demo.txt","content":"next line","add_newline":true}
  write-at-json {"path":"data.bin","offset":4,"data_base64":"AQID"}
  preview-json {"operations":[{"kind":"overwrite","path":"demo.txt","content":"preview\n"}]}
  batch-json {"operations":[{"kind":"create","path":"a.txt","content":"a"},{"kind":"create","path":"b.txt","content":"b"}]}
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
