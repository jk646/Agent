package session

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/example/agent-shell-tool/internal/executor"
	"github.com/example/agent-shell-tool/internal/output"
	"github.com/example/agent-shell-tool/internal/policy"
)

var ErrNotFound = errors.New("session not found")
var ErrCapacity = errors.New("session capacity reached")
var ErrConflict = errors.New("session_id already exists")
var ErrBusy = errors.New("session is busy")

type Manager struct {
	cfg       Config
	policy    policy.ExecutionPolicy
	mu        sync.Mutex
	sessions  map[string]*Session
	closed    chan struct{}
	closeOnce sync.Once
}
type Session struct {
	id         string
	owner      string
	shell      string
	cmd        *exec.Cmd
	ptmx       *os.File
	manager    *Manager
	mu         sync.Mutex
	emitter    output.Emitter
	createdAt  time.Time
	lastActive time.Time
	detachedAt time.Time
	sequence   uint64
	pending    *pendingRun
	closed     bool
	done       chan struct{}
	doneOnce   sync.Once
}
type pendingRun struct {
	runID     string
	marker    []byte
	buffer    []byte
	stream    *output.Stream
	startedAt time.Time
	done      chan runCompletion
}
type runCompletion struct {
	exitCode int
	err      error
}

func NewManager(cfg Config, executionPolicy policy.ExecutionPolicy) *Manager {
	if cfg.MaxSessions <= 0 {
		cfg.MaxSessions = 4
	}
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = 10 * time.Minute
	}
	if executionPolicy == nil {
		executionPolicy = policy.AllowAll{}
	}
	manager := &Manager{cfg: cfg, policy: executionPolicy, sessions: make(map[string]*Session), closed: make(chan struct{})}
	go manager.reapLoop()
	return manager
}

func (m *Manager) Open(ctx context.Context, owner string, params OpenParams, emitter output.Emitter) (OpenResult, error) {
	if params.SessionID == "" {
		params.SessionID = randomID("session")
	}
	if params.Shell == "" {
		params.Shell = m.cfg.DefaultShell
	}
	if params.Rows == 0 {
		params.Rows = 24
	}
	if params.Cols == 0 {
		params.Cols = 80
	}
	if err := executor.Validate(executor.StartParams{Command: "true", Cwd: params.Cwd, Env: params.Env, Shell: params.Shell}); err != nil {
		return OpenResult{}, err
	}
	if err := m.policy.Authorize(ctx, policy.Request{Command: "<persistent-session>", Cwd: params.Cwd, Env: params.Env, Shell: params.Shell}); err != nil {
		return OpenResult{}, fmt.Errorf("%w: %v", policy.ErrRejected, err)
	}
	m.mu.Lock()
	if _, exists := m.sessions[params.SessionID]; exists {
		m.mu.Unlock()
		return OpenResult{}, ErrConflict
	}
	if len(m.sessions) >= m.cfg.MaxSessions {
		m.mu.Unlock()
		return OpenResult{}, ErrCapacity
	}
	m.mu.Unlock()
	cmd := exec.Command(params.Shell, shellArguments(params.Shell)...)
	cmd.Dir = params.Cwd
	cmd.Env = mergeEnvironment(params.Env, map[string]string{"PS1": "", "PROMPT_COMMAND": ""})
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: params.Rows, Cols: params.Cols})
	if err != nil {
		return OpenResult{}, err
	}
	if err := disableEcho(ptmx); err != nil {
		cleanupStartedShell(cmd, ptmx)
		return OpenResult{}, fmt.Errorf("disable PTY echo: %w", err)
	}
	now := time.Now()
	session := &Session{id: params.SessionID, owner: owner, shell: params.Shell, cmd: cmd, ptmx: ptmx, manager: m, emitter: emitter, createdAt: now, lastActive: now, done: make(chan struct{})}
	m.mu.Lock()
	if _, exists := m.sessions[params.SessionID]; exists {
		m.mu.Unlock()
		cleanupStartedShell(cmd, ptmx)
		return OpenResult{}, ErrConflict
	}
	if len(m.sessions) >= m.cfg.MaxSessions {
		m.mu.Unlock()
		cleanupStartedShell(cmd, ptmx)
		return OpenResult{}, ErrCapacity
	}
	m.sessions[session.id] = session
	m.mu.Unlock()
	go session.readLoop()
	go session.waitLoop()
	return OpenResult{SessionID: session.id, PID: cmd.Process.Pid, CreatedAt: now.UTC().Format(time.RFC3339Nano)}, nil
}

