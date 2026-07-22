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
	"strconv"
	"strings"

	"github.com/example/agent-shell-tool/internal/filereader"
	"github.com/example/agent-shell-tool/pkg/client"
)

type console struct {
	client *client.Client
	writer io.Writer
}

func main() {
	socket := flag.String("socket", "/run/agent/read-file-tool.sock", "read-file-tool Unix socket")
	flag.Parse()
	rpcClient, err := client.DialUnix(*socket)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect %s: %v\n", *socket, err)
		os.Exit(1)
	}
	defer rpcClient.Close()
	terminal := &console{client: rpcClient, writer: os.Stdout}
	fmt.Printf("Agent Read File Tool console connected to %s\n", *socket)
	fmt.Println("输入 help 查看命令；输入 quit 退出。")
	if err := terminal.repl(os.Stdin); err != nil && !errors.Is(err, io.EOF) {
		fmt.Fprintf(os.Stderr, "console error: %v\n", err)
		os.Exit(1)
	}
}

func (c *console) repl(reader io.Reader) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	for {
		fmt.Fprint(c.writer, "read-file-tool> ")
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
		return false, c.callAndPrint("system.health", map[string]any{})
	case "capabilities":
		return false, c.callAndPrint("system.capabilities", map[string]any{})
	case "stat":
		if remainder == "" {
			return false, errors.New("usage: stat <path>")
		}
		return false, c.callAndPrint("read.stat", filereader.StatParams{Path: remainder, IncludeHash: true})
	case "read", "lines":
		params, err := parseLines(remainder)
		if err != nil {
			return false, err
		}
		return false, c.callAndPrint("read.lines", params)
	case "text-json":
		var params filereader.TextParams
		if err := parseJSON(remainder, &params); err != nil {
			return false, err
		}
		return false, c.callAndPrint("read.text", params)
	case "lines-json":
		var params filereader.LinesParams
		if err := parseJSON(remainder, &params); err != nil {
			return false, err
		}
		return false, c.callAndPrint("read.lines", params)
	case "bytes":
		params, err := parseBytes(remainder)
		if err != nil {
			return false, err
		}
		return false, c.callAndPrint("read.bytes", params)
	case "bytes-json", "binary-json":
		var params filereader.BytesParams
		if err := parseJSON(remainder, &params); err != nil {
			return false, err
		}
		return false, c.callAndPrint("read.bytes", params)
	case "hash":
		if remainder == "" {
			return false, errors.New("usage: hash <path>")
		}
		return false, c.callAndPrint("read.hash", filereader.HashParams{Path: remainder})
	case "batch-json":
		var params filereader.BatchParams
		if err := parseJSON(remainder, &params); err != nil {
			return false, err
		}
		return false, c.callAndPrint("read.batch", params)
	case "cancel":
		if remainder == "" {
			return false, errors.New("usage: cancel <read_id>")
		}
		return false, c.callAndPrint("read.cancel", filereader.CancelParams{ReadID: remainder})
	case "call":
		method, rawParams := cutToken(remainder)
		if method == "" {
			return false, errors.New("usage: call <method> [json params]")
		}
		params := map[string]any{}
		if rawParams != "" {
			if err := json.Unmarshal([]byte(rawParams), &params); err != nil {
				return false, fmt.Errorf("invalid JSON params: %w", err)
			}
		}
		return false, c.callAndPrint(method, params)
	default:
		return false, fmt.Errorf("unknown command %q; use help", command)
	}
	return false, nil
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
	fmt.Fprintln(c.writer, string(data))
	return nil
}

func (c *console) printHelp() {
	fmt.Fprint(c.writer, `Commands:
  health
  capabilities
  stat <path>
  read <path> [start_line] [end_line]
  lines <path> [start_line] [end_line]
  text-json <json>
  lines-json <json>
  bytes <path> [offset] [length]
  bytes-json <json>
  binary-json <json>
  hash <path>
  batch-json <json>
  cancel <read_id>
  call <method> [json]
  quit

Examples:
  stat README.md
  read README.md 1 30
  lines-json {"path":"README.md","start_line":10,"end_line":30,"include_line_numbers":true,"max_bytes":65536}
  text-json {"path":"README.md","start_char":0,"end_char":200,"max_bytes":4096}
  bytes README.md 0 64
  bytes-json {"path":"assets/image.png","offset":0,"length":65536,"include_hash":true}
  hash README.md
  batch-json {"reads":[{"kind":"stat","stat":{"path":"README.md"}},{"kind":"lines","lines":{"path":"README.md","start_line":1,"end_line":5}}]}
`)
}

func parseLines(raw string) (filereader.LinesParams, error) {
	fields := strings.Fields(raw)
	if len(fields) == 0 || len(fields) > 3 {
		return filereader.LinesParams{}, errors.New("usage: read <path> [start_line] [end_line]")
	}
	params := filereader.LinesParams{Path: fields[0]}
	var err error
	if len(fields) > 1 {
		params.StartLine, err = strconv.Atoi(fields[1])
		if err != nil {
			return filereader.LinesParams{}, errors.New("start_line must be an integer")
		}
	}
	if len(fields) > 2 {
		params.EndLine, err = strconv.Atoi(fields[2])
		if err != nil {
			return filereader.LinesParams{}, errors.New("end_line must be an integer")
		}
	}
	return params, nil
}

func parseBytes(raw string) (filereader.BytesParams, error) {
	fields := strings.Fields(raw)
	if len(fields) == 0 || len(fields) > 3 {
		return filereader.BytesParams{}, errors.New("usage: bytes <path> [offset] [length]")
	}
	params := filereader.BytesParams{Path: fields[0]}
	var err error
	if len(fields) > 1 {
		params.Offset, err = strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return filereader.BytesParams{}, errors.New("offset must be an integer")
		}
	}
	if len(fields) > 2 {
		params.Length, err = strconv.Atoi(fields[2])
		if err != nil {
			return filereader.BytesParams{}, errors.New("length must be an integer")
		}
	}
	return params, nil
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
