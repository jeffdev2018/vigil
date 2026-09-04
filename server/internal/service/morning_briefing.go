package service

import "encoding/json"

// Morning briefing (K30): a workspace policy in workspace.settings.
//
//	"morning_briefing": {"enabled": true, "hour": 8, "timezone": "Europe/Paris"}
type MorningBriefing struct {
	Enabled  bool   `json:"enabled"`
	Hour     int    `json:"hour"`
	Timezone string `json:"timezone"`
	// Channels (K64) are the chats the digest is also delivered to, one
	// active installation of the given type per workspace.
	Channels []BriefingChannel `json:"channels,omitempty"`
}

// BriefingChannel names one delivery target: a channel type ("slack",
// "telegram", …) and the platform chat id to post into.
type BriefingChannel struct {
	Type   string `json:"type"`
	ChatID string `json:"chat_id"`
}

// MorningBriefingSettings reads the policy; ok is false when it is absent
// or disabled. An hour outside 0-23 falls back to 8.
func MorningBriefingSettings(settings []byte) (MorningBriefing, bool) {
	if len(settings) == 0 {
		return MorningBriefing{}, false
	}
	var s struct {
		Briefing *MorningBriefing `json:"morning_briefing"`
	}
	if err := json.Unmarshal(settings, &s); err != nil || s.Briefing == nil || !s.Briefing.Enabled {
		return MorningBriefing{}, false
	}
	b := *s.Briefing
	if b.Hour < 0 || b.Hour > 23 {
		b.Hour = 8
	}
	if b.Timezone == "" {
		b.Timezone = "UTC"
	}
	channels := make([]BriefingChannel, 0, len(b.Channels))
	for _, c := range b.Channels {
		if c.Type != "" && c.ChatID != "" {
			channels = append(channels, c)
		}
	}
	b.Channels = channels
	return b, true
}

// CrossReview (K15) is the workspace policy in workspace.settings:
//
//	"cross_review": {"enabled": true, "opt_out_project_ids": ["…"]}
//
// Absent means enabled everywhere.
type CrossReview struct {
	Enabled          bool     `json:"enabled"`
	OptOutProjectIDs []string `json:"opt_out_project_ids"`
}

func CrossReviewSettings(settings []byte) CrossReview {
	out := CrossReview{Enabled: true, OptOutProjectIDs: []string{}}
	if len(settings) == 0 {
		return out
	}
	var s struct {
		CrossReview *struct {
			Enabled          *bool    `json:"enabled"`
			OptOutProjectIDs []string `json:"opt_out_project_ids"`
		} `json:"cross_review"`
	}
	if err := json.Unmarshal(settings, &s); err != nil || s.CrossReview == nil {
		return out
	}
	if s.CrossReview.Enabled != nil {
		out.Enabled = *s.CrossReview.Enabled
	}
	for _, id := range s.CrossReview.OptOutProjectIDs {
		if id != "" {
			out.OptOutProjectIDs = append(out.OptOutProjectIDs, id)
		}
	}
	return out
}

// Allows reports whether an issue of projectID (empty when none) gets a
// cross-provider review under this policy.
func (c CrossReview) Allows(projectID string) bool {
	if !c.Enabled {
		return false
	}
	for _, id := range c.OptOutProjectIDs {
		if id == projectID {
			return false
		}
	}
	return true
}
