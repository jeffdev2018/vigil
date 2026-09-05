package service

import "encoding/json"

// Contest (K72) auto mode: which agent outputs get contested before a human
// sees them, and the projects that opt out. Stored under
// workspace.settings["contest"]. Off by default: a contest costs a run.
type Contest struct {
	Targets          map[string]bool `json:"targets"`
	OptOutProjectIDs []string        `json:"opt_out_project_ids"`
}

// ContestTargetTypes lists what can be contested, in the order the UI shows.
var ContestTargetTypes = []string{"task_result", "plan", "triage_verdict", "meeting_summary"}

func ContestSettings(settings []byte) Contest {
	out := Contest{Targets: map[string]bool{}, OptOutProjectIDs: []string{}}
	for _, t := range ContestTargetTypes {
		out.Targets[t] = false
	}
	if len(settings) == 0 {
		return out
	}
	var s struct {
		Contest *struct {
			Targets          map[string]bool `json:"targets"`
			OptOutProjectIDs []string        `json:"opt_out_project_ids"`
		} `json:"contest"`
	}
	if err := json.Unmarshal(settings, &s); err != nil || s.Contest == nil {
		return out
	}
	for _, t := range ContestTargetTypes {
		if v, ok := s.Contest.Targets[t]; ok {
			out.Targets[t] = v
		}
	}
	for _, id := range s.Contest.OptOutProjectIDs {
		if id != "" {
			out.OptOutProjectIDs = append(out.OptOutProjectIDs, id)
		}
	}
	return out
}

// Auto reports whether an output of targetType in projectID (empty when
// none) is contested automatically under this policy.
func (c Contest) Auto(targetType, projectID string) bool {
	if !c.Targets[targetType] {
		return false
	}
	for _, id := range c.OptOutProjectIDs {
		if id == projectID {
			return false
		}
	}
	return true
}
