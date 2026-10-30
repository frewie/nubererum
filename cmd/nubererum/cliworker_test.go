package main

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeServeCLI writes a things-cli stand-in with serve mode that logs each
// start and request.
func fakeServeCLI(t *testing.T) (cli string, log func() string) {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "log")
	cli = filepath.Join(dir, "things-cli")
	script := `#!/bin/sh
if [ "$1" != serve ]; then
  echo "single $*" >> "` + logPath + `"
  echo '[{"uuid":"single"}]'
  exit 0
fi
echo "start $THINGS_USERNAME" >> "` + logPath + `"
echo '{"ready":true,"protocol":1}'
while IFS= read -r line; do
  echo "$line" >> "` + logPath + `"
  case "$line" in
    *'"conflict"'*) echo '{"ok":false,"error":"batch write: OutdatedAncestor"}' ;;
    *'"die"'*) echo "crashed" >&2; exit 3 ;;
    *'"hang"'*) sleep 5 ;;
    *) echo '{"ok":true,"output":"[{\"uuid\":\"served\"}]"}' ;;
  esac
done
`
	if err := os.WriteFile(cli, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("THINGS_CLOUD_CLI", cli)
	t.Setenv("THINGS_CLI_WORKER", "1")
	t.Cleanup(worker.stop)
	return cli, func() string {
		data, _ := os.ReadFile(logPath)
		return string(data)
	}
}

func TestCLIWorkerRunsCommandsInOneProcess(t *testing.T) {
	_, log := fakeServeCLI(t)
	run := func(email string, input []byte, offline bool, args ...string) (string, error) {
		out, err := execCLI(email, "pw", input, offline, args...)
		return string(out), err
	}
	if out, err := run("a@example.com", nil, false, "list"); err != nil || out != `[{"uuid":"served"}]` {
		t.Fatalf("list = %q, %v", out, err)
	}
	if _, err := run("a@example.com", nil, true, "today"); err != nil {
		t.Fatal(err)
	}
	if _, err := run("a@example.com", []byte(`[{"cmd":"trash","uuid":"x"}]`), false, "batch"); err != nil {
		t.Fatal(err)
	}
	got := log()
	if strings.Count(got, "start ") != 1 ||
		!strings.Contains(got, `{"args":["today"],"input":null,"offline":true}`) ||
		!strings.Contains(got, `{"args":["batch"],"input":[{"cmd":"trash","uuid":"x"}],"offline":false}`) {
		t.Fatalf("log =\n%s", got)
	}

	// A failed command keeps the worker; its error stays retryable.
	_, err := run("a@example.com", nil, false, "conflict")
	var statusErr statusError
	if !errors.As(err, &statusErr) || statusErr.Status != http.StatusBadGateway || !retryable(err) || strings.Count(log(), "start ") != 1 {
		t.Fatalf("conflict = %v", err)
	}

	// A crash is reported with stderr and not retried; the next command
	// starts a new worker.
	if _, err := run("a@example.com", nil, false, "die"); err == nil || !strings.Contains(err.Error(), "crashed") || retryable(err) {
		t.Fatalf("die = %v", err)
	}
	if _, err := run("a@example.com", nil, false, "list"); err != nil || strings.Count(log(), "start ") != 2 {
		t.Fatalf("after crash: %v, log =\n%s", err, log())
	}

	// Another account gets its own worker.
	if _, err := run("b@example.com", nil, false, "list"); err != nil || !strings.Contains(log(), "start b@example.com") {
		t.Fatalf("other account: %v, log =\n%s", err, log())
	}

	// A command that does not answer ends the worker.
	previous := workerTimeout
	workerTimeout = 200 * time.Millisecond
	t.Cleanup(func() { workerTimeout = previous })
	if _, err := run("b@example.com", nil, false, "hang"); !errors.As(err, &statusErr) || statusErr.Status != http.StatusGatewayTimeout {
		t.Fatalf("hang = %v", err)
	}
	if _, err := run("b@example.com", nil, false, "list"); err != nil || strings.Count(log(), "start ") != 4 {
		t.Fatalf("after timeout: %v, log =\n%s", err, log())
	}
}

func TestCLIWorkerFallsBackWithoutServeMode(t *testing.T) {
	dir := t.TempDir()
	cli := filepath.Join(dir, "things-cli")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\necho \"unknown command: $1\" >&2\n[ \"$1\" = serve ] && exit 1\necho '[]'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("THINGS_CLOUD_CLI", cli)
	t.Setenv("THINGS_CLI_WORKER", "1")
	t.Cleanup(func() {
		worker.mu.Lock()
		worker.unsupported = ""
		worker.mu.Unlock()
	})
	for range 2 {
		if out, err := execCLI("a@example.com", "pw", nil, true, "today"); err != nil || strings.TrimSpace(string(out)) != "[]" {
			t.Fatalf("today = %q, %v", out, err)
		}
	}
	if worker.unsupported != cli {
		t.Fatal("serve mode was not marked unsupported")
	}
}
