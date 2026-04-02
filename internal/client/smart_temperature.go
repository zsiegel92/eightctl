package client

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// SmartTemperatureStage is one of the app-style sleep stages for smart temperature.
type SmartTemperatureStage string

const (
	SmartTemperatureStageBedtime SmartTemperatureStage = "bedtime"
	SmartTemperatureStageNight   SmartTemperatureStage = "night"
	SmartTemperatureStageDawn    SmartTemperatureStage = "dawn"
)

// ResolveSmartTemperatureStage accepts user-facing aliases and returns a canonical stage.
func ResolveSmartTemperatureStage(value string) (SmartTemperatureStage, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.NewReplacer("-", "", "_", "", " ", "").Replace(normalized)
	switch normalized {
	case "bedtime", "bedtimelevel":
		return SmartTemperatureStageBedtime, nil
	case "night", "early", "initial", "initialsleeplevel":
		return SmartTemperatureStageNight, nil
	case "dawn", "late", "final", "finalsleeplevel":
		return SmartTemperatureStageDawn, nil
	default:
		return "", fmt.Errorf("invalid smart temperature stage %q; use bedtime, night, or dawn", value)
	}
}

func (s SmartTemperatureStage) apiField() string {
	switch s {
	case SmartTemperatureStageBedtime:
		return "bedTimeLevel"
	case SmartTemperatureStageNight:
		return "initialSleepLevel"
	case SmartTemperatureStageDawn:
		return "finalSleepLevel"
	default:
		return ""
	}
}

// SmartTemperatureSettings represents the app-style bedtime/night/dawn targets.
type SmartTemperatureSettings struct {
	BedTimeLevel      int `json:"bedTimeLevel"`
	InitialSleepLevel int `json:"initialSleepLevel"`
	FinalSleepLevel   int `json:"finalSleepLevel"`
}

// SmartTemperatureStatus represents the temperature payload including smart stage settings.
type SmartTemperatureStatus struct {
	CurrentLevel int `json:"currentLevel"`
	CurrentState struct {
		Type string `json:"type"`
	} `json:"currentState"`
	Smart *SmartTemperatureSettings `json:"smart"`
}

// GetSmartTemperatureStatus fetches smart sleep-stage temperature settings.
func (c *Client) GetSmartTemperatureStatus(ctx context.Context) (*SmartTemperatureStatus, error) {
	if err := c.requireUser(ctx); err != nil {
		return nil, err
	}
	path := fmt.Sprintf("%s/v1/users/%s/temperature", appBaseURL, c.UserID)
	var res SmartTemperatureStatus
	if err := c.do(ctx, http.MethodGet, path, nil, nil, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// SetSmartTemperatureLevel updates one smart sleep-stage temperature target.
func (c *Client) SetSmartTemperatureLevel(ctx context.Context, stage SmartTemperatureStage, level int) (*SmartTemperatureSettings, error) {
	if err := c.requireUser(ctx); err != nil {
		return nil, err
	}
	if level < -100 || level > 100 {
		return nil, fmt.Errorf("level must be between -100 and 100")
	}

	status, err := c.GetSmartTemperatureStatus(ctx)
	if err != nil {
		return nil, err
	}
	if status.Smart == nil {
		return nil, fmt.Errorf("smart temperature settings not present in response")
	}

	settings := *status.Smart
	switch stage {
	case SmartTemperatureStageBedtime:
		settings.BedTimeLevel = level
	case SmartTemperatureStageNight:
		settings.InitialSleepLevel = level
	case SmartTemperatureStageDawn:
		settings.FinalSleepLevel = level
	default:
		return nil, fmt.Errorf("unsupported smart temperature stage %q", stage)
	}

	path := fmt.Sprintf("%s/v1/users/%s/temperature", appBaseURL, c.UserID)
	body := struct {
		Smart SmartTemperatureSettings `json:"smart"`
	}{
		Smart: settings,
	}
	if err := c.do(ctx, http.MethodPut, path, nil, body, nil); err != nil {
		return nil, err
	}
	return &settings, nil
}
