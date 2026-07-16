package executor

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/example/agent-shell-tool/internal/output"
	"github.com/example/agent-shell-tool/internal/policy"
)

var ErrNotFound = errors.New("execution not found")
var ErrCapacity = errors.New("execution capacity reached")
var ErrConflict = errors.New("request_id already exists")

type Manager struct {
	cfg    Config
	policy policy.ExecutionPolicy
	sem    chan struct{}
	mu     sync.Mutex
	tasks  map[string]*Task
}

type Task struct {
	id              string
	owner           string
	params          StartParams
	manager         *Manager
	emitter         output.Emitter
	mu              sync.Mutex
	cmd             *exec.Cmd
	stdin           io.WriteCloser
	done            chan struct{}
	finish          sync.Once
	canceled        bool
	timedOut        bool
	cancelRequested bool
}

func NewManager(cfg Config, executionPolicy policy.ExecutionPolicy) *Manager {
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 8
	}
	if executionPolicy == nil {
		executionPolicy = policy.AllowAll{}
	}
	return &Manager{cfg: cfg, policy: executionPolicy, sem: make(chan struct{}, cfg.MaxConcurrent), tasks: make(map[string]*Task)}
}

func (m *Manager) Start(ctx context.Context, owner string, params StartParams, emitter output.Emitter) (StartResult, error) {
	if err := Validate(params); err != nil {
		return StartResult{}, err
	}
	if params.RequestID == "" {
		params.RequestID = randomID("exec")
	}
	if params.Shell == "" {
		params.Shell = m.cfg.DefaultShell
	}
	if err := m.policy.Authorize(ctx, policy.Request{Command: params.Command, Cwd: params.Cwd, Env: params.Env, Shell: params.Shell}); err != nil {
		return StartResult{}, fmt.Errorf("%w: %v", policy.ErrRejected, err)
	}
	select {
	case m.sem <- struct{}{}:
	default:
		return StartResult{}, ErrCapacity
	}
	task := &Task{id: params.RequestID, owner: owner, params: params, manager: m, emitter: emitter, done: make(chan struct{})}
	m.mu.Lock()
	if _, exists := m.tasks[task.id]; exists {
		m.mu.Unlock()
		<-m.sem
		return StartResult{}, ErrConflict
	}
	m.tasks[task.id] = task
	m.mu.Unlock()
	go task.run()
	return StartResult{RequestID: task.id, Accepted: true}, nil
}

func (m *Manager) Cancel(requestID string) error {
	task, err := m.get(requestID)
	if err != nil {
		return err
	}
	task.cancel(false)
	return nil
}
func (m *Manager) Write(requestID string, data []byte, closeInput bool) error {
	task, err := m.get(requestID)
	if err != nil {
		return err
	}
	return task.write(data, closeInput)
}
func (m *Manager) CancelOwner(owner string) {
	m.mu.Lock()
	tasks := make([]*Task, 0)
	for _, task := range m.tasks {
		if task.owner == owner {
			tasks = append(tasks, task)
		}
	}
	m.mu.Unlock()
	for _, task := range tasks {
		task.cancel(false)
	}
}
func (m *Manager) Shutdown() {
	m.mu.Lock()
	tasks := make([]*Task, 0, len(m.tasks))
	for _, task := range m.tasks {
		tasks = append(tasks, task)
	}
	m.mu.Unlock()
	for _, task := range tasks {
		task.cancel(false)
	}
}
func (m *Manager) Count() int { m.mu.Lock(); defer m.mu.Unlock(); return len(m.tasks) }
func (m *Manager) get(id string) (*Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	task, ok := m.tasks[id]
	if !ok {
		return nil, ErrNotFound
	}
	return task, nil
}
func (m *Manager) remove(id string) { m.mu.Lock(); delete(m.tasks, id); m.mu.Unlock(); <-m.sem }

