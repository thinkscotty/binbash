package ai

import (
	"strings"
	"testing"
)

// The custom prompt is appended, not substituted: the built-in instructions --
// above all the comma-separated response format parseTags depends on -- have to
// survive whatever the operator writes.
func TestSystemPromptKeepsBuiltInsAlongsideCustomPrompt(t *testing.T) {
	opts := TagOptions{
		Count:       4,
		Breadth:     "narrow",
		ExtraPrompt: "Every tag must be a single word.",
	}
	got := systemPrompt(opts)

	for _, want := range []string{
		"up to 4 short search keywords",           // count still applied
		"only close synonyms and alternate names", // breadth guidance still applied
		"comma-separated list",                    // response format still stated
		"Every tag must be a single word.",        // and the custom instructions are there
	} {
		if !strings.Contains(got, want) {
			t.Errorf("system prompt is missing %q:\n%s", want, got)
		}
	}

	// Ordering carries the precedence: the custom instructions come last and the
	// sentence introducing them declares they win any conflict, which is what
	// makes an instruction like "no categories" override the built-in guidance
	// telling the model to suggest categories.
	intro := strings.Index(got, "Where they conflict")
	custom := strings.Index(got, "Every tag must be a single word.")
	if intro == -1 || custom < intro {
		t.Errorf("custom instructions should follow the precedence sentence:\n%s", got)
	}
}

func TestSystemPromptWithoutCustomPrompt(t *testing.T) {
	// Whitespace only is the same as unset -- a tag_prompt left as an empty
	// TOML multi-line string shouldn't bolt an empty "additional instructions"
	// section onto every request.
	for _, extra := range []string{"", "   \n\t "} {
		got := systemPrompt(TagOptions{Count: 3, Breadth: "moderate", ExtraPrompt: extra})
		if strings.Contains(got, "Additional instructions") {
			t.Errorf("ExtraPrompt %q should add no instructions section:\n%s", extra, got)
		}
	}
}

func TestParseTags(t *testing.T) {
	tests := []struct {
		name string
		in   string
		max  int
		want []string
	}{
		{"comma separated", "toys, bricks, travel", 3, []string{"toys", "bricks", "travel"}},
		{"newline separated", "toys\nbricks\ntravel", 3, []string{"toys", "bricks", "travel"}},
		{"quoted", `"toys", 'bricks'`, 3, []string{"toys", "bricks"}},
		{"truncated to max", "toys, bricks, travel, souvenir", 2, []string{"toys", "bricks"}},
		{"blank fields dropped", "toys,,  , bricks", 3, []string{"toys", "bricks"}},
		{"empty reply", "", 3, []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseTags(tt.in, tt.max)
			if len(got) != len(tt.want) {
				t.Fatalf("parseTags(%q) = %v, want %v", tt.in, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("parseTags(%q) = %v, want %v", tt.in, got, tt.want)
				}
			}
		})
	}
}
