package tankabot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultSudachiAPIURL = "http://localhost:8080"
	sudachiSplitMode     = "B"
	maxSudachiResponse   = 8 << 20
)

// sudachiToken is the JSON representation returned by the Sudachi HTTP API.
type sudachiToken struct {
	Surface         string   `json:"surface"`
	PartOfSpeech    []string `json:"part_of_speech"`
	NormalizedForm  string   `json:"normalized_form"`
	DictionaryForm  string   `json:"dictionary_form"`
	ReadingForm     string   `json:"reading_form"`
	DictionaryID    int      `json:"dictionary_id"`
	SynonymGroupIDs []int    `json:"synonym_group_ids"`
	OOV             bool     `json:"oov"`
}

type sudachiAnalyzeResponse struct {
	Tokens []sudachiToken `json:"tokens"`
	Count  int            `json:"count"`
	Mode   string         `json:"mode"`
}

type sudachiErrorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// sudachiAnalyzer calls the remote Sudachi API and limits concurrent requests.
type sudachiAnalyzer struct {
	endpoint string
	client   *http.Client
	jobs     chan struct{}
	timeout  time.Duration
}

func newSudachiAnalyzer(baseURL string, timeout time.Duration, maxConcurrent int) (*sudachiAnalyzer, error) {
	baseURL = strings.TrimSpace(baseURL)
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("Sudachi API URLが不正です: %q", baseURL)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("Sudachi API URLのschemeはhttpまたはhttpsである必要があります: %q", parsed.Scheme)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("Sudachi API URLにqueryまたはfragmentは指定できません")
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("Sudachi API timeoutは正の値である必要があります")
	}
	if maxConcurrent <= 0 {
		return nil, fmt.Errorf("Sudachi API同時実行数は正の値である必要があります")
	}

	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/v1/analyze"
	return &sudachiAnalyzer{
		endpoint: parsed.String(),
		client:   &http.Client{Timeout: timeout},
		jobs:     make(chan struct{}, maxConcurrent),
		timeout:  timeout,
	}, nil
}

func (a *sudachiAnalyzer) analyze(ctx context.Context, text string) ([]sudachiToken, error) {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	select {
	case a.jobs <- struct{}{}:
		defer func() { <-a.jobs }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	body, err := json.Marshal(struct {
		Text string `json:"text"`
		Mode string `json:"mode"`
	}{Text: text, Mode: sudachiSplitMode})
	if err != nil {
		return nil, fmt.Errorf("Sudachi API requestの作成に失敗しました: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("Sudachi API requestの作成に失敗しました: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Sudachi APIへの接続に失敗しました: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, maxSudachiResponse+1))
	if err != nil {
		return nil, fmt.Errorf("Sudachi API responseの読み込みに失敗しました: %w", err)
	}
	if len(responseBody) > maxSudachiResponse {
		return nil, fmt.Errorf("Sudachi API responseが上限の%d bytesを超えました", maxSudachiResponse)
	}
	if resp.StatusCode != http.StatusOK {
		var apiErr sudachiErrorResponse
		if json.Unmarshal(responseBody, &apiErr) == nil && apiErr.Error.Message != "" {
			return nil, fmt.Errorf("Sudachi API error (%s, %s): %s", resp.Status, apiErr.Error.Code, apiErr.Error.Message)
		}
		return nil, fmt.Errorf("Sudachi API error: %s", resp.Status)
	}

	var result sudachiAnalyzeResponse
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return nil, fmt.Errorf("Sudachi API responseのJSONが不正です: %w", err)
	}
	if result.Mode != sudachiSplitMode {
		return nil, fmt.Errorf("Sudachi API responseのmodeが不正です: got %q, want %q", result.Mode, sudachiSplitMode)
	}
	if result.Count != len(result.Tokens) {
		return nil, fmt.Errorf("Sudachi API responseのcountが一致しません: count=%d, tokens=%d", result.Count, len(result.Tokens))
	}
	for i, token := range result.Tokens {
		if err := validateSudachiPartOfSpeech(token.PartOfSpeech); err != nil {
			return nil, fmt.Errorf("Sudachi API responseのtoken %d: %w", i, err)
		}
	}

	return result.Tokens, nil
}
