package tankabot

import (
	"bytes"
	"log"
	"os/exec"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html"
)

// mecabNode はMecabで分節されたノードとそのメタデータを含む構造体。
type mecabNode struct {
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
func extractTankas(str string, jpl chan int) (tankas string) {
	if str == "" || !isJap(str) {
		return
	}
	//str = width.Fold.String(str)
	str = strings.ReplaceAll(str, "\t", "")

	phrases := segmentByPhrase(str, jpl)

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
	tankas = strings.Join(ts, "\n\n")

	return
}

// segmentByPhrase は文字列を短歌の句として切れる単位に分割する。
func segmentByPhrase(str string, jpl chan int) (phrases []phrase) {
	nodes := parse(str, jpl)

	if len(nodes) < 2 {
		return
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
			if !n.nounOrSymbol {
				p.nounOrSymbol = false
			}
			p.nounOrSymbol = n.nounOrSymbol
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

	return
}

// parse は文字列をMecabで形態素解析し、ノードのスライスを返す。
func parse(str string, jpl chan int) (nodes []mecabNode) {
	cmd := exec.Command("mecab")
	cmd.Stdin = strings.NewReader(str)
	jpl <- 0
	out, err := cmd.Output()
	<-jpl
	if err != nil {
		log.Printf("info: 形態素解析器が正常に起動できませんでした：%s", err)
		return
	}

	nodeStrs := strings.Split(string(out), "\n")
	nodes = make([]mecabNode, 0)
	for _, s := range nodeStrs {
		if s == "" || strings.HasPrefix(s, ",") {
			continue
		}

		s = strings.Replace(s, "\t", ",", 1)
		props := strings.SplitN(s, ",", 10)
		var node mecabNode
		switch {
		case isWord(props):
			node.surface = props[0]
			node.moraCount = moraCount(props[8])
			node.dependent = isDependent(props)
			node.divisible = isDivisible(node.dependent, props)
			node.prefix = isPrefix(props)
			node.nounOrSymbol = isNoun(props)
		case isKatakana(props):
			node.surface = props[0]
			node.moraCount = moraCount(props[0])
			node.dependent = false
			node.divisible = true
		case isPeriod(props):
			node.surface = "。"
			node.moraCount = 0
			node.dependent = true
			node.divisible = false
			node.nounOrSymbol = true
		case isOpen(props):
			node.surface = "「"
			node.moraCount = 0
			node.dependent = false
			node.divisible = true
			node.prefix = true
			node.nounOrSymbol = true
		case isClose(props):
			node.surface = "」"
			node.moraCount = 0
			node.dependent = true
			node.divisible = false
			node.nounOrSymbol = true
		case isAnd(props):
			node.surface = props[0]
			node.moraCount = 3
			node.dependent = true
			node.divisible = false
			node.nounOrSymbol = true
		case isUnknown(props):
			node.surface = props[0]
			node.moraCount = 8
			node.dependent = false
			node.divisible = true
		default:
			continue
		}
		nodes = append(nodes, node)
	}

	return
}

func isWord(props []string) bool {
	return len(props) == 10 && props[1] != "記号"
}

func isKatakana(props []string) bool {
	props[0] = strings.Replace(props[0], "・", "", -1)
	for _, r := range props[0] {
		if !unicode.In(r, unicode.Katakana) && string(r) != "ー" {
			return false
		}
	}
	return true
}

func isDependent(props []string) bool {
	return strings.Contains(props[1], "助") || props[2] == "非自立" || props[2] == "接尾" || props[5] == "サ変・スル" || (props[1] == "動詞" && props[7] == "ある") || (props[1] == "形容詞" && props[7] == "ない") || (props[1] == "動詞" && props[7] == "なる")
}

func isDivisible(dep bool, props []string) bool {
	return !dep || props[0] == "もの" || props[0] == "こと" || props[2] == "副助詞" || props[0] == "日" || props[8] == "イイ" || props[8] == "ヨイ" || props[8] == "トキ" || props[8] == "トコロ" || (props[5] == "サ変・スル" && props[0] != "し") || (props[1] == "動詞" && props[7] == "ある") || (props[1] == "形容詞" && props[7] == "ない") || (props[1] == "動詞" && props[7] == "なる")
}

func isPrefix(props []string) bool {
	return props[1] == "接頭詞"
}

func isPeriod(props []string) bool {
	return props[0] == "。" || props[0] == "?" || props[0] == "!" || props[0] == "EOS" || props[0] == ":" || props[0] == ";" || props[0] == "▼" || props[0] == "▲"
}

func isOpen(props []string) bool {
	return props[2] == "括弧開" || props[0] == "(" || props[0] == "<" || props[0] == "{" || props[0] == "["
}

func isClose(props []string) bool {
	return props[2] == "括弧閉" || props[0] == ")" || props[0] == ">" || props[0] == "}" || props[0] == "]"
}

func isAnd(props []string) bool {
	return props[0] == "&"
}

func isUnknown(props []string) bool {
	return len(props) == 8 && props[1] == "名詞"
}

func isNoun(props []string) bool {
	return props[1] == "名詞" || props[1] == "連体詞"
}

// moraCount は文字列が何拍で発音されるかを返す。
func moraCount(word string) (count int) {
	rep := strings.NewReplacer("ァ", "", "ィ", "", "ゥ", "", "ェ", "", "ォ", "", "ャ", "", "ュ", "", "ョ", "", "ヮ", "")
	word = rep.Replace(word)
	count = utf8.RuneCountInString(word)

	return
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

	rule := []phraseRule{{"", 5}, {" ", 7}, {" ", 5}, {"\n", 7}, {" ", 7}}

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

	// カッコの処理
	if strings.Count(tanka, "「") != strings.Count(tanka, "」") {
		return ""
	}
	endKakko := strings.HasSuffix(tanka, "」")
	if strings.HasPrefix(tanka, "「") {
		tp = true
	}
	rep := strings.NewReplacer("。」", "", "「", "", "」", "")
	tanka = rep.Replace(tanka)

	// 文頭もしくは文末もしくは名詞短歌でなかったら帰る
	if !(tp || endKakko || strings.HasSuffix(tanka, "。") || nounOnly) {
		return ""
	}
	// 名詞短歌ではなく、しかも文末でなく、しかも途中にピリオドがあったら帰る
	if !nounOnly && !strings.HasSuffix(tanka, "。") && strings.Contains(tanka, "。") {
		return ""
	}

	tanka = strings.ReplaceAll(tanka, "。", "")

	return
}

// findKu は文の先頭が指定の拍数ぴったりに収まればその部分文字列を返す。
func findKu(phrases []phrase, mc int) (ku string, no bool, remainder []phrase) {
	ic := len(phrases)
	if ic == 0 {
		return
	}
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
