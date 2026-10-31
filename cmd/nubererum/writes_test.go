package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWithWriteRetryRetriesOnlyWritesThatCannotHaveArrived(t *testing.T) {
	previous := writeRetryDelays
	writeRetryDelays = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	t.Cleanup(func() { writeRetryDelays = previous })

	attempts := 0
	_, err := withWriteRetry(func() ([]byte, error) {
		attempts++
		if attempts < 3 {
			return nil, errors.New("HTTP/1.1 409 Conflict Things-Response: OutdatedAncestor")
		}
		return []byte("ok"), nil
	})
	if err != nil || attempts != 3 {
		t.Fatalf("attempts = %d, err %v; want success on the third try", attempts, err)
	}

	attempts = 0
	_, err = withWriteRetry(func() ([]byte, error) {
		attempts++
		return nil, errors.New("Post https://cloud.culturedcode.com: i/o timeout")
	})
	if err == nil || attempts != 1 {
		t.Fatalf("timeout retried %d times; it may have been written", attempts)
	}

	if !retryable(errors.New(`dial tcp: lookup cloud.culturedcode.com on 127.0.0.11:53: server misbehaving`)) {
		t.Fatal("DNS failure not retryable")
	}
}

func TestWritesRunOneAtATime(t *testing.T) {
	var running, maxRunning int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = withWriteRetry(func() ([]byte, error) {
				mu.Lock()
				running++
				maxRunning = max(maxRunning, running)
				mu.Unlock()
				time.Sleep(5 * time.Millisecond)
				mu.Lock()
				running--
				mu.Unlock()
				return nil, nil
			})
		}()
	}
	wg.Wait()
	if maxRunning != 1 {
		t.Fatalf("%d writes ran at once", maxRunning)
	}
}

