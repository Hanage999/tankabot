package tankabot

import (
	"database/sql"
	"log"
	"math/rand"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql" // for sql library
)

// DB は、データベース接続を格納する。
type DB struct {
	*sql.DB
}

// Item は、itemsテーブルの行データを格納する。
type Item struct {
	ID      int
	Title   string
	URL     string
	Content string
	Summary string
	Songs   string
	Updated time.Time
}

// newDBは、新たなデータベース接続を作成する。
func newDB(cr map[string]string) (db DB, err error) {
	dbase, err := sql.Open("mysql", cr["user"]+":"+
		cr["password"]+
		"@tcp("+cr["Server"]+")/"+
		cr["database"]+
		"?parseTime=true&loc=Asia%2FTokyo")
	if err != nil {
		log.Printf("alert: データベースがOpenできませんでした：%s", err)
		return db, err
	}

	// 接続確認
	if err := dbase.Ping(); err != nil {
		log.Printf("alert: データベースに接続できませんでした：%s", err)
		return db, err
	}

	db = DB{dbase}
	return
}

// addNewBotは、もし新しいbotだったらデータベースに登録する。
func (db DB) addNewBot(bot *Persona) (err error) {
	vst := "(?, ?, ?)"
	now := time.Now()
	_, err = db.Exec(`
		INSERT IGNORE INTO
			bots (name, created_at, updated_at)
		VALUES `+vst,
		bot.Name, now, now,
	)
	if err != nil {
		log.Printf("info: botテーブルが更新できませんでした：%s", err)
		return
	}

	// auto_incrementの値を調整
	_, err = db.Exec(`
		ALTER TABLE bots
		AUTO_INCREMENT = 1
	`)
	if err != nil {
		log.Printf("info: itemsテーブルの自動採番値が調整できませんでした：%s", err)
		return
	}

	return
}

// deleteOldCandidates は、多すぎるトゥート候補を古いものから削除する
func (db DB) deleteOldCandidates(bot *Persona) (err error) {
	_, err = db.Exec(`
		DELETE FROM song_candidates
		WHERE
			bot_id = ? AND id not in (
				SELECT * FROM (
					SELECT id FROM song_candidates
					WHERE bot_id = ?
					ORDER BY updated_at DESC limit ?
				) v
			)`,
		bot.DBID,
		bot.DBID,
		bot.ItemPool,
	)
	if err != nil {
		log.Printf("alert: %s のDBエラーです：%s", bot.Name, err)
	}
	return
}

