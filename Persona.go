package tankabot

import (
	"context"
	"log"
	"math/rand"
	"regexp"
	"runtime"
	"strings"
	"time"

	mastodon "github.com/hanage999/go-mastodon"
)

// Persona は、botの属性を格納する。
type Persona struct {
	Name     string
	Instance string
	MyApp    *MastoApp
	Email    string
	Password string
	Client   *mastodon.Client
	MyID     mastodon.ID
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

// moitor はwebsocketでタイムラインを監視して反応する。
func (bot *Persona) monitor(ctx context.Context) {
	log.Printf("trace: Goroutines: %d", runtime.NumGoroutine())
	log.Printf("info: %s がタイムライン監視を開始しました", bot.Name)
	newCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	evch, err := bot.openStreaming(newCtx)
	if err != nil {
		log.Printf("info: %s がストリーミングを受信開始できませんでした", bot.Name)
		return
	}

	ers := ""
	for ev := range evch {
		switch t := ev.(type) {
		case *mastodon.UpdateEvent:
			go func() {
				if err := bot.respondToUpdate(newCtx, t); err != nil {
					log.Printf("info: %s がトゥートに反応できませんでした", bot.Name)
				}
			}()
		case *mastodon.NotificationEvent:
			go func() {
				if err := bot.respondToNotification(newCtx, t); err != nil {
					log.Printf("info: %s が通知に反応できませんでした", bot.Name)
				}
			}()
		case *mastodon.ErrorEvent:
			ers = t.Error()
			log.Printf("info: %s がエラーイベントを受信しました：%s", bot.Name, ers)
		}
	}

	if ctx.Err() != nil {
		log.Printf("info: %s が今回のタイムライン監視を終了しました：%s", bot.Name, ctx.Err())
	} else {
		itvl := rand.Intn(4000) + 1000
		log.Printf("info: %s の接続が切れました。%dミリ秒後に再接続します：%s", bot.Name, itvl, ers)
		time.Sleep(time.Duration(itvl) * time.Millisecond)
		go bot.monitor(ctx)
	}
}

// openStreaming はHTLのストリーミング接続を開始する。失敗したらmaxRetryを上限に再試行する。
func (bot *Persona) openStreaming(ctx context.Context) (evch chan mastodon.Event, err error) {
	wsc := bot.Client.NewWSClient()
	for i := 0; i < bot.commonSettings.maxRetry; i++ {
		evch, err = wsc.StreamingWSUser(ctx)
		if err != nil {
			time.Sleep(bot.commonSettings.retryInterval)
			log.Printf("info: %s のストリーミング受信開始をリトライします：%s", bot.Name, err)
			continue
		}
		log.Printf("trace: %s のストリーミング受信に成功しました", bot.Name)
		return
	}
	log.Printf("info: %s のストリーミング受信開始に失敗しました：%s", bot.Name, err)
	return
}

// respondToUpdate はstatusに反応する。
func (bot *Persona) respondToUpdate(ctx context.Context, ev *mastodon.UpdateEvent) (err error) {
	orig := ev.Status
	rebl := false
	if orig.Reblog != nil {
		orig = orig.Reblog
		rebl = true
	}

	// メンション・ブースト・プライベートは無視
	if len(ev.Status.Mentions) != 0 || rebl || orig.Visibility == "private" {
		return
	}

	// 自分の投稿は無視
	if orig.Account.ID == bot.MyID {
		return
	}

	// 投稿から短歌を探す
	text := textContent(orig.Content)
	if text == "" || !isJap(text) {
		return
	}
	tankas := extractTankas(text, bot.langJobPool)

	if tankas != "" {
		msg := "@" + orig.Account.Acct + " 短歌を発見しました！\n" + tankas
		st := ""
		if orig.SpoilerText != "" {
			st = "短歌を発見しました！"
			msg = "@" + orig.Account.Acct + " \n" + tankas
		}
		toot := mastodon.Toot{Status: msg, SpoilerText: st, Visibility: orig.Visibility, InReplyToID: orig.ID}
		if err = bot.post(ctx, toot); err != nil {
			log.Printf("info: %s がリプライに失敗しました", bot.Name)
			return err
		}
	}

	return
}

// respondToNotification は通知に反応する。
func (bot *Persona) respondToNotification(ctx context.Context, ev *mastodon.NotificationEvent) (err error) {
	switch ev.Notification.Type {
	case "mention":
		if err = bot.respondToMention(ctx, ev.Notification.Account, ev.Notification.Status); err != nil {
			log.Printf("info: %s がメンションに反応できませんでした：%s", bot.Name, err)
			return
		}
	case "reblog":
		// TODO
	case "favourite":
		// TODO
	case "follow":
		if err = bot.respondToFollow(ctx, ev.Notification.Account); err != nil {
			log.Printf("info: %s がフォローに反応できませんでした：%s", bot.Name, err)
			return
		}
	}
	return
}

// respondToMention はメンションに反応する。
func (bot *Persona) respondToMention(ctx context.Context, account mastodon.Account, status *mastodon.Status) (err error) {
	r := regexp.MustCompile(`:.*:\z`)
	name := account.DisplayName
	if r.MatchString(name) {
		name = name + " "
	}
	txt := textContent(status.Content)

	if strings.Contains(txt, "フォロー解除") {
		rel, err := bot.relationWith(ctx, account.ID)
		if err != nil {
			log.Printf("info: %s が関係取得に失敗しました", bot.Name)
			return err
		}
		if (*rel[0]).Following == true {
			if err = bot.unfollow(ctx, account.ID); err != nil {
				log.Printf("info: %s がアンフォローに失敗しました", bot.Name)
				return err
			}
		}
	}

	return
}

// respondToFollow はフォローに反応する。
func (bot *Persona) respondToFollow(ctx context.Context, account mastodon.Account) (err error) {
	rel, err := bot.relationWith(ctx, account.ID)
	if err != nil {
		log.Printf("info: %s が関係取得に失敗しました", bot.Name)
		return err
	}
	if (*rel[0]).Following == false {
		if err = bot.follow(ctx, account.ID); err != nil {
			log.Printf("info: %s がフォローに失敗しました", bot.Name)
			return err
		}
	}

	return
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
