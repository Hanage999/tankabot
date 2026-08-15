package tankabot

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// KugiriSegment is one unit at whose beginning tankabot may place a ku boundary.
type KugiriSegment struct {
	Surface     string
	MoraCount   int
	CanStart    bool
	SentenceTop bool
	NounOnly    bool
}

// KugiriResult contains tankabot's segmentation and any detected tankas.
type KugiriResult struct {
	Segments []KugiriSegment
	Tankas   string
}

// AnalyzeKugiri runs the same Sudachi mode-B analysis and boundary rules used by
// the bot. The API URL is a base URL; /v1/analyze is appended automatically.
func AnalyzeKugiri(ctx context.Context, text, apiURL string, timeout time.Duration) (KugiriResult, error) {
	if strings.TrimSpace(text) == "" {
		return KugiriResult{}, fmt.Errorf("解析する文字列が空です")
	}
	analyzer, err := newSudachiAnalyzer(apiURL, timeout, 1)
	if err != nil {
		return KugiriResult{}, err
	}
	nodes, err := parse(ctx, text, analyzer)
	if err != nil {
		return KugiriResult{}, err
	}
	phrases := segmentNodesByPhrase(nodes)
	result := KugiriResult{
		Segments: make([]KugiriSegment, len(phrases)),
		Tankas:   extractTankasFromPhrases(phrases),
	}
	for i, p := range phrases {
		result.Segments[i] = KugiriSegment{
			Surface:     p.surface,
			MoraCount:   p.moraCount,
			CanStart:    p.canStart,
			SentenceTop: p.sentenceTop,
			NounOnly:    p.nounOrSymbol,
		}
	}
	return result, nil
}
