# tankabot

自分がフォローしているアカウントの投稿から短歌（五七五七七）になっている部分を見つけ出し、リプライでお知らせする Mastodon ボットです。[俳句検出bot](https://github.com/theoria24/FindHaiku4Mstdn)の亜流です。

## 依存ソフトウェア
以下があらかじめ利用可能でないと起動・動作しません。
+ MySQL
+ Sudachi HTTP API（既定値 `http://localhost:8080`、分割モードB）

Sudachi APIの接続先とタイムアウトは、`config.yml` の `SudachiAPIURL` と `SudachiAPITimeout` で変更できます。接続先にはベースURLを指定し、botが `/v1/analyze` を付加します。

Sudachiの品詞階層は `sudachidict_full` v20260723 の全52種類を監査済みです。未監査の品詞階層が辞書更新やユーザー辞書によって返された場合は、誤判定を続けず解析エラーとして扱います。辞書更新後の実API互換性は次で確認できます。

```bash
TANKABOT_SUDACHI_API_URL=http://localhost:8080 go test -run TestLiveSudachiAPICompatibility
```

## 機能
+ ホームタイムラインにいるアカウントの投稿を見守って短歌を検出する。
+ フォローすると自動でフォローバックしてくる。
+ 「フォロー解除」とメンションするかDMすると、フォローを解除してくる。
+ 寝る。寝ている間はトゥートも反応もしない。寝ている間に通知が来ていたら、起きた時に対応する。就寝時刻と起床時刻は自由に設定可。二つを同時刻に設定すれば、寝ない。
+ 設定ファイルでLivesWithSunをtrueに設定すると、LatitudeとLongitudeで指定した地点での太陽の出入り時刻に応じて寝起きする。ジオコーディングデータは[Yahoo! YOLP API](https://developer.yahoo.co.jp/webapi/map/)から、時刻は[Sunrise Sunset](https://sunrise-sunset.org/api)からそれぞれ取得。
+ 設定ファイルでRandomFrequencyをゼロ以上にすると、不定期にネットの記事から短歌を拾って呟く。（この機能を使わない場合は、RandomFrequencyはゼロに設定してください）
+ -p <整数> オプション付きで起動すると、<整数>分限定で起動する。

## 句切り確認コマンド

`cmd/kugiri` は、任意の文字列をbotと同じ規則で区切り、各単位の拍数と検出された短歌を表示します。

```bash
go build -o kugiri ./cmd/kugiri
./kugiri -api-url http://localhost:8080 '解析する文字列'
```

文字列は標準入力からも渡せます。接続先は `TANKABOT_SUDACHI_API_URL` 環境変数でも指定できます。

```bash
echo '解析する文字列' | TANKABOT_SUDACHI_API_URL=http://localhost:8080 ./kugiri
```

## 使い方
0. 下準備：database_tables.sql の記載に従って、MySQLデータベースにテーブルを作成する。定期的に[feedAggregator](https://blog.crazynewworld.net/2018/10/29/323/)などを使ってRSSアイテムを収集しておく。
1. cmd/tankabot フォルダで go get、go build すると、フォルダに tankabot コマンドができる。
1. config.yml.example を config.yml にリネームまたはコピーし、自分の環境に応じて変更してください。
1. ./tankabot で起動。screen などと併用するか、systemd でサービス化してください。

## クレジット
+ Webサービス by Yahoo! JAPAN (https://developer.yahoo.co.jp/sitemap/)
