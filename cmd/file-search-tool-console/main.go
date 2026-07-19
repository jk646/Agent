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

	"github.com/example/agent-shell-tool/internal/filesearch"
	"github.com/example/agent-shell-tool/pkg/client"
)

type console struct {
	client *client.Client
	writer io.Writer
}

func main() {
	socket := flag.String("socket", "/run/agent/file-search-tool.sock", "file-search-tool Unix socket")
	flag.Parse()
	rpcClient, err := client.DialUnix(*socket)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect %s: %v\n", *socket, err)
		os.Exit(1)
	}
	defer rpcClient.Close()
	terminal := &console{client: rpcClient, writer: os.Stdout}
	fmt.Printf("Agent File Search Tool console connected to %s\n", *socket)
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
		fmt.Fprint(c.writer, "file-search-tool> ")
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
		return false, c.callAndPrint("search.stat", filesearch.StatParams{Path: remainder})
	case "find":
		params, err := parseFind(remainder, false)
		if err != nil {
			return false, err
		}
		return false, c.callAndPrint("search.find", params)
	case "glob":
		params, err := parseFind(remainder, true)
		if err != nil {
			return false, err
		}
		return false, c.callAndPrint("search.find", params)
	case "find-json":
		var params filesearch.FindParams
		if err := parseJSON(remainder, &params); err != nil {
			return false, err
		}
		return false, c.callAndPrint("search.find", params)
	case "content-json":
		var params filesearch.ContentParams
		if err := parseJSON(remainder, &params); err != nil {
			return false, err
		}
		return false, c.callAndPrint("search.content", params)
	case "batch-json":
		var params filesearch.BatchParams
		if err := parseJSON(remainder, &params); err != nil {
			return false, err
		}
		return false, c.callAndPrint("search.batch", params)
	case "cancel":
		if remainder == "" {
			return false, errors.New("usage: cancel <search_id>")
		}
		return false, c.callAndPrint("search.cancel", filesearch.CancelParams{SearchID: remainder})
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
  find <name> [path] [limit]
  glob <pattern> [path] [limit]
  find-json <json>
  content-json <json>
  batch-json <json>
  cancel <search_id>
  call <method> [json]
  quit

Examples:
  find README . 20
  glob *.go . 100
  find-json {"path":".","name":"server","type":"file","extensions":[".go"],"max_depth":8,"limit":50}
  content-json {"path":".","query":"TODO","file_pattern":"*.go","context_before":1,"context_after":2,"limit":50}
  content-json {"search_id":"manual-1","path":".","query":"func\\s+main","regex":true,"case_sensitive":true}
  batch-json {"searches":[{"kind":"find","find":{"pattern":"*.md"}},{"kind":"content","content":{"query":"JSON-RPC"}}]}
`)
}

func parseFind(raw string, glob bool) (filesearch.FindParams, error) {
	fields := strings.Fields(raw)
	if len(fields) == 0 || len(fields) > 3 {
		return filesearch.FindParams{}, errors.New("usage: find/glob <value> [path] [limit]")
	}
	params := filesearch.FindParams{Path: ".", Limit: 100}
	if glob {
		params.Pattern = fields[0]
	} else {
		params.Name = fields[0]
	}
	if len(fields) > 1 {
		params.Path = fields[1]
	}
	if len(fields) > 2 {
		limit, err := strconv.Atoi(fields[2])
		if err != nil {
			return filesearch.FindParams{}, errors.New("limit must be an integer")
		}
		params.Limit = limit
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
