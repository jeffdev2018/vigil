package service

// Brain search text (JEF-412): one normalization for everything the Brain
// indexes and everything a person types, so accents, ligatures and CJK
// sub-words compare the same way on both sides. The database has neither
// unaccent nor pg_bigm, so PostgreSQL's 'simple' parser only ever sees text
// this file already folded.

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Brain query item kinds, each compiled to its own tsquery constructor in
// SearchBrainNotes.
const (
	BrainItemPrefix = "prefix" // to_tsquery(stem || ':*')
	BrainItemWord   = "word"   // to_tsquery(text)
	BrainItemPhrase = "phrase" // phraseto_tsquery(text)
)

const (
	brainQueryMaxItems    = 24
	brainQueryMaxExcluded = 8
	brainPrefixMinRunes   = 4
	brainSnippetRunes     = 200
)

// searchFoldSpecial covers the letters NFKD leaves whole.
var searchFoldSpecial = map[rune]string{
	'œ': "oe", 'Œ': "oe", 'æ': "ae", 'Æ': "ae", 'ß': "ss", 'ẞ': "ss",
	'ø': "o", 'Ø': "o", 'ł': "l", 'Ł': "l", 'đ': "d", 'Đ': "d",
}

// isCJK reports whether r belongs to a script written without spaces
// between words, which the index splits into overlapping bigrams.
func isCJK(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) || r == 'ー'
}

// writeFoldedRune appends the folded form of r: compatibility decomposition,
// combining marks dropped, ligatures spelled out, lower case. CJK runes keep
// their composed form (NFKC) so kana voicing and Hangul syllables survive.
func writeFoldedRune(b *strings.Builder, r rune) {
	if r < utf8.RuneSelf {
		b.WriteRune(unicode.ToLower(r))
		return
	}
	if isCJK(r) {
		b.WriteString(norm.NFKC.String(string(r)))
		return
	}
	if s, ok := searchFoldSpecial[r]; ok {
		b.WriteString(s)
		return
	}
	for _, d := range norm.NFKD.String(string(r)) {
		if unicode.Is(unicode.Mn, d) {
			continue
		}
		if s, ok := searchFoldSpecial[d]; ok {
			b.WriteString(s)
			continue
		}
		b.WriteRune(unicode.ToLower(d))
	}
}

// NormalizeSearchText folds s for the Brain index and queries. Runs of CJK
// characters become space-separated overlapping bigrams (a lone character
// stays itself), surrounded by spaces; everything else keeps its punctuation
// so identifiers, versions, paths and e-mails tokenize as the 'simple'
// parser always did.
func NormalizeSearchText(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 8)
	var run []rune
	flush := func() {
		if len(run) == 0 {
			return
		}
		b.WriteByte(' ')
		if len(run) == 1 {
			b.WriteRune(run[0])
		}
		for i := 0; i+1 < len(run); i++ {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteRune(run[i])
			b.WriteRune(run[i+1])
		}
		b.WriteByte(' ')
		run = run[:0]
	}
	var one strings.Builder
	for _, r := range s {
		if isCJK(r) {
			one.Reset()
			writeFoldedRune(&one, r)
			run = append(run, []rune(one.String())...)
			continue
		}
		flush()
		writeFoldedRune(&b, r)
	}
	flush()
	return b.String()
}

// BrainQueryItem is one compiled piece of a Brain query. Text is normalized;
// for prefix and word items it holds letters and digits only, so it can
// never inject tsquery syntax.
type BrainQueryItem struct {
	Text string
	Kind string
	// terms is the phrase as folded tokens (CJK one rune each), for marking
	// matches in a snippet.
	terms []string
}

// BrainQuery is what a person typed, compiled for SearchBrainNotes.
type BrainQuery struct {
	Items    []BrainQueryItem
	Excluded []BrainQueryItem
	// Strict requires every present item: set by a quoted phrase or an
	// exclusion, the two signs that the person is being precise.
	Strict bool
}