func (m *Manager) Run(ctx context.Context, owner string, params RunParams, emitter output.Emitter) (RunResult, error) {
	if params.SessionID == "" {
		return RunResult{}, errors.New("session_id and command are required")
	}
	if err := executor.Validate(executor.StartParams{Command: params.Command, TimeoutMS: params.TimeoutMS, OutputLimitBytes: params.OutputLimitBytes}); err != nil {
		return RunResult{}, err
	}
	session, err := m.attach(params.SessionID, owner, emitter)
	if err != nil {
		return RunResult{}, err
	}
	return session.run(ctx, params)
}
func (m *Manager) Write(owner string, params WriteParams, emitter output.Emitter) error {
	session, err := m.attach(params.SessionID, owner, emitter)
	if err != nil {
		return err
	}
	data, err := base64.StdEncoding.DecodeString(params.DataBase64)
	if err != nil {
		return err
	}
	session.touch()
	_, err = session.ptmx.Write(data)
	return err
}
func (m *Manager) Resize(owner string, params ResizeParams, emitter output.Emitter) error {
	if params.Rows == 0 || params.Cols == 0 {
		return errors.New("rows and cols must be positive")
	}
	session, err := m.attach(params.SessionID, owner, emitter)
	if err != nil {
		return err
	}
	session.touch()
	return pty.Setsize(session.ptmx, &pty.Winsize{Rows: params.Rows, Cols: params.Cols})
}
func (m *Manager) Interrupt(owner string, params IDParams, emitter output.Emitter) error {
	session, err := m.attach(params.SessionID, owner, emitter)
	if err != nil {
		return err
	}
	session.touch()
	return signalForeground(session.ptmx, session.cmd.Process.Pid, syscall.SIGINT)
}
func (m *Manager) Close(sessionID string) error {
	m.mu.Lock()
	session, ok := m.sessions[sessionID]
	if ok {
		delete(m.sessions, sessionID)
	}
	m.mu.Unlock()
	if !ok {
		return ErrNotFound
	}
	session.close()
	return nil
}
func (m *Manager) List() []Info {
	m.mu.Lock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	m.mu.Unlock()
	result := make([]Info, 0, len(sessions))
	for _, session := range sessions {
		result = append(result, session.info())
	}
	sort.Slice(result, func(i, j int) bool { return result[i].SessionID < result[j].SessionID })
	return result
}
func (m *Manager) Count() int { m.mu.Lock(); defer m.mu.Unlock(); return len(m.sessions) }
func (m *Manager) DetachOwner(owner string) {
	now := time.Now()
	m.mu.Lock()
	for _, session := range m.sessions {
		session.mu.Lock()
		if session.owner == owner {
			session.owner = ""
			session.detachedAt = now
			session.emitter = nil
		}
		session.mu.Unlock()
	}
	m.mu.Unlock()
}
func (m *Manager) Shutdown() {
	m.closeOnce.Do(func() { close(m.closed) })
	m.mu.Lock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	m.sessions = make(map[string]*Session)
	m.mu.Unlock()
	for _, session := range sessions {
		session.close()
	}
}
func (m *Manager) attach(id, owner string, emitter output.Emitter) (*Session, error) {
	m.mu.Lock()
	session, ok := m.sessions[id]
	m.mu.Unlock()
	if !ok {
		return nil, ErrNotFound
	}
	session.mu.Lock()
	session.owner = owner
	session.emitter = emitter
	session.detachedAt = time.Time{}
	session.lastActive = time.Now()
	session.mu.Unlock()
	return session, nil
}
func (m *Manager) remove(id string) { m.mu.Lock(); delete(m.sessions, id); m.mu.Unlock() }

