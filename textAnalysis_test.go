package tankabot

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
	"time"
)

func TestNodeFromSudachiTokenPreservesPhraseRules(t *testing.T) {
	tests := []struct {
		name  string
		token sudachiToken
		want  analysisNode
	}{
		{
			name:  "ordinary noun",
			token: testToken("言葉", "名詞", "普通名詞", "*", "言葉", "コトバ", false),
			want:  analysisNode{surface: "言葉", moraCount: 3, divisible: true, nounOrSymbol: true},
		},
		{
			name:  "ordinary Sudachi verb is independent",
			token: testToken("行く", "動詞", "非自立可能", "五段-カ行", "行く", "イク", false),
			want:  analysisNode{surface: "行く", moraCount: 2, divisible: true},
		},
		{
			name:  "particle",
			token: testToken("へ", "助詞", "格助詞", "*", "へ", "ヘ", false),
			want:  analysisNode{surface: "へ", moraCount: 1, dependent: true},
		},
		{
			name:  "divisible adverbial particle",
			token: testToken("まで", "助詞", "副助詞", "*", "まで", "マデ", false),
			want:  analysisNode{surface: "まで", moraCount: 2, dependent: true, divisible: true},
		},
		{
			name:  "nominal suffix",
			token: testToken("人", "接尾辞", "名詞的", "*", "人", "ジン", false),
			want:  analysisNode{surface: "人", moraCount: 2, dependent: true, nounOrSymbol: true},
		},
		{
			name:  "divisible dependent noun",
			token: testToken("もの", "名詞", "普通名詞", "*", "もの", "モノ", false),
			want:  analysisNode{surface: "もの", moraCount: 2, dependent: true, divisible: true, nounOrSymbol: true},
		},
		{
			name:  "sahen continuative form",
			token: testToken("し", "動詞", "非自立可能", "サ行変格", "する", "シ", false),
			want:  analysisNode{surface: "し", moraCount: 1, dependent: true},
		},
		{
			name:  "sahen dictionary form",
			token: testToken("する", "動詞", "非自立可能", "サ行変格", "する", "スル", false),
			want:  analysisNode{surface: "する", moraCount: 2, dependent: true, divisible: true},
		},
		{
			name:  "special verb",
			token: testToken("有る", "動詞", "非自立可能", "五段-ラ行", "ある", "アル", false),
			want:  analysisNode{surface: "有る", moraCount: 2, dependent: true, divisible: true},
		},
		{
			name:  "special reading",
			token: testToken("時", "名詞", "普通名詞", "*", "時", "トキ", false),
			want:  analysisNode{surface: "時", moraCount: 2, dependent: true, divisible: true, nounOrSymbol: true},
		},
		{
			name:  "prefix",
			token: testToken("御", "接頭辞", "*", "*", "御", "オ", false),
			want:  analysisNode{surface: "御", moraCount: 1, divisible: true, prefix: true},
		},
		{
			name:  "readable katakana OOV",
			token: testToken("ニュース", "名詞", "普通名詞", "*", "ニュース", "ニュース", true),
			want:  analysisNode{surface: "ニュース", moraCount: 3, divisible: true},
		},
		{
			name:  "unreadable non-katakana OOV",
			token: testToken("未知語", "名詞", "普通名詞", "*", "未知語", "", true),
			want:  analysisNode{surface: "未知語", moraCount: 8, divisible: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nodeFromLexicalToken(tt.token, nil); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("node = %#v, want %#v", got, tt.want)
			}
		})
	}

	counter := testToken("日", "名詞", "普通名詞", "*", "日", "ニチ", false)
	counter.PartOfSpeech[2] = "助数詞可能"
	numeral := testToken("十五", "名詞", "数詞", "*", "十五", "ジュウゴ", false)
	wantCounter := analysisNode{surface: "日", moraCount: 2, dependent: true, divisible: true, nounOrSymbol: true}
	if got := nodeFromLexicalToken(counter, &numeral); !reflect.DeepEqual(got, wantCounter) {
		t.Fatalf("counter node = %#v, want %#v", got, wantCounter)
	}
}

