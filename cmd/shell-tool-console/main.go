package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/example/agent-shell-tool/internal/executor"
	"github.com/example/agent-shell-tool/internal/output"
	"github.com/example/agent-shell-tool/internal/session"
	"github.com/example/agent-shell-tool/pkg/client"
)

type execCompletion struct {
	event executor.ExitedEvent
	err   error
}

type console struct {
	client      *client.Client
	outputMu    sync.Mutex
	waitersMu   sync.Mutex
	execWaiters map[string]chan execCompletion
}

func main() {
	socket := flag.String("socket", "/run/agent/shell-tool.sock", "shell-tool Unix socket")
	flag.Parse()

	rpcClient, err := client.DialUnix(*socket)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect %s: %v\n", *socket, err)
		os.Exit(1)
	}
	defer rpcClient.Close()

	terminal := &console{client: rpcClient, execWaiters: make(map[string]chan execCompletion)}
	go terminal.consumeNotifications()

	fmt.Printf("Agent Shell Tool console connected to %s\n", *socket)
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
		fmt.Print("shell-tool> ")
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
	case "health":
		return false, c.callAndPrint("system.health", map[string]any{})
	case "capabilities":
		return false, c.callAndPrint("system.capabilities", map[string]any{})
	case "list":
		return false, c.callAndPrint("session.list", map[string]any{})
	case "exec":
		if remainder == "" {
			return false, errors.New("usage: exec <shell command>")
		}
		return false, c.exec(executor.StartParams{Command: remainder})
	case "exec-json":
		var params executor.StartParams
		if err := parseJSON(remainder, &params); err != nil {
			return false, err
		}
		return false, c.exec(params)
	case "exec-bg":
		if remainder == "" {
			return false, errors.New("usage: exec-bg <shell command>")
		}
		params := executor.StartParams{Command: remainder}
		if params.RequestID == "" {
			params.RequestID = localID("exec")
		}
		var result executor.StartResult
		if err := c.client.Call(context.Background(), "exec.start", params, &result); err != nil {
			return false, err
		}
		c.printJSON(result)
	case "cancel":
		if remainder == "" {
			return false, errors.New("usage: cancel <request_id>")
		}
		return false, c.callAndPrint("exec.cancel", executor.CancelParams{RequestID: remainder})
	case "open":
		sessionID, cwd := cutToken(remainder)
		return false, c.open(session.OpenParams{SessionID: sessionID, Cwd: cwd})
	case "open-json":
		var params session.OpenParams
		if err := parseJSON(remainder, &params); err != nil {
			return false, err
		}
		return false, c.open(params)
	case "run", "run-bg":
		sessionID, shellCommand := cutToken(remainder)
		if sessionID == "" || shellCommand == "" {
			return false, fmt.Errorf("usage: %s <session_id> <shell command>", command)
		}
		params := session.RunParams{SessionID: sessionID, Command: shellCommand}
		if command == "run-bg" {
			go c.runSession(params)
			c.printf("session run started in background\n")
			return false, nil
		}
		return false, c.runSession(params)
	case "run-json", "run-json-bg":
		var params session.RunParams
		if err := parseJSON(remainder, &params); err != nil {
			return false, err
		}
		if command == "run-json-bg" {
			go c.runSession(params)
			c.printf("session run started in background\n")
			return false, nil
		}
		return false, c.runSession(params)
	case "write":
		sessionID, text := cutToken(remainder)
		if sessionID == "" {
			return false, errors.New("usage: write <session_id> <text>")
		}
		params := session.WriteParams{SessionID: sessionID, DataBase64: base64.StdEncoding.EncodeToString([]byte(text))}
		return false, c.callAndPrint("session.write", params)
	case "write-json":
		var params session.WriteParams
		if err := parseJSON(remainder, &params); err != nil {
			return false, err
		}
		return false, c.callAndPrint("session.write", params)
	case "resize":
		fields := strings.Fields(remainder)
		if len(fields) != 3 {
			return false, errors.New("usage: resize <session_id> <rows> <cols>")
		}
		rows, err := parseUint16(fields[1])
		if err != nil {
			return false, fmt.Errorf("rows: %w", err)
		}
		cols, err := parseUint16(fields[2])
		if err != nil {
			return false, fmt.Errorf("cols: %w", err)
		}
		return false, c.callAndPrint("session.resize", session.ResizeParams{SessionID: fields[0], Rows: rows, Cols: cols})
	case "interrupt":
		if remainder == "" {
			return false, errors.New("usage: interrupt <session_id>")
		}
		return false, c.callAndPrint("session.interrupt", session.IDParams{SessionID: remainder})
	case "close":
		if remainder == "" {
			return false, errors.New("usage: close <session_id>")
		}
		return false, c.callAndPrint("session.close", session.IDParams{SessionID: remainder})
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

func (c *console) exec(params executor.StartParams) error {
	if params.RequestID == "" {
		params.RequestID = localID("exec")
	}
	completion := make(chan execCompletion, 1)
	c.waitersMu.Lock()
	c.execWaiters[params.RequestID] = completion
	c.waitersMu.Unlock()
	defer c.removeWaiter(params.RequestID)

	var accepted executor.StartResult
	if err := c.client.Call(context.Background(), "exec.start", params, &accepted); err != nil {
		return err
	}
	c.printJSON(accepted)
	select {
	case result := <-completion:
		if result.err != nil {
			return result.err
		}
		c.printJSON(result.event)
		return nil
	case <-c.client.Done():
		return errors.New("connection closed while waiting for execution")
	}
}

