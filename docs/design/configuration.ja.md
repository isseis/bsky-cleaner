[English](configuration.md) | Japanese

# 設定リファレンス

- 作成日: 2026-07-02
- ステータス: Draft
- 関連ドキュメント: [プロジェクト概要](../overview.ja.md), [設定管理 タスク](../tasks/0001_config/01_requirements.md)

## 位置付け

本ドキュメントは `internal/config` パッケージが読み込む設定値（TOML 設定ファイル・環境変数の双方）の統合リファレンスである。各項目の型・必須/任意・デフォルト値・書式や制約・記述例を記載する。実装レベルの設計判断（なぜこの構成にしたか）は [設定管理 アーキテクチャ設計書](../tasks/0001_config/02_architecture.md) を参照する。

## TOML 設定ファイル

`internal/config.Load(path)` が読み込む。以下の方針で動作する。

- **必須項目**: `slack_allowed_host` と `schedule` を除く全項目。いずれかが欠落している場合は読み込みが失敗する（必須項目についてデフォルト値での暗黙の補完は行わない）。
- **任意項目**: デフォルト値は下記テーブルに明示する。
- **秘匿情報の扱い**: app パスワード・Slack Webhook URL は TOML には書かず、下記「環境変数」の節で扱う。
- **`slack_allowed_host` を TOML に置く理由**: Webhook URL 自体とは異なりそれ単体では投稿権限を持たないため秘匿情報として扱わない（[0006_slack_notification](../tasks/0006_slack_notification/01_requirements.md) 参照）。

| 項目名 | 型 | 必須/任意 | デフォルト値 | 書式・制約 |
|---|---|---|---|---|
| `retention_days` | 整数 | 必須 | なし | 正の整数（`1` 以上）。`0` 以下は起動失敗（全投稿即削除を防ぐ fail-closed 検証） |
| `schedule` | 文字列 | 任意 | `""`（キー省略時） | cron 相当のスケジュール文字列。Docker 配布時の `print-schedule` サブコマンド（[0007_docker_distribution](../tasks/0007_docker_distribution/01_requirements.md)）でのみ使用するため、Docker を介さず直接実行・crontab 登録する場合は不要。本パッケージ自体は値の有無のみを扱い、cron 構文としての妥当性検証は `print-schedule` 側の責務 |
| `execution_timeout_seconds` | 整数 | 必須 | なし | 秒単位。`1`〜`86400`（24時間）の範囲の整数。`0` 以下または `86400` を超える値は起動失敗 |
| `slack_allowed_host` | 文字列 | `BSKY_SLACK_WEBHOOK_URL_SUCCESS`/`BSKY_SLACK_WEBHOOK_URL_FAILURE` のいずれかが設定されている場合は必須 | 未設定 | Slack Webhook URL のホスト部として許可する値（例: `hooks.slack.com`）。設定されている Webhook URL のホスト部（ポート番号を除く、大文字小文字を区別しない）がこの値と一致しない場合、起動失敗（fail-closed）。Webhook URL が両方とも未設定の場合は本項目が未設定でも起動失敗しない（[0006_slack_notification](../tasks/0006_slack_notification/01_requirements.md) F-005） |
| `hostname` | 文字列 | 任意 | TOML キーが省略された場合・空文字列が設定された場合・空白のみの文字列が設定された場合、`os.Hostname()` の実行結果にフォールバック | Slack 通知で bsky-cleaner を実行しているマシンを識別するために使用する（[0013_slack_notification_context](../tasks/0013_slack_notification_context/01_requirements.md) 参照）。キー省略時と `""` 設定時は区別されない（`slack_allowed_host` と同様）。空白のみの値（例: `"  "`）も未設定として扱う。いずれの場合も `os.Hostname()` にフォールバックする |

### 記述例

```toml
retention_days = 30
schedule = "0 3 * * *"
execution_timeout_seconds = 3600
slack_allowed_host = "hooks.slack.com"
hostname = "worker-1"
```

### `execution_timeout_seconds` 設定時の注意点

`execution_timeout_seconds` は、投稿一覧取得・投稿削除などの個々の API 呼び出しがリトライ込みで要する最悪ケース時間を考慮して設定すること。以下に考慮すべき要素を列挙する。