func TestAmbiguousSudachiPOSUsesContext(t *testing.T) {
	connectionParticle := testToken("て", "助詞", "接続助詞", "*", "て", "テ", false)
	clauseParticle := testToken("から", "助詞", "接続助詞", "*", "から", "カラ", false)
	caseParticle := testToken("で", "助詞", "格助詞", "*", "で", "デ", false)
	continuativeVerb := testToken("読み", "動詞", "一般", "五段-マ行", "読む", "ヨミ", false)
	continuativeVerb.PartOfSpeech[5] = "連用形-一般"

	tests := []struct {
		name     string
		token    sudachiToken
		previous *sudachiToken
		want     analysisNode
	}{
		{
			name:     "verb after conjunctive particle is auxiliary",
			token:    testToken("いる", "動詞", "非自立可能", "上一段-ア行", "いる", "イル", false),
			previous: &connectionParticle,
			want:     analysisNode{surface: "いる", moraCount: 2, dependent: true},
		},
		{
			name:     "verb after continuative verb is auxiliary",
			token:    testToken("始める", "動詞", "非自立可能", "下一段-マ行", "始める", "ハジメル", false),
			previous: &continuativeVerb,
			want:     analysisNode{surface: "始める", moraCount: 4, dependent: true},
		},
		{
			name:     "adjective after conjunctive particle is auxiliary",
			token:    testToken("ほしい", "形容詞", "非自立可能", "形容詞", "ほしい", "ホシイ", false),
			previous: &connectionParticle,
			want:     analysisNode{surface: "ほしい", moraCount: 3, dependent: true},
		},
		{
			name:     "adjective after case particle remains independent",
			token:    testToken("いい", "形容詞", "非自立可能", "形容詞", "いい", "イイ", false),
			previous: &caseParticle,
			want:     analysisNode{surface: "いい", moraCount: 2, divisible: true},
		},
		{
			name:     "verb after clause conjunction remains independent",
			token:    testToken("行く", "動詞", "非自立可能", "五段-カ行", "行く", "イク", false),
			previous: &clauseParticle,
			want:     analysisNode{surface: "行く", moraCount: 2, divisible: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nodeFromLexicalToken(tt.token, tt.previous); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("node = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestSudachiContextProducesNaturalKuBoundaries(t *testing.T) {
	t.Run("do not split a compound verb before its auxiliary verb", func(t *testing.T) {
		continuative := testToken("読み", "動詞", "一般", "五段-マ行", "読む", "ヨミ", false)
		continuative.PartOfSpeech[5] = "連用形-一般"
		tokens := []sudachiToken{
			testToken("本", "名詞", "普通名詞", "*", "本", "ホン", false),
			testToken("を", "助詞", "格助詞", "*", "を", "ヲ", false),
			continuative,
			testToken("始める", "動詞", "非自立可能", "下一段-マ行", "始める", "ハジメル", false),
		}

		phrases := phrasesFromTokens(t, "本を読み始める", tokens)
		if ku, _, _ := findKu(phrases, 5); ku != "" {
			t.Fatalf("findKu() = %q; must not split 読み始める after 本を読み", ku)
		}
		if got, want := phraseSurfaces(phrases), []string{"本を", "読み始める。"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("phrases = %q, want %q", got, want)
		}
	})

	t.Run("allow a new independent clause after kara", func(t *testing.T) {
		tokens := []sudachiToken{
			testToken("雨", "名詞", "普通名詞", "*", "雨", "アメ", false),
			testToken("だ", "助動詞", "*", "助動詞-ダ", "だ", "ダ", false),
			testToken("から", "助詞", "接続助詞", "*", "から", "カラ", false),
			testToken("行く", "動詞", "非自立可能", "五段-カ行", "行く", "イク", false),
		}

		phrases := phrasesFromTokens(t, "雨だから行く", tokens)
		ku, _, remainder := findKu(phrases, 5)
		if ku != "雨だから" {
			t.Fatalf("findKu() = %q, want %q", ku, "雨だから")
		}
		if len(remainder) == 0 || remainder[0].surface != "行く。" || !remainder[0].canStart {
			t.Fatalf("remainder = %#v; 行く must remain a valid next-line start", remainder)
		}
	})
}

func TestCounterPossibleIsDependentOnlyAfterNumeral(t *testing.T) {
	counter := testToken("回", "名詞", "普通名詞", "*", "回", "カイ", false)
	counter.PartOfSpeech[2] = "助数詞可能"
	numeral := testToken("三", "名詞", "数詞", "*", "三", "サン", false)

	standalone := nodeFromLexicalToken(counter, nil)
	if standalone.dependent || !standalone.divisible {
		t.Fatalf("standalone counter-like noun = %#v, want independent and divisible", standalone)
	}
	afterNumeral := nodeFromLexicalToken(counter, &numeral)
	if !afterNumeral.dependent || afterNumeral.divisible {
		t.Fatalf("counter after numeral = %#v, want dependent and indivisible", afterNumeral)
	}
}

func TestPreviousSyntacticTokenSkipsWhitespace(t *testing.T) {
	connection := testToken("て", "助詞", "接続助詞", "*", "て", "テ", false)
	space := testToken(" ", "空白", "*", "*", " ", "", false)
	auxiliary := testToken("いる", "動詞", "非自立可能", "上一段-ア行", "いる", "イル", false)
	tokens := []sudachiToken{connection, space, auxiliary}

	previous := previousSyntacticToken(tokens, 2)
	if previous == nil || previous.Surface != "て" {
		t.Fatalf("previous token = %#v, want て", previous)
	}
	if got := nodeFromLexicalToken(auxiliary, previous); !got.dependent {
		t.Fatalf("auxiliary after whitespace = %#v, want dependent", got)
	}
}

func TestAuxiliaryStemAndAdjectivalNounSuffix(t *testing.T) {
	auxiliaryStem := testToken("よう", "形状詞", "助動詞語幹", "*", "よう", "ヨウ", false)
	if got := nodeFromLexicalToken(auxiliaryStem, nil); !got.dependent || got.divisible || !got.nounOrSymbol {
		t.Fatalf("auxiliary stem = %#v", got)
	}

	suffix := testToken("げ", "接尾辞", "形状詞的", "*", "げ", "ゲ", false)
	if got := nodeFromLexicalToken(suffix, nil); !got.dependent || got.divisible || !got.nounOrSymbol {
		t.Fatalf("adjectival-noun suffix = %#v", got)
	}
}

func TestNodesFromSudachiPreservesPunctuationRules(t *testing.T) {
	tokens := []sudachiToken{
		testToken("！", "補助記号", "句点", "*", "！", "", false),
		testToken("（", "補助記号", "括弧開", "*", "（", "", false),
		testToken("）", "補助記号", "括弧閉", "*", "）", "", false),
		testToken("＆", "補助記号", "一般", "*", "&", "アンド", false),
	}
	nodes := nodesFromSudachi("！（）&", tokens)
	want := []analysisNode{
		periodNode(),
		{surface: "「", divisible: true, prefix: true, nounOrSymbol: true},
		{surface: "」", dependent: true, nounOrSymbol: true},
		{surface: "＆", moraCount: 3, dependent: true, nounOrSymbol: true},
		periodNode(),
	}
	if !reflect.DeepEqual(nodes, want) {
		t.Fatalf("nodes = %#v, want %#v", nodes, want)
	}
}

func TestNodesFromSudachiRestoresNewlineAndEOS(t *testing.T) {
	tokens := []sudachiToken{
		testToken("あい", "名詞", "普通名詞", "*", "あい", "アイ", false),
		testToken("うえ", "名詞", "普通名詞", "*", "うえ", "ウエ", false),
	}
	nodes := nodesFromSudachi("あい\nうえ", tokens)
	wantSurfaces := []string{"あい", "。", "うえ", "。"}
	if len(nodes) != len(wantSurfaces) {
		t.Fatalf("len(nodes) = %d, want %d: %#v", len(nodes), len(wantSurfaces), nodes)
	}
	for i, want := range wantSurfaces {
		if nodes[i].surface != want {
			t.Errorf("nodes[%d].surface = %q, want %q", i, nodes[i].surface, want)
		}
	}
}

func TestExtractTankasWithSudachiResponse(t *testing.T) {
	// Fixture based on the configured full-dictionary Sudachi API in mode B.
	tokens := []sudachiToken{
		testToken("東京都", "名詞", "固有名詞", "*", "東京都", "トウキョウト", false),
		testToken("、", "補助記号", "読点", "*", "、", "、", false),
		testToken("プログラミング", "名詞", "普通名詞", "*", "プログラミング", "プログラミング", false),
		testToken("、", "補助記号", "読点", "*", "、", "、", false),
		testToken("コンピュータ", "名詞", "普通名詞", "*", "コンピュータ", "コンピュータ", false),
		testToken("、", "補助記号", "読点", "*", "、", "、", false),
		testToken("インターネット", "名詞", "普通名詞", "*", "インターネット", "インターネット", false),
		testToken("、", "補助記号", "読点", "*", "、", "、", false),
		testToken("コミュニケーション", "名詞", "普通名詞", "*", "コミュニケーション", "コミュニケーション", false),
	}

	responseBody, err := json.Marshal(sudachiAnalyzeResponse{Tokens: tokens, Count: len(tokens), Mode: "B"})
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	analyzer, err := newSudachiAnalyzer("http://sudachi.test", time.Second, 1)
	if err != nil {
		t.Fatalf("newSudachiAnalyzer: %v", err)
	}
	analyzer.client.Transport = roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return testHTTPResponse(http.StatusOK, string(responseBody)), nil
	})

	input := "東京都、プログラミング、コンピュータ、インターネット、コミュニケーション"
	want := "『東京都 プログラミング コンピュータ インターネット コミュニケーション』"
	got, err := extractTankas(context.Background(), input, analyzer)
	if err != nil {
		t.Fatalf("extractTankas: %v", err)
	}
	if got != want {
		t.Fatalf("extractTankas() = %q, want %q", got, want)
	}
}

func phrasesFromTokens(t *testing.T, input string, tokens []sudachiToken) []phrase {
	t.Helper()
	responseBody, err := json.Marshal(sudachiAnalyzeResponse{Tokens: tokens, Count: len(tokens), Mode: "B"})
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	analyzer, err := newSudachiAnalyzer("http://sudachi.test", time.Second, 1)
	if err != nil {
		t.Fatalf("newSudachiAnalyzer: %v", err)
	}
	analyzer.client.Transport = roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return testHTTPResponse(http.StatusOK, string(responseBody)), nil
	})

	phrases, err := segmentByPhrase(context.Background(), input, analyzer)
	if err != nil {
		t.Fatalf("segmentByPhrase: %v", err)
	}
	return phrases
}

