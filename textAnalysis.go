package tankabot

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html"
)

// analysisNode は形態素と短歌検出に必要なメタデータを含む構造体。
type analysisNode struct {
	surface      string
	moraCount    int
	dependent    bool // dependent はそのノードが付属語かどうか。
	divisible    bool // divisible はそのノードで区切れができるかどうか。
	prefix       bool // prefix はそのノードが接頭語相当かどうか。
	nounOrSymbol bool
}

// phrase は文節とそのメタデータを含む構造体。
type phrase struct {
	surface      string
	moraCount    int
	canStart     bool // canStart は短歌の先頭句になりうるかどうか。
	sentenceTop  bool // sentenceTop は文頭かどうか。
	nounOrSymbol bool
}

// extractTankas は文字列の中に短歌（五七五七七）が含まれていればそれを返す。
func extractTankas(ctx context.Context, str string, analyzer *sudachiAnalyzer) (tankas string, err error) {
	if str == "" || !isJap(str) {
		return "", nil
	}
	//str = width.Fold.String(str)
	str = strings.ReplaceAll(str, "\t", "")

	phrases, err := segmentByPhrase(ctx, str, analyzer)
	if err != nil {
		return "", err
	}
	return extractTankasFromPhrases(phrases), nil
}

func extractTankasFromPhrases(phrases []phrase) string {
	ts := make([]string, 0)
	for i := range phrases {
		uta := detectTanka(phrases[i:])
		if uta != "" {
			dup := false
			for _, t := range ts {
				if "『"+uta+"』" == t {
					dup = true
				}
			}
			if !dup {
				ts = append(ts, "『"+uta+"』")
			}
		}
	}
	return strings.Join(ts, "\n\n")
}

// detectTanka はフレーズスライスの冒頭が短歌になっていればそれを返す。
func detectTanka(phrases []phrase) (tanka string) {
	if !phrases[0].canStart {
		return
	}

	type phraseRule struct {
		delimiter string
		moraCount int
	}

	rule := []phraseRule{{"", 5}, {" ", 7}, {" ", 5}, {" ", 7}, {" ", 7}}

	tp := phrases[0].sentenceTop

	nounOnly := true
	for _, pr := range rule {
		ku, no, ps := findKu(phrases, pr.moraCount)
		if ku == "" {
			return ""
		}
		tanka += pr.delimiter + ku
		if !no {
			nounOnly = false
		}
		phrases = ps
	}
	end := strings.HasSuffix(tanka, "。")
	tanka = strings.Trim(tanka, "。")

	// カッコの処理
	if strings.Count(tanka, "「") != strings.Count(tanka, "」") {
		return ""
	}
	end = end || strings.HasSuffix(tanka, "」")
	tp = tp || strings.HasPrefix(tanka, "「")
	rep := strings.NewReplacer("。」", "", "「", "", "」", "")
	tanka = rep.Replace(tanka)

	// 途中にピリオドがあるかどうか
	mp := strings.Contains(tanka, "。")

	tanka = strings.ReplaceAll(tanka, "。", "")

	// もし名詞短歌だったら即採用
	if nounOnly {
		return
	}

	// 文頭もしくは文末でなかったら帰る
	if !(tp || end) {
		return ""
	}

	// 文頭開始かつ文末終了でなく、途中にピリオドがあったら帰る
	if !(tp && end) && mp {
		return ""
	}

	return
}

// findKu は文の先頭が指定の拍数ぴったりに収まればその部分文字列を返す。
func findKu(phrases []phrase, mc int) (ku string, no bool, remainder []phrase) {
	ic := len(phrases)
	if ic == 0 {
		return
	}
	no = true
	morae := 0
	var empty []phrase
	remainder = phrases
	for morae < mc {
		if !remainder[0].nounOrSymbol {
			no = false
		}
		morae += remainder[0].moraCount
		if morae > mc {
			return "", false, empty
		}
		ku += remainder[0].surface
		remainder = remainder[1:]
		if len(remainder) == 0 && morae != mc {
			return "", false, empty
		}
	}

	return
}

// segmentByPhrase は文字列を短歌の句として切れる単位に分割する。
func segmentByPhrase(ctx context.Context, str string, analyzer *sudachiAnalyzer) (phrases []phrase, err error) {
	nodes, err := parse(ctx, str, analyzer)
	if err != nil {
		return nil, err
	}
	return segmentNodesByPhrase(nodes), nil
}

func segmentNodesByPhrase(nodes []analysisNode) (phrases []phrase) {
	if len(nodes) < 2 {
		return nil
	}

	var p phrase
	prefixed := false
	for _, n := range nodes {
		if !n.divisible || prefixed {
			p.surface += n.surface
			p.moraCount += n.moraCount
			if prefixed {
				p.canStart = !n.dependent
			}
			prefixed = n.prefix
			p.nounOrSymbol = p.nounOrSymbol && n.nounOrSymbol
			continue
		}
		phrases = append(phrases, p)
		p.sentenceTop = strings.HasSuffix(p.surface, "。")
		p.surface = n.surface
		p.moraCount = n.moraCount
		p.canStart = !n.dependent
		p.nounOrSymbol = n.nounOrSymbol
		prefixed = n.prefix
	}
	phrases = append(phrases, p)
	if phrases[0].surface == "" {
		phrases = phrases[1:]
	}
	phrases[0].sentenceTop = true

	return phrases
}