func (t *Task) run() {
	startedAt := time.Now()
	defer func() { t.manager.remove(t.id); t.finish.Do(func() { close(t.done) }) }()
	limit := t.params.OutputLimitBytes
	if limit == 0 {
		limit = t.manager.cfg.OutputLimitBytes
	}
	stream, err := output.New(t.id, "exec", t.manager.cfg.TempDir, limit, t.emitter)
	if err != nil {
		t.emitFailed(err)
		return
	}
	cmd := exec.Command(t.params.Shell, "-lc", t.params.Command)
	cmd.Dir = t.params.Cwd
	cmd.Env = mergeEnvironment(t.params.Env)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stream.Close()
		t.emitFailed(err)
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		stream.Close()
		t.emitFailed(err)
		return
	}
	var stdin io.WriteCloser
	if t.params.EnableStdin {
		stdin, err = cmd.StdinPipe()
		if err != nil {
			stream.Close()
			t.emitFailed(err)
			return
		}
	}
	t.mu.Lock()
	t.cmd = cmd
	t.stdin = stdin
	t.mu.Unlock()
	if err := cmd.Start(); err != nil {
		stream.Close()
		t.emitFailed(err)
		return
	}
	t.mu.Lock()
	cancelRequested := t.cancelRequested
	t.mu.Unlock()
	if cancelRequested {
		terminateProcessGroup(cmd.Process.Pid, t.manager.cfg.KillGrace, t.done)
	}
	_ = t.emitter("exec.started", StartedEvent{RequestID: t.id, PID: cmd.Process.Pid, Timestamp: time.Now().UTC().Format(time.RFC3339Nano)})
	timeout := t.manager.cfg.DefaultTimeout
	if t.params.TimeoutMS > 0 {
		timeout = time.Duration(t.params.TimeoutMS) * time.Millisecond
	}
	timer := time.AfterFunc(timeout, func() { t.cancel(true) })
	defer timer.Stop()
	var readers sync.WaitGroup
	readers.Add(2)
	go copyOutput(&readers, stdout, "stdout", stream)
	go copyOutput(&readers, stderr, "stderr", stream)
	waitErr := cmd.Wait()
	readers.Wait()
	summary := stream.Close()
	exitCode, signal := exitStatus(cmd, waitErr)
	t.mu.Lock()
	timedOut, canceled := t.timedOut, t.canceled
	t.mu.Unlock()
	_ = t.emitter("exec.exited", ExitedEvent{RequestID: t.id, ExitCode: exitCode, Signal: signal, DurationMS: time.Since(startedAt).Milliseconds(), TimedOut: timedOut, Canceled: canceled && !timedOut, TotalOutputBytes: summary.TotalBytes, Truncated: summary.Truncated, LogPath: summary.LogPath, TailBase64: summary.TailBase64, Timestamp: time.Now().UTC().Format(time.RFC3339Nano)})
}

func copyOutput(wg *sync.WaitGroup, reader io.Reader, name string, stream *output.Stream) {
	defer wg.Done()
	buffer := make([]byte, 32<<10)
	for {
		count, err := reader.Read(buffer)
		if count > 0 {
			_ = stream.Write(name, buffer[:count])
		}
		if err != nil {
			return
		}
	}
}
func (t *Task) emitFailed(err error) {
	_ = t.emitter("exec.failed", FailedEvent{RequestID: t.id, Message: err.Error(), Timestamp: time.Now().UTC().Format(time.RFC3339Nano)})
}
func (t *Task) cancel(timeout bool) {
	t.mu.Lock()
	if timeout {
		t.timedOut = true
	} else {
		t.canceled = true
	}
	t.cancelRequested = true
	cmd := t.cmd
	t.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return
	}
	terminateProcessGroup(cmd.Process.Pid, t.manager.cfg.KillGrace, t.done)
}
func (t *Task) write(data []byte, closeInput bool) error {
	t.mu.Lock()
	stdin := t.stdin
	t.mu.Unlock()
	if stdin == nil {
		return errors.New("stdin is not enabled")
	}
	if len(data) > 0 {
		if _, err := stdin.Write(data); err != nil {
			return err
		}
	}
	if closeInput {
		return stdin.Close()
	}
	return nil
}

func terminateProcessGroup(pid int, grace time.Duration, done <-chan struct{}) {
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	go func() {
		timer := time.NewTimer(grace)
		defer timer.Stop()
		select {
		case <-done:
			return
		case <-timer.C:
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
	}()
}
func exitStatus(cmd *exec.Cmd, waitErr error) (int, string) {
	if cmd.ProcessState == nil {
		return -1, ""
	}
	status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
	if !ok {
		return cmd.ProcessState.ExitCode(), ""
	}
	if status.Signaled() {
		return 128 + int(status.Signal()), status.Signal().String()
	}
	return status.ExitStatus(), ""
}
func mergeEnvironment(overrides map[string]string) []string {
	values := make(map[string]string)
	for _, item := range os.Environ() {
		if index := strings.IndexByte(item, '='); index > 0 {
			values[item[:index]] = item[index+1:]
		}
	}
	for key, value := range overrides {
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result
}
func DecodeInput(value string) ([]byte, error) {
	if value == "" {
		return nil, nil
	}
	return base64.StdEncoding.DecodeString(value)
}
func randomID(prefix string) string {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + base64.RawURLEncoding.EncodeToString(buffer)
}
