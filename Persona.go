package tankabot

import (
	"context"
	"log"
	"math/rand"
	"runtime"
	"time"

	mastodon "github.com/hanage999/go-mastodon"
)

// Persona は、botの属性を格納する。
type Persona struct {
	Name            string
	Instance        string
	MyApp           *MastoApp
	Email           string
	Password        string
	Client          *mastodon.Client
	MyID            mastodon.ID
	Title           string
	Starter         string
	Assertion       string
	ItemPool        int
	Hashtags        []string
	DBID            int
	WakeHour        int
	WakeMin         int
	SleepHour       int
	SleepMin        int
	LivesWithSun    bool
	Latitude        float64
	Longitude       float64
	LocInfo         OCResult
	RandomFrequency int
	Awake           time.Duration
	*commonSettings
}

// connectPersona はbotとMastodonサーバの接続を確立する。
func connectPersona(apps []*MastoApp, bot *Persona) (err error) {
	ctx := context.Background()

	bot.MyApp, err = getApp(bot.Instance, apps)
	if err != nil {
		log.Printf("alert: %s のためのアプリが取得できませんでした：%s", bot.Name, err)
		return
	}

	bot.Client = mastodon.NewClient(&mastodon.Config{
		Server:       bot.Instance,
		ClientID:     bot.MyApp.ClientID,
		ClientSecret: bot.MyApp.ClientSecret,
	})

	err = bot.Client.Authenticate(ctx, bot.Email, bot.Password)
	if err != nil {
		log.Printf("alert: %s がアクセストークンの取得に失敗しました：%s", bot.Name, err)
		return
	}

	acc, err := bot.Client.GetAccountCurrentUser(ctx)
	if err != nil {
		log.Printf("alert: %s のアカウントIDが取得できませんでした：%s", bot.Name, err)
		return
	}
	bot.MyID = acc.ID

	return
}

// spawn は、botの活動を開始する
func (bot *Persona) spawn(ctx context.Context, db DB, firstLaunch bool, nextDayOfPolarNight bool) {
	sleep, active := getDayCycle(bot.WakeHour, bot.WakeMin, bot.SleepHour, bot.SleepMin)

	if bot.LivesWithSun {
		sl, ac, cond, err := getDayCycleBySunMovement(bot.LocInfo.Annotations.Timezone.Name, bot.Latitude, bot.Longitude)
		if err == nil {
			sleep, active = sl, ac
			bot.Awake = ac
			switch cond {
			case "白夜":
				log.Printf("info: %s がいる %s は今、白夜です", bot.Name, getLocString(bot.LocInfo, false))
				if !firstLaunch {
					go func() {
						toot := mastodon.Toot{Status: getLocString(bot.LocInfo, false) + "はいま、もっとも昏き頃合いなれど、白き夜ゆえ日隠るることなし。さてもわが目の閉じるやあらむ"}
						if err := bot.post(ctx, toot); err != nil {
							log.Printf("info: %s がトゥートできませんでした。今回は諦めます……", bot.Name)
						}
					}()
				}
			case "極夜":
				log.Printf("info: %s がいる %s は今、極夜です", bot.Name, getLocString(bot.LocInfo, false))
				if !firstLaunch && nextDayOfPolarNight {
					go func() {
						toot := mastodon.Toot{Status: getLocString(bot.LocInfo, false) + "はいま、もっとも日高き頃合いなれど、夜極まりて光も射さず、たえてわが目の覚むることなし💤"}
						if err := bot.post(ctx, toot); err != nil {
							log.Printf("info: %s がトゥートできませんでした。今回は諦めます……", bot.Name)
						}
					}()
				}
			default:
				log.Printf("info: %s の所在地、起床までの時間、起床後の活動時間：", bot.Name)
				log.Printf("info: 　%s、%s、%s", getLocString(bot.LocInfo, true), sleep, active)
			}
		} else {
			log.Printf("info: %s の生活サイクルが太陽の出没から決められませんでした。デフォルトの起居時刻を使います：%s", bot.Name, err)
		}
	}

	go bot.daylife(ctx, db, sleep, active, firstLaunch, nextDayOfPolarNight)
}

