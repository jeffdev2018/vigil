package service

import "encoding/json"

// Standup and retro (K34): a workspace policy in workspace.settings.
//
//	"standup": {"enabled": true, "blocked_hours": 24, "weekly_retro": true}
//
// The local clock comes from the morning briefing timezone when set.
type Standup struct {
	Enabled      bool `json:"enabled"`
	BlockedHours int  `json:"blocked_hours"`
	WeeklyRetro  bool `json:"weekly_retro"`
}

func StandupSettings(settings []byte) Standup {
	if len(settings) == 0 {
		return Standup{BlockedHours: 24}
	}
	var s struct {
		Standup *Standup `json:"standup"`
	}
	if err := json.Unmarshal(settings, &s); err != nil || s.Standup == nil {
		return Standup{BlockedHours: 24}
	}
	out := *s.Standup
	if out.BlockedHours <= 0 {
		out.BlockedHours = 24
	}
	return out
}

// WorkspaceTimezone is the briefing timezone when configured, else UTC.
func WorkspaceTimezone(settings []byte) string {
	if len(settings) == 0 {
		return "UTC"
	}
	var s struct {
		Briefing *struct {
			Timezone string `json:"timezone"`
		} `json:"morning_briefing"`
	}
	if err := json.Unmarshal(settings, &s); err != nil || s.Briefing == nil || s.Briefing.Timezone == "" {
		return "UTC"
	}
	return s.Briefing.Timezone
}
