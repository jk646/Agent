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

	"github.com/example/agent-shell-tool/internal/textsearch"
	"github.com/example/agent-shell-tool/pkg/client"
)

type console struct {
	client *client.Client
	writer io.Writer
}

func main() {
	socket := flag.String("socket", "/run/agent/search-text-tool.sock", "search-text-tool Unix socket")
	flag.Parse()
	rpcClient, err := client.DialUnix(*socket)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect %s: %v\n", *socket, err)
		os.Exit(1)
	}
	defer rpcClient.Close()
	terminal := &console{client: rpcClient, writer: os.Stdout}
	fmt.Printf("Agent Search Text Tool console connected to %s\n", *socket)
	fmt.Println("Enter help for commands; enter quit to exit.")
	if err := terminal.repl(os.Stdin); err != nil && !errors.Is(err, io.EOF) {
		fmt.Fprintf(os.Stderr, "console error: %v\n", err)
		os.Exit(1)
	}
}

func (c *console) repl(reader io.Reader) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	for {
		fmt.Fprint(c.writer, "search-text-tool> ")
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
	case "search", "regex", "files", "count":
		params, err := parseSimple(remainder)
		if err != nil {
			return false, err
		}
		return false, c.callAndPrint("search_text."+command, params)
	case "search-json", "regex-json", "files-json", "count-json":
		var params textsearch.SearchParams
		if err := parseJSON(remainder, &params); err != nil {
			return false, err
		}
		return false, c.callAndPrint("search_text."+strings.TrimSuffix(command, "-json"), params)
	case "multi-json":
		var params textsearch.MultiParams
		if err := parseJSON(remainder, &params); err != nil {
			return false, err
		}
		return false, c.callAndPrint("search_text.multi", params)
	case "batch-json":
		var params textsearch.BatchParams
		if err := parseJSON(remainder, &params); err != nil {
			return false, err
		}
		return false, c.callAndPrint("search_text.batch", params)
	case "cancel":
		if remainder == "" {
			return false, errors.New("usage: cancel <search_id>")
		}
		return false, c.callAndPrint("search_text.cancel", textsearch.CancelParams{SearchID: remainder})
	case "call":
		method, raw := cutToken(remainder)
		if method == "" {
			return false, errors.New("usage: call <method> [json params]")
		}
		params := map[string]any{}
		if raw != "" {
			if err := json.Unmarshal([]byte(raw), &params); err != nil {
				return false, fmt.Errorf("invalid JSON params: %w", err)
			}
		}
		return false, c.callAndPrint(method, params)
	default:
		return false, fmt.Errorf("unknown command %q; use help", command)
	}
	return false, nil
}

func parseSimple(raw string) (textsearch.SearchParams, error) {
	fields := strings.Fields(raw)
	if len(fields) == 0 || len(fields) > 3 {
		return textsearch.SearchParams{}, errors.New("usage: <command> <query> [path] [limit]")
	}
	params := textsearch.SearchParams{Query: fields[0], Path: "."}
	if len(fields) > 1 {
		params.Path = fields[1]
	}
	if len(fields) > 2 {
		limit, err := strconv.Atoi(fields[2])
		if err != nil {
			return textsearch.SearchParams{}, errors.New("limit must be an integer")
		}
		params.Limit = limit
	}
	return params, nil
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
  search <literal> [path] [limit]
  regex <expression> [path] [limit]
  files <literal> [path] [limit]
  count <literal> [path] [limit]
  search-json <json>
  regex-json <json>
  multi-json <json>
  files-json <json>
  count-json <json>
  batch-json <json>
  cancel <search_id>
  call <method> [json]
  quit

Examples:
  search TODO . 50
  regex "func\\s+\\w+" internal 100
  search-json {"path":".","query":"TODO","extensions":[".go"],"whole_word":true,"context_before":1,"context_after":1}
  regex-json {"path":".","query":"(?m)^package\\s+\\w+","include_patterns":["**/*.go"],"limit":50}
  multi-json {"path":".","patterns":[{"id":"todo","query":"TODO"},{"id":"fixme","query":"FIXME"}],"extensions":["go"]}
  files-json {"path":".","query":"JSON-RPC","exclude_patterns":["docs/**"]}
  count-json {"path":".","query":"error","case_sensitive":false}
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
