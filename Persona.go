package tankabot

import (
	"context"
	"log"
	"math/rand"
	"runtime"
	"sort"
	"strconv"
	"time"

	mastodon "github.com/hanage999/go-mastodon"
)

// Persona は、botの属性を格納する。
type Persona struct {
	Name            string
	Instance        string
	Client          *mastodon.Client
	AccessToken     string
	MyID            mastodon.ID
	Title           string
	Starter         string
	Assertion       string
	ItemPool        int
	MorningComments []string
	EveningComments []string
	Hashtags        []string
	DBID            int
	WakeHour        int
	WakeMin         int
	SleepHour       int
	SleepMin        int
	LivesWithSun    bool
	Latitude        float64
	Longitude       float64
	PlaceName       string
	TimeZone        string
	RandomFrequency int
	Awake           time.Duration
	*commonSettings
}

// getMastoID はbotのMastodonアカウントIDを取得する。
func (bot *Persona) getMastoID() (err error) {
	ctx := context.Background()

	bot.Client = mastodon.NewClient(&mastodon.Config{
		Server:      bot.Instance,
		AccessToken: bot.AccessToken,
	})

	var acc *mastodon.Account
	for i := 0; i < bot.commonSettings.maxRetry+45; i++ {
		acc, err = bot.Client.GetAccountCurrentUser(ctx)
		if err == nil {
			bot.MyID = acc.ID
			return
		}
		time.Sleep(bot.commonSettings.retryInterval)
		log.Printf("alert: %s のアカウントIDが取得できません：%s", bot.Name, err)
	}

	log.Printf("alert: %s のアカウントID取得がリトライ上限に達しました：%s", bot.Name, err)
	return
}