func (s *Session) run(ctx context.Context, params RunParams) (RunResult, error) {
	if params.RunID == "" {
		params.RunID = randomID("run")
	}
	limit := params.OutputLimitBytes
	if limit == 0 {
		limit = s.manager.cfg.OutputLimitBytes
	}
	emitter := func(method string, value any) error { return s.emitRun(method, params.RunID, value) }
	stream, err := output.New(params.RunID, "session", filepath.Join(s.manager.cfg.TempDir, "sessions", s.id), limit, emitter)
	if err != nil {
		return RunResult{}, err
	}
	token := randomToken()
	pending := &pendingRun{runID: params.RunID, marker: []byte("\x1eAGENT_END_" + token + ":"), stream: stream, startedAt: time.Now(), done: make(chan runCompletion, 1)}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		stream.Close()
		return RunResult{}, ErrNotFound
	}
	if s.pending != nil {
		s.mu.Unlock()
		stream.Close()
		return RunResult{}, ErrBusy
	}
	s.pending = pending
	s.lastActive = time.Now()
	s.mu.Unlock()
	wrapped := buildCommand(params.Command, token)
	if _, err := io.WriteString(s.ptmx, wrapped); err != nil {
		s.clearPending(pending)
		stream.Close()
		return RunResult{}, err
	}
	timeoutDuration := s.manager.cfg.DefaultTimeout
	if params.TimeoutMS > 0 {
		timeoutDuration = time.Duration(params.TimeoutMS) * time.Millisecond
	}
	timer := time.NewTimer(timeoutDuration)
	timeout := timer.C
	defer timer.Stop()
	timedOut := false
	var completion runCompletion
	select {
	case completion = <-pending.done:
	case <-ctx.Done():
		s.interrupt()
		completion = <-s.waitAfterInterrupt(pending)
		if completion.err == nil {
			completion.err = ctx.Err()
		}
	case <-timeout:
		timedOut = true
		s.interrupt()
		completion = <-s.waitAfterInterrupt(pending)
	}
	summary := stream.Close()
	result := RunResult{SessionID: s.id, RunID: params.RunID, ExitCode: completion.exitCode, DurationMS: time.Since(pending.startedAt).Milliseconds(), TimedOut: timedOut, TotalOutputBytes: summary.TotalBytes, Truncated: summary.Truncated, LogPath: summary.LogPath, TailBase64: summary.TailBase64}
	if completion.err != nil && !timedOut {
		return result, completion.err
	}
	return result, nil
}

func (s *Session) waitAfterInterrupt(pending *pendingRun) <-chan runCompletion {
	result := make(chan runCompletion, 1)
	go func() {
		select {
		case completion := <-pending.done:
			result <- completion
		case <-time.After(s.manager.cfg.KillGrace):
			s.close()
			result <- runCompletion{exitCode: -1, err: errors.New("session did not recover after interrupt")}
		}
	}()
	return result
}
func (s *Session) interrupt() { _ = signalForeground(s.ptmx, s.cmd.Process.Pid, syscall.SIGINT) }
func (s *Session) clearPending(target *pendingRun) {
	s.mu.Lock()
	if s.pending == target {
		s.pending = nil
	}
	s.mu.Unlock()
}
func (s *Session) readLoop() {
	buffer := make([]byte, 32<<10)
	for {
		count, err := s.ptmx.Read(buffer)
		if count > 0 {
			s.processOutput(buffer[:count])
		}
		if err != nil {
			return
		}
	}
}
func (s *Session) processOutput(data []byte) {
	s.mu.Lock()
	s.lastActive = time.Now()
	pending := s.pending
	if pending == nil {
		emitter := s.emitter
		s.sequence++
		sequence := s.sequence
		sessionID := s.id
		s.mu.Unlock()
		if emitter != nil {
			_ = emitter("session.output", OutputEvent{SessionID: sessionID, Sequence: sequence, DataBase64: base64.StdEncoding.EncodeToString(data), Timestamp: time.Now().UTC().Format(time.RFC3339Nano)})
		}
		return
	}
	pending.buffer = append(pending.buffer, data...)
	markerIndex := bytes.Index(pending.buffer, pending.marker)
	if markerIndex < 0 {
		keep := len(pending.marker) + 16
		if len(pending.buffer) > keep {
			emit := append([]byte(nil), pending.buffer[:len(pending.buffer)-keep]...)
			pending.buffer = append(pending.buffer[:0], pending.buffer[len(pending.buffer)-keep:]...)
			s.mu.Unlock()
			_ = pending.stream.Write("pty", emit)
			return
		}
		s.mu.Unlock()
		return
	}
	terminatorOffset := bytes.IndexByte(pending.buffer[markerIndex+len(pending.marker):], 0x1f)
	if terminatorOffset < 0 {
		s.mu.Unlock()
		return
	}
	codeStart := markerIndex + len(pending.marker)
	codeEnd := codeStart + terminatorOffset
	before := append([]byte(nil), pending.buffer[:markerIndex]...)
	after := append([]byte(nil), pending.buffer[codeEnd+1:]...)
	code, parseErr := strconv.Atoi(string(pending.buffer[codeStart:codeEnd]))
	s.pending = nil
	emitter := s.emitter
	s.sequence++
	sequence := s.sequence
	sessionID := s.id
	runID := pending.runID
	s.mu.Unlock()
	if len(before) > 0 {
		_ = pending.stream.Write("pty", before)
	}
	pending.done <- runCompletion{exitCode: code, err: parseErr}
	if len(after) > 0 && emitter != nil {
		_ = emitter("session.output", OutputEvent{SessionID: sessionID, RunID: runID, Sequence: sequence, DataBase64: base64.StdEncoding.EncodeToString(after), Timestamp: time.Now().UTC().Format(time.RFC3339Nano)})
	}
}
func (s *Session) emitRun(method, runID string, value any) error {
	s.mu.Lock()
	emitter := s.emitter
	s.sequence++
	sequence := s.sequence
	sessionID := s.id
	s.mu.Unlock()
	if emitter == nil {
		return nil
	}
	switch event := value.(type) {
	case output.ChunkEvent:
		return emitter("session.output", OutputEvent{SessionID: sessionID, RunID: runID, Sequence: sequence, DataBase64: event.DataBase64, Timestamp: event.Timestamp})
	case output.TruncatedEvent:
		return emitter("session.truncated", TruncatedEvent{SessionID: sessionID, RunID: runID, Sequence: sequence, LogPath: event.LogPath, TotalBytes: event.TotalBytes, Timestamp: event.Timestamp})
	default:
		return emitter(method, value)
	}
}
func (s *Session) waitLoop() {
	_ = s.cmd.Wait()
	s.manager.remove(s.id)
	s.mu.Lock()
	s.closed = true
	pending := s.pending
	s.pending = nil
	s.mu.Unlock()
	_ = s.ptmx.Close()
	s.doneOnce.Do(func() { close(s.done) })
	if pending != nil {
		pending.done <- runCompletion{exitCode: -1, err: errors.New("session exited")}
	}
}
func (s *Session) close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	pending := s.pending
	s.pending = nil
	s.mu.Unlock()
	_ = s.ptmx.Close()
	terminateProcess(s.cmd.Process.Pid, s.manager.cfg.KillGrace, s.done)
	if pending != nil {
		pending.done <- runCompletion{exitCode: -1, err: errors.New("session closed")}
	}
}
func (s *Session) touch() { s.mu.Lock(); s.lastActive = time.Now(); s.mu.Unlock() }
func (s *Session) info() Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Info{SessionID: s.id, PID: s.cmd.Process.Pid, CreatedAt: s.createdAt.UTC().Format(time.RFC3339Nano), LastActiveAt: s.lastActive.UTC().Format(time.RFC3339Nano), Attached: s.owner != "", Running: s.pending != nil}
}

