package tankabot

import (
	"context"
	"log"
	"os/exec"
	"strconv"
	"time"

	"github.com/comail/colog"
	"github.com/spf13/viper"
)

var (
	version = "1"
)

type commonSettings struct {
	maxRetry      int
	retryInterval time.Duration
	yahooClientID string
	langJobPool   chan int
}

// Initialize は、config.ymlに従ってbotとデータベース接続を初期化する。
func Initialize() (bot Persona, db DB, err error) {
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
			return bot, db, err
		}
	}

	var cr map[string]string

	// bot設定ファイル読み込み
	conf := viper.New()
	conf.SetConfigName("config")
	conf.AddConfigPath(".")
	conf.SetConfigType("yaml")
	if err := conf.ReadInConfig(); err != nil {
		log.Printf("alert: 設定ファイルが読み込めませんでした")
		return bot, db, err
	}
	conf.UnmarshalKey("Persona", &bot)
	var cmn commonSettings
	cmn.maxRetry = 5
	cmn.retryInterval = time.Duration(5) * time.Second
	cmn.yahooClientID = conf.GetString("OpenCageKey")
	nOfJobs := conf.GetInt("NumConcurrentLangJobs")
	if nOfJobs <= 0 {
		nOfJobs = 1
	} else if nOfJobs > 10 {
		nOfJobs = 10
	}
	cmn.langJobPool = make(chan int, nOfJobs)
	bot.commonSettings = &cmn
	cr = conf.GetStringMapString("DBCredentials")

	// botをMastodonサーバに接続
	if err := connectPersona(&bot); err != nil {
		log.Printf("alert: %s をMastodonサーバに接続できませんでした。終了します", bot.Name)
		return bot, db, err
	}

	// データベースへの接続
	db, err = newDB(cr)
	if err != nil {
		log.Printf("alert: データベースへの接続が確保できませんでした")
		return bot, db, err
	}

	// botがまだデータベースに登録されていなかったら登録
	if err = db.addNewBot(&bot); err != nil {
		log.Printf("alert: データベースにbotが登録できませんでした")
		return bot, db, err
	}

	// botのデータベース上のIDを取得
	id, err := db.botID(&bot)
	if err != nil {
		log.Printf("alert: botのデータベース上のIDが取得できませんでした")
		return bot, db, err
	}
	bot.DBID = id

	// botの住処を登録
	if bot.LivesWithSun {
		log.Printf("info: %s の所在地を設定しています……", bot.Name)
		time.Sleep(1001 * time.Millisecond)
		bot.PlaceName, bot.TimeZone, err = getLocDataFromCoordinates(bot.commonSettings.yahooClientID, bot.Latitude, bot.Longitude)
		if err != nil {
			log.Printf("alert: %s の所在地情報の設定に失敗しました：%s", bot.Name, err)
			return bot, db, err
		}
	}

	return
}

// ActivateBot は、botを活動させる。
func ActivateBot(bot *Persona, db DB, p int) (err error) {
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
	go bot.spawn(ctx, db, true, false)

	<-ctx.Done()
	log.Printf("info: %d分経ったのでシャットダウンします", p)
	return
}