// spawn は、botの活動を開始する
func (bot *Persona) spawn(ctx context.Context, db DB, firstLaunch bool, nextDayOfPolarNight bool) {
	sleep, active := getDayCycle(bot.WakeHour, bot.WakeMin, bot.SleepHour, bot.SleepMin)
	bot.Awake = active

	if bot.LivesWithSun {
		sl, ac, cond, err := getDayCycleBySunMovement(bot.TimeZone, bot.Latitude, bot.Longitude)
		if err == nil {
			sleep, active = sl, ac
			bot.Awake = ac
			switch cond {
			case "白夜":
				log.Printf("info: %s がいる %s は今、白夜です", bot.Name, bot.PlaceName)
				if !firstLaunch {
					go func() {
						toot := mastodon.Toot{Status: bot.PlaceName + "はいま、もっとも昏き頃合いなれど、白き夜ゆえ日隠るることなし。さてもわが目の閉じるやあらむ"}
						if err := bot.post(ctx, toot); err != nil {
							log.Printf("info: %s がトゥートできませんでした。今回は諦めます……", bot.Name)
						}
					}()
				}
			case "極夜":
				log.Printf("info: %s がいる %s は今、極夜です", bot.Name, bot.PlaceName)
				if !firstLaunch && nextDayOfPolarNight {
					go func() {
						toot := mastodon.Toot{Status: bot.PlaceName + "はいま、もっとも日高き頃合いなれど、夜極まりて光も射さず、たえてわが目の覚むることなし💤"}
						if err := bot.post(ctx, toot); err != nil {
							log.Printf("info: %s がトゥートできませんでした。今回は諦めます……", bot.Name)
						}
					}()
				}
			default:
				log.Printf("info: %s の所在地、起床までの時間、起床後の活動時間：", bot.Name)
				log.Printf("info: %s、%s、%s", bot.PlaceName, sleep, active)
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
		wakeWithSun = bot.PlaceName + "も"
		sleepWithSun = bot.PlaceName + "より"
	}

	if sleep > 0 {
		t := time.NewTimer(sleep)
		defer t.Stop()
		if !firstLaunch && !nextDayOfPolarNight {
			go func() {
				idx := rand.Intn(len(bot.EveningComments))
				msg := bot.EveningComments[idx]
				toot := mastodon.Toot{Status: msg + sleepWithSun + "今宵はこれにて💤……"}
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
		if err := bot.checkNotifications(newCtx); err != nil {
			log.Printf("info: %s が通知を遡れませんでした。今回は諦めます……", bot.Name)
		}
		if sleep > 0 {
			go func() {
				idx := rand.Intn(len(bot.MorningComments))
				msg := bot.MorningComments[idx]
				toot := mastodon.Toot{Status: msg + wakeWithSun + "夜が明けましてござります"}
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

func (bot *Persona) checkNotifications(ctx context.Context) (err error) {
	ns, err := bot.notifications(ctx)
	if err != nil {
		log.Printf("info: %s が通知一覧を取得できませんでした：%s", bot.Name, err)
		return
	}

	sort.Sort(ns)

	for _, n := range ns {
		switch n.Type {
		case "mention":
			if err = bot.respondToMention(ctx, n.Account, n.Status); err != nil {
				log.Printf("info: %s がメンションに反応できませんでした：%s", bot.Name, err)
				return
			}
		case "reblog":
			// TODO
		case "favourite":
			// TODO
		case "follow":
			if err = bot.respondToFollow(ctx, n.Account); err != nil {
				log.Printf("info: %s がフォローに反応できませんでした：%s", bot.Name, err)
				return
			}
		}
		if err = bot.dismissNotification(ctx, n.ID); err != nil {
			log.Printf("info: %s が id:%s の通知を削除できませんでした：%s", bot.Name, string(n.ID), err)
			return
		}
	}

	return
}

type Notifications []*mastodon.Notification

func (ns Notifications) Len() int {
	return len(ns)
}

func (ns Notifications) Swap(i, j int) {
	ns[i], ns[j] = ns[j], ns[i]
}

func (ns Notifications) Less(i, j int) bool {
	iv, _ := strconv.Atoi(string(ns[i].ID))
	jv, _ := strconv.Atoi(string(ns[j].ID))
	return iv < jv
}

// favは、ステータスをふぁぼる。失敗したらmaxRetryを上限に再試行する。
func (bot *Persona) fav(ctx context.Context, id mastodon.ID) (err error) {
	time.Sleep(time.Duration(rand.Intn(2000)+1000) * time.Millisecond)
	for i := 0; i < bot.commonSettings.maxRetry; i++ {
		_, err = bot.Client.Favourite(ctx, id)
		if err == nil {
			return
		}
		log.Printf("info: %s がふぁぼれません：%s", bot.Name, err)
		time.Sleep(bot.commonSettings.retryInterval)
	}

	log.Printf("info: %s のふぁぼがリトライ上限に達しました：%s", bot.Name, err)
	return
}

// post は投稿する。失敗したらmaxRetryを上限に再試行する。
func (bot *Persona) post(ctx context.Context, toot mastodon.Toot) (err error) {
	time.Sleep(time.Duration(rand.Intn(5000)+3000) * time.Millisecond)
	for i := 0; i < bot.commonSettings.maxRetry; i++ {
		_, err = bot.Client.PostStatus(ctx, &toot)
		if err == nil {
			return
		}
		log.Printf("info: %s がトゥートできません：%s\n %s", bot.Name, toot.Status, err)
		time.Sleep(bot.commonSettings.retryInterval)
	}

	log.Printf("info: %s のトゥートがリトライ上限に達しました：%s\n %s", bot.Name, toot.Status, err)
	return
}

// follow はアカウントをフォローする。失敗したらmaxRetryを上限に再試行する。
func (bot *Persona) follow(ctx context.Context, id mastodon.ID) (err error) {
	time.Sleep(time.Duration(rand.Intn(2000)+1000) * time.Millisecond)
	for i := 0; i < bot.commonSettings.maxRetry; i++ {
		_, err = bot.Client.AccountFollow(ctx, id)
		if err == nil {
			return
		}
		log.Printf("info: %s がフォローできません：%s", bot.Name, err)
		time.Sleep(bot.commonSettings.retryInterval)
	}

	log.Printf("info: %s のフォローがリトライ上限に達しました：%s", bot.Name, err)
	return
}

// follow はアカウントをアンフォローする。失敗したらmaxRetryを上限に再試行する。
func (bot *Persona) unfollow(ctx context.Context, id mastodon.ID) (err error) {
	time.Sleep(time.Duration(rand.Intn(2000)+1000) * time.Millisecond)
	for i := 0; i < bot.commonSettings.maxRetry; i++ {
		_, err = bot.Client.AccountUnfollow(ctx, id)
		if err == nil {
			return
		}
		log.Printf("info: %s がアンフォローできません：%s", bot.Name, err)
		time.Sleep(bot.commonSettings.retryInterval)
	}

	log.Printf("info: %s アンフォローがリトライ上限に達しました：%s", bot.Name, err)
	return
}

// relationWith はアカウントと自分との関係を取得する。失敗したらmaxRetryを上限に再実行する。
func (bot *Persona) relationWith(ctx context.Context, id mastodon.ID) (rel []*mastodon.Relationship, err error) {
	for i := 0; i < bot.commonSettings.maxRetry; i++ {
		rel, err = bot.Client.GetAccountRelationships(ctx, []string{string(id)})
		if err == nil {
			return
		}
		log.Printf("info: %s と id:%s の関係が取得できません：%s", bot.Name, string(id), err)
		time.Sleep(bot.commonSettings.retryInterval)
	}

	log.Printf("info: %s と id:%s の関係取得がリトライ上限に達しました：%s", bot.Name, string(id), err)
	return
}

func (bot *Persona) notifications(ctx context.Context) (ns Notifications, err error) {
	var pg mastodon.Pagination
	for i := 0; i < bot.commonSettings.maxRetry; i++ {
		ns, err = bot.Client.GetNotifications(ctx, &pg)
		if err == nil {
			return
		}
		log.Printf("info: %s が通知一覧を取得できません：%s", bot.Name, err)
		time.Sleep(bot.commonSettings.retryInterval)
	}

	log.Printf("info: %s の通知一覧取得がリトライ上限に達しました：%s", bot.Name, err)
	return
}

func (bot *Persona) dismissNotification(ctx context.Context, id mastodon.ID) (err error) {
	for i := 0; i < bot.commonSettings.maxRetry; i++ {
		err = bot.Client.DismissNotification(ctx, id)
		if err == nil {
			return
		}
		log.Printf("info: %s が id:%s の通知を削除できません：%s", bot.Name, string(id), err)
		time.Sleep(bot.commonSettings.retryInterval)
	}

	log.Printf("info: %s が id:%s の通知削除がリトライ上限に達しました：%s", bot.Name, string(id), err)
	return
}
