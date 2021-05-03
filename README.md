# tankabot

自分がフォローしているアカウントの投稿から短歌（五七五七七）になっている部分を見つけ出し、リプライでお知らせする Mastodon ボットです。[俳句検出bot](https://github.com/theoria24/FindHaiku4Mstdn)の亜流です。

## 依存ソフトウェア
以下があらかじめインストールされていないと起動しません。
+ MySQL
+ [mecab](https://github.com/taku910/mecab)

## 機能
+ ホームタイムラインにいるアカウントの投稿を見守って短歌を検出する。
+ フォローすると自動でフォローバックしてくる。
+ 「フォロー解除」とメンションするかDMすると、フォローを解除してくる。
+ 寝る。寝ている間はトゥートも反応もしない。寝ている間に通知が来ていたら、起きた時に対応する。就寝時刻と起床時刻は自由に設定可。二つを同時刻に設定すれば、寝ない。
+ 設定ファイルでLivesWithSunをtrueに設定すると、LatitudeとLongitudeで指定した地点での太陽の出入り時刻に応じて寝起きする。逆ジオコーディングデータは[OpenCage Geocoder](https://opencagedata.com/api)から、時刻は[Sunrise Sunset](https://sunrise-sunset.org/api)からそれぞれ取得。
+ 設定ファイルでRandomFrequencyをゼロ以上にすると、不定期にネットの記事から短歌を拾って呟く。（この機能を使わない場合は、RandomFrequencyはゼロに設定してください）
+ -p <整数> オプション付きで起動すると、<整数>分限定で起動する。

## 使い方
0. 下準備：database_tables.sql の記載に従って、MySQLデータベースにテーブルを作成する。定期的に[feedAggregator](https://blog.crazynewworld.net/2018/10/29/323/)などを使ってRSSアイテムを収集しておく。
1. cmd/tankabot フォルダで go get、go build すると、フォルダに tankabot コマンドができる。
1. config.yml.example を config.yml にリネームまたはコピーし、自分の環境に応じて変更してください。
1. ./tankabot で起動。screen などと併用するか、systemd でサービス化してください。