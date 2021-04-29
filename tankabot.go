package tankabot

import (
	"context"
	"log"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/comail/colog"
	"github.com/spf13/viper"
)

var (
	version  = "1"
	revision = "0"
)

type commonSettings struct {
	maxRetry      int
	retryInterval time.Duration
	langJobPool   chan int
}

// Initialize は、config.ymlに従ってbotとデータベース接続を初期化する。
func Initialize() (bot Persona, err error) {
	// colog 設定
	if version == "" {
		colog.SetDefaultLevel(colog.LDebug)
		colog.SetMinLevel(colog.LTrace)
		colog.SetFormatter(&colog.StdFormatter{
			Colors: true,
			Flag:   log.Ldate | log.Ltime | log.Lshortfile,
		})
	} else {
		colog.SetDefaultLevel(colog.LDebug)
		colog.SetMinLevel(colog.LInfo)
		colog.SetFormatter(&colog.StdFormatter{
			Colors: true,
			Flag:   log.Ldate | log.Ltime,
		})
	}
	colog.Register()

	// 依存アプリの存在確認
	for _, cmd := range []string{"mecab"} {
		_, err := exec.LookPath(cmd)
		if err != nil {
			log.Printf("alert: %s がインストールされていません！", cmd)
			return bot, err
		}
	}

	var appName string
	var apps []*MastoApp

	// bot設定ファイル読み込み
	conf := viper.New()
	conf.SetConfigName("config")
	conf.AddConfigPath(".")
	conf.SetConfigType("yaml")
	if err := conf.ReadInConfig(); err != nil {
		log.Printf("alert: 設定ファイルが読み込めませんでした")
		return bot, err
	}
	appName = conf.GetString("MastoAppName")
	conf.UnmarshalKey("Persona", &bot)
	var cmn commonSettings
	cmn.maxRetry = 5
	cmn.retryInterval = time.Duration(5) * time.Second
	nOfJobs := conf.GetInt("NumConcurrentLangJobs")
	if nOfJobs <= 0 {
		nOfJobs = 1
	} else if nOfJobs > 10 {
		nOfJobs = 10
	}
	cmn.langJobPool = make(chan int, nOfJobs)
	bot.commonSettings = &cmn

	// マストドンアプリ設定ファイル読み込み
	file, err := os.OpenFile("apps.yml", os.O_CREATE, 0666)
	if err != nil {
		log.Printf("alert: アプリ設定ファイルが作成できませんでした")
		return bot, err
	}
	file.Close()
	appConf := viper.New()
	appConf.AddConfigPath(".")
	appConf.SetConfigName("apps")
	appConf.SetConfigType("yaml")
	if err := appConf.ReadInConfig(); err != nil {
		log.Printf("alert: アプリ設定ファイルが読み込めませんでした")
		return bot, err
	}
	appConf.UnmarshalKey("MastoApps", &apps)

	// Mastodonクライアントの登録
	dirtyConfig := false
	updatedApps, err := initMastoApps(apps, appName, bot.Instance)
	if err != nil {
		log.Printf("alert: %s のためのアプリを登録できませんでした", bot.Instance)
		return bot, err
	}
	if len(updatedApps) > 0 {
		apps = updatedApps
		dirtyConfig = true
	}
	if dirtyConfig {
		appConf.Set("MastoApps", apps)
		if err := appConf.WriteConfig(); err != nil {
			log.Printf("alert: アプリ設定ファイルが書き込めませんでした：%s", err)
			return bot, err
		}
		log.Printf("info: 設定ファイルを更新しました")
	}

	// botをMastodonサーバに接続
	if err := connectPersona(apps, &bot); err != nil {
		log.Printf("alert: %s をMastodonサーバに接続できませんでした", bot.Name)
		return bot, err
	}

	return
}

// ActivateBot は、botを活動させる。
func ActivateBot(bot Persona, p int) (err error) {
	// 全てをシャットダウンするタイムアウトの設定
	ctx := context.Background()
	var cancel context.CancelFunc
	msg := "tankabot、時間無制限でスタートです！"
	if p > 0 {
		msg = "tankabots、" + strconv.Itoa(p) + "分間動きます！"
		dur := time.Duration(p) * time.Minute
		ctx, cancel = context.WithTimeout(ctx, dur)
		defer cancel()
	}
	log.Printf("info: " + msg)

	// 行ってらっしゃい
	go bot.monitor(ctx)

	<-ctx.Done()
	log.Printf("info: %d分経ったのでシャットダウンします", p)
	return
}
