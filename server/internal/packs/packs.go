// Package packs holds the pack format (OS plan, vague B): a manifest on top
// of a workspace transfer bundle, authored in YAML, shipped embedded as the
// built-in catalogue and accepted as an upload. FORMAT.md is the contract
// authors follow; this package parses the file and validates the manifest,
// the handler package validates and applies the bundle.
package packs

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed builtin/*/pack.yaml
var builtinFS embed.FS

// FormatVersion is the transfer format version a pack file implies.
const FormatVersion = 2

// MaxFileBytes caps an uploaded pack file.
const MaxFileBytes = 4 << 20

var (
	idPattern      = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	versionPattern = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)$`)
	// Domains mirror the "beyond dev" waves: wave 1 helpdesk + ops, wave 2
	// support, sales, marketing, leadership, research, wave 3 hr, finance, legal.
	Domains           = []string{"helpdesk", "ops", "support", "sales", "marketing", "leadership", "research", "hr", "finance", "legal", "engineering", "other"}
	PrerequisiteKinds = []string{"native_runtime", "runtime", "integration", "channel"}
)

// Manifest is the `pack:` block.
type Manifest struct {
	ID                 string         `json:"id" yaml:"id"`
	Version            string         `json:"version" yaml:"version"`
	Title              string         `json:"title" yaml:"title"`
	Summary            string         `json:"summary" yaml:"summary"`
	Description        string         `json:"description" yaml:"description"`
	Domain             string         `json:"domain" yaml:"domain"`
	Wave               int            `json:"wave" yaml:"wave"`
	Author             string         `json:"author" yaml:"author"`
	License            string         `json:"license" yaml:"license"`
	Tags               []string       `json:"tags" yaml:"tags"`
	WorksWithoutAgents bool           `json:"works_without_agents" yaml:"works_without_agents"`
	Metric             Metric         `json:"metric" yaml:"metric"`
	Prerequisites      []Prerequisite `json:"prerequisites" yaml:"prerequisites"`
	Changelog          []ChangelogRow `json:"changelog" yaml:"changelog"`
}

type Metric struct {
	Label       string `json:"label" yaml:"label"`
	Description string `json:"description" yaml:"description"`
	Hint        string `json:"hint" yaml:"hint"`
}

type Prerequisite struct {
	Kind     string `json:"kind" yaml:"kind"`
	Name     string `json:"name" yaml:"name"`
	Optional bool   `json:"optional" yaml:"optional"`
	Note     string `json:"note" yaml:"note"`
}

type ChangelogRow struct {
	Version string `json:"version" yaml:"version"`
	Note    string `json:"note" yaml:"note"`
}

// Pack is a parsed file: the manifest plus the bundle body as JSON, in the
// transfer bundle's own field names, ready for the handler to decode.
type Pack struct {
	Manifest Manifest
	// Body is the JSON object of every top-level key except `pack`.
	Body json.RawMessage
	// Builtin marks a catalogue pack (as opposed to an upload).
	Builtin bool
	// Source is the raw YAML, kept for export and for the run ledger digest.
	Source []byte
}

// Parse decodes one pack file and validates its manifest.
func Parse(raw []byte) (*Pack, error) {
	if len(raw) > MaxFileBytes {
		return nil, fmt.Errorf("pack file is larger than %d bytes", MaxFileBytes)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("pack: not valid YAML: %w", err)
	}
	if doc == nil {
		return nil, errors.New("pack: empty file")
	}
	manifestRaw, ok := doc["pack"]
	if !ok {
		return nil, errors.New("pack: missing the `pack:` manifest block")
	}
	manifestJSON, err := json.Marshal(manifestRaw)
	if err != nil {
		return nil, fmt.Errorf("pack: manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(manifestJSON, &m); err != nil {
		return nil, fmt.Errorf("pack: manifest: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	delete(doc, "pack")
	body, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("pack: body: %w", err)
	}
	return &Pack{Manifest: m, Body: body, Source: raw}, nil
}

// Validate checks the manifest fields FORMAT.md requires.
func (m Manifest) Validate() error {
	var problems []string
	if !idPattern.MatchString(m.ID) || len(m.ID) > 64 {
		problems = append(problems, "id must be kebab-case (letters, digits, dashes), at most 64 characters")
	}
	if !versionPattern.MatchString(m.Version) {
		problems = append(problems, "version must be semver (major.minor.patch)")
	}
	if strings.TrimSpace(m.Title) == "" || len(m.Title) > 120 {
		problems = append(problems, "title is required (at most 120 characters)")
	}
	if strings.TrimSpace(m.Summary) == "" || len(m.Summary) > 300 {
		problems = append(problems, "summary is required (at most 300 characters)")
	}
	if !contains(Domains, m.Domain) {
		problems = append(problems, "domain must be one of "+strings.Join(Domains, ", "))
	}
	if m.Wave < 0 || m.Wave > 3 {
		problems = append(problems, "wave must be 1, 2 or 3 (0 = unset)")
	}
	if strings.TrimSpace(m.Metric.Label) == "" || strings.TrimSpace(m.Metric.Description) == "" {
		problems = append(problems, "metric.label and metric.description are required")
	}
	for i, p := range m.Prerequisites {
		if !contains(PrerequisiteKinds, p.Kind) || strings.TrimSpace(p.Name) == "" {
			problems = append(problems, fmt.Sprintf("prerequisites[%d]: kind must be one of %s and name is required", i, strings.Join(PrerequisiteKinds, ", ")))
		}
	}
	for i, row := range m.Changelog {
		if !versionPattern.MatchString(row.Version) {
			problems = append(problems, fmt.Sprintf("changelog[%d]: version must be semver", i))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("pack manifest: %s", strings.Join(problems, "; "))
	}
	return nil
}

// CompareVersions orders two semver strings: -1, 0 or 1.
func CompareVersions(a, b string) int {
	pa, pb := versionPattern.FindStringSubmatch(a), versionPattern.FindStringSubmatch(b)
	if pa == nil || pb == nil {
		return strings.Compare(a, b)
	}
	for i := 1; i <= 3; i++ {
		var x, y int
		fmt.Sscanf(pa[i], "%d", &x)
		fmt.Sscanf(pb[i], "%d", &y)
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// Builtin returns the embedded catalogue, sorted by wave then title.
func Builtin() ([]*Pack, error) {
	var out []*Pack
	err := fs.WalkDir(builtinFS, "builtin", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, "/pack.yaml") {
			return err
		}
		raw, err := builtinFS.ReadFile(path)
		if err != nil {
			return err
		}
		p, err := Parse(raw)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		dir := strings.TrimSuffix(strings.TrimPrefix(path, "builtin/"), "/pack.yaml")
		if dir != p.Manifest.ID {
			return fmt.Errorf("%s: directory %q must match pack id %q", path, dir, p.Manifest.ID)
		}
		p.Builtin = true
		out = append(out, p)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Manifest.Wave != out[j].Manifest.Wave {
			return out[i].Manifest.Wave < out[j].Manifest.Wave
		}
		return out[i].Manifest.Title < out[j].Manifest.Title
	})
	return out, nil
}

// FindBuiltin returns one catalogue pack by id.
func FindBuiltin(id string) (*Pack, error) {
	all, err := Builtin()
	if err != nil {
		return nil, err
	}
	for _, p := range all {
		if p.Manifest.ID == id {
			return p, nil
		}
	}
	return nil, fmt.Errorf("pack %q is not in the catalogue", id)
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
