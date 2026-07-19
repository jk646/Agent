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

	"github.com/example/agent-shell-tool/internal/filetool"
	"github.com/example/agent-shell-tool/pkg/client"
)

type console struct {
	client *client.Client
	writer io.Writer
}

type replaceInput struct {
	Path                string `json:"path"`
	OldText             string `json:"old_text"`
	NewText             string `json:"new_text"`
	ExpectedOccurrences int    `json:"expected_occurrences,omitempty"`
	DryRun              bool   `json:"dry_run,omitempty"`
	Permanent           bool   `json:"permanent,omitempty"`
}

func main() {
	socket := flag.String("socket", "/run/agent/file-tool.sock", "file-tool Unix socket")
	flag.Parse()
	rpcClient, err := client.DialUnix(*socket)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect %s: %v\n", *socket, err)
		os.Exit(1)
	}
	defer rpcClient.Close()
	terminal := &console{client: rpcClient, writer: os.Stdout}
	fmt.Printf("Agent File Tool console connected to %s\n", *socket)
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
		fmt.Fprint(c.writer, "file-tool> ")
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
		return false, c.callAndPrint("file.stat", filetool.StatParams{Path: remainder, IncludeHash: true})
	case "read":
		params, err := parseRead(remainder)
		if err != nil {
			return false, err
		}
		return false, c.callAndPrint("file.read", params)
	case "read-json":
		var params filetool.ReadParams
		if err := parseJSON(remainder, &params); err != nil {
			return false, err
		}
		return false, c.callAndPrint("file.read", params)
	case "list":
		params, err := parseList(remainder)
		if err != nil {
			return false, err
		}
		return false, c.callAndPrint("file.list", params)
	case "list-json":
		var params filetool.ListParams
		if err := parseJSON(remainder, &params); err != nil {
			return false, err
		}
		return false, c.callAndPrint("file.list", params)
	case "find-json":
		var params filetool.FindParams
		if err := parseJSON(remainder, &params); err != nil {
			return false, err
		}
		return false, c.callAndPrint("file.find", params)
	case "search-json":
		var params filetool.SearchParams
		if err := parseJSON(remainder, &params); err != nil {
			return false, err
		}
		return false, c.callAndPrint("file.search", params)
	case "replace-json":
		var params replaceInput
		if err := parseJSON(remainder, &params); err != nil {
			return false, err
		}
		return false, c.replace(params)
	case "create-json", "mkdir-json", "copy-json", "move-json", "delete-json", "chmod-json":
		var operation filetool.Operation
		if err := parseJSON(remainder, &operation); err != nil {
			return false, err
		}
		method := "file." + strings.TrimSuffix(command, "-json")
		if err := c.fillExpectedHashes(&operation, method); err != nil {
			return false, err
		}
		return false, c.callAndPrint(method, operation)
	case "apply-json":
		var params filetool.ApplyEditsParams
		if err := parseJSON(remainder, &params); err != nil {
			return false, err
		}
		return false, c.callAndPrint("file.apply_edits", params)
	case "batch-json":
		var params filetool.BatchParams
		if err := parseJSON(remainder, &params); err != nil {
			return false, err
		}
		return false, c.callAndPrint("file.batch", params)
	case "rollback", "restore":
		if remainder == "" {
			return false, fmt.Errorf("usage: %s <transaction_id>", command)
		}
		return false, c.callAndPrint("file.rollback", filetool.RollbackParams{TransactionID: remainder})
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

func (c *console) replace(params replaceInput) error {
	if params.Path == "" || params.OldText == "" {
		return errors.New("replace-json requires path and old_text")
	}
	var info filetool.FileInfo
	if err := c.client.Call(context.Background(), "file.stat", filetool.StatParams{Path: params.Path, IncludeHash: true}, &info); err != nil {
		return err
	}
	result := filetool.TransactionResult{}
	err := c.client.Call(context.Background(), "file.apply_edits", filetool.ApplyEditsParams{
		DryRun: params.DryRun, Permanent: params.Permanent,
		Changes: []filetool.Operation{{
			Kind: "replace", Path: params.Path, ExpectedSHA256: info.SHA256,
			Replacements: []filetool.Replacement{{OldText: params.OldText, NewText: params.NewText, ExpectedOccurrences: params.ExpectedOccurrences}},
		}},
	}, &result)
	if err != nil {
		return err
	}
	c.printJSON(result)
	return nil
}

