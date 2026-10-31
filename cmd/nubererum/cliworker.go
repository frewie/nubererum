package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// A things-cli run takes about 1.7 s, nearly all of it signing in and
// opening a connection to Things Cloud, plus reading and writing the state
// cache. The worker keeps one `things-cli serve` process running instead:
// it signs in once, keeps the connection and the state, and runs the
// commands one after another, so they never race on the cache file.
// THINGS_CLI_WORKER=0 turns it off; a things-cli without serve mode falls
// back to one process per command.

// workerTimeout ends a command that does not answer; the worker is then
// restarted.
var workerTimeout = 2 * time.Minute

// workerStartTimeout is how long the worker may take to report ready.
const workerStartTimeout = 15 * time.Second

type cliWorker struct {
	mu          sync.Mutex
	proc        *exec.Cmd
	stdin       io.WriteCloser
	lines       chan []byte
	done        chan struct{}
	stderr      *tailBuffer
	cli         string
	email       string
	password    string
	unsupported string // the CLI path that has no serve mode
}

var worker = &cliWorker{}

type workerResponse struct {
	OK     bool   `json:"ok"`
	Output string `json:"output"`
	Error  string `json:"error"`
}

func workerEnabled() bool {
	return os.Getenv("THINGS_CLI_WORKER") != "0"
}

// call runs one command in the worker. used is false when the worker cannot
// run it; the caller then starts a single things-cli process.
func (w *cliWorker) call(cli, email, password string, input []byte, offline bool, args []string) (out []byte, err error, used bool) {
	if !workerEnabled() {
		return nil, nil, false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.unsupported == cli {
		return nil, nil, false
	}
	if w.proc != nil && (w.cli != cli || w.email != email || w.password != password) {
		w.stopLocked()
	}
	if w.proc == nil {
		if !w.startLocked(cli, email, password) {
			return nil, nil, false
		}
	}
	request, _ := json.Marshal(map[string]any{"args": args, "input": json.RawMessage(orNull(input)), "offline": offline})
	if _, err := w.stdin.Write(append(request, '\n')); err != nil {
		w.stopLocked()
		return nil, statusError{Status: http.StatusBadGateway, Message: "things-cli stopped: " + err.Error()}, true
	}
	timer := time.NewTimer(workerTimeout)
	defer timer.Stop()
	select {
	case line, ok := <-w.lines:
		if !ok {
			message := strings.TrimSpace(w.stderr.String())
			w.stopLocked()
			return nil, statusError{Status: http.StatusBadGateway, Message: "things-cli stopped: " + message}, true
		}
		var response workerResponse
		if err := json.Unmarshal(line, &response); err != nil {
			w.stopLocked()
			return nil, statusError{Status: http.StatusBadGateway, Message: "things-cli returned an invalid answer"}, true
		}
		if !response.OK {
			return nil, statusError{Status: http.StatusBadGateway, Message: response.Error}, true
		}
		return []byte(response.Output), nil, true
	case <-timer.C:
		w.stopLocked()
		return nil, statusError{Status: http.StatusGatewayTimeout, Message: "things-cli did not answer in time"}, true
	}
}

// startLocked starts `things-cli serve` and waits for it to report ready.
func (w *cliWorker) startLocked(cli, email, password string) bool {
	proc := exec.Command(cli, "serve")
	proc.Env = cliEnv(email, password)
	stdin, err := proc.StdinPipe()
	if err != nil {
		return false
	}
	stdout, err := proc.StdoutPipe()
	if err != nil {
		return false
	}
	stderr := &tailBuffer{max: 4096}
	proc.Stderr = stderr
	if err := proc.Start(); err != nil {
		return false
	}
	lines, done := make(chan []byte, 1), make(chan struct{})
	go func() {
		reader := bufio.NewReaderSize(stdout, 1<<20)
		for {
			line, err := reader.ReadBytes('\n')
			if len(bytes.TrimSpace(line)) > 0 {
				select {
				case lines <- line:
				case <-done:
				}
			}
			if err != nil {
				close(lines)
				_ = proc.Wait()
				return
			}
		}
	}()
	ready := false
	select {
	case line, ok := <-lines:
		var hello struct {
			Ready    bool `json:"ready"`
			Protocol int  `json:"protocol"`
		}
		ready = ok && json.Unmarshal(line, &hello) == nil && hello.Ready && hello.Protocol == 1
	case <-time.After(workerStartTimeout):
	}
	if !ready {
		close(done)
		_ = stdin.Close()
		_ = proc.Process.Kill()
		w.unsupported = cli
		slog.Info("things-cli has no serve mode; starting it once per command", "cli", cliName(cli))
		return false
	}
	w.proc, w.stdin, w.lines, w.done, w.stderr = proc, stdin, lines, done, stderr
	w.cli, w.email, w.password = cli, email, password
	return true
}

func (w *cliWorker) stopLocked() {
	if w.proc == nil {
		return
	}
	close(w.done)
	_ = w.stdin.Close()
	_ = w.proc.Process.Kill()
	w.proc, w.stdin, w.lines, w.done, w.stderr = nil, nil, nil, nil, nil
}

// slowCommand is the duration from which things-cli commands are logged.
const slowCommand = 2 * time.Second

func (w *cliWorker) running() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.proc != nil
}

// stop ends the worker, e.g. when the Things account changes.
func (w *cliWorker) stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stopLocked()
}

func orNull(input []byte) []byte {
	if len(bytes.TrimSpace(input)) == 0 {
		return []byte("null")
	}
	return input
}

// execCLI runs one things-cli command, in the worker when possible.
// Offline reads may use the state of the previous command.
func execCLI(email, password string, input []byte, offline bool, args ...string) ([]byte, error) {
	if email == "" || password == "" {
		return nil, statusError{Status: http.StatusUnauthorized, Message: "Sign in with your Things Cloud email and password first."}
	}
	cli := cliPath()
	if cli == "" {
		return nil, statusError{Status: http.StatusFailedDependency, Message: "things-cli is missing. Install it or set THINGS_CLOUD_CLI."}
	}
	started := time.Now()
	defer func() {
		if duration := time.Since(started); duration >= slowCommand {
			slog.Info("slow things-cli command", "command", args[0], "duration_ms", duration.Milliseconds(), "worker", workerEnabled() && worker.running())
		}
	}()
	if out, err, used := worker.call(cli, email, password, input, offline, args); used {
		return out, err
	}
	cmd := exec.Command(cli, args...)
	cmd.Env = cliEnv(email, password)
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	out, err := cmd.Output()
	if err != nil {
		message := err.Error()
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			message = string(exitErr.Stderr)
		}
		return nil, statusError{Status: http.StatusBadGateway, Message: strings.TrimSpace(message)}
	}
	return out, nil
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	mu   sync.Mutex
	max  int
	data []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	if len(b.data) > b.max {
		b.data = b.data[len(b.data)-b.max:]
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}
