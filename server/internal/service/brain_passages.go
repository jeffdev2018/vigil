package service

// Brain passages (JEF-412): a note is indexed as the sections a person
// reads rather than as one blob, so a search answers with the section that
// holds the answer and a vector describes a few paragraphs, not the head of
// a long note.

import (
	"regexp"
	"strings"
	"unicode"
)

// BrainChunkerVersion identifies how SplitNotePassages cuts. Bumping it
// re-indexes every note on the next search or backfill.
const BrainChunkerVersion = 1

const (
	brainPassageMaxRunes     = 1200
	brainPassageOverlapRunes = 150
	brainPassageMaxCount     = 40
)

// NotePassage is one indexed section of a note. Heading is the path of the
// headings above it, joined by " › ".
type NotePassage struct {
	Ordinal int
	Heading string
	Body    string
}

var atxHeading = regexp.MustCompile(`^ {0,3}(#{1,6})[ \t]+(.*?)(?:[ \t]+#+)?[ \t]*$`)

// codeFence returns the fence a line opens (``` or ~~~, at any length), or "".
func codeFence(trimmed string) string {
	for _, marker := range []string{"```", "~~~"} {
		if strings.HasPrefix(trimmed, marker) {
			n := len(trimmed) - len(strings.TrimLeft(trimmed, marker[:1]))
			return trimmed[:n]
		}
	}
	return ""
}

// SplitNotePassages cuts a note's Markdown into passages: one per ATX
// heading section (a # inside a code block is never a heading), long
// sections cut at paragraphs, then sentence ends or spaces, at most 1 200
// runes, each window after the first starting with the tail of the one
// before. Sections without a body are skipped; a note always has at least
// one passage, so a title-only note stays findable. The title is not cut:
// the index stores it beside every passage.
func SplitNotePassages(_ string, content string) []NotePassage {
	var passages []NotePassage
	var stack [6]string
	heading, fence := "", ""
	var body []string
	emit := func() {
		text := strings.TrimSpace(strings.Join(body, "\n"))
		body = body[:0]
		if text == "" {
			return
		}
		for _, chunk := range splitPassageBody(text) {
			if len(passages) == brainPassageMaxCount {
				return
			}
			passages = append(passages, NotePassage{Ordinal: len(passages) + 1, Heading: heading, Body: chunk})
		}
	}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimLeft(line, " \t")
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, fence[:1]+" \t") == "" {
				fence = ""
			}
			body = append(body, line)
			continue
		}
		if f := codeFence(trimmed); f != "" {
			fence = f
			body = append(body, line)
			continue
		}
		if m := atxHeading.FindStringSubmatch(line); m != nil && strings.TrimSpace(m[2]) != "" {
			emit()
			level := len(m[1])
			stack[level-1] = strings.TrimSpace(m[2])
			for i := level; i < len(stack); i++ {
				stack[i] = ""
			}
			path := make([]string, 0, level)
			for _, h := range stack[:level] {
				if h != "" {
					path = append(path, h)
				}
			}
			heading = strings.Join(path, " › ")
			continue
		}
		body = append(body, line)
	}
	emit()
	if len(passages) == 0 {
		return []NotePassage{{Ordinal: 1}}
	}
	return passages
}

// splitPassageBody windows a section longer than brainPassageMaxRunes.
func splitPassageBody(text string) []string {
	runes := []rune(text)
	if len(runes) <= brainPassageMaxRunes {
		return []string{text}
	}
	var chunks []string
	for start := 0; start < len(runes) && len(chunks) < brainPassageMaxCount; {
		end := min(start+brainPassageMaxRunes, len(runes))
		if end < len(runes) {
			end = passageCut(runes, start, end)
		}
		if chunk := strings.TrimSpace(string(runes[start:end])); chunk != "" {
			chunks = append(chunks, chunk)
		}
		if end >= len(runes) {
			break
		}
		next := end
		for k := max(start+1, end-brainPassageOverlapRunes); k < end; k++ {
			if !unicode.IsSpace(runes[k]) && (unicode.IsSpace(runes[k-1]) || isCJK(runes[k])) {
				next = k
				break
			}
		}
		start = next
	}
	return chunks
}

// passageCut picks where a window ending at end is cut: the last paragraph
// break, else the last sentence end, else the last space, in the second half
// of the window; else a hard cut.
func passageCut(runes []rune, start, end int) int {
	floor := start + brainPassageMaxRunes/2
	for k := end; k > floor; k-- {
		if runes[k-1] == '\n' && runes[k-2] == '\n' {
			return k
		}
	}
	for k := end; k > floor; k-- {
		switch runes[k-1] {
		case '。', '！', '？':
			return k
		case '.', '!', '?':
			if unicode.IsSpace(runes[k]) {
				return k
			}
		}
	}
	for k := end; k > floor; k-- {
		if unicode.IsSpace(runes[k-1]) {
			return k
		}
	}
	return end
}