func (c *console) fillExpectedHashes(operation *filetool.Operation, method string) error {
	var source string
	switch method {
	case "file.delete", "file.chmod":
		source = operation.Path
	case "file.copy", "file.move":
		source = operation.From
	}
	if source != "" && operation.ExpectedSHA256 == "" {
		var info filetool.FileInfo
		if err := c.client.Call(context.Background(), "file.stat", filetool.StatParams{Path: source, IncludeHash: true}, &info); err != nil {
			return err
		}
		operation.ExpectedSHA256 = info.SHA256
	}
	return nil
}

func (c *console) callAndPrint(method string, params any) error {
	var result any
	if err := c.client.Call(context.Background(), method, params, &result); err != nil {
		return err
	}
	c.printJSON(result)
	return nil
}

func (c *console) printJSON(value any) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fmt.Fprintln(c.writer, value)
		return
	}
	fmt.Fprintln(c.writer, string(data))
}

func (c *console) printHelp() {
	fmt.Fprint(c.writer, `Commands:
  health
  capabilities
  stat <path>
  read <path> [start_line] [end_line]
  read-json <json>
  list [path] [depth]
  list-json <json>
  find-json <json>
  search-json <json>
  replace-json <json>      自动读取 SHA-256 后精确替换
  create-json <json>
  mkdir-json <json>
  copy-json <json>
  move-json <json>
  delete-json <json>
  chmod-json <json>
  apply-json <json>        完整 file.apply_edits 参数
  batch-json <json>        完整 file.batch 参数
  rollback <transaction_id>
  call <method> [json]
  quit

Examples:
  stat README.md
  read README.md 1 40
  replace-json {"path":"demo.txt","old_text":"old","new_text":"new","expected_occurrences":1}
  replace-json {"path":"demo.txt","old_text":"old","new_text":"new","dry_run":true}
  create-json {"path":"demo.txt","content":"hello\n","create_parents":true}
  mkdir-json {"path":"tmp/example","create_parents":true}
  copy-json {"from":"demo.txt","to":"demo-copy.txt"}
  move-json {"from":"demo-copy.txt","to":"archive/demo.txt","create_parents":true}
  delete-json {"path":"archive/demo.txt"}
  rollback filetx-...
`)
}

func parseRead(raw string) (filetool.ReadParams, error) {
	fields := strings.Fields(raw)
	if len(fields) == 0 || len(fields) > 3 {
		return filetool.ReadParams{}, errors.New("usage: read <path> [start_line] [end_line]")
	}
	params := filetool.ReadParams{Path: fields[0]}
	var err error
	if len(fields) > 1 {
		params.StartLine, err = strconv.Atoi(fields[1])
		if err != nil {
			return filetool.ReadParams{}, errors.New("start_line must be an integer")
		}
	}
	if len(fields) > 2 {
		params.EndLine, err = strconv.Atoi(fields[2])
		if err != nil {
			return filetool.ReadParams{}, errors.New("end_line must be an integer")
		}
	}
	return params, nil
}

func parseList(raw string) (filetool.ListParams, error) {
	fields := strings.Fields(raw)
	if len(fields) > 2 {
		return filetool.ListParams{}, errors.New("usage: list [path] [depth]")
	}
	params := filetool.ListParams{}
	if len(fields) > 0 {
		params.Path = fields[0]
	}
	if len(fields) > 1 {
		depth, err := strconv.Atoi(fields[1])
		if err != nil {
			return filetool.ListParams{}, errors.New("depth must be an integer")
		}
		params.Depth = depth
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