// parse は文字列をSudachiで形態素解析し、短歌検出用ノードへ変換する。
func parse(ctx context.Context, str string, analyzer *sudachiAnalyzer) ([]analysisNode, error) {
	if analyzer == nil {
		return nil, fmt.Errorf("形態素解析器が設定されていません")
	}
	tokens, err := analyzer.analyze(ctx, str)
	if err != nil {
		return nil, fmt.Errorf("Sudachiによる形態素解析に失敗しました: %w", err)
	}
	return nodesFromSudachi(str, tokens), nil
}

func nodesFromSudachi(text string, tokens []sudachiToken) (nodes []analysisNode) {
	nodes = make([]analysisNode, 0, len(tokens)+1)
	offset := 0
	for i, token := range tokens {
		if token.Surface == "" {
			continue
		}

		// Sudachi CLI's EOS markers are not part of the API response. Recover
		// sentence boundaries between tokens from the original input.
		if offset <= len(text) {
			if relative := strings.Index(text[offset:], token.Surface); relative >= 0 {
				gap := text[offset : offset+relative]
				if strings.ContainsAny(gap, "\r\n") {
					nodes = append(nodes, periodNode())
				}
				offset += relative + len(token.Surface)
			}
		}

		var node analysisNode
		switch {
		case isPeriod(token):
			node = periodNode()
		case isOpen(token):
			node.surface = "「"
			node.moraCount = 0
			node.dependent = false
			node.divisible = true
			node.prefix = true
			node.nounOrSymbol = true
		case isClose(token):
			node.surface = "」"
			node.moraCount = 0
			node.dependent = true
			node.divisible = false
			node.nounOrSymbol = true
		case token.Surface == "&" || token.NormalizedForm == "&":
			node.surface = token.Surface
			node.moraCount = 3
			node.dependent = true
			node.divisible = false
			node.nounOrSymbol = true
		case isLexical(token):
			node = nodeFromLexicalToken(token, previousSyntacticToken(tokens, i))
			if node.surface == "" {
				continue
			}
		default:
			continue
		}
		nodes = append(nodes, node)
	}

	if offset < len(text) && strings.ContainsAny(text[offset:], "\r\n") {
		nodes = append(nodes, periodNode())
	}
	// MeCab emitted EOS after every input. Preserve that sentence-end signal.
	nodes = append(nodes, periodNode())
	return nodes
}

func periodNode() analysisNode {
	return analysisNode{surface: "。", dependent: true, divisible: false, nounOrSymbol: true}
}

func nodeFromLexicalToken(token sudachiToken, previous *sudachiToken) analysisNode {
	node := analysisNode{surface: token.Surface}
	reading := token.ReadingForm
	if reading == "" || reading == "*" {
		katakana := strings.ReplaceAll(token.Surface, "・", "")
		if isKatakana(katakana) {
			node.surface = katakana
			reading = katakana
		} else if token.OOV {
			// Match the old MeCab fallback for unknown non-katakana nouns.
			node.moraCount = 8
			node.divisible = true
			return node
		} else {
			return analysisNode{}
		}
	}

	node.moraCount = moraCount(reading)
	if token.OOV {
		// MeCab treated readable katakana OOVs as independent words.
		node.divisible = true
		return node
	}
	node.dependent = isDependent(token, previous)
	node.divisible = isDivisible(node.dependent, token)
	node.prefix = token.PartOfSpeech[0] == "接頭辞"
	node.nounOrSymbol = isNoun(token)
	return node
}

func isLexical(token sudachiToken) bool {
	switch token.PartOfSpeech[0] {
	case "代名詞", "副詞", "助動詞", "助詞", "動詞", "名詞", "形容詞", "形状詞",
		"感動詞", "接尾辞", "接続詞", "接頭辞", "連体詞":
		return true
	default:
		return false
	}
}

func previousSyntacticToken(tokens []sudachiToken, current int) *sudachiToken {
	for i := current - 1; i >= 0; i-- {
		if tokens[i].PartOfSpeech[0] != "空白" {
			return &tokens[i]
		}
	}
	return nil
}

func isKatakana(word string) bool {
	if word == "" {
		return false
	}
	for _, r := range word {
		if !unicode.In(r, unicode.Katakana) && string(r) != "ー" {
			return false
		}
	}
	return true
}

