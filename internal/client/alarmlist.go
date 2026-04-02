package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
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

type routinesPayload struct {
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

func (c *Client) ListAlarms(ctx context.Context) ([]Alarm, error) {
	if err := c.requireUser(ctx); err != nil {
		return nil, err
	}
	res, err := c.fetchRoutinesPayload(ctx)
	if err != nil {
		return nil, err
	}
	return alarmsFromPayload(res), nil
}

func (c *Client) fetchRoutinesPayload(ctx context.Context) (*routinesPayload, error) {
	path := fmt.Sprintf("%s/v2/users/%s/routines", appBaseURL, c.UserID)
	var res routinesPayload
	if err := c.do(ctx, http.MethodGet, path, nil, nil, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func alarmsFromPayload(res *routinesPayload) []Alarm {
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
	return alarms
}

type routineAlarmGroup struct {
	ID       string              `json:"id"`
	Days     []int               `json:"days"`
	Alarms   []routineAlarmEntry `json:"alarms"`
	Override struct {
		Alarms []routineAlarmEntry `json:"alarms"`
	} `json:"override"`
}

type routineAlarmEntry struct {
	AlarmID              string `json:"alarmId"`
	EnabledSince         string `json:"enabledSince,omitempty"`
	Enabled              bool   `json:"enabled"`
	DisabledIndividually bool   `json:"disabledIndividually"`
	Time                 string `json:"time"`
	DismissedUntil       string `json:"dismissedUntil"`
	SnoozedUntil         string `json:"snoozedUntil"`
	TimeWithOffset       struct {
		Time string `json:"time"`
	} `json:"timeWithOffset"`
	Settings alarmSettings `json:"settings"`
}

type alarmSettings struct {
	Vibration alarmVibrationSettings `json:"vibration"`
	Thermal   alarmThermalSettings   `json:"thermal"`
}

type alarmVibrationSettings struct {
	Enabled    bool   `json:"enabled"`
	PowerLevel int    `json:"powerLevel,omitempty"`
	Pattern    string `json:"pattern,omitempty"`
}

type alarmThermalSettings struct {
	Enabled bool `json:"enabled"`
	Level   int  `json:"level,omitempty"`
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

type alarmMatch struct {
	Alarm       Alarm
	OneOff      bool
	OneOffIndex int
	RoutineID   string
}

func (c *Client) ToggleAlarm(ctx context.Context, selector string) (*Alarm, error) {
	if err := c.requireUser(ctx); err != nil {
		return nil, err
	}
	payload, err := c.fetchRoutinesPayload(ctx)
	if err != nil {
		return nil, err
	}
	match, err := resolveAlarmSelector(payload, selector)
	if err != nil {
		return nil, err
	}
	if !match.OneOff {
		return nil, fmt.Errorf("toggle currently supports one-off alarms only")
	}

	entry := &payload.Settings.OneOffAlarms[match.OneOffIndex]
	entry.Enabled = !entry.Enabled
	if entry.Enabled && entry.EnabledSince == "" {
		entry.EnabledSince = time.Now().UTC().Format(time.RFC3339)
	}

	path := fmt.Sprintf("%s/v2/users/%s/routines", appBaseURL, c.UserID)
	query := url.Values{"ignoreDeviceErrors": {"false"}}
	body := struct {
		OneOffAlarms []routineAlarmEntry `json:"oneOffAlarms"`
	}{
		OneOffAlarms: payload.Settings.OneOffAlarms,
	}
	if err := c.do(ctx, http.MethodPut, path, query, body, nil); err != nil {
		return nil, err
	}

	updated, err := c.ListAlarms(ctx)
	if err != nil {
		return nil, err
	}
	for _, alarm := range updated {
		if alarm.OneOff == match.OneOff && alarm.Time == match.Alarm.Time {
			return &alarm, nil
		}
	}
	for _, alarm := range updated {
		if alarm.ID == match.Alarm.ID {
			return &alarm, nil
		}
	}
	return nil, fmt.Errorf("toggled alarm %s but could not refetch an updated match", match.Alarm.ID)
}

func resolveAlarmSelector(payload *routinesPayload, selector string) (*alarmMatch, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return nil, fmt.Errorf("alarm selector is required")
	}

	candidates := buildAlarmMatches(payload)

	if selector == "next" {
		nextID := payload.State.NextAlarm.AlarmID
		if nextID == "" {
			return nil, fmt.Errorf("no next alarm found")
		}
		for _, candidate := range candidates {
			if candidate.Alarm.ID == nextID {
				c := candidate
				return &c, nil
			}
		}
		return nil, fmt.Errorf("next alarm %s not found in routines payload", nextID)
	}

	for _, candidate := range candidates {
		if candidate.Alarm.ID == selector {
			c := candidate
			return &c, nil
		}
	}

	normalizedSelector, ok := normalizeAlarmTime(selector)
	if !ok {
		return nil, fmt.Errorf("selector %q must be 'next', exact HH:MM[:SS], or a full alarm id", selector)
	}
	matches := []alarmMatch{}
	for _, candidate := range candidates {
		candidateTime, ok := normalizeAlarmTime(candidate.Alarm.Time)
		if ok && candidateTime == normalizedSelector {
			matches = append(matches, candidate)
		}
	}
	if len(matches) == 1 {
		c := matches[0]
		return &c, nil
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("selector %q matched multiple alarms; use the full alarm id", selector)
	}
	return nil, fmt.Errorf("no alarm matched selector %q", selector)
}

func buildAlarmMatches(payload *routinesPayload) []alarmMatch {
	nextID := payload.State.NextAlarm.AlarmID
	matches := []alarmMatch{}
	for _, routine := range payload.Settings.Routines {
		for _, entry := range routine.Alarms {
			matches = append(matches, alarmMatch{
				Alarm:     buildAlarm(entry, append([]int(nil), routine.Days...), nextID, false),
				RoutineID: routine.ID,
			})
		}
		for _, entry := range routine.Override.Alarms {
			matches = append(matches, alarmMatch{
				Alarm:     buildAlarm(entry, append([]int(nil), routine.Days...), nextID, false),
				RoutineID: routine.ID,
			})
		}
	}
	for idx, entry := range payload.Settings.OneOffAlarms {
		matches = append(matches, alarmMatch{
			Alarm:       buildAlarm(entry, []int{}, nextID, true),
			OneOff:      true,
			OneOffIndex: idx,
		})
	}
	return matches
}

func normalizeAlarmTime(s string) (string, bool) {
	s = strings.TrimSpace(s)
	switch strings.Count(s, ":") {
	case 1:
		return s + ":00", true
	case 2:
		return s, true
	default:
		return "", false
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
