package tankabot

import (
	"context"
	"log"
	"math/rand"
	"strings"
	"time"

	mastodon "github.com/hanage999/go-mastodon"
)

// randomTootは、ランダムにトゥートする。
func (bot *Persona) randomToot(ctx context.Context, db DB) {
	bt := 24 * 60 / bot.RandomFrequency
	ft := bt - bt*2/3 + rand.Intn(bt*4/3)
	itvl := time.Duration(ft) * time.Minute

	t := time.NewTimer(itvl)

	select {
	case <-t.C:
		go func() {
			if err := db.deleteOldCandidates(bot); err != nil {
				log.Printf("info :%s が古いトゥート候補の削除に失敗しました", bot.Name)
				return
			}
			stock, err := db.stockItems(bot)
			if err != nil {
				log.Printf("info: %s がアイテムの収集に失敗しました", bot.Name)
				return
			}
			if err := bot.newsToot(ctx, stock, db); err != nil {
				log.Printf("info: %s がニューストゥートに失敗しました", bot.Name)
			}
		}()
		bot.randomToot(ctx, db)
	case <-ctx.Done():
		t.Stop()
	}
}

func (bot *Persona) newsToot(ctx context.Context, stock int, db DB) (err error) {
	if stock == 0 {
		return
	}

	toot, item, err := bot.createSongNewsToot(db)
	if err != nil {
		log.Printf("info :%s が短歌ニューストゥートの作成に失敗しました", bot.Name)
		return err
	}
	if item.Title != "" {
		if err = bot.post(ctx, toot); err != nil {
			log.Printf("info: %s がトゥートできませんでした。今回は諦めます……", bot.Name)
		} else {
			if err = db.deleteItem(bot, item); err != nil {
				log.Printf("info: %s がトゥート済みアイテムの削除に失敗しました", bot.Name)
			}
		}
	}

	return
}

// createNewsTootはトゥートする内容を作成する。
func (bot *Persona) createSongNewsToot(db DB) (toot mastodon.Toot, item Item, err error) {
	// たまった候補からランダムに一つ選ぶ
	item, err = db.pickItem(bot)
	if err != nil {
		log.Printf("info: %s が投稿アイテムを選択できませんでした", bot.Name)
	}
	if item.Title == "" {
		return
	}

	// 投稿トゥート作成
	msg, err := bot.messageFromItem(item)
	if err != nil {
		log.Printf("info: %s がアイテムid %d から投稿文の作成に失敗しました：%s", bot.Name, item.ID, err)
	}

	if msg != "" {
		toot = mastodon.Toot{Status: msg}
	}
	return
}

// messageFromItemは、itemの内容から投稿文を作成する。
func (bot *Persona) messageFromItem(item Item) (msg string, err error) {
	var hashtagStr string
	for _, t := range bot.Hashtags {
		hashtagStr += `#` + t + " "
	}
	hashtagStr = strings.TrimSpace(hashtagStr)

	msg = item.Songs
	msg += "\n\n" + item.Title + " " + item.URL + "\n\n" + hashtagStr
	log.Printf("trace: %s のトゥート内容：\n\n%s", bot.Name, msg)
	return
}
