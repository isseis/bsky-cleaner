[English](README.md) | Japanese

# bsky-cleaner

[Bluesky](https://bsky.app/)（AT Protocol）アカウントを定期的にクリーンアップする Go 製 CLI ツールです。
設定した保持期間より古い投稿を削除します。

## 前提条件

- Bluesky アカウントの [アプリパスワード](https://bsky.app/settings/app-passwords)
- Docker Compose（v2 以降）※ Docker Compose での実行方法を使う場合

## インストールと実行（Docker Compose）

Docker イメージが [GHCR](https://ghcr.io/isseis/bsky-cleaner) で配布されており、
Docker Compose を使うのが標準的な利用方法です。

### 1. 環境変数ファイルを準備する

```sh
curl -O https://raw.githubusercontent.com/isseis/bsky-cleaner/main/dot.env.example
cp dot.env.example .env
```

`.env` を編集し、以下の秘匿情報を設定する。`BSKY_HANDLE` と `BSKY_APP_PASSWORD` は例の値ではなく、
自分の Bluesky アカウントの値に置き換えること。

```sh
BSKY_HANDLE=alice.bsky.social      # 自分のハンドルに置き換える
BSKY_APP_PASSWORD=xxxx-xxxx-xxxx-xxxx  # 自分のアプリパスワードに置き換える
# 以下は Slack 通知を使う場合のみ設定する（省略可）
BSKY_SLACK_WEBHOOK_URL_SUCCESS=https://hooks.slack.com/services/...
BSKY_SLACK_WEBHOOK_URL_FAILURE=https://hooks.slack.com/services/...
```

`BSKY_APP_PASSWORD` は、通常のログインパスワードではなく
[アプリパスワード発行ページ](https://bsky.app/settings/app-passwords)で発行した専用のパスワードを使う。

各変数の詳細は後述の[環境変数](#環境変数)を参照。

### 2. 設定ファイル（TOML）を用意する

```sh
mkdir -p config
```

`config/config.toml` を作成する。

```toml
retention_days = 30
schedule = "0 3 * * *"  # 毎日 3:00 に実行（分 時 日 月 曜日 の順。* は「毎回（すべて）」を表す）
execution_timeout_seconds = 3600
slack_allowed_host = "hooks.slack.com"  # Slack 通知を使う場合のみ設定する
```

各フィールドの詳細は後述の[TOML 設定ファイル](#toml-設定ファイル)を参照。

### 3. docker-compose.yml を用意する

```sh
curl -O https://raw.githubusercontent.com/isseis/bsky-cleaner/main/docker-compose.yml
```

`image:` のバージョンタグを、使いたいリリースバージョンに合わせて確認・修正する。

### 4. コンテナを起動する

```sh
docker compose pull
docker compose up -d
```

コンテナは TOML 設定ファイルの `schedule` フィールドに従って定期実行される。
起動確認は以下のコマンドで行う。

```sh
docker compose ps
docker compose logs
```

### 5. 設定を確認する（試験実行）

`schedule` による定期実行を待つと、設定ミスに気づくのが早くても翌日以降になってしまう。
コンテナ内のバイナリを直接呼び出すことで、`schedule` を待たずにその場で試験実行できる。

```sh
docker compose exec bsky-cleaner /usr/local/bin/bsky-cleaner --config /config/config.toml
```

試験実行なので投稿は削除されない。ログイン・設定読み込み・削除対象の一覧表示までが
エラーなく完了すれば、`.env` と `config.toml` は正しく設定されている。

### アップグレード

1. `docker-compose.yml` を編集し、`image:` のバージョンタグを上げる
2. `docker compose pull` で新しいイメージを取得する
3. `docker compose up -d` で新しいイメージで再起動する

## インストールと実行（ビルド済み実行ファイル）

Docker を使わずホスト上で直接実行したい場合は、[GitHub Releases](https://github.com/isseis/bsky-cleaner/releases)
からビルド済みバイナリ（`linux/amd64`）をダウンロードできる。

```sh
# vX.Y.Z は使いたいリリースバージョンに置き換える
curl -LO https://github.com/isseis/bsky-cleaner/releases/download/vX.Y.Z/bsky-cleaner-vX.Y.Z-linux-amd64.tar.gz
curl -LO https://github.com/isseis/bsky-cleaner/releases/download/vX.Y.Z/SHA256SUMS

# チェックサムを検証する
sha256sum -c SHA256SUMS

tar xzf bsky-cleaner-vX.Y.Z-linux-amd64.tar.gz
```

展開された `bsky-cleaner` バイナリを、後述の[使い方](#使い方)に従って実行する。
定期実行にはシステムの cron を使う（[cron によるスケジューリング](#cron-によるスケジューリング)を参照）。

現時点で配布しているのは `linux/amd64` バイナリのみ。macOS/Windows 向けバイナリは提供していない。

ソースからビルドしたい場合は、[ソースからのビルド](docs/dev/developer_guide/build_from_source.md)を参照。

## 設定

bsky-cleaner は秘匿情報以外の設定を **TOML 設定ファイル** から、秘匿情報を **環境変数** から読み込む。
秘匿情報が TOML ファイルに書き込まれることはない。

### TOML 設定ファイル

TOML ファイル（例: `config.toml`）を作成する。

```toml
retention_days = 30
schedule = "0 3 * * *"  # 毎日 3:00 に実行
execution_timeout_seconds = 3600
slack_allowed_host = "hooks.slack.com"
```

| フィールド | 型 | 必須 | 説明 |
|---|---|---|---|
| `retention_days` | int | Yes | この日数より古い投稿を削除する。1 以上である必要がある |
| `execution_timeout_seconds` | int | Yes | 最大実行時間（秒）。1〜86400 |
| `schedule` | string | No | 定期実行する時刻を [cron 形式](https://en.wikipedia.org/wiki/Cron#Overview)（`分 時 日 月 曜日` の5項目、`*` は「毎回（すべて）」を表す。例: `0 3 * * *` = 毎日 3:00）で指定する。コンテナ内蔵のスケジューラー（supercronic）での定期実行を使う場合にのみ必要。直接実行やシステム crontab を使う場合は省略する |
| `slack_allowed_host` | string | 条件付き | Slack webhook URL を設定する場合は必須。webhook URL がこのホスト（例: `hooks.slack.com`）を指しているか検証する |

詳細は[設定リファレンス](docs/design/configuration.ja.md)を参照。

### 環境変数

`BSKY_HANDLE` と `BSKY_APP_PASSWORD` は例の値ではなく、自分の Bluesky アカウントの値に置き換えること。

```sh
export BSKY_HANDLE=alice.bsky.social      # 自分のハンドルに置き換える
export BSKY_APP_PASSWORD=xxxx-xxxx-xxxx-xxxx  # 自分のアプリパスワードに置き換える
# 以下は Slack 通知を使う場合のみ設定する（省略可）
export BSKY_SLACK_WEBHOOK_URL_SUCCESS=https://hooks.slack.com/services/...
export BSKY_SLACK_WEBHOOK_URL_FAILURE=https://hooks.slack.com/services/...
```

| 変数 | 必須 | 説明 |
|---|---|---|
| `BSKY_HANDLE` | Yes | Bluesky のハンドル（例: `alice.bsky.social`） |
| `BSKY_APP_PASSWORD` | Yes | Bluesky のアプリパスワード。通常のログインパスワードではなく、[アプリパスワード発行ページ](https://bsky.app/settings/app-passwords)で発行した専用のパスワードを使う |
| `BSKY_SLACK_WEBHOOK_URL_SUCCESS` | No | 成功通知用の Slack webhook |
| `BSKY_SLACK_WEBHOOK_URL_FAILURE` | No | 失敗通知用の Slack webhook |

### 設定を確認する（試験実行）

Docker を使わない場合は、TOML 設定ファイルと環境変数の準備ができたら、まずは試験実行して設定が正しいことを確認する。

```sh
./bsky-cleaner --config config.toml
```

試験実行なので投稿は削除されない。ログイン・設定読み込み・削除対象の一覧表示までが
エラーなく完了すれば、設定は正しく行われている。

## 使い方

```sh
# 試験実行: 削除対象の投稿を一覧表示するだけで、実際には削除しない
bsky-cleaner --config config.toml

# 適用: 実際に投稿を削除する
bsky-cleaner --config config.toml --apply

# バージョン情報を表示する
bsky-cleaner --version
# 出力例: v1.2.3 (a1b2c3d)

# cron スケジュールを表示する（Docker/cron での定期実行用）
bsky-cleaner print-schedule --config config.toml
```

### 終了コード

| コード | 意味 |
|---|---|
| `0` | 成功 — 対象の投稿をすべて削除した（または試験実行が完了した） |
| `1` | セットアップ/実行時の失敗 — 設定エラー、ログイン失敗、ネットワークエラーなど |
| `2` | 使い方エラー — `--config` の指定漏れ、未知のフラグ、余分な位置引数など |
| `3` | 部分的な失敗 — 一部の投稿は削除できたが、少なくとも1件の削除に失敗した |

### cron によるスケジューリング

Docker を使わずホスト上で定期実行したい場合は、システムの crontab にバイナリを登録する
（TOML の `schedule` フィールドは使わない）。

秘匿情報を crontab に直接書き込むと、cron がジョブ実行時にコマンドライン全体（環境変数の値を含む）を
syslog に記録することがあり、意図せず秘匿情報がログに残ってしまう。これを避けるため、秘匿情報は
`export VAR=VALUE` 形式のファイル（`cron.env` など。Docker Compose 版の `.env` とは形式が異なるため
別名にする）に書き込み、パーミッションを `600` に制限した上で、cron エントリからは読み込むだけにする。

`cron.env`（例。`BSKY_HANDLE` と `BSKY_APP_PASSWORD` は自分の値に置き換える）:

```sh
export BSKY_HANDLE=alice.bsky.social
export BSKY_APP_PASSWORD=xxxx-xxxx-xxxx-xxxx
# 以下は Slack 通知を使う場合のみ設定する（省略可）
export BSKY_SLACK_WEBHOOK_URL_SUCCESS=https://hooks.slack.com/services/...
export BSKY_SLACK_WEBHOOK_URL_FAILURE=https://hooks.slack.com/services/...
```

```sh
chmod 600 /path/to/cron.env
```

crontab に登録するエントリは `分 時 日 月 曜日 コマンド` の形式で書く（`*` は「毎回（すべて）」を表す）。
以下の例の `0 3 * * *` は「毎日 3:00 に実行する」という意味になる。

```cron
0 3 * * * . /path/to/cron.env && /path/to/bsky-cleaner --apply --config /path/to/config.toml
```

`crontab -e` でエディタが開くので、上記の行を追記して保存する。

## 安全性

- **デフォルトでは試験実行** — `--apply` を指定しない限り投稿は削除されない
- **フェイルクローズ** — 不正な設定（例: `retention_days = 0`、Slack webhook ホストの不一致）はデフォルト値で
  処理を続行せず、起動時に失敗する
- **秘匿情報のマスキング** — アプリパスワードと webhook URL は、
  ログやエラーメッセージ上では `[REDACTED]` と表示される
- **固定表示（ピン留め）された投稿は削除対象から除外される**
- **Slack 通知は at-least-once 配信を保証しない** — 何らかの理由（プロセスクラッシュを含む）で
  通知の送信に失敗した場合、実行結果、特に完了済みの削除結果が通知されないまま
  失われることがある

詳細は[セキュリティ設計](docs/design/security.ja.md)を参照。

## 開発者向け情報

- **GitHub リポジトリ**: [https://github.com/isseis/bsky-cleaner](https://github.com/isseis/bsky-cleaner)
- **ソースコードの取得・ビルド**: [ソースからのビルド](docs/dev/developer_guide/build_from_source.md)
- **プロジェクトの規約**: [CLAUDE.md](CLAUDE.md)
- **開発者向けドキュメント**: [docs/dev/developer_guide/](docs/dev/developer_guide/)

## ライセンス

[MIT](LICENSE)