func phraseSurfaces(phrases []phrase) []string {
	surfaces := make([]string, len(phrases))
	for i := range phrases {
		surfaces[i] = phrases[i].surface
	}
	return surfaces
}

func TestExtractTankasReturnsAnalyzerError(t *testing.T) {
	analyzer, err := newSudachiAnalyzer("http://sudachi.test", time.Second, 1)
	if err != nil {
		t.Fatalf("newSudachiAnalyzer: %v", err)
	}
	analyzer.client.Transport = roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return testHTTPResponse(http.StatusBadGateway, `{"error":{"code":"sudachi_failed","message":"failed"}}`), nil
	})

	got, err := extractTankas(context.Background(), "にほんご", analyzer)
	if err == nil {
		t.Fatal("extractTankas returned nil error for an API failure")
	}
	if got != "" {
		t.Fatalf("extractTankas result = %q, want empty", got)
	}
}

func TestPhraseNounFlagRequiresEveryNodeToBeNominal(t *testing.T) {
	tokens := []sudachiToken{
		testToken("走る", "動詞", "非自立可能", "五段-ラ行", "走る", "ハシル", false),
		testToken("人", "接尾辞", "名詞的", "*", "人", "ヒト", false),
	}
	responseBody, err := json.Marshal(sudachiAnalyzeResponse{Tokens: tokens, Count: len(tokens), Mode: "B"})
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	analyzer, err := newSudachiAnalyzer("http://sudachi.test", time.Second, 1)
	if err != nil {
		t.Fatalf("newSudachiAnalyzer: %v", err)
	}
	analyzer.client.Transport = roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return testHTTPResponse(http.StatusOK, string(responseBody)), nil
	})

	phrases, err := segmentByPhrase(context.Background(), "走る人", analyzer)
	if err != nil {
		t.Fatalf("segmentByPhrase: %v", err)
	}
	if len(phrases) != 1 {
		t.Fatalf("phrases = %#v, want one phrase", phrases)
	}
	if phrases[0].nounOrSymbol {
		t.Fatal("phrase containing a verb was marked noun-only")
	}
}