- **リトライポリシーの最悪待機時間**: 本ツールのリトライポリシー（既定値: 最大リトライ回数5回、初回バックオフ1秒、最大バックオフ30秒）では、1回の API 呼び出しが継続的に一時的エラー（429/5xx/タイムアウト）に遭遇した場合の最悪ケース待機時間は約31秒である（[0005_retry_timeout アーキテクチャ設計書](../tasks/0005_retry_timeout/02_architecture.md#34-f-002実行タイムアウトの充足状況とリトライポリシーの数値ac-05ac-07)）。
- **削除対象投稿数への比例**: 削除対象の投稿数が多い場合、上記の待機時間が呼び出し回数分積み重なりうる。そのため `execution_timeout_seconds` はスケジュール間隔（`schedule`）より十分小さい範囲で、想定される最大投稿数を踏まえて余裕を持たせて設定することを推奨する。
- **タイムアウト到達時の安全性**: 実行タイムアウトに到達した場合、実行中の削除呼び出しは強制中断されるが、これによってデータが破壊されることはない（`DeleteRecord` の冪等性、[0002_atproto_client](../tasks/0002_atproto_client/01_requirements.md) AC-12 参照）。
- **Slack 通知の追加タイムアウト予算**: Slack 通知処理（[0006_slack_notification アーキテクチャ設計書](../tasks/0006_slack_notification/02_architecture.md#35-副作用契約dry-run--apply-とネットワーク送信) 3.5節）が、削除処理とは独立したタイムアウト予算（既定で約12秒: 単発HTTPタイムアウト3秒 × 最大3試行 + バックオフ1秒+2秒）を追加で消費する。`execution_timeout_seconds` とスケジュール間隔の余裕を検討する際はこの追加分も考慮すること。

## 環境変数

`internal/config.LoadCredentials()` が読み込む。以下の方針で動作する。

- **秘匿情報**: いずれも秘匿情報として扱い、TOML には書かない。
- **Docker 配布時**: `.env` 経由で渡す（[Docker 配布の詳細設計](docker_deployment.ja.md) 参照）。

| 変数名 | 型 | 必須/任意 | デフォルト値 | 書式・制約 |
|---|---|---|---|---|
| `BSKY_HANDLE` | 文字列 | 必須 | なし | Bluesky アカウントの handle（例: `alice.bsky.social`）。空文字列・未設定はいずれも起動失敗 |
| `BSKY_APP_PASSWORD` | 文字列（秘匿） | 必須 | なし | Bluesky の app パスワード。空文字列・未設定はいずれも起動失敗 |
| `BSKY_SLACK_WEBHOOK_URL_SUCCESS` | 文字列（秘匿・URL） | 任意 | 未設定（該当チャンネルへの通知を行わない） | 成功時通知用の Slack Incoming Webhook URL。設定する場合はスキームが `https` であること、かつホスト部を含む構文的に妥当な URL であることのみを検証する（パス・クエリパラメータの内容には制約を設けない）。未設定の場合はエラーにせず、当該チャンネルへの通知を行わない設定として扱う。なお環境変数が未設定の場合と空文字列に設定された場合は区別されず、いずれも「通知を行わない」として同一に扱われる |
| `BSKY_SLACK_WEBHOOK_URL_FAILURE` | 文字列（秘匿・URL） | 任意 | 未設定（該当チャンネルへの通知を行わない） | 失敗時通知用の Slack Incoming Webhook URL。制約・未設定時の挙動は `BSKY_SLACK_WEBHOOK_URL_SUCCESS` と同じ |

### ホスト検証のタイミング

Slack Webhook URL のホスト部が TOML `slack_allowed_host` と一致することの検証は、TOML（`Config`）と環境変数（`Credentials`）の両方を必要とする。このため `LoadAppConfig()` が両方を読み込んだ後に行う（[0006_slack_notification](../tasks/0006_slack_notification/01_requirements.md) F-005）。

### 記述例（`.env`）

```
BSKY_HANDLE=alice.bsky.social
BSKY_APP_PASSWORD=xxxx-xxxx-xxxx-xxxx
BSKY_SLACK_WEBHOOK_URL_SUCCESS=https://hooks.slack.com/services/<workspace-id>/<channel-id>/<token>
BSKY_SLACK_WEBHOOK_URL_FAILURE=https://hooks.slack.com/services/<workspace-id>/<channel-id>/<token>
```

## 秘匿情報の取り扱い

`BSKY_APP_PASSWORD` と Slack Webhook URL（正常系・異常系）は `internal/config.SecretString` 型でラップされる。これにより以下の特性を持つ。

- **ログ出力時の保護**: `%v`/`%s`/`%#v` によるフォーマットや `log/slog` 経由の構造化ログでは固定文字列（`[REDACTED]`）に置き換わる。
- **値の取得**: 実際の値を取得できるのは `Reveal()` の呼び出しのみであり、認証リクエスト構築や Slack 通知ペイロード構築などの利用直前でのみ呼び出す運用とする。
- **`BSKY_HANDLE` は対象外**: Bluesky 上で公開されるアカウント識別子であり秘匿情報ではないため、`SecretString` でラップしない。