// stockItemsは、新規RSSアイテムの中からbotが興味を持ったitemをストックする。
func (db DB) stockItems(bot *Persona) (inStock int, err error) {
	// botの情報を取得
	var checkedUntil int
	if err := db.QueryRow(`
		SELECT
			checked_until
		FROM
			bots
		WHERE
			name = ?`,
		bot.Name,
	).Scan(&checkedUntil); err != nil {
		log.Printf("info: botsテーブルから %s の情報取得に失敗しました：%s", bot.Name, err)
		return 0, err
	}

	// itemsテーブルから新規itemを取得
	rows, err := db.Query(`
		SELECT
			id, title, url, updated_at, content
		FROM
			items
		WHERE
			id > ?
		ORDER BY
			id DESC`,
		checkedUntil,
	)
	if err != nil {
		log.Printf("info: itemsテーブルから %s の趣味を集め損ねました：%s", bot.Name, err)
		return
	}
	defer rows.Close()

	// 結果を保存
	items := make([]Item, 0)
	for rows.Next() {
		var id int
		var title, url, content string
		var updated time.Time
		if err := rows.Scan(&id, &title, &url, &updated, &content); err != nil {
			log.Printf("info: itemsテーブルから一行の情報取得に失敗しました：%s", err)
			continue
		}
		items = append(items, Item{ID: id, Title: title, URL: url, Updated: updated, Content: content})
	}
	err = rows.Err()
	if err != nil {
		log.Printf("info: itemテーブルへの接続に結局失敗しました：%s", err)
		return
	}
	rows.Close()

	// 結果から、興味のある物件を収集
	tb := time.Now()

	myItems := make([]Item, 0)
	for _, item := range items {
		str := item.Content
		songs := extractTankas(str, bot.langJobPool)
		if songs == "" {
			continue
		}
		newItem := item
		newItem.Songs = songs
		myItems = append(myItems, newItem)
		log.Printf("trace: 収集されたitem_id: %d、 短歌：%s", newItem.ID, newItem.Songs)
	}

	// 新規物件があったらcandidatesに登録
	if len(myItems) > 0 {
		vsts := make([]string, 0)
		params := make([]interface{}, 0)
		now := time.Now()
		for _, item := range myItems {
			vsts = append(vsts, "(?, ?, ?, ?, ?)")
			params = append(params, bot.DBID, item.ID, now, item.Updated, item.Songs)
		}
		vst := strings.Join(vsts, ", ")
		_, err = db.Exec(`
			INSERT IGNORE INTO
				song_candidates (bot_id, item_id, created_at, updated_at, songs)
			VALUES `+vst,
			params...,
		)
		if err != nil {
			log.Printf("info: song_candidatesテーブルが更新できませんでした：%s", err)
			return
		}
	}

	tf := time.Now()
	const layout = "01-02 15:04:05"
	log.Printf("trace: %s が、%s に見始めた %d 件のアイテムを %s に見終わりました", bot.Name, tb.Format(layout), len(items), tf.Format(layout))

	// botsテーブルのchecked_untilを更新
	if len(items) == 0 {
		return
	}
	_, err = db.Exec(`
		UPDATE bots
		SET checked_until = ?, updated_at = ?
		WHERE id = ?`,
		items[0].ID,
		time.Now(),
		bot.DBID,
	)
	if err != nil {
		log.Printf("info: %s のchecked_untilが更新できませんでした：%s", bot.Name, err)
		return
	}

	// candidatesの数を取得
	err = db.QueryRow(`
		SELECT
			COUNT(id)
		FROM song_candidates
		WHERE bot_id = ?
		`,
		bot.DBID,
	).Scan(&inStock)
	switch err {
	case sql.ErrNoRows, nil:
		err = nil
	default:
		log.Printf("info: song_candidatesテーブルから %s のネタストック数を取得し損ねました：%s", bot.Name, err)
	}

	return
}

// pickItemは、candidateから一件のitemをランダムで選択する。
func (db DB) pickItem(bot *Persona) (item Item, err error) {
	// candidates, itemsテーブルから新規itemを取得
	rows, err := db.Query(`
		SELECT
			song_candidates.item_id, song_candidates.songs, items.title, items.url
		FROM
			song_candidates
		INNER JOIN
			items
		ON
			song_candidates.item_id = items.id
		WHERE
			song_candidates.bot_id = ?`,
		bot.DBID,
	)
	if err != nil {
		log.Printf("info: %s の投稿候補を集め損ねました：%s", bot.Name, err)
		return
	}
	defer rows.Close()

	// 結果を保存
	items := make([]Item, 0)
	for rows.Next() {
		var id int
		var title, url, songs string
		if err := rows.Scan(&id, &songs, &title, &url); err != nil {
			log.Printf("info: itemsテーブルから一行の情報取得に失敗しました：%s", err)
			continue
		}
		items = append(items, Item{ID: id, Title: title, URL: url, Songs: songs})
	}
	err = rows.Err()
	if err != nil {
		log.Printf("info: itemテーブルの行読み込みに結局失敗しました：%s", err)
		return
	}
	rows.Close()
	if len(items) == 0 {
		return
	}

	// 一つランダムに選んで戻す
	n := len(items)
	if n > 0 {
		idx := 0
		if n > 1 {
			idx = rand.Intn(n)
		}

		item = items[idx]
	}

	return
}

// botIDは、botのデータベース上のIDを取得する。
func (db DB) botID(bot *Persona) (id int, err error) {
	if err = db.QueryRow(`
		SELECT
			id
		FROM
			bots
		WHERE
			name = ?`,
		bot.Name,
	).Scan(&id); err != nil {
		log.Printf("info: botsテーブルから %s のID取得に失敗しました：%s", bot.Name, err)
		return
	}
	return
}

// deleteItemは、song_candidatesから一件を削除する。
func (db DB) deleteItem(bot *Persona, item Item) (err error) {
	_, err = db.Exec(`
		DELETE FROM
			song_candidates
		WHERE
			item_id = ? AND bot_id = ?`,
		item.ID,
		bot.DBID,
	)
	if err != nil {
		log.Printf("alert: %s がsong_candidatesから%dの削除に失敗しました：%s", bot.Name, item.ID, err)
	}
	return
}
