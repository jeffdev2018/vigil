package service

import (
	"reflect"
	"testing"
)

func TestParseCitedNoteIDs(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "single citation",
			text: "See [Deploy procedure](mention://note/11111111-2222-3333-4444-555555555555) for the steps.",
			want: []string{"11111111-2222-3333-4444-555555555555"},
		},
		{
			name: "multiple citations, first-seen order",
			text: "As [A](mention://note/11111111-2222-3333-4444-555555555555) and [B](mention://note/22222222-2222-3333-4444-555555555555) say.",
			want: []string{"11111111-2222-3333-4444-555555555555", "22222222-2222-3333-4444-555555555555"},
		},
		{
			name: "duplicates collapse to one",
			text: "[A](mention://note/11111111-2222-3333-4444-555555555555) ... again [A](mention://note/11111111-2222-3333-4444-555555555555)",
			want: []string{"11111111-2222-3333-4444-555555555555"},
		},
		{
			name: "malformed uuid is ignored",
			text: "[A](mention://note/not-a-uuid) and [B](mention://note/11111111-2222-3333-4444)",
			want: nil,
		},
		{
			name: "uppercase id is normalized to lowercase",
			text: "[A](mention://note/AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE)",
			want: []string{"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"},
		},
		{
			name: "citation inside a fenced code block is ignored",
			text: "Real one [A](mention://note/11111111-2222-3333-4444-555555555555).\n```\nExample: [B](mention://note/22222222-2222-3333-4444-555555555555)\n```\n",
			want: []string{"11111111-2222-3333-4444-555555555555"},
		},
		{
			name: "citation inside a tilde-fenced code block is ignored",
			text: "~~~\n[B](mention://note/22222222-2222-3333-4444-555555555555)\n~~~\n[A](mention://note/11111111-2222-3333-4444-555555555555)",
			want: []string{"11111111-2222-3333-4444-555555555555"},
		},
		{
			name: "no citations at all",
			text: "Nothing to see here.",
			want: nil,
		},
		{
			name: "wrong scheme is not a citation",
			text: "[A](https://example.com/note/11111111-2222-3333-4444-555555555555)",
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseCitedNoteIDs(tt.text)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseCitedNoteIDs(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}
