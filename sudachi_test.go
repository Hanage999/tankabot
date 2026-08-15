package tankabot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSudachiAnalyzerUsesModeB(t *testing.T) {
	t.Helper()
	wantToken := testToken("東京都", "名詞", "固有名詞", "*", "東京都", "トウキョウト", false)
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/sudachi/v1/analyze" {
			t.Errorf("path = %s, want /sudachi/v1/analyze", r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		var request struct {
			Text string `json:"text"`
			Mode string `json:"mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if request.Text != "東京都へ" {
			t.Errorf("text = %q, want 東京都へ", request.Text)
		}
		if request.Mode != "B" {
			t.Errorf("mode = %q, want B", request.Mode)
		}
		responseBody, err := json.Marshal(sudachiAnalyzeResponse{
			Tokens: []sudachiToken{wantToken},
			Count:  1,
			Mode:   "B",
		})
		if err != nil {
			return nil, err
		}
		return testHTTPResponse(http.StatusOK, string(responseBody)), nil
	})

	analyzer, err := newSudachiAnalyzer("http://sudachi.test/sudachi/", time.Second, 1)
	if err != nil {
		t.Fatalf("newSudachiAnalyzer: %v", err)
	}
	analyzer.client.Transport = transport
	tokens, err := analyzer.analyze(context.Background(), "東京都へ")
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if len(tokens) != 1 || tokens[0].Surface != wantToken.Surface {
		t.Fatalf("tokens = %#v, want %#v", tokens, []sudachiToken{wantToken})
	}
}

func TestSudachiAnalyzerRejectsInvalidResponses(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantError  string
	}{
		{
			name:       "API error",
			statusCode: http.StatusBadGateway,
			body:       `{"error":{"code":"sudachi_failed","message":"analysis failed"}}`,
			wantError:  "sudachi_failed",
		},
		{
			name:       "malformed JSON",
			statusCode: http.StatusOK,
			body:       `{`,
			wantError:  "JSONが不正",
		},
		{
			name:       "wrong mode",
			statusCode: http.StatusOK,
			body:       `{"tokens":[],"count":0,"mode":"C"}`,
			wantError:  "modeが不正",
		},
		{
			name:       "wrong count",
			statusCode: http.StatusOK,
			body:       `{"tokens":[],"count":1,"mode":"B"}`,
			wantError:  "countが一致しません",
		},
		{
			name:       "invalid POS",
			statusCode: http.StatusOK,
			body:       `{"tokens":[{"surface":"語","part_of_speech":["名詞"]}],"count":1,"mode":"B"}`,
			wantError:  "品詞情報が6項目ではありません",
		},
		{
			name:       "unsupported POS hierarchy",
			statusCode: http.StatusOK,
			body:       `{"tokens":[{"surface":"語","part_of_speech":["動詞","非自立","*","*","五段-ラ行","終止形-一般"]}],"count":1,"mode":"B"}`,
			wantError:  "未対応の品詞階層",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			analyzer, err := newSudachiAnalyzer("http://sudachi.test", time.Second, 1)
			if err != nil {
				t.Fatalf("newSudachiAnalyzer: %v", err)
			}
			analyzer.client.Transport = roundTripFunc(func(_ *http.Request) (*http.Response, error) {
				return testHTTPResponse(tt.statusCode, tt.body), nil
			})
			_, err = analyzer.analyze(context.Background(), "語")
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("error = %v, want substring %q", err, tt.wantError)
			}
		})
	}
}

func TestSudachiAnalyzerHonorsContextWhileWaitingForSlot(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	analyzer, err := newSudachiAnalyzer("http://sudachi.test", time.Second, 1)
	if err != nil {
		t.Fatalf("newSudachiAnalyzer: %v", err)
	}
	analyzer.client.Transport = roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		close(started)
		<-release
		return testHTTPResponse(http.StatusOK, `{"tokens":[],"count":0,"mode":"B"}`), nil
	})
	firstDone := make(chan error, 1)
	go func() {
		_, err := analyzer.analyze(context.Background(), "一件目")
		firstDone <- err
	}()
	<-started

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = analyzer.analyze(ctx, "二件目")
	if err != context.Canceled {
		t.Fatalf("error = %v, want context.Canceled", err)
	}

	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first analyze: %v", err)
	}
}

func TestSudachiAnalyzerTimeoutIncludesWaitingForSlot(t *testing.T) {
	analyzer, err := newSudachiAnalyzer("http://sudachi.test", 20*time.Millisecond, 1)
	if err != nil {
		t.Fatalf("newSudachiAnalyzer: %v", err)
	}
	// Fill the semaphore without making an HTTP request.
	analyzer.jobs <- struct{}{}
	defer func() { <-analyzer.jobs }()

	started := time.Now()
	_, err = analyzer.analyze(context.Background(), "待機")
	if err != context.DeadlineExceeded {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("slot timeout took too long: %v", elapsed)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func testHTTPResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Status:     http.StatusText(statusCode),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func testToken(surface, pos0, pos1, conjugation, dictionary, reading string, oov bool) sudachiToken {
	pos2 := "*"
	switch {
	case pos0 == "名詞" && pos1 == "普通名詞":
		pos2 = "一般"
	case pos0 == "名詞" && pos1 == "固有名詞":
		pos2 = "一般"
	case pos0 == "接尾辞" && pos1 == "名詞的":
		pos2 = "一般"
	}
	return sudachiToken{
		Surface:        surface,
		PartOfSpeech:   []string{pos0, pos1, pos2, "*", conjugation, "*"},
		NormalizedForm: dictionary,
		DictionaryForm: dictionary,
		ReadingForm:    reading,
		DictionaryID:   0,
		OOV:            oov,
	}
}