func TestMoveKeepsScheduleAndTakesInboxToAnytime(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	cli := filepath.Join(dir, "things-cli")
	script := "#!/bin/sh\ncat >> " + log + "\necho >> " + log + "\necho '{\"status\":\"ok\",\"results\":[]}'\n"
	if err := os.WriteFile(cli, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("THINGS_CLOUD_CLI", cli)
	t.Setenv("APP_READ_ONLY", "")
	previousModels := models
	built := false
	models = newModelCache(func() (map[string]any, error) {
		if built {
			select {}
		}
		built = true
		today := map[string]any{"uuid": "TaskTodayyyyyyyyyyyyyy1", "title": "Call", "views": []string{"today", "anytime"}, "project": "ProjOldHousessssssssss1"}
		inbox := map[string]any{"uuid": "TaskInboxxxxxxxxxxxxxx1", "title": "Idea", "views": []string{"inbox"}}
		return map[string]any{
			"tasks":    []map[string]any{today, inbox},
			"lists":    map[string][]map[string]any{"today": {today}, "anytime": {today}, "inbox": {inbox}},
			"projects": []map[string]any{{"uuid": "ProjMoveggggggggggggggg", "title": "Relocation", "area": "AreaPrivateeeeeeeeeee1"}, {"uuid": "ProjOldHousessssssssss1", "title": "Old House"}},
			"areas":    []map[string]any{{"uuid": "AreaPrivateeeeeeeeeee1", "title": "Private"}},
			"headings": []map[string]any{{"uuid": "HeadKuechhhhhhhhhhhhhh1", "title": "Küche", "project": "ProjMoveggggggggggggggg", "index": 100}},
		}, nil
	})
	sessionMu.Lock()
	previousSession := session
	session = sessionState{Email: "user@example.com", Password: "secret"}
	sessionMu.Unlock()
	t.Cleanup(func() {
		models = previousModels
		sessionMu.Lock()
		session = previousSession
		sessionMu.Unlock()
	})

	request := func(body string) int {
		req := httptestRequest(http.MethodPost, "/api/move", body)
		recorder := httptest.NewRecorder()
		apiHandler(recorder, req)
		return recorder.Code
	}
	if code := request(`{"uuids":["TaskTodayyyyyyyyyyyyyy1","TaskInboxxxxxxxxxxxxxx1"],"project":"ProjMoveggggggggggggggg"}`); code != http.StatusOK {
		t.Fatalf("move = %d", code)
	}
	calls, _ := os.ReadFile(log)
	want := `[{"cmd":"move","project":"ProjMoveggggggggggggggg","uuid":"TaskTodayyyyyyyyyyyyyy1"},{"cmd":"move-to-project","project":"ProjMoveggggggggggggggg","uuid":"TaskInboxxxxxxxxxxxxxx1"}]`
	if !strings.Contains(string(calls), want) {
		t.Fatalf("batch = %s\nwant %s", calls, want)
	}
	models.mu.Lock()
	lists := models.model["lists"].(map[string][]map[string]any)
	models.mu.Unlock()
	if len(lists["today"]) != 1 || lists["today"][0]["projectTitle"] != "Relocation" || len(lists["inbox"]) != 0 || len(lists["anytime"]) != 2 {
		t.Fatalf("cached lists = %v", lists)
	}
	if code := request(`{"uuids":["TaskTodayyyyyyyyyyyyyy1"],"heading":"HeadKuechhhhhhhhhhhhhh1"}`); code != http.StatusOK {
		t.Fatalf("move to heading = %d", code)
	}
	calls, _ = os.ReadFile(log)
	if !strings.Contains(string(calls), `{"cmd":"move","heading":"HeadKuechhhhhhhhhhhhhh1","uuid":"TaskTodayyyyyyyyyyyyyy1"}`) {
		t.Fatalf("heading move batch = %s", calls)
	}
	models.mu.Lock()
	for _, task := range models.model["tasks"].([]map[string]any) {
		if task["uuid"] == "TaskTodayyyyyyyyyyyyyy1" && (task["headingTitle"] != "Küche" || task["projectTitle"] != "Relocation" || task["areaTitle"] != "Private") {
			models.mu.Unlock()
			t.Fatalf("task under heading = %v", task)
		}
	}
	models.mu.Unlock()
	if code := request(`{"uuids":["TaskTodayyyyyyyyyyyyyy1"],"when":"someday"}`); code != http.StatusOK {
		t.Fatalf("move to someday = %d", code)
	}
	for _, bad := range []string{
		`{"uuids":["TaskTodayyyyyyyyyyyyyy1"]}`,
		`{"uuids":["TaskTodayyyyyyyyyyyyyy1"],"when":"inbox"}`,
		`{"uuids":["TaskTodayyyyyyyyyyyyyy1"],"project":"ProjMoveggggggggggggggg","when":"today"}`,
	} {
		if code := request(bad); code != http.StatusBadRequest {
			t.Fatalf("%s = %d, want 400", bad, code)
		}
	}
	if code := request(`{"uuids":["TaskTodayyyyyyyyyyyyyy1"],"project":"ProjGoneeeeeeeeeeeeeee1"}`); code != http.StatusConflict {
		t.Fatalf("unknown project = %d, want 409", code)
	}
}

func httptestRequest(method, path, body string) *http.Request {
	request, _ := http.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("content-type", "application/json")
	return request
}

func TestWriteRefusedOnRepeatingTemplateExplainsItself(t *testing.T) {
	refusal := statusError{Status: http.StatusBadGateway, Message: "complete task: Task7 write preflight: target 9aBcD is a repeating template; only its title, notes and trash can be changed"}
	attempts := 0
	_, err := withWriteRetry(func() ([]byte, error) { attempts++; return nil, refusal })
	var status statusError
	if !errors.As(err, &status) || status.Status != http.StatusConflict || !strings.Contains(status.Message, "repeating to-do") || attempts != 1 {
		t.Fatalf("err = %#v after %d attempts, want one attempt and a 409 that explains the template", err, attempts)
	}
	other := statusError{Status: http.StatusBadGateway, Message: "complete task: something else"}
	if _, err := withWriteRetry(func() ([]byte, error) { return nil, other }); err != other {
		t.Fatalf("other errors changed: %#v", err)
	}
}
