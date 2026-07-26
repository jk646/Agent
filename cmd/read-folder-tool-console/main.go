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

	"github.com/example/agent-shell-tool/internal/folderreader"
	"github.com/example/agent-shell-tool/pkg/client"
)

type console struct {
	client *client.Client
	writer io.Writer
}

func main() {
	socket := flag.String("socket", "/run/agent/read-folder-tool.sock", "read-folder-tool Unix socket")
	flag.Parse()
	rpcClient, err := client.DialUnix(*socket)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect %s: %v\n", *socket, err)
		os.Exit(1)
	}
	defer rpcClient.Close()
	terminal := &console{client: rpcClient, writer: os.Stdout}
	fmt.Printf("Agent Read Folder Tool console connected to %s\n", *socket)
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
		fmt.Fprint(c.writer, "read-folder-tool> ")
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
		path := remainder
		if path == "" {
			path = "."
		}
		return false, c.callAndPrint("read_folder.stat", folderreader.StatParams{Path: path, IncludeDigest: true})
	case "list":
		params, err := parseList(remainder)
		if err != nil {
			return false, err
		}
		return false, c.callAndPrint("read_folder.list", params)
	case "tree":
		params, err := parseTree(remainder)
		if err != nil {
			return false, err
		}
		return false, c.callAndPrint("read_folder.tree", params)
	case "summary":
		params, err := parseSummary(remainder)
		if err != nil {
			return false, err
		}
		return false, c.callAndPrint("read_folder.summary", params)
	case "snapshot":
		params, err := parseSnapshot(remainder)
		if err != nil {
			return false, err
		}
		return false, c.callAndPrint("read_folder.snapshot", params)
	case "stat-json":
		var params folderreader.StatParams
		if err := parseJSON(remainder, &params); err != nil {
			return false, err
		}
		return false, c.callAndPrint("read_folder.stat", params)
	case "list-json":
		var params folderreader.ListParams
		if err := parseJSON(remainder, &params); err != nil {
			return false, err
		}
		return false, c.callAndPrint("read_folder.list", params)
	case "tree-json":
		var params folderreader.TreeParams
		if err := parseJSON(remainder, &params); err != nil {
			return false, err
		}
		return false, c.callAndPrint("read_folder.tree", params)
	case "summary-json":
		var params folderreader.SummaryParams
		if err := parseJSON(remainder, &params); err != nil {
			return false, err
		}
		return false, c.callAndPrint("read_folder.summary", params)
	case "snapshot-json":
		var params folderreader.SnapshotParams
		if err := parseJSON(remainder, &params); err != nil {
			return false, err
		}
		return false, c.callAndPrint("read_folder.snapshot", params)
	case "compare-json":
		var params folderreader.CompareParams
		if err := parseJSON(remainder, &params); err != nil {
			return false, err
		}
		return false, c.callAndPrint("read_folder.compare", params)
	case "batch-json":
		var params folderreader.BatchParams
		if err := parseJSON(remainder, &params); err != nil {
			return false, err
		}
		return false, c.callAndPrint("read_folder.batch", params)
	case "cancel":
		if remainder == "" {
			return false, errors.New("usage: cancel <read_id>")
		}
		return false, c.callAndPrint("read_folder.cancel", folderreader.CancelParams{ReadID: remainder})
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
  stat [path]
  list [path] [depth] [limit]
  tree [path] [depth]
  summary [path] [depth]
  snapshot [path] [depth]
  stat-json <json>
  list-json <json>
  tree-json <json>
  summary-json <json>
  snapshot-json <json>
  compare-json <json>
  batch-json <json>
  cancel <read_id>
  call <method> [json]
  quit

Examples:
  stat .
  list internal 2 100
  tree cmd 3
  summary . 10
  snapshot-json {"path":"internal","depth":3,"include_file_hashes":false,"limit":500}
  list-json {"path":".","depth":3,"extensions":[".go"],"sort_by":"size","sort_order":"desc","limit":50}
  tree-json {"path":"internal","depth":2,"include_files":true,"max_entries":200}
  batch-json {"reads":[{"kind":"stat","stat":{"path":"cmd"}},{"kind":"summary","summary":{"path":"internal","depth":3}}]}
`)
}

func parseList(raw string) (folderreader.ListParams, error) {
	fields := strings.Fields(raw)
	if len(fields) > 3 {
		return folderreader.ListParams{}, errors.New("usage: list [path] [depth] [limit]")
	}
	params := folderreader.ListParams{Path: "."}
	if len(fields) > 0 {
		params.Path = fields[0]
	}
	var err error
	if len(fields) > 1 {
		params.Depth, err = strconv.Atoi(fields[1])
		if err != nil {
			return folderreader.ListParams{}, errors.New("depth must be an integer")
		}
	}
	if len(fields) > 2 {
		params.Limit, err = strconv.Atoi(fields[2])
		if err != nil {
			return folderreader.ListParams{}, errors.New("limit must be an integer")
		}
	}
	return params, nil
}

func parseTree(raw string) (folderreader.TreeParams, error) {
	path, depth, err := parsePathDepth(raw, "tree")
	return folderreader.TreeParams{Path: path, Depth: depth}, err
}

func parseSummary(raw string) (folderreader.SummaryParams, error) {
	path, depth, err := parsePathDepth(raw, "summary")
	return folderreader.SummaryParams{Path: path, Depth: depth}, err
}

func parseSnapshot(raw string) (folderreader.SnapshotParams, error) {
	path, depth, err := parsePathDepth(raw, "snapshot")
	return folderreader.SnapshotParams{Path: path, Depth: depth}, err
}

func parsePathDepth(raw, command string) (string, int, error) {
	fields := strings.Fields(raw)
	if len(fields) > 2 {
		return "", 0, fmt.Errorf("usage: %s [path] [depth]", command)
	}
	path := "."
	if len(fields) > 0 {
		path = fields[0]
	}
	depth := 0
	if len(fields) > 1 {
		parsed, err := strconv.Atoi(fields[1])
		if err != nil {
			return "", 0, errors.New("depth must be an integer")
		}
		depth = parsed
	}
	return path, depth, nil
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