// daylife は、botの活動サイクルを作る
func (bot *Persona) daylife(ctx context.Context, db DB, sleep time.Duration, active time.Duration, firstLaunch bool, nextDayOfPolarNight bool) {
	wakeWithSun, sleepWithSun := "", ""
	if bot.LivesWithSun {
		wakeWithSun = getLocString(bot.LocInfo, false) + "も"
		sleepWithSun = getLocString(bot.LocInfo, true) + "より"
	}

	if sleep > 0 {
		t := time.NewTimer(sleep)
		defer t.Stop()
		if !firstLaunch && !nextDayOfPolarNight {
			go func() {
				toot := mastodon.Toot{Status: "山高み夕日隠りぬ浅茅原。" + sleepWithSun + "今宵はこれにて💤……"}
				if err := bot.post(ctx, toot); err != nil {
					log.Printf("info: %s がトゥートできませんでした。今回は諦めます……", bot.Name)
				}
			}()
		}
	LOOP:
		for {
			select {
			case <-t.C:
				break LOOP
			case <-ctx.Done():
				return
			}
		}
	}

	newCtx, cancel := context.WithTimeout(ctx, active)
	defer cancel()

	if active > 0 {
		log.Printf("info: %s が起きたところ", bot.Name)
		log.Printf("trace: Goroutines: %d", runtime.NumGoroutine())
		nextDayOfPolarNight = false
		bot.activities(newCtx, db)
		if sleep > 0 {
			go func() {
				toot := mastodon.Toot{Status: "やうやう白くなりゆく山際。" + wakeWithSun + "夜が明けましてございます"}
				if err := bot.post(newCtx, toot); err != nil {
					log.Printf("info: %s がトゥートできませんでした。今回は諦めます……", bot.Name)
				}
			}()
		}
	} else {
		nextDayOfPolarNight = true
	}

	<-newCtx.Done()
	log.Printf("info: %s が寝たところ", bot.Name)
	log.Printf("trace: Goroutines: %d", runtime.NumGoroutine())
	if ctx.Err() == nil {
		bot.spawn(ctx, db, false, nextDayOfPolarNight)
	}
}

// activities は、botの活動の全てを実行する
func (bot *Persona) activities(ctx context.Context, db DB) {
	go bot.monitor(ctx)
	go bot.randomToot(ctx, db)
}

// post は投稿する。失敗したらmaxRetryを上限に再試行する。
func (bot *Persona) post(ctx context.Context, toot mastodon.Toot) (err error) {
	time.Sleep(time.Duration(rand.Intn(5000)+3000) * time.Millisecond)
	for i := 0; i < bot.commonSettings.maxRetry; i++ {
		_, err = bot.Client.PostStatus(ctx, &toot)
		if err != nil {
			time.Sleep(bot.commonSettings.retryInterval)
			log.Printf("info: %s がトゥートできませんでした。リトライします：%s\n %s", bot.Name, toot.Status, err)
			continue
		}
		break
	}
	return
}

// follow はアカウントをフォローする。失敗したらmaxRetryを上限に再試行する。
func (bot *Persona) follow(ctx context.Context, id mastodon.ID) (err error) {
	time.Sleep(time.Duration(rand.Intn(2000)+1000) * time.Millisecond)
	for i := 0; i < bot.commonSettings.maxRetry; i++ {
		_, err = bot.Client.AccountFollow(ctx, id)
		if err != nil {
			time.Sleep(bot.commonSettings.retryInterval)
			log.Printf("info: %s がフォローできませんでした。リトライします：%s", bot.Name, err)
			continue
		}
		break
	}
	return
}

// follow はアカウントをアンフォローする。失敗したらmaxRetryを上限に再試行する。
func (bot *Persona) unfollow(ctx context.Context, id mastodon.ID) (err error) {
	time.Sleep(time.Duration(rand.Intn(2000)+1000) * time.Millisecond)
	for i := 0; i < bot.commonSettings.maxRetry; i++ {
		_, err = bot.Client.AccountUnfollow(ctx, id)
		if err != nil {
			time.Sleep(bot.commonSettings.retryInterval)
			log.Printf("info: %s がアンフォローできませんでした。リトライします：%s", bot.Name, err)
			continue
		}
		break
	}
	return
}

// relationWith はアカウントと自分との関係を取得する。失敗したらmaxRetryを上限に再実行する。
func (bot *Persona) relationWith(ctx context.Context, id mastodon.ID) (rel []*mastodon.Relationship, err error) {
	for i := 0; i < bot.commonSettings.maxRetry; i++ {
		rel, err = bot.Client.GetAccountRelationships(ctx, []string{string(id)})
		if err != nil {
			time.Sleep(bot.commonSettings.retryInterval)
			log.Printf("info: %s と id:%s の関係が取得できませんでした。リトライします：%s", bot.Name, string(id), err)
			continue
		}
		break
	}
	return
}