// brainStopwords are dropped from unquoted queries, so a question asked in
// plain French or English is not required to contain "comment" or "the".
var brainStopwords = func() map[string]bool {
	words := strings.Fields(`
a an the and of to in on at by for with from as is are was were be been being
do does did how what when where why which who whom whose that this these those
it its i we you he she they me my our your their them us there here can could
should would will shall may might must not no so if than then into about also
just have has had get
le la les l un une des du de d et ou au aux en dans sur pour par avec sans ce
cet cette ces c ca cela qui que qu quoi quel quelle quels quelles est sont etre
ete ai as avons avez ont on il ils elle elles je j tu nous vous me m te t se s
son sa ses leur leurs mon ma mes ton ta tes notre nos votre vos y ne n pas plus
comment pourquoi quand est-ce faut mais donc car si`)
	set := make(map[string]bool, len(words))
	for _, w := range words {
		set[w] = true
	}
	return set
}()

var brainStemSuffixes = []string{"ements", "ations", "ement", "ation", "ing", "ed", "es", "er", "ez", "e", "s"}

// brainStem strips one light English or French suffix, the longest that
// leaves a stem of at least four runes.
func brainStem(w string) string {
	n := utf8.RuneCountInString(w)
	for _, suffix := range brainStemSuffixes {
		if strings.HasSuffix(w, suffix) && n-len(suffix) >= brainPrefixMinRunes {
			return w[:len(w)-len(suffix)]
		}
	}
	return w
}

func isLetterOrDigit(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) }

// trimQueryToken drops the sentence punctuation around a typed word but not
// the characters that make an identifier (a leading slash, a dot inside).
func trimQueryToken(s string) string {
	s = strings.TrimLeft(s, `([{"'«“‘¿¡`)
	return strings.TrimRight(s, `.,;:!?)]}"'»”’…。，、；：！？`)
}

// classifyQueryToken turns one folded, whitespace-free token into an item.
func classifyQueryToken(tok string) BrainQueryItem {
	plain, latin, letter := true, true, false
	for _, r := range tok {
		if !isLetterOrDigit(r) {
			plain = false
			break
		}
		if unicode.IsLetter(r) {
			letter = true
			if !unicode.Is(unicode.Latin, r) {
				latin = false
			}
		} else if r > '9' || r < '0' {
			latin = false
		}
	}
	if !plain {
		return BrainQueryItem{Text: tok, Kind: BrainItemPhrase, terms: searchTerms(tok)}
	}
	if latin && letter {
		if stem := brainStem(tok); utf8.RuneCountInString(stem) >= brainPrefixMinRunes {
			return BrainQueryItem{Text: stem, Kind: BrainItemPrefix}
		}
	}
	return BrainQueryItem{Text: tok, Kind: BrainItemWord}
}

// phraseItem compiles a quoted phrase, or a CJK run that must stay together.
func phraseItem(raw string) (BrainQueryItem, bool) {
	folded := strings.Join(strings.Fields(NormalizeSearchText(raw)), " ")
	terms := searchTerms(raw)
	if folded == "" || len(terms) == 0 {
		return BrainQueryItem{}, false
	}
	return BrainQueryItem{Text: folded, Kind: BrainItemPhrase, terms: terms}, true
}

