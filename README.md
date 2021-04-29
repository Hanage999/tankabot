# tankabot

自分がフォローしているアカウントの投稿から短歌（五七五七七）になっている部分を見つけ出し、リプライでお知らせする Mastodon ボットです。[俳句検出bot](https://github.com/theoria24/FindHaiku4Mstdn)の亜流です。

## 依存ソフトウェア
以下があらかじめインストールされていないと起動しません。
+ [mecab](https://github.com/taku910/mecab)

## 機能
+ ホームタイムラインにいるアカウントの投稿を見守って短歌を検出する。
+ フォローすると自動でフォローバックしてくる。
+ 「フォロー解除」とメンションするかDMすると、フォローを解除してくる。
+ -p <整数> オプション付きで起動すると、<整数>分限定で起動する。

## 使い方
1. cmd/tankabot フォルダで go get、go build すると、フォルダに tankabot コマンドができる。
1. config.yml.example を config.yml にリネームまたはコピーし、自分の環境に応じて変更してください。
1. ./tankabot で起動。screen などと併用するか、systemd でサービス化してください。