func (c *console) open(params session.OpenParams) error {
	var result session.OpenResult
	if err := c.client.Call(context.Background(), "session.open", params, &result); err != nil {
		return err
	}
	c.printJSON(result)
	return nil
}

func (c *console) runSession(params session.RunParams) error {
	var result session.RunResult
	if err := c.client.Call(context.Background(), "session.run", params, &result); err != nil {
		c.printf("session.run error: %v\n", err)
		return err
	}
	c.printJSON(result)
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

func (c *console) consumeNotifications() {
	for notification := range c.client.Notifications() {
		c.handleNotification(notification)
	}
}

func (c *console) handleNotification(notification client.Notification) {
	switch notification.Method {
	case "exec.output":
		var event output.ChunkEvent
		if json.Unmarshal(notification.Params, &event) == nil {
			c.writeDecoded(event.DataBase64, event.Stream == "stderr")
			return
		}
	case "session.output":
		var event session.OutputEvent
		if json.Unmarshal(notification.Params, &event) == nil {
			c.writeDecoded(event.DataBase64, false)
			return
		}
	case "exec.exited":
		var event executor.ExitedEvent
		if json.Unmarshal(notification.Params, &event) == nil {
			if c.completeExec(event.RequestID, execCompletion{event: event}) {
				return
			}
		}
	case "exec.failed":
		var event executor.FailedEvent
		if json.Unmarshal(notification.Params, &event) == nil {
			if c.completeExec(event.RequestID, execCompletion{err: errors.New(event.Message)}) {
				return
			}
		}
	case "exec.started":
		var event executor.StartedEvent
		if json.Unmarshal(notification.Params, &event) == nil {
			c.printf("[started request=%s pid=%d]\n", event.RequestID, event.PID)
			return
		}
	case "exec.truncated":
		var event output.TruncatedEvent
		if json.Unmarshal(notification.Params, &event) == nil {
			c.printf("[output truncated request=%s log=%s bytes=%d]\n", event.RequestID, event.LogPath, event.TotalBytes)
			return
		}
	case "session.truncated":
		var event session.TruncatedEvent
		if json.Unmarshal(notification.Params, &event) == nil {
			c.printf("[session output truncated session=%s run=%s log=%s bytes=%d]\n", event.SessionID, event.RunID, event.LogPath, event.TotalBytes)
			return
		}
	}
	c.printf("[notification %s] %s\n", notification.Method, string(notification.Params))
}

func (c *console) completeExec(requestID string, result execCompletion) bool {
	c.waitersMu.Lock()
	waiter := c.execWaiters[requestID]
	c.waitersMu.Unlock()
	if waiter == nil {
		return false
	}
	select {
	case waiter <- result:
	default:
	}
	return true
}

func (c *console) removeWaiter(requestID string) {
	c.waitersMu.Lock()
	delete(c.execWaiters, requestID)
	c.waitersMu.Unlock()
}

func (c *console) writeDecoded(encoded string, stderr bool) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		c.printf("[invalid base64 output: %v]\n", err)
		return
	}
	c.outputMu.Lock()
	defer c.outputMu.Unlock()
	writer := io.Writer(os.Stdout)
	if stderr {
		writer = os.Stderr
	}
	_, _ = writer.Write(data)
}

func (c *console) printJSON(value any) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		c.printf("%v\n", value)
		return
	}
	c.printf("%s\n", data)
}

func (c *console) printf(format string, args ...any) {
	c.outputMu.Lock()
	defer c.outputMu.Unlock()
	fmt.Printf(format, args...)
}

func (c *console) printHelp() {
	c.printf("%s", `Commands:
  health                              查看服务健康状态
  capabilities                        查看服务能力
  exec <command>                      前台执行无状态命令并等待退出
  exec-json <json>                    使用完整 exec.start 参数执行
  exec-bg <command>                   后台启动无状态命令
  cancel <request_id>                 取消无状态命令
  open [session_id] [cwd]             创建持久 PTY 会话
  open-json <json>                    使用完整 session.open 参数
  run <session_id> <command>          在持久会话中执行并等待结果
  run-bg <session_id> <command>       后台执行，允许继续 write/resize/interrupt
  run-json <json>                     使用完整 session.run 参数
  run-json-bg <json>                  后台使用完整 session.run 参数
  write <session_id> <text>           向 PTY 写入文本
  write-json <json>                   传入 base64 的完整 session.write 参数
  resize <session_id> <rows> <cols>   修改终端尺寸
  interrupt <session_id>              发送 SIGINT
  close <session_id>                  关闭会话
  list                                列出会话
  call <method> [json]                调用任意 JSON-RPC 方法
  quit                                退出控制台

Examples:
  exec pwd && id
  exec-json {"command":"printf hi","cwd":"/tmp","timeout_ms":5000}
  open dev /workspace
  run dev export MODE=test; cd /tmp
  run dev printf '%s:%s\\n' "$MODE" "$PWD"
  run-bg dev cat
  write dev hello
  write-json {"session_id":"dev","data_base64":"Cg=="}
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

func parseUint16(value string) (uint16, error) {
	parsed, err := strconv.ParseUint(value, 10, 16)
	if err != nil || parsed == 0 {
		return 0, errors.New("must be an integer between 1 and 65535")
	}
	return uint16(parsed), nil
}

func localID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}
