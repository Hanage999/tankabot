package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hanage999/tankabot"
)

func TestRunPrintsTankabotSegmentation(t *testing.T) {
	replaceAnalyzer(t, func(_ context.Context, text, apiURL string, timeout time.Duration) (tankabot.KugiriResult, error) {
		if text != "和田アキ子" || apiURL != "http://sudachi.test" || timeout != 20*time.Second {
			t.Fatalf("analyzer args = (%q, %q, %s)", text, apiURL, timeout)
		}
		return tankabot.KugiriResult{Segments: []tankabot.KugiriSegment{
			{Surface: "和田", MoraCount: 2},
			{Surface: "アキ子。", MoraCount: 3},
		}}, nil
	})

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"-api-url", "http://sudachi.test", "和田アキ子"}, strings.NewReader(""), &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %q", exitCode, stderr.String())
	}
	want := "区切り:\n和田｜アキ子。\n拍数:\n2｜3\n短歌:\nなし\n"
	if got := stdout.String(); got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestRunReadsStandardInput(t *testing.T) {
	replaceAnalyzer(t, func(_ context.Context, text, _ string, _ time.Duration) (tankabot.KugiriResult, error) {
		if text != "言葉" {
			t.Fatalf("text = %q, want 言葉", text)
		}
		return tankabot.KugiriResult{Segments: []tankabot.KugiriSegment{{Surface: "言葉。", MoraCount: 3}}}, nil
	})

	var stdout, stderr bytes.Buffer
	exitCode := run(nil, strings.NewReader("言葉\n"), &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "言葉。") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func replaceAnalyzer(t *testing.T, replacement func(context.Context, string, string, time.Duration) (tankabot.KugiriResult, error)) {
	t.Helper()
	original := analyzeKugiri
	analyzeKugiri = replacement
	t.Cleanup(func() { analyzeKugiri = original })
}

func TestRunRejectsEmptyInput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if exitCode := run(nil, strings.NewReader("\n"), &stdout, &stderr); exitCode != 2 {
		t.Fatalf("exit code = %d, want 2", exitCode)
	}
	if !strings.Contains(stderr.String(), "使い方:") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunHelpSucceeds(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if exitCode := run([]string{"-h"}, strings.NewReader(""), &stdout, &stderr); exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}
	if !strings.Contains(stderr.String(), "使い方:") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}
