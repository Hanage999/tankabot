package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/hanage999/tankabot"
)

const defaultAPIURL = "http://localhost:8080"

var analyzeKugiri = tankabot.AnalyzeKugiri

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("kugiri", flag.ContinueOnError)
	flags.SetOutput(stderr)
	apiURL := flags.String("api-url", apiURLFromEnvironment(), "Sudachi HTTP APIのベースURL")
	timeout := flags.Duration("timeout", 20*time.Second, "Sudachi API解析のタイムアウト")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "使い方: kugiri [オプション] [文字列]")
		fmt.Fprintln(stderr, "文字列を省略した場合は標準入力から読み込みます。")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	input := strings.Join(flags.Args(), " ")
	if input == "" {
		data, err := io.ReadAll(stdin)
		if err != nil {
			fmt.Fprintf(stderr, "標準入力を読み込めませんでした: %v\n", err)
			return 1
		}
		input = strings.TrimRight(string(data), "\r\n")
	}
	if strings.TrimSpace(input) == "" {
		flags.Usage()
		return 2
	}

	result, err := analyzeKugiri(context.Background(), input, *apiURL, *timeout)
	if err != nil {
		fmt.Fprintf(stderr, "区切りを解析できませんでした: %v\n", err)
		return 1
	}

	surfaces := make([]string, len(result.Segments))
	morae := make([]string, len(result.Segments))
	for i, segment := range result.Segments {
		surfaces[i] = segment.Surface
		morae[i] = fmt.Sprintf("%d", segment.MoraCount)
	}
	fmt.Fprintln(stdout, "区切り:")
	fmt.Fprintln(stdout, strings.Join(surfaces, "｜"))
	fmt.Fprintln(stdout, "拍数:")
	fmt.Fprintln(stdout, strings.Join(morae, "｜"))
	fmt.Fprintln(stdout, "短歌:")
	if result.Tankas == "" {
		fmt.Fprintln(stdout, "なし")
	} else {
		fmt.Fprintln(stdout, result.Tankas)
	}
	return 0
}

func apiURLFromEnvironment() string {
	if value := strings.TrimSpace(os.Getenv("TANKABOT_SUDACHI_API_URL")); value != "" {
		return value
	}
	return defaultAPIURL
}
