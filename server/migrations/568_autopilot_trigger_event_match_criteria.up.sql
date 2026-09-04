-- Natural-language event routing: a webhook trigger may describe, in the
-- user's own words, which events should wake it; the server asks the LLM
-- before starting a run. Empty means the static event_filters alone decide.
ALTER TABLE autopilot_trigger ADD COLUMN IF NOT EXISTS event_match_criteria TEXT NOT NULL DEFAULT '';