// ParseBrainQuery compiles a typed query: "double quotes" keep a phrase, a
// leading - excludes a word or a phrase, OR is accepted and ignored (items
// are alternatives already), stopwords go unless the query is nothing else.
func ParseBrainQuery(q string) BrainQuery {
	type candidate struct {
		item     BrainQueryItem
		stopword bool
	}
	var positives []candidate
	var out BrainQuery
	seen := map[string]bool{}
	addExcluded := func(it BrainQueryItem) {
		key := it.Kind + "\x00" + it.Text
		if len(out.Excluded) < brainQueryMaxExcluded && !seen["-"+key] {
			seen["-"+key] = true
			out.Excluded = append(out.Excluded, it)
		}
	}

	rs := []rune(q)
	for i := 0; i < len(rs); {
		if unicode.IsSpace(rs[i]) {
			i++
			continue
		}
		negated := false
		if rs[i] == '-' && i+1 < len(rs) && (rs[i+1] == '"' || isLetterOrDigit(rs[i+1])) {
			negated = true
			i++
		}
		if rs[i] == '"' {
			end := i + 1
			for end < len(rs) && rs[end] != '"' {
				end++
			}
			raw := string(rs[i+1 : end])
			i = end + 1
			if it, ok := phraseItem(raw); ok {
				if negated {
					addExcluded(it)
				} else {
					positives = append(positives, candidate{item: it})
					out.Strict = true
				}
			}
			continue
		}
		end := i
		for end < len(rs) && !unicode.IsSpace(rs[end]) {
			end++
		}
		raw := string(rs[i:end])
		i = end
		if negated {
			// An exclusion is exact: never a prefix, which would also drop
			// every note with a longer word.
			folded := strings.Fields(NormalizeSearchText(raw))
			if len(folded) != 1 {
				if it, ok := phraseItem(trimQueryToken(raw)); ok {
					addExcluded(it)
				}
				continue
			}
			tok := trimQueryToken(folded[0])
			if tok == "" {
				continue
			}
			it := classifyQueryToken(tok)
			if it.Kind != BrainItemPhrase {
				it = BrainQueryItem{Text: tok, Kind: BrainItemWord}
			}
			addExcluded(it)
			continue
		}
		for _, part := range strings.Fields(NormalizeSearchText(raw)) {
			// French elision (l'équipe, qu'on) and English contractions are
			// separate words; an apostrophe is never part of an identifier.
			for _, piece := range strings.FieldsFunc(part, func(r rune) bool { return r == '\'' || r == '’' }) {
				tok := trimQueryToken(piece)
				if tok == "" || tok == "or" || !strings.ContainsFunc(tok, isLetterOrDigit) {
					continue
				}
				positives = append(positives, candidate{item: classifyQueryToken(tok), stopword: brainStopwords[tok]})
			}
		}
	}

	allStop := true
	for _, c := range positives {
		if !c.stopword {
			allStop = false
			break
		}
	}
	for _, c := range positives {
		if c.stopword && !allStop {
			continue
		}
		key := c.item.Kind + "\x00" + c.item.Text
		if seen[key] {
			continue
		}
		seen[key] = true
		if len(out.Items) < brainQueryMaxItems {
			out.Items = append(out.Items, c.item)
		}
	}
	if len(out.Excluded) > 0 {
		out.Strict = true
	}
	return out
}

// searchToken is one word of an original text: a run of letters, digits and
// combining marks, or a single CJK rune, with its byte span and folded form.
type searchToken struct {
	start, end int
	folded     string
	cjk        bool
}

func tokenizeForSearch(s string) []searchToken {
	var tokens []searchToken
	var b strings.Builder
	start := -1
	closeWord := func(end int) {
		if start >= 0 {
			tokens = append(tokens, searchToken{start: start, end: end, folded: b.String()})
			start = -1
			b.Reset()
		}
	}
	for i, r := range s {
		switch {
		case isCJK(r):
			closeWord(i)
			var one strings.Builder
			writeFoldedRune(&one, r)
			tokens = append(tokens, searchToken{start: i, end: i + utf8.RuneLen(r), folded: one.String(), cjk: true})
		case isLetterOrDigit(r) || unicode.Is(unicode.Mn, r):
			if start < 0 {
				start = i
			}
			writeFoldedRune(&b, r)
		default:
			closeWord(i)
		}
	}
	closeWord(len(s))
	return tokens
}

func searchTerms(s string) []string {
	tokens := tokenizeForSearch(s)
	terms := make([]string, 0, len(tokens))
	for _, t := range tokens {
		if t.folded != "" {
			terms = append(terms, t.folded)
		}
	}
	return terms
}

