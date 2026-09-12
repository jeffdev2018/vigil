package service

// Pure unit tests: no database.

import (
	"fmt"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func TestNormalizeSearchText(t *testing.T) {
	cases := map[string]string{
		"Sécurité DÉPLOIEMENT":             "securite deploiement",
		"cœur Æther Straße Øre Łódź Đorđe": "coeur aether strasse ore lodz dorde",
		"ﬁn de l’été":                      "fin de l’ete",
		"été":                            "ete",
		"生产环境部署流程":                         " 生产 产环 环境 境部 部署 署流 流程 ",
		"用 Go":                             " 用  go",
		"v2部署":                             "v2 部署 ",
		"デプロイ手順":                           " デプ プロ ロイ イ手 手順 ",
		"ｶ":                                " カ ",
		"한국어":                              " 한국 국어 ",
		"billing_webhook_secret_name v2.14.3 sup-1042 ops@example.com /api/x": "billing_webhook_secret_name v2.14.3 sup-1042 ops@example.com /api/x",
	}
	for in, want := range cases {
		if got := NormalizeSearchText(in); got != want {
			t.Errorf("NormalizeSearchText(%q) = %q, want %q", in, got, want)
		}
	}
}

func items(q BrainQuery) []string {
	out := make([]string, 0, len(q.Items))
	for _, it := range q.Items {
		out = append(out, it.Kind+":"+it.Text)
	}
	return out
}

func excluded(q BrainQuery) []string {
	out := make([]string, 0, len(q.Excluded))
	for _, it := range q.Excluded {
		out = append(out, it.Kind+":"+it.Text)
	}
	return out
}

func TestParseBrainQuery(t *testing.T) {
	cases := []struct {
		in       string
		items    string
		excluded string
		strict   bool
	}{
		{"comment est-ce qu'on déploie le vendredi ?", "prefix:deploi prefix:vendredi", "", false},
		{"How do we deploy on Fridays?", "prefix:deploy prefix:friday", "", false},
		{`"release tag" -friday`, "phrase:release tag", "word:friday", true},
		{`-"old way" deploying OR tags`, "prefix:deploy prefix:tags", "phrase:old way", true},
		{"tag 1042 v2", "word:tag word:1042 word:v2", "", false},
		{"nations créations déploiements", "prefix:nation prefix:creation prefix:deploi", "", false},
		{"部署流程", "word:部署 word:署流 word:流程", "", false},
		{`"部署流程"`, "phrase:部署 署流 流程", "", true},
		{"billing_webhook_secret_name v2.14.3. sup-1042", "phrase:billing_webhook_secret_name phrase:v2.14.3 phrase:sup-1042", "", false},
		{"the of", "word:the word:of", "", false},
		{"securite, deploiement!", "prefix:securit prefix:deploi", "", false},
		{"-部署流程 printer", "prefix:print", "phrase:部署 署流 流程", true},
		{"printer printers", "prefix:print prefix:printer", "", false},
		{"   ", "", "", false},
		{`"`, "", "", false},
	}
	for _, c := range cases {
		q := ParseBrainQuery(c.in)
		if got := strings.Join(items(q), " "); got != c.items {
			t.Errorf("ParseBrainQuery(%q) items = %q, want %q", c.in, got, c.items)
		}
		if got := strings.Join(excluded(q), " "); got != c.excluded {
			t.Errorf("ParseBrainQuery(%q) excluded = %q, want %q", c.in, got, c.excluded)
		}
		if q.Strict != c.strict {
			t.Errorf("ParseBrainQuery(%q) strict = %v, want %v", c.in, q.Strict, c.strict)
		}
	}
}

// A prefix or word item reaches to_tsquery verbatim, so it must never carry
// tsquery syntax, whatever was typed.
func TestParseBrainQueryNeverInjectsTsquerySyntax(t *testing.T) {
	q := ParseBrainQuery(`foo:* & bar | !baz <-> (qux) 'quoted' a:B -x:* -!y "z & w" ab\c d<e`)
	for _, it := range append(append([]BrainQueryItem{}, q.Items...), q.Excluded...) {
		if it.Kind == BrainItemPhrase {
			continue
		}
		for _, r := range it.Text {
			if !unicode.IsLetter(r) && !unicode.IsNumber(r) {
				t.Errorf("%s item %q carries %q", it.Kind, it.Text, r)
			}
		}
	}
}

func TestParseBrainQueryCaps(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "word%dx -skip%dx ", i, i)
	}
	q := ParseBrainQuery(b.String())
	if len(q.Items) != brainQueryMaxItems || len(q.Excluded) != brainQueryMaxExcluded {
		t.Fatalf("caps = %d items, %d exclusions; want %d and %d", len(q.Items), len(q.Excluded), brainQueryMaxItems, brainQueryMaxExcluded)
	}
}

func TestBrainSnippetMarksOriginalText(t *testing.T) {
	cases := []struct {
		body, query string
		want        []string
	}{
		{"La sécurité du déploiement passe par la revue.", "securite deploiement", []string{"<mark>sécurité</mark>", "<mark>déploiement</mark>"}},
		{"我们的生产环境部署流程很简单", "部署", []string{"生产环境<mark>部署</mark>流程"}},
		{"我们的生产环境部署流程很简单", `"部署流程"`, []string{"<mark>部署流程</mark>很简单"}},
		{"Deployments happen on Tuesday", "deploy", []string{"<mark>Deployments</mark> happen"}},
		{"set billing_webhook_secret_name first", "billing_webhook_secret_name", []string{"set <mark>billing_webhook_secret_name</mark> first"}},
		{"<script>deploy</script>", "deploy", []string{"<script><mark>deploy</mark></script>"}},
		{"a\n\n   b   deploy", "deploy", []string{"a b <mark>deploy</mark>"}},
	}
	for _, c := range cases {
		got := BrainSnippet(c.body, ParseBrainQuery(c.query))
		for _, want := range c.want {
			if !strings.Contains(got, want) {
				t.Errorf("BrainSnippet(%q, %q) = %q, want it to contain %q", c.body, c.query, got, want)
			}
		}
	}
}

func TestBrainSnippetWindow(t *testing.T) {
	filler := strings.Repeat("lorem ipsum dolor ", 40) // 720 runes
	body := filler + "the rollback needs the release tag " + filler

	got := BrainSnippet(body, ParseBrainQuery("rollback release"))
	if !strings.HasPrefix(got, "… ") || !strings.HasSuffix(got, " …") {
		t.Errorf("cut snippet = %q, want … on both sides", got)
	}
	if !strings.Contains(got, "<mark>rollback</mark>") || !strings.Contains(got, "<mark>release</mark>") {
		t.Errorf("snippet %q lost the matches", got)
	}
	plain := strings.NewReplacer("<mark>", "", "</mark>", "").Replace(got)
	if n := utf8.RuneCountInString(plain); n > brainSnippetRunes+4 {
		t.Errorf("snippet is %d runes, want about %d", n, brainSnippetRunes)
	}

	// No match (a vector-only hit): the head of the passage.
	head := BrainSnippet(body, ParseBrainQuery("zzz"))
	if !strings.HasPrefix(head, "lorem ipsum") || strings.Contains(head, "<mark>") || !strings.HasSuffix(head, " …") {
		t.Errorf("head snippet = %q", head)
	}
	if got := BrainSnippet("", ParseBrainQuery("x")); got != "" {
		t.Errorf("empty body snippet = %q", got)
	}
}
