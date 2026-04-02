package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// mockServer builds a test server that can serve a handful of endpoints the client expects.
func mockServer(t *testing.T) (*httptest.Server, *Client) {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/users/me", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"user":{"userId":"uid-123","currentDevice":{"id":"dev-1"}}}`))
	})

	mux.HandleFunc("/users/uid-123/temperature", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"currentLevel":5,"currentState":{"type":"on"}}`))
			return
		}
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(w, r)
	})

	mux.HandleFunc("/v1/users/uid-123/temperature", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		bodyStr := string(body)
		if !strings.Contains(bodyStr, `"currentState"`) {
			t.Fatalf("unexpected body: %s", string(body))
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		// first call rate limits, second succeeds
		if r.Header.Get("X-Test-Retry") == "done" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"ok":true}`))
			return
		}
		w.WriteHeader(http.StatusTooManyRequests)
	})

	srv := httptest.NewServer(mux)

	// client with pre-set token to skip auth
	c := New("email", "pass", "", "", "")
	c.BaseURL = srv.URL
	c.token = "t"
	c.tokenExp = time.Now().Add(time.Hour)
	c.HTTP = srv.Client()

	return srv, c
}

func TestRequireUserFilledAutomatically(t *testing.T) {
	srv, c := mockServer(t)
	defer srv.Close()

	// UserID empty; GetStatus should fetch it from /users/me
	st, err := c.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if c.UserID != "uid-123" {
		t.Fatalf("expected user id populated, got %s", c.UserID)
	}
	if st.CurrentLevel != 5 || st.CurrentState.Type != "on" {
		t.Fatalf("unexpected status %+v", st)
	}
}

func Test429Retry(t *testing.T) {
	count := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		count++
		if count == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New("email", "pass", "uid", "", "")
	c.BaseURL = srv.URL
	c.token = "t"
	c.tokenExp = time.Now().Add(time.Hour)
	c.HTTP = srv.Client()

	start := time.Now()
	if err := c.do(context.Background(), http.MethodGet, "/ping", nil, nil, nil); err != nil {
		t.Fatalf("do retry: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 attempts, got %d", count)
	}
	if elapsed := time.Since(start); elapsed < 2*time.Second {
		t.Fatalf("expected backoff, got %v", elapsed)
	}
}

func TestTurnOnOffUsesTemperatureStateEndpoint(t *testing.T) {
	powerStates := []string{}
	mux := http.NewServeMux()
	mux.HandleFunc("/users/me", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"user":{"userId":"uid-123","currentDevice":{"id":"dev-1"}}}`))
	})
	mux.HandleFunc("/v1/users/uid-123/temperature", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		bodyStr := string(body)
		switch {
		case strings.Contains(bodyStr, `"type":"smart"`):
			powerStates = append(powerStates, "smart")
		case strings.Contains(bodyStr, `"type":"off"`):
			powerStates = append(powerStates, "off")
		default:
			t.Fatalf("unexpected power state body: %s", bodyStr)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New("email", "pass", "", "", "")
	c.BaseURL = srv.URL
	c.token = "t"
	c.tokenExp = time.Now().Add(time.Hour)
	c.HTTP = srv.Client()

	prevAppBaseURL := appBaseURL
	appBaseURL = srv.URL
	defer func() { appBaseURL = prevAppBaseURL }()

	if err := c.TurnOn(context.Background()); err != nil {
		t.Fatalf("turn on: %v", err)
	}
	if err := c.TurnOff(context.Background()); err != nil {
		t.Fatalf("turn off: %v", err)
	}
	if got := strings.Join(powerStates, ","); got != "smart,off" {
		t.Fatalf("expected smart/off calls, got %q", got)
	}
}

func TestToggleAlarmByTimeUpdatesOneOffAlarm(t *testing.T) {
	enabled := false

	mux := http.NewServeMux()
	mux.HandleFunc("/v2/users/uid-123/routines", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(fmt.Sprintf(`{"settings":{"routines":[],"oneOffAlarms":[{"alarmId":"alarm-1","time":"07:15:00","enabled":%t,"settings":{"vibration":{"enabled":false,"powerLevel":50,"pattern":"INTENSE"},"thermal":{"enabled":true,"level":40}},"dismissedUntil":"1970-01-01T00:00:00Z","snoozedUntil":"1970-01-01T00:00:00Z"}]},"state":{"status":"None","nextAlarm":{}}}`, enabled)))
		case http.MethodPut:
			if got := r.URL.Query().Get("ignoreDeviceErrors"); got != "false" {
				t.Fatalf("ignoreDeviceErrors query = %q", got)
			}
			var body struct {
				OneOffAlarms []struct {
					AlarmID string `json:"alarmId"`
					Enabled bool   `json:"enabled"`
				} `json:"oneOffAlarms"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if len(body.OneOffAlarms) != 1 || body.OneOffAlarms[0].AlarmID != "alarm-1" {
				t.Fatalf("unexpected body: %+v", body)
			}
			enabled = body.OneOffAlarms[0].Enabled
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New("email", "pass", "uid-123", "", "")
	c.token = "t"
	c.tokenExp = time.Now().Add(time.Hour)
	c.HTTP = srv.Client()

	prevAppBaseURL := appBaseURL
	appBaseURL = srv.URL
	defer func() { appBaseURL = prevAppBaseURL }()

	alarm, err := c.ToggleAlarm(context.Background(), "07:15")
	if err != nil {
		t.Fatalf("toggle alarm: %v", err)
	}
	if !enabled {
		t.Fatalf("expected toggle PUT to enable alarm")
	}
	if !alarm.Enabled || alarm.State != "enabled" {
		t.Fatalf("unexpected updated alarm %+v", alarm)
	}
}