func isDependent(token sudachiToken, previous *sudachiToken) bool {
	pos := token.PartOfSpeech
	return strings.Contains(pos[0], "助") ||
		pos[0] == "接尾辞" ||
		pos[1] == "助動詞語幹" ||
		(pos[2] == "助数詞可能" && isNumeral(previous)) ||
		(pos[1] == "非自立可能" && followsContinuative(previous)) ||
		token.Surface == "もの" || token.Surface == "こと" ||
		token.ReadingForm == "トキ" || token.ReadingForm == "トコロ" ||
		isSahen(token) ||
		(pos[0] == "動詞" && lemmaIs(token, "ある", "有る")) ||
		(pos[0] == "形容詞" && lemmaIs(token, "ない", "無い")) ||
		(pos[0] == "動詞" && lemmaIs(token, "なる", "成る"))
}

func isNumeral(token *sudachiToken) bool {
	return token != nil && token.PartOfSpeech[0] == "名詞" && token.PartOfSpeech[1] == "数詞"
}

func followsContinuative(token *sudachiToken) bool {
	if token == nil {
		return false
	}
	pos := token.PartOfSpeech
	if pos[0] == "助詞" && pos[1] == "接続助詞" {
		return token.DictionaryForm == "て" || token.DictionaryForm == "で"
	}
	if pos[0] != "動詞" && pos[0] != "形容詞" && pos[0] != "助動詞" {
		return false
	}
	return strings.HasPrefix(pos[5], "連用形")
}

func isDivisible(dependent bool, token sudachiToken) bool {
	pos := token.PartOfSpeech
	return !dependent || token.Surface == "もの" || token.Surface == "こと" ||
		token.Surface == "日" ||
		token.ReadingForm == "イイ" || token.ReadingForm == "ヨイ" ||
		token.ReadingForm == "トキ" || token.ReadingForm == "トコロ" ||
		(isSahen(token) && token.Surface != "し") ||
		(pos[0] == "動詞" && lemmaIs(token, "ある", "有る")) ||
		(pos[0] == "形容詞" && lemmaIs(token, "ない", "無い")) ||
		(pos[0] == "動詞" && lemmaIs(token, "なる", "成る")) ||
		(pos[1] == "副助詞" && !lemmaIs(token, "や", "か", "し", "ぞ", "って", "つ")) ||
		(pos[1] == "係助詞" && !lemmaIs(token, "は", "も", "ぞ", "や"))
}

func isSahen(token sudachiToken) bool {
	conjugationType := token.PartOfSpeech[4]
	return strings.Contains(conjugationType, "サ行変格") || strings.Contains(conjugationType, "サ変")
}

func lemmaIs(token sudachiToken, forms ...string) bool {
	for _, form := range forms {
		if token.DictionaryForm == form || token.NormalizedForm == form {
			return true
		}
	}
	return false
}

func isPeriod(token sudachiToken) bool {
	switch token.Surface {
	case "。", "?", "!", "？", "！", ":", ";", "：", "；", "▼", "▲":
		return true
	default:
		return false
	}
}

func isOpen(token sudachiToken) bool {
	if len(token.PartOfSpeech) > 1 && token.PartOfSpeech[1] == "括弧開" {
		return true
	}
	switch token.Surface {
	case "(", "<", "{", "[", "（", "＜", "｛", "［":
		return true
	default:
		return false
	}
}

func isClose(token sudachiToken) bool {
	if len(token.PartOfSpeech) > 1 && token.PartOfSpeech[1] == "括弧閉" {
		return true
	}
	switch token.Surface {
	case ")", ">", "}", "]", "）", "＞", "｝", "］":
		return true
	default:
		return false
	}
}

func isNoun(token sudachiToken) bool {
	switch token.PartOfSpeech[0] {
	case "名詞", "代名詞", "連体詞", "形状詞":
		return true
	case "接尾辞":
		return token.PartOfSpeech[1] == "名詞的" || token.PartOfSpeech[1] == "形状詞的"
	default:
		return false
	}
}

// moraCount は文字列が何拍で発音されるかを返す。
func moraCount(word string) (count int) {
	rep := strings.NewReplacer("ァ", "", "ィ", "", "ゥ", "", "ェ", "", "ォ", "", "ャ", "", "ュ", "", "ョ", "", "ヮ", "")
	word = rep.Replace(word)
	count = utf8.RuneCountInString(word)

	return
}

// isJap はテキストが日本語かどうか判定する。
func isJap(text string) bool {
	for _, r := range text {
		if unicode.In(r, unicode.Hiragana, unicode.Katakana) {
			return true
		}
	}
	return false
}

// textContent はhtmlからテキストを抽出する。
// https://github.com/mattn/go-mastodon/blob/master/cmd/mstdn/main.go より拝借
func textContent(s string) string {
	doc, err := html.Parse(strings.NewReader(s))
	if err != nil {
		return s
	}
	var buf bytes.Buffer

	var extractText func(node *html.Node, w *bytes.Buffer)
	extractText = func(node *html.Node, w *bytes.Buffer) {
		if node.Type == html.TextNode {
			data := strings.Trim(node.Data, "\r\n")
			if data != "" {
				w.WriteString(data)
			}
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			extractText(c, w)
		}
		if node.Type == html.ElementNode {
			name := strings.ToLower(node.Data)
			if name == "br" {
				w.WriteString("\n")
			}
		}
	}
	extractText(doc, &buf)

	return buf.String()
}
