package client

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"time"
)

// Alarm represents alarm payload.
type Alarm struct {
	ID             string  `json:"id"`
	Enabled        bool    `json:"enabled"`
	Time           string  `json:"time"`
	DaysOfWeek     []int   `json:"daysOfWeek"`
	Vibration      bool    `json:"vibration"`
	Sound          *string `json:"sound,omitempty"`
	Next           bool    `json:"next"`
	State          string  `json:"state"`
	DismissedUntil string  `json:"dismissedUntil,omitempty"`
	SnoozedUntil   string  `json:"snoozedUntil,omitempty"`
	OneOff         bool    `json:"oneOff"`
	Stale          bool    `json:"stale"`
}

func (c *Client) ListAlarms(ctx context.Context) ([]Alarm, error) {
	if err := c.requireUser(ctx); err != nil {
		return nil, err
	}
	path := fmt.Sprintf("%s/v2/users/%s/routines", appBaseURL, c.UserID)
	var res struct {
		Settings struct {
			Routines     []routineAlarmGroup `json:"routines"`
			OneOffAlarms []routineAlarmEntry `json:"oneOffAlarms"`
		} `json:"settings"`
		State struct {
			NextAlarm struct {
				AlarmID string `json:"alarmId"`
			} `json:"nextAlarm"`
		} `json:"state"`
	}
	if err := c.do(ctx, http.MethodGet, path, nil, nil, &res); err != nil {
		return nil, err
	}

	nextID := res.State.NextAlarm.AlarmID
	alarms := make([]Alarm, 0, len(res.Settings.OneOffAlarms))
	for _, routine := range res.Settings.Routines {
		for _, alarm := range routine.Alarms {
			alarms = append(alarms, buildAlarm(alarm, append([]int(nil), routine.Days...), nextID, false))
		}
		for _, alarm := range routine.Override.Alarms {
			alarms = append(alarms, buildAlarm(alarm, append([]int(nil), routine.Days...), nextID, false))
		}
	}
	for _, alarm := range res.Settings.OneOffAlarms {
		alarms = append(alarms, buildAlarm(alarm, []int{}, nextID, true))
	}

	sort.SliceStable(alarms, func(i, j int) bool {
		wi := alarmOrderWeight(alarms[i])
		wj := alarmOrderWeight(alarms[j])
		if wi != wj {
			return wi < wj
		}
		if alarms[i].Time != alarms[j].Time {
			return alarms[i].Time < alarms[j].Time
		}
		return alarms[i].ID < alarms[j].ID
	})

	return alarms, nil
}

type routineAlarmGroup struct {
	Days     []int               `json:"days"`
	Alarms   []routineAlarmEntry `json:"alarms"`
	Override struct {
		Alarms []routineAlarmEntry `json:"alarms"`
	} `json:"override"`
}

type routineAlarmEntry struct {
	AlarmID              string `json:"alarmId"`
	Enabled              bool   `json:"enabled"`
	DisabledIndividually bool   `json:"disabledIndividually"`
	Time                 string `json:"time"`
	DismissedUntil       string `json:"dismissedUntil"`
	SnoozedUntil         string `json:"snoozedUntil"`
	TimeWithOffset       struct {
		Time string `json:"time"`
	} `json:"timeWithOffset"`
	Settings struct {
		Vibration struct {
			Enabled bool `json:"enabled"`
		} `json:"vibration"`
	} `json:"settings"`
}

func buildAlarm(entry routineAlarmEntry, days []int, nextID string, oneOff bool) Alarm {
	a := Alarm{
		ID:             entry.AlarmID,
		Time:           entry.Time,
		DaysOfWeek:     days,
		Vibration:      entry.Settings.Vibration.Enabled,
		DismissedUntil: normalizeAlarmTimestamp(entry.DismissedUntil),
		SnoozedUntil:   normalizeAlarmTimestamp(entry.SnoozedUntil),
		OneOff:         oneOff,
	}
	if a.Time == "" {
		a.Time = entry.TimeWithOffset.Time
	}
	if oneOff {
		a.Enabled = entry.Enabled
	} else {
		a.Enabled = !entry.DisabledIndividually
	}
	a.Next = a.ID == nextID
	a.Stale = isStalePastOneOff(a)
	a.State = alarmState(a)
	return a
}

func alarmState(a Alarm) string {
	switch {
	case a.Next:
		return "next"
	case !a.Enabled:
		return "disabled"
	case a.SnoozedUntil != "":
		return "snoozed"
	case a.DismissedUntil != "":
		return "dismissed"
	default:
		return "enabled"
	}
}

func isStalePastOneOff(a Alarm) bool {
	if !a.OneOff || a.Next || !a.Enabled {
		return false
	}
	now := time.Now()
	for _, ts := range []string{a.DismissedUntil, a.SnoozedUntil} {
		if t, ok := parseAlarmTimestamp(ts); ok && !t.After(now) {
			return true
		}
	}
	return false
}

func normalizeAlarmTimestamp(ts string) string {
	t, ok := parseAlarmTimestamp(ts)
	if !ok {
		return ""
	}
	if t.Year() == 1970 {
		return ""
	}
	return t.Format(time.RFC3339)
}

func parseAlarmTimestamp(ts string) (time.Time, bool) {
	if ts == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func alarmOrderWeight(a Alarm) int {
	switch a.State {
	case "next":
		return 0
	case "enabled", "snoozed":
		return 1
	case "disabled":
		return 2
	case "dismissed":
		return 3
	default:
		return 4
	}
}

func (c *Client) CreateAlarm(ctx context.Context, alarm Alarm) (*Alarm, error) {
	if err := c.requireUser(ctx); err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/users/%s/alarms", c.UserID)
	var res struct {
		Alarm Alarm `json:"alarm"`
	}
	if err := c.do(ctx, http.MethodPost, path, nil, alarm, &res); err != nil {
		return nil, err
	}
	return &res.Alarm, nil
}

func (c *Client) UpdateAlarm(ctx context.Context, alarmID string, patch map[string]any) (*Alarm, error) {
	if err := c.requireUser(ctx); err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/users/%s/alarms/%s", c.UserID, alarmID)
	var res struct {
		Alarm Alarm `json:"alarm"`
	}
	if err := c.do(ctx, http.MethodPatch, path, nil, patch, &res); err != nil {
		return nil, err
	}
	return &res.Alarm, nil
}

func (c *Client) DeleteAlarm(ctx context.Context, alarmID string) error {
	if err := c.requireUser(ctx); err != nil {
		return err
	}
	path := fmt.Sprintf("/users/%s/alarms/%s", c.UserID, alarmID)
	return c.do(ctx, http.MethodDelete, path, nil, nil, nil)
}
