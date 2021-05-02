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
	surface   string
	moraCount int
	dependent bool // dependent はそのノードが付属語かどうか。
	divisible bool // divisible はそのノードで区切れができるかどうか。
}

// phrase は文節とそのメタデータを含む構造体。
type phrase struct {
	surface     string
	moraCount   int
	canStart    bool // canStart は短歌の先頭句になりうるかどうか。
	sentenceTop bool // sentenceTop は文頭かどうか。
}

// extractTankas は文字列の中に短歌（五七五七七）が含まれていればそれを返す。
func extractTankas(str string, jpl chan int) (tankas string) {
	if str == "" || !isJap(str) {
		return
	}

	phrases := segmentByPhrase(str, jpl)

	for i := range phrases {
		uta := detectTanka(phrases[i:])
		if uta != "" {
			tankas += "『" + uta + "』" + "\n\n"
		}
	}
	tankas = strings.TrimSuffix(tankas, "\n\n")

	return
}

// segmentByPhrase は文字列を短歌の句として切れる単位に分割する。
func segmentByPhrase(str string, jpl chan int) (phrases []phrase) {
	nodes := parse(str, jpl)

	if len(nodes) < 2 {
		return
	}

	var p phrase
	for _, n := range nodes {
		if n.divisible {
			compP := p
			phrases = append(phrases, compP)
			p.sentenceTop = false
			if strings.HasSuffix(p.surface, "。") || strings.HasSuffix(p.surface, "EOS") {
				p.sentenceTop = true
			}
			p.surface = n.surface
			p.moraCount = n.moraCount
			p.canStart = true
			if n.dependent {
				p.canStart = false
			}
		} else {
			p.surface += n.surface
			p.moraCount += n.moraCount
		}
	}
	phrases = append(phrases, p)
	phrases = phrases[1:]

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
		s = strings.Replace(s, "\t", ",", 1)
		props := strings.SplitN(s, ",", 10)
		if len(props) == 10 && props[1] != "記号" {
			var node mecabNode
			node.surface = props[0]
			node.moraCount = moraCount(props[8])
			node.dependent = strings.Contains(props[1], "助") || props[2] == "非自立" || props[2] == "接尾" || props[5] == "サ変・スル" || (props[1] == "動詞" && props[7] == "ある") || (props[1] == "形容詞" && props[7] == "ない") || (props[1] == "動詞" && props[7] == "なる")
			node.divisible = !node.dependent || props[0] == "もの" || props[0] == "こと" || props[2] == "副助詞" || props[0] == "日" || props[8] == "イイ" || props[8] == "ヨイ" || props[8] == "トキ" || props[8] == "トコロ" || props[5] == "サ変・スル" || (props[1] == "動詞" && props[7] == "ある") || (props[1] == "形容詞" && props[7] == "ない") || (props[1] == "動詞" && props[7] == "なる")
			nodes = append(nodes, node)
		} else if props[0] == "。" || props[0] == "「" || props[0] == "」" || props[0] == "EOS" || props[0] == "(" || props[0] == ")" || props[0] == "（" || props[0] == "）" {
			var node mecabNode
			node.surface = props[0]
			node.moraCount = 0
			node.dependent = true
			node.divisible = false
			nodes = append(nodes, node)
		} else if len(props) == 8 && props[2] == "数" {
			var node mecabNode
			node.surface = props[0]
			node.moraCount = 8
			node.dependent = false
			node.divisible = true
			nodes = append(nodes, node)
		}
	}

	return
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
	ku := ""
	sentenceTop := false
	tempSTop := false
	for i, pr := range rule {
		ku, tempSTop, phrases = findKu(phrases, pr.moraCount)
		if i == 0 {
			sentenceTop = tempSTop
		}
		if ku == "" {
			return ""
		}
		tanka += pr.delimiter + ku
	}

	if strings.Count(tanka, "「") != strings.Count(tanka, "」") {
		return ""
	}
	rep := strings.NewReplacer("。」", "", "「", "", "」", "")
	tanka = rep.Replace(tanka)

	if !(sentenceTop && (strings.HasSuffix(tanka, "。") || strings.HasSuffix(tanka, "EOS"))) {
		tanka = strings.Trim(tanka, "。")
		tanka = strings.Trim(tanka, "EOS")
		if strings.Contains(tanka, "。") || strings.Contains(tanka, "EOS") {
			return ""
		}
	}
	rep = strings.NewReplacer("。", "", "EOS", "", "(", "", ")", "", "（", "", "）", "")
	tanka = rep.Replace(tanka)

	return
}

// findKu は文の先頭が指定の拍数ぴったりに収まればその部分文字列を返す。
func findKu(phrases []phrase, mc int) (ku string, sentenceTop bool, remainder []phrase) {
	ic := len(phrases)
	if ic == 0 {
		return
	}
	morae := 0
	var empty []phrase
	sentenceTop = phrases[0].sentenceTop
	remainder = phrases
	for morae < mc {
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
