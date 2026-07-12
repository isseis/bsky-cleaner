[English](slack_notification_security.md) | Japanese

# Slack 通知のセキュリティ設計

- 作成日: 2026-07-09
- 最終更新日: 2026-07-12
- ステータス: Final
- 関連ドキュメント: [プロジェクト概要](../overview.ja.md), [設定リファレンス](configuration.ja.md)

## 位置付け

本ドキュメントは、Slack 通知機能（`internal/notify` パッケージ）におけるセキュリティ対策の詳細を記載する。以下の攻撃ベクトルに対する防御を扱う。

- Webhook URL の漏洩（ログ・エラーメッセージ経由）
- 投稿本文を介した Slack マークアップインジェクション（`@channel` / `@here` メンション拡散）
- 制御文字によるログ偽装・画面操作
- ペイロードサイズの際限ない増大
- 悪意あるホストへの Webhook 送信（SSRF 的リスク）

## 対象範囲

- `internal/config` パッケージの Webhook URL 検証・秘匿化
- `internal/notify` パッケージのペイロード構築・送信
- `internal/notify` パッケージによる、`internal/retry` のログ URL マスキング機構を用いたリトライログのマスキング

以下のトピックは本ドキュメントの対象外とする。

- 実行結果の通知仕様そのもの（正常系・異常系の振り分けなど）: [プロジェクト概要](../overview.ja.md#実行結果の通知) を参照
- Slack Incoming Webhook の取得方法・Slack アプリの設定手順

## 設計

### Webhook URL の秘匿化

Slack Webhook URL は、値を知っていれば該当チャンネルに投稿できてしまうケーパビリティを持つ値であるため、app パスワードと同様に秘匿情報として扱う。

#### SecretString 型によるラップ

`internal/config` パッケージの `SecretString` 型で Webhook URL をラップする。これにより以下の特性を持つ。

- **ログ出力時の保護**: `%v`/`%s`/`%#v` によるフォーマットや `log/slog` 経由の構造化ログでは固定文字列 `[REDACTED]` に置き換わる
- **値の取得**: 実際の値は `Reveal()` の呼び出しのみで取得可能。Slack 通知ペイロード構築時の HTTP リクエスト直前でのみ呼び出す運用とする

`SecretString` の実装は `internal/config/secret.go` にあり、`fmt.Stringer`・`fmt.GoStringer`・`slog.LogValuer` の各インターフェースを実装している。

#### Webhook URL のログマスキング

`internal/notify` パッケージの `redactWebhookURL` 関数が、`internal/retry` パッケージのリトライログに出力される URL をマスキングする。ログにはスキームとホスト部のみが記録され、パス（Webhook トークンを含む）は `[REDACTED]` に置き換わる。

```go
func redactWebhookURL(req *http.Request) string {
    return fmt.Sprintf("%s://%s/[REDACTED]", req.URL.Scheme, req.URL.Host)
}
```

### Webhook URL のホスト検証

TOML 設定ファイルの `slack_allowed_host` フィールドで許可するホスト（例: `hooks.slack.com`）を指定し、設定された Webhook URL のホスト部と一致することを検証する。検証は `internal/config/validate.go` の `validateSlackAllowedHost` 関数が行う。

- 成功時・失敗時それぞれの Webhook URL について、ホスト部（ポート番号を除く、大文字小文字を区別しない）が `slack_allowed_host` と一致することを確認する
- 不一致の場合は fail-closed とする。すなわち、検証エラー時に通知を諦めて送信するのではなく、設定エラーとしてツールの起動自体を失敗させる
- 両方の Webhook URL が未設定（環境変数が空）の場合は、`slack_allowed_host` が未設定でも起動失敗しない
- いずれかの Webhook URL が設定されている場合は `slack_allowed_host` が必須となる

### ペイロードのサニタイズ

`internal/notify/payload.go` の `sanitizeForPayload` 関数が、Slack 通知ペイロードに含める外部由来テキストに対して二段階のサニタイズを適用する。

#### 第一段階: 制御文字の除去

`internal/notify/sanitize.go` の `Sanitize` 関数（`internal/sanitize.ControlChars` に委譲）が、C0 制御文字（0x00-0x1F、ESC 0x1B を含む）と DEL（0x7F）を除去する。

- **ANSI エスケープシーケンス対策**: ESC（0x1B）を除去することで、端末エミュレータに対する画面操作・クリップボード書き込み等の ANSI エスケープ注入を防ぐ
- **log injection 対策**: 改行（`\n`、`\r`）も C0 制御文字として同時に除去されるため、Slack メッセージの構造破壊やログ偽装を防ぐ

#### 第二段階: Slack マークアップのエスケープ

`escapeSlackMarkup` 関数が、Slack の mrkdwn 特殊記法を無害化する。特に以下のパターンを防ぐ。

- `<!channel>` / `<!here>` / `<!subteam^ID>` 等のメンション構文（`<` で始まる）
- その他のマークアップ構文

エスケープの順序は以下の通り。

1. `&` → `&amp;`（最初に行うことで、後続のエスケープで生成される `&` が再エスケープされるのを防ぐ）
2. `<` → `&lt;`
3. `>` → `&gt;`

#### ペイロードに含める情報の制限

Slack 通知ペイロードには以下の情報のみを含め、投稿本文は一切含めない。

- 実行結果（成功/失敗）の種別
- 実行ホスト名（`Host`。TOML の `hostname` フィールド、または未設定時は `os.Hostname()`）
- 認証に使用した Bluesky ハンドル（`Account`）
- 削除対象数・削除件数・実行時間（`Targets`/`Deleted`/`Duration`。`report.Result` を生成できた実行でのみ付与）
- 削除に失敗した投稿の rkey とエラー種別、または実行自体を中断させたエラー種別（`errorKind` による分類テキスト）

Host・Account・Targets/Deleted/Duration はいずれも秘密情報ではないが、外部由来の値（ホスト名、ハンドル、rkey、エラーテキスト）は他のフィールドと同様にサニタイズ・エスケープの対象となる（後述）。

`atproto.Post` は投稿本文のフィールドを持たない設計としており、コードレベルで投稿本文がペイロードに含まれる経路が存在しない（`internal/atproto/posts.go` の `Post` 構造体、`internal/notify/payload.go` の `buildPayload` で確認）。

### ペイロードサイズの制限

`buildPayload` 関数は出力テキストの上限を `maxPayloadLength`（4,000 バイト）に制限する。超過時は末尾を切り詰め、`...(truncated)` を付加する。これにより、大量の削除失敗が発生した場合でもペイロードが無制限に肥大化するのを防ぐ。

### エラーメッセージの秘密情報保護

`internal/notify/notify.go` の `SendError` 型は、Webhook 送信エラーのエラーメッセージに以下を含めない。

- Webhook URL そのもの
- 生の `net/http` / `net/url` エラー文字列

`Error()` メソッドはエラー種別（`errorKind` による分類）または HTTP ステータスコードのみを返す。

```go
func (e *SendError) Error() string {
    if e.StatusCode == 0 {
        return fmt.Sprintf("slack notify: send failed: %s", errorKind(e.Err))
    }
    return fmt.Sprintf("slack notify: send failed: HTTP status %d", e.StatusCode)
}
```

### リトライとタイムアウト

#### リトライポリシー

Slack 通知のリトライポリシーは、API 呼び出し（`internal/atproto`）のリトライよりも意図的に軽量に設定している。理由は Slack 通知の失敗が CLI の終了コードに影響しない（非 fatal）ためであり、短い worst-case 遅延に抑えることを優先するためである。

- 最大リトライ回数: 2 回
- 初回バックオフ: 1 秒
- 最大バックオフ: 4 秒

#### 試行ごとのタイムアウト

`perAttemptTimeoutDoer` が各リトライ試行に独立したタイムアウト（デフォルト 3 秒）を適用する。これにより、1 回の試行が遅延しても後続の試行の猶予を消費しない。`internal/retry` の `Doer` は全試行で同一のリクエスト（とそのコンテキスト）をクローンして使うため、このラッパーなしでは 1 回目の試行がタイムアウトするとそれ以降の試行も即座にコンテキストキャンセルされる。

## 攻撃ベクトルと対策の対応表

| 攻撃ベクトル | 対策 | 実装箇所 |
|---|---|---|
| Webhook URL のログ漏洩 | `SecretString` 型によるマスキング | `internal/config/secret.go` |
| Webhook URL のログ漏洩（リトライログ） | `redactWebhookURL` による URL マスキング | `internal/notify/notify.go` |
| 悪意あるホストへの Webhook 送信 | `slack_allowed_host` によるホスト検証（fail-closed） | `internal/config/validate.go` |
| Slack メンションインジェクション（`@channel` 等） | `escapeSlackMarkup` による `<`/`>`/`&` のエスケープ | `internal/notify/payload.go` |
| ANSI エスケープ注入 | `Sanitize` による ESC（0x1B）の除去 | `internal/notify/sanitize.go` |
| log injection（改行混入） | `Sanitize` による改行の除去 | `internal/notify/sanitize.go` |
| ペイロード肥大化 | `maxPayloadLength`（4,000 バイト）による制限 | `internal/notify/payload.go` |
| エラーメッセージ経由の秘密情報漏洩 | `SendError.Error()` の URL・生エラー文字列除外 | `internal/notify/notify.go` |
| 送信のハング | `perAttemptTimeoutDoer` による試行ごとのタイムアウト | `internal/notify/notify.go` |
