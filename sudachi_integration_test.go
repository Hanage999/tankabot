package tankabot

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestLiveSudachiAPICompatibility checks the configured, deployed dictionary.
// It is opt-in so the normal unit test suite remains network-independent.
func TestLiveSudachiAPICompatibility(t *testing.T) {
	baseURL := os.Getenv("TANKABOT_SUDACHI_API_URL")
	if baseURL == "" {
		t.Skip("set TANKABOT_SUDACHI_API_URL to run the live Sudachi compatibility test")
	}

	analyzer, err := newSudachiAnalyzer(baseURL, 30*time.Second, 1)
	if err != nil {
		t.Fatalf("newSudachiAnalyzer: %v", err)
	}

	t.Run("57577", func(t *testing.T) {
		input := "東京都、プログラミング、コンピュータ、インターネット、コミュニケーション"
		want := "『東京都 プログラミング コンピュータ インターネット コミュニケーション』"
		got, err := extractTankas(context.Background(), input, analyzer)
		if err != nil {
			t.Fatalf("extractTankas: %v", err)
		}
		if got != want {
			t.Fatalf("extractTankas() = %q, want %q", got, want)
		}
	})

	t.Run("ambiguous POS context", func(t *testing.T) {
		tokens, err := analyzer.analyze(context.Background(), "東京へ行く。歩いて行く。終わったから行く。回を重ねる。三回。子供のようだ。")
		if err != nil {
			t.Fatalf("analyze: %v", err)
		}

		var iku, rounds int
		for i, token := range tokens {
			switch token.Surface {
			case "行く":
				iku++
				node := nodeFromLexicalToken(token, previousSyntacticToken(tokens, i))
				if iku == 1 && node.dependent {
					t.Errorf("standalone 行く was classified as dependent")
				}
				if iku == 2 && !node.dependent {
					t.Errorf("auxiliary 行く was classified as independent")
				}
				if iku == 3 && node.dependent {
					t.Errorf("行く after a clause conjunction was classified as dependent")
				}
			case "回":
				rounds++
				node := nodeFromLexicalToken(token, previousSyntacticToken(tokens, i))
				if rounds == 1 && node.dependent {
					t.Errorf("standalone 回 was classified as dependent")
				}
				if rounds == 2 && !node.dependent {
					t.Errorf("counter 回 was classified as independent")
				}
			case "よう":
				node := nodeFromLexicalToken(token, previousSyntacticToken(tokens, i))
				if !node.dependent {
					t.Errorf("助動詞語幹 よう was classified as independent")
				}
			}
		}
		if iku != 3 || rounds != 2 {
			t.Fatalf("unexpected fixture tokens: 行く=%d 回=%d", iku, rounds)
		}
	})
}