func (m *Manager) reapLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.reap()
		case <-m.closed:
			return
		}
	}
}
func (m *Manager) reap() {
	now := time.Now()
	ids := make([]string, 0)
	m.mu.Lock()
	for id, session := range m.sessions {
		session.mu.Lock()
		idle := now.Sub(session.lastActive) >= m.cfg.IdleTTL
		detached := !session.detachedAt.IsZero() && now.Sub(session.detachedAt) >= m.cfg.DetachTTL
		running := session.pending != nil
		session.mu.Unlock()
		if (!running && idle) || detached {
			ids = append(ids, id)
		}
	}
	m.mu.Unlock()
	for _, id := range ids {
		_ = m.Close(id)
	}
}

func buildCommand(command, token string) string {
	escaped := strings.ReplaceAll(command, "'", "'\\''")
	return "printf -v __agent_cmd '%s' '" + escaped + "'; eval \"$__agent_cmd\"; __agent_status=$?; printf '\\036AGENT_END_" + token + ":%d\\037' \"$__agent_status\"\n"
}
func shellArguments(shell string) []string {
	if strings.Contains(filepath.Base(shell), "bash") {
		return []string{"--noprofile", "--norc", "--noediting", "-i"}
	}
	return []string{"-i"}
}
func cleanupStartedShell(cmd *exec.Cmd, ptmx *os.File) {
	_ = ptmx.Close()
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
}
func mergeEnvironment(primary, defaults map[string]string) []string {
	values := make(map[string]string)
	for _, item := range os.Environ() {
		if index := strings.IndexByte(item, '='); index > 0 {
			values[item[:index]] = item[index+1:]
		}
	}
	for key, value := range defaults {
		values[key] = value
	}
	for key, value := range primary {
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
func randomToken() string {
	buffer := make([]byte, 18)
	_, _ = rand.Read(buffer)
	return base64.RawURLEncoding.EncodeToString(buffer)
}
func randomID(prefix string) string { return prefix + "-" + randomToken() }
func terminateProcess(pid int, grace time.Duration, done <-chan struct{}) {
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