// BrainSnippet is about 200 runes of body around its densest cluster of
// query matches, the matches wrapped in <mark></mark>, " … " where the text
// was cut, whitespace collapsed. Matching runs on folded text and marks the
// original characters: an accent-free query marks the accented word, a CJK
// bigram marks its two characters, a prefix marks the whole word. Nothing is
// HTML-escaped: the contract is "<mark> only, clients escape"
// (packages/core/brain/snippet.ts, apps/mobile/lib/brain-display.ts).
func BrainSnippet(body string, q BrainQuery) string {
	tokens := tokenizeForSearch(body)
	type span struct{ start, end int }
	var marks []span
	for i, t := range tokens {
		for _, it := range q.Items {
			switch it.Kind {
			case BrainItemPrefix:
				if !t.cjk && strings.HasPrefix(t.folded, it.Text) {
					marks = append(marks, span{t.start, t.end})
				}
			case BrainItemWord:
				if t.folded == it.Text {
					marks = append(marks, span{t.start, t.end})
				} else if t.cjk && i+1 < len(tokens) && tokens[i+1].cjk && tokens[i+1].start == t.end && t.folded+tokens[i+1].folded == it.Text {
					marks = append(marks, span{t.start, tokens[i+1].end})
				}
			case BrainItemPhrase:
				n := len(it.terms)
				if n == 0 || i+n > len(tokens) {
					continue
				}
				match := true
				for j, term := range it.terms {
					if tokens[i+j].folded != term {
						match = false
						break
					}
				}
				if match {
					marks = append(marks, span{t.start, tokens[i+n-1].end})
				}
			}
		}
	}

	// Rune offsets for the window arithmetic.
	byteAt := make([]int, 0, len(body)+1)
	for i := range body {
		byteAt = append(byteAt, i)
	}
	total := len(byteAt)
	byteAt = append(byteAt, len(body))
	runeOf := func(b int) int {
		lo, hi := 0, total
		for lo < hi {
			mid := (lo + hi) / 2
			if byteAt[mid] < b {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		return lo
	}

	// Merge overlapping marks, in text order.
	for i := 1; i < len(marks); i++ {
		for j := i; j > 0 && marks[j].start < marks[j-1].start; j-- {
			marks[j], marks[j-1] = marks[j-1], marks[j]
		}
	}
	merged := marks[:0]
	for _, m := range marks {
		if n := len(merged); n > 0 && m.start <= merged[n-1].end {
			merged[n-1].end = max(merged[n-1].end, m.end)
			continue
		}
		merged = append(merged, m)
	}
	marks = merged

	winStart, winEnd := 0, min(total, brainSnippetRunes)
	if len(marks) > 0 {
		best, bestCount := 0, 0
		for i := range marks {
			count := 0
			for j := i; j < len(marks) && runeOf(marks[j].end)-runeOf(marks[i].start) <= brainSnippetRunes; j++ {
				count++
			}
			if count > bestCount {
				best, bestCount = i, count
			}
		}
		first := runeOf(marks[best].start)
		last := runeOf(marks[best+bestCount-1].end)
		winStart = max(0, first-(brainSnippetRunes-(last-first))/2)
		winEnd = min(total, winStart+brainSnippetRunes)
		winStart = max(0, winEnd-brainSnippetRunes)
		// Start and end on a word boundary when one is close and no match
		// is lost to it.
		runes := []rune(body)
		if winStart > 0 {
			for k := winStart; k < min(first, winStart+20); k++ {
				if unicode.IsSpace(runes[k]) {
					winStart = k + 1
					break
				}
			}
		}
		if winEnd < total {
			for k := winEnd; k > max(last, winEnd-20); k-- {
				if unicode.IsSpace(runes[k-1]) {
					winEnd = k - 1
					break
				}
			}
		}
	}

	from, to := byteAt[winStart], byteAt[winEnd]
	var b strings.Builder
	space := false
	write := func(s string) {
		for _, r := range s {
			if unicode.IsSpace(r) {
				space = true
				continue
			}
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
		}
	}
	cursor := from
	for _, m := range marks {
		s, e := max(m.start, from), min(m.end, to)
		if s >= e {
			continue
		}
		write(body[cursor:s])
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteString("<mark>")
		write(body[s:e])
		b.WriteString("</mark>")
		cursor = e
	}
	write(body[cursor:to])

	out := b.String()
	if out == "" {
		return ""
	}
	if winStart > 0 {
		out = "… " + out
	}
	if winEnd < total {
		out += " …"
	}
	return out
}
