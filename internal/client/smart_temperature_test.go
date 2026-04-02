package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestResolveSmartTemperatureStage(t *testing.T) {
	t.Parallel()

	cases := map[string]SmartTemperatureStage{
		"bedtime":           SmartTemperatureStageBedtime,
		"bedTimeLevel":      SmartTemperatureStageBedtime,
		"night":             SmartTemperatureStageNight,
		"early":             SmartTemperatureStageNight,
		"initialSleepLevel": SmartTemperatureStageNight,
		"dawn":              SmartTemperatureStageDawn,
		"late":              SmartTemperatureStageDawn,
		"finalSleepLevel":   SmartTemperatureStageDawn,
	}

	for input, want := range cases {
		got, err := ResolveSmartTemperatureStage(input)
		if err != nil {
			t.Fatalf("resolve %q: %v", input, err)
		}
		if got != want {
			t.Fatalf("resolve %q = %q, want %q", input, got, want)
		}
	}

	if _, err := ResolveSmartTemperatureStage("middle"); err == nil {
		t.Fatalf("expected invalid stage error")
	}
}

func TestGetSmartTemperatureStatusUsesAppEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/users/me", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"user":{"userId":"uid-123"}}`))
	})
	mux.HandleFunc("/v1/users/uid-123/temperature", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"currentLevel":5,"currentState":{"type":"smart:initial"},"smart":{"bedTimeLevel":10,"initialSleepLevel":-20,"finalSleepLevel":0}}`))
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

	st, err := c.GetSmartTemperatureStatus(context.Background())
	if err != nil {
		t.Fatalf("get smart temperature status: %v", err)
	}
	if c.UserID != "uid-123" {
		t.Fatalf("expected user id populated, got %q", c.UserID)
	}
	if st.CurrentState.Type != "smart:initial" || st.CurrentLevel != 5 {
		t.Fatalf("unexpected status: %+v", st)
	}
	if st.Smart == nil {
		t.Fatalf("expected smart settings")
	}
	if st.Smart.BedTimeLevel != 10 || st.Smart.InitialSleepLevel != -20 || st.Smart.FinalSleepLevel != 0 {
		t.Fatalf("unexpected smart settings: %+v", st.Smart)
	}
}

func TestSetSmartTemperatureLevelPreservesOtherStages(t *testing.T) {
	var putBody struct {
		Smart SmartTemperatureSettings `json:"smart"`
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/users/uid-123/temperature", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"currentLevel":5,"currentState":{"type":"smart:bedtime"},"smart":{"bedTimeLevel":10,"initialSleepLevel":-20,"finalSleepLevel":0}}`))
		case http.MethodPut:
			if err := json.NewDecoder(r.Body).Decode(&putBody); err != nil {
				t.Fatalf("decode PUT body: %v", err)
			}
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

	settings, err := c.SetSmartTemperatureLevel(context.Background(), SmartTemperatureStageNight, -30)
	if err != nil {
		t.Fatalf("set smart temperature level: %v", err)
	}
	if settings.InitialSleepLevel != -30 {
		t.Fatalf("returned settings not updated: %+v", settings)
	}
	if putBody.Smart.BedTimeLevel != 10 || putBody.Smart.InitialSleepLevel != -30 || putBody.Smart.FinalSleepLevel != 0 {
		t.Fatalf("unexpected PUT body: %+v", putBody.Smart)
	}
}
