# 設定リファレンス

- 作成日: 2026-07-02
- ステータス: Draft
- 関連ドキュメント: [プロジェクト概要](../overview.md), [設定管理 タスク](../tasks/0001_config/01_requirements.md)

## 位置付け

本ドキュメントは `internal/config` パッケージが読み込む設定値（TOML 設定ファイル・環境変数の双方）の統合リファレンスである。各項目の型・必須/任意・デフォルト値・書式や制約・記述例を記載する。実装レベルの設計判断（なぜこの構成にしたか）は [設定管理 アーキテクチャ設計書](../tasks/0001_config/02_architecture.md) を参照する。

## TOML 設定ファイル

`internal/config.Load(path)` が読み込む。すべての項目が必須であり、いずれかが欠落している場合は読み込みが失敗する（デフォルト値での黙った補完は行わない）。秘匿情報（app パスワード・Slack Webhook URL）は TOML には書かず、下記「環境変数」の節で扱う。

| 項目名 | 型 | 必須/任意 | デフォルト値 | 書式・制約 |
|---|---|---|---|---|
| `retention_days` | 整数 | 必須 | なし | 正の整数（`1` 以上）。`0` 以下は起動失敗（全投稿即削除を防ぐ fail-closed 検証） |
| `schedule` | 文字列 | 必須 | なし | cron 相当のスケジュール文字列。本パッケージはキーの存在確認のみを行い（空文字列 `""` は許容される）、cron 構文としての妥当性検証は行わない（[0007_docker_distribution](../tasks/0007_docker_distribution/01_requirements.md) の `print-schedule` サブコマンドの責務） |
| `execution_timeout_seconds` | 整数 | 必須 | なし | 秒単位。`1`〜`86400`（24時間）の範囲の整数。`0` 以下または `86400` を超える値は起動失敗 |

### 記述例

```toml
retention_days = 30
schedule = "0 3 * * *"
execution_timeout_seconds = 3600
```

## 環境変数

`internal/config.LoadCredentials()` が読み込む。いずれも秘匿情報として扱い、TOML には書かない。Docker 配布時は `.env` 経由で渡す（[Docker 配布の詳細設計](docker_deployment.md) 参照）。

| 変数名 | 型 | 必須/任意 | デフォルト値 | 書式・制約 |
|---|---|---|---|---|
| `BSKY_HANDLE` | 文字列 | 必須 | なし | Bluesky アカウントの handle（例: `alice.bsky.social`）。空文字列・未設定はいずれも起動失敗 |
| `BSKY_APP_PASSWORD` | 文字列（秘匿） | 必須 | なし | Bluesky の app パスワード。空文字列・未設定はいずれも起動失敗 |
| `BSKY_SLACK_WEBHOOK_URL_SUCCESS` | 文字列（秘匿・URL） | 任意 | 未設定（該当チャンネルへの通知を行わない） | 成功時通知用の Slack Incoming Webhook URL。設定する場合はスキームが `https` であること、かつホスト部を含む構文的に妥当な URL であることのみを検証する（パス・クエリパラメータの内容には制約を設けない）。未設定の場合はエラーにせず、当該チャンネルへの通知を行わない設定として扱う |
| `BSKY_SLACK_WEBHOOK_URL_FAILURE` | 文字列（秘匿・URL） | 任意 | 未設定（該当チャンネルへの通知を行わない） | 失敗時通知用の Slack Incoming Webhook URL。制約・未設定時の挙動は `BSKY_SLACK_WEBHOOK_URL_SUCCESS` と同じ |

Slack Webhook URL のホスト（`hooks.slack.com` 等）が実際に Slack のものであることの一致検証は本パッケージでは行わない（[0006_slack_notification](../tasks/0006_slack_notification/01_requirements.md) の責務）。

### 記述例（`.env`）

```
BSKY_HANDLE=alice.bsky.social
BSKY_APP_PASSWORD=xxxx-xxxx-xxxx-xxxx
BSKY_SLACK_WEBHOOK_URL_SUCCESS=https://hooks.slack.com/services/<workspace-id>/<channel-id>/<token>
BSKY_SLACK_WEBHOOK_URL_FAILURE=https://hooks.slack.com/services/<workspace-id>/<channel-id>/<token>
```

## 秘匿情報の取り扱い

`BSKY_APP_PASSWORD` と Slack Webhook URL（正常系・異常系）は `internal/config.SecretString` 型でラップされ、`%v`/`%s`/`%#v` によるフォーマットや `log/slog` 経由の構造化ログでは固定文字列（`"[REDACTED]"`）に置き換わる。実際の値を取得できるのは `Reveal()` の呼び出しのみであり、認証リクエスト構築や Slack 通知ペイロード構築などの利用直前でのみ呼び出す運用とする。`BSKY_HANDLE` は Bluesky 上で公開されるアカウント識別子であり秘匿情報ではないため、`SecretString` でラップしない。
