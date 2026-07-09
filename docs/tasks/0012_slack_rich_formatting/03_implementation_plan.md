# Slack 通知リッチフォーマット — 実装計画書

## Document Status

| Item | Value |
|---|---|
| Status | `draft` |
| Created | 2026-07-09 |
| Review date | - |
| Reviewer | - |
| Comments | - |

関連ドキュメント: [要件定義書](01_requirements.md) / [アーキテクチャ設計書](02_architecture.md)

## 1. 実装概要

### 1.1 目的

`internal/notify` が構築する Slack 通知ペイロードを、現在の `{"text": "..."}` のみの構造から、見出しへの絵文字付与・サマリ/詳細分離・詳細ブロックへの色付け（`text` + `attachments`）に拡張する（F-001〜F-004）。既存のサニタイズ・切り詰め・チャンネル振り分け・秘密情報非混入の保証（0006 で確立済み）は後退させない。

### 1.2 実装方針

[アーキテクチャ設計書](02_architecture.md) 8節の実装優先順位にそのまま従う。

- フェーズ1: `payload.go` に `webhookPayload`/`slackAttachment`/`slackField` 型を追加し、`notify.go` から `webhookPayload` の定義を移す（設計書 3.2節、型のみの変更）
- フェーズ2: `isFailure()` を `payload.go` に抽出し、`send()`（`notify.go`）のインライン判定式を置き換える（設計書 3.1節、純粋なリファクタリング）
- フェーズ3: `buildPayload()` を新しい戻り値型 `webhookPayload` に対応させる（設計書 3.3節）
- フェーズ4: 切り詰め共通ヘルパー `truncate()` を抽出し、`text` と失敗一覧フィールドの `Value` の両方に独立して適用する（設計書 3.4節）
- フェーズ5: `internal/notify/test_helpers.go` の `BuildPayloadPreview` の戻り値型を追従させる
- フェーズ6: `payload_test.go` を新しいペイロード構造に合わせて更新し、設計書 7節のテストケースを追加する
- フェーズ7: `internal/notify/notifypreview/main.go` の `printScenarios()` を新構造の整形表示に対応させる（設計書 3.5節）
- フェーズ8: `make notify-preview-send` による実送信確認（設計書 3.5節・7節）
- フェーズ9: `docs/dev/developer_guide/package_reference.md` の記述更新
- フェーズ10: 品質確認（`make fmt` → `make test` → `make lint`）

### 1.3 既存コード調査結果

- **`internal/notify/notify.go`**（67-71行目）に `webhookPayload` 型が定義されている。現在は `Text string` の1フィールドのみ。`send()`（120行目）に判定式 `failed := outcome.Err != nil || (outcome.Result != nil && len(outcome.Result.Failed) > 0)` がインライン展開されており、132行目で `json.Marshal(webhookPayload{Text: buildPayload(outcome)})` として使われている。
- **`internal/notify/payload.go`** に現行の `buildPayload(outcome Outcome) string`（60行目）が存在し、`switch` の3分岐（`outcome.Err != nil` / `outcome.Result == nil` / デフォルト）でテキストを組み立てたのち、`maxPayloadLength`（25行目、4000）と `truncatedMarker`（29行目、`"...(truncated)"`）・`truncationCutPoint()`（93行目）で単一の切り詰め処理を行っている。`sanitizeForPayload()`（49行目）・`escapeSlackMarkup()`（37行目）は変更不要で、そのまま再利用する。
- **`internal/notify/test_helpers.go`** の `BuildPayloadPreview(outcome Outcome) string`（11行目）は `buildPayload` をそのまま返す薄いラッパー。戻り値型を `buildPayload` に追従させるだけで、ロジック変更は不要。
- **`internal/notify/payload_test.go`** に現行の `buildPayload` の全分岐をカバーするテストが存在する（`TestBuildPayload_SuccessOutcome_IncludesDeleteCountAndStatus` 等、全11件）。これらは戻り値が `string` から `webhookPayload` になることに伴い、`Text`/`Attachments` フィールドへのアサーションに書き換える。既存テストが検証している不変条件（サニタイズ・エスケープ・UTF-8安全な切り詰め・投稿本文非含有）はいずれも本タスク後も成立させる必要があるため、置き換え後のテストで同じ不変条件を確認する（フェーズ6で詳述）。
- **`internal/notify/notify_test.go`** の `TestSend_Success_PostsToSelectedWebhook`（50行目）は `json.Unmarshal(gotBody, &payload)` で `webhookPayload` にデコードし `payload.Text` を `assert.Contains` で検証している。`webhookPayload` の定義場所が `payload.go` に変わっても同一パッケージ内のため、このテストはフィールド構造さえ壊さなければ無変更で通る（`Text` は成功時サマリ文言を含み続けるため）。他の `TestSend_*`（送信先振り分け・リトライ・タイムアウト・URL秘匿系、計12件）は `buildPayload`/`isFailure` の戻り値型変更と無関係な振る舞い（送信先choice, HTTPリトライ）を検証しており、`isFailure()` への置き換えが振る舞いを変えないことの回帰テストとしてそのまま活用する（変更不要）。
- **`internal/notify/notifypreview/main.go`** の `printScenarios()`（68行目）は `notify.BuildPayloadPreview(s.outcome)` の戻り値をそのまま `fmt.Printf` で出力している。戻り値型が `webhookPayload`（非公開型のためこのパッケージ外からはフィールドを通じてのみアクセス可能）に変わるため、`text`・色・フィールドを人間可読に整形する処理に変更する。`fixtures.go`（`scenarios()`・`truncationResult()`）は `notify.Outcome` を組み立てるだけで `buildPayload`/`webhookPayload` に直接依存していないため無変更。
- **`docs/dev/developer_guide/package_reference.md`**（54行目）に `internal/notify` の説明として `builds a Slack Incoming Webhook payload (`text` field only) from a report.Result/error outcome` という記述がある。この「`text` field only」の部分を `text` + `attachments`（色・絵文字付き）に更新する。
- **`docs/dev/developer_guide/notify_preview.md`** は `buildPayload`/`BuildPayloadPreview` の戻り値の型を明記した記述を含まない（"renders them through the same formatting function" という抽象的な説明のみ）。本タスクによる変更は不要と判断した（`rg -n "string" docs/dev/developer_guide/notify_preview.md` で型を明記した記述がないことを確認済み）。
- **`rg -n "webhookPayload|buildPayload|BuildPayloadPreview|isFailure|colorFor|truncationCutPoint|maxPayloadLength|truncatedMarker" -g'*.go' internal/` で全呼び出し箇所を確認済み**。上記5ファイル（`notify.go`・`payload.go`・`payload_test.go`・`test_helpers.go`・`notifypreview/main.go`・`notifypreview/fixtures.go`）以外に参照は存在しない。`fixtures.go` 内の `buildPayload`/`BuildPayloadPreview` への言及はコメントのみで、コード上の呼び出しではない。
- **`atproto.Post` 構造体**（`internal/atproto/posts.go:69-74`）に `Body`/本文に相当するフィールドが存在しないことを確認済み（`RKey`/`Type`/`CreatedAt`/`Pinned` の4フィールドのみ）。AC-12（投稿本文の非含有）は型レベルで保証されており、本タスクでも変更しない。
- **`internal/config.SecretString`**: `cfg.SuccessWebhookURL`/`FailureWebhookURL` は `notify.go` の `send()` 内でのみ `.Reveal()` される（132行目付近の `buildPayload(outcome)` 呼び出しに `cfg`/`Config` は渡されない）。`buildPayload`/`webhookPayload` の構築経路に `Config` 型の値が渡ることはなく、この経路の型シグネチャ自体が AC-11 の秘密情報非混入を保証する構造になっている（フェーズ9のAC検証で `rg` により確認する）。

## 2. 実装ステップ

### フェーズ1: ペイロード型の追加（設計書 3.2節）

**対象ファイル**: `internal/notify/payload.go`、`internal/notify/notify.go`

- [ ] `internal/notify/notify.go` の66-71行目（`webhookPayload` 型定義とその直前のコメント）を削除する。
- [ ] `internal/notify/payload.go` に `webhookPayload`（`Text string`, `Attachments []slackAttachment` の2フィールド、`json:"attachments,omitempty"`）を追加する。コメントは設計書 3.2節のものをそのまま用いる。
- [ ] `internal/notify/payload.go` に `slackAttachment`（`Color string`, `Fields []slackField`、いずれも `omitempty`）を追加する。
- [ ] `internal/notify/payload.go` に `slackField`（`Title string`, `Value string`、`Short` フィールドは持たない — 設計書付録の決定履歴参照）を追加する。
- [ ] `internal/notify/notify.go` の132行目 `json.Marshal(webhookPayload{Text: buildPayload(outcome)})` を `json.Marshal(buildPayload(outcome))` に変更する（この時点では `buildPayload` はまだ `string` を返すため、フェーズ3までは一時的にコンパイルエラーになる。フェーズ1〜3は1コミット内で連続して行い、コンパイルが通る状態でのみ `make test`/`make lint` を実行する）。
- [ ] `make fmt && make test -run '^$' ./internal/notify/...`（コンパイルのみ確認、フェーズ3完了までテストは緑にならない前提）で構文エラーがないことを確認する。

### フェーズ2: `isFailure()` の抽出（設計書 3.1節）

**対象ファイル**: `internal/notify/payload.go`、`internal/notify/notify.go`

- [ ] `internal/notify/payload.go` に `func isFailure(outcome Outcome) bool { return outcome.Err != nil || (outcome.Result != nil && len(outcome.Result.Failed) > 0) }` を追加する（設計書 3.1節のコメントをそのまま用いる）。
- [ ] `internal/notify/notify.go` の120行目 `failed := outcome.Err != nil || (outcome.Result != nil && len(outcome.Result.Failed) > 0)` を `failed := isFailure(outcome)` に変更する。
- [ ] `make test ./internal/notify/...` を実行し、既存の `TestSend_ChannelRouting_*`（3件）・`TestSend_SameWebhookURLForBothChannels_RoutesCorrectlyInBothOutcomes`・`TestSend_SelectedWebhookURLEmpty_SkipsSendReturnsNil` が変更前と同じ結果で緑であることを確認する（振る舞い不変の回帰確認）。

### フェーズ3: `buildPayload()` の再設計（設計書 3.3節）

**対象ファイル**: `internal/notify/payload.go`

- [ ] `internal/notify/payload.go` に定数 `emojiSuccess = "✅"` / `emojiFailure = "❌"` / `colorGood = "good"` / `colorDanger = "danger"` を追加する。
- [ ] `func colorFor(failed bool) string`（設計書 3.3節のシグネチャ・コメント）を追加する。`failed` が `true` なら `colorDanger`、`false` なら `colorGood` を返す。
- [ ] `buildPayload` のシグネチャを `func buildPayload(outcome Outcome) string` から `func buildPayload(outcome Outcome) webhookPayload` に変更する。
- [ ] `text` の組み立てロジックを、既存の3分岐（`outcome.Err != nil` / `outcome.Result == nil` / デフォルト）の一行サマリ文言はそのまま維持しつつ、デフォルト分岐末尾の「失敗した投稿ごとの個別行を連結するループ」（既存コード76-78行目）を削除する形に変更する。具体的な before/after:
  - Before（既存コード60-79行目、`outcome.Result != nil` かつ `Failed` が1件以上のケースの実行結果、`buildPayload` の戻り値文字列）: `"bsky-cleaner run completed with failures: deleted 2 post(s), 1 failure(s).\n  rkey1: atproto http error: com.atproto.repo.deleteRecord status=500\n"`
  - After（`text` フィールドの内容、絵文字プレフィックス付き）: `"❌ bsky-cleaner run completed with failures: deleted 2 post(s), 1 failure(s)."`（末尾の失敗一覧2行は削除し、代わりに `Attachments[0].Fields[0].Value` に `"rkey1: atproto http error: com.atproto.repo.deleteRecord status=500"` として格納する。以降複数件ある場合は `\n` 区切りで連結する）
  - `outcome.Err != nil` の分岐（既存: `"bsky-cleaner run failed: %s\n"`）は `"❌ bsky-cleaner run failed: %s"` に、`outcome.Result == nil` の分岐（既存: `"bsky-cleaner run failed: unknown error\n"`）は `"❌ bsky-cleaner run failed: unknown error"` に、完全成功の分岐（既存: `"bsky-cleaner run succeeded: deleted %d post(s).\n"`）は `"✅ bsky-cleaner run succeeded: deleted %d post(s)."` にする（いずれも末尾の `\n` を落とし、絵文字を先頭に付与する）。
- [ ] `attachments` を常に1件生成する: `slackAttachment{Color: colorFor(isFailure(outcome))}`（設計書 3.3節手順3、AC-06/AC-07）。この時点では `Fields` は未設定（ゼロ値）のままにする。
- [ ] `outcome.Result != nil && len(outcome.Result.Failed) > 0` の場合のみ、既存コード76-78行目のループ内容（`sanitizeForPayload(failure.Post.RKey)` と `sanitizeForPayload(errorKind(failure.Err))` を `": "` で連結した行）を失敗投稿ごとに生成し、`"\n"` で連結したテキストを組み立てる。`slackField{Title: "Failed posts", Value: <組み立てたテキスト>}` を作り、`attachments[0].Fields` に設定する（設計書 3.3節手順4）。
- [ ] `outcome.Result == nil` または `len(outcome.Result.Failed) == 0` の場合、`attachments[0].Fields` はゼロ値（空スライス）のままにする（設計書 3.3節手順5、AC-05）。
- [ ] `webhookPayload{Text: text, Attachments: []slackAttachment{attachment}}` を返す（この時点では `text`/`Value` への切り詰め適用はフェーズ4で行う）。

### フェーズ4: 切り詰めヘルパーの抽出と適用（設計書 3.4節）

**対象ファイル**: `internal/notify/payload.go`

- [ ] 既存の切り詰めロジック（既存コード81-85行目 `text := b.String(); if len(text) <= maxPayloadLength { return text }; return text[:truncationCutPoint(text)] + truncatedMarker`）を `func truncate(s string) string` という独立した小さなヘルパーとして抽出する（`truncationCutPoint` はそのまま再利用する内部関数として維持）。
- [ ] フェーズ3で組み立てた `text`（絵文字+サマリ文言）に対して独立に `truncate()` を適用する（設計書 3.4節1番目の理由: `errorKind(outcome.Err)` が外部由来の無制限長文字列を埋め込みうるため）。
- [ ] フェーズ3で組み立てた失敗一覧の `Value` に対して独立に `truncate()` を適用する（設計書 3.4節2番目の理由: 大量の削除失敗による肥大化防止、既存と同じ役割の引き継ぎ）。
- [ ] `make test ./internal/notify/...` を実行し、この時点で `buildPayload` 関連の既存テスト（フェーズ6で書き換える前の状態）がコンパイルエラーになることを確認したうえで、フェーズ6に進む（型変更に伴う一時的な赤は許容し、フェーズ6完了時点で緑に戻す）。

### フェーズ5: `BuildPayloadPreview` の戻り値型追従

**対象ファイル**: `internal/notify/test_helpers.go`

- [ ] `func BuildPayloadPreview(outcome Outcome) string { return buildPayload(outcome) }` の戻り値型を `webhookPayload` に変更する（ラッパー自体のロジックは変更しない、`internal/notify/test_helpers.go:11-13`）。
- [ ] コメント（5-10行目）内の型に関する記述に矛盾がないか確認する（現状「exposes buildPayload」という抽象的な説明のみで型を明記していないため、変更不要と判断する。実装時に型を明記する記述が追加されていないか再確認する）。

### フェーズ6: `payload_test.go` の更新（設計書 7節）

**対象ファイル**: `internal/notify/payload_test.go`

既存11件のテストのうち、`TestEscapeSlackMarkup_EscapesAmpersandLtGt`（14行目、`escapeSlackMarkup` 単体テストで `buildPayload` に依存しない）は無変更。残り10件は `buildPayload` の戻り値が `webhookPayload` になることに伴い書き換える。

- [ ] `TestBuildPayload_SuccessOutcome_IncludesDeleteCountAndStatus`（19行目）を、`got.Text` に `"2"` と絵文字 `emojiSuccess`（`"✅"`）を含み、`got.Attachments` が1件で `Color == colorGood`、`Fields` が空であることを検証するテストに書き換える（AC-01, AC-05, AC-06）。
- [ ] `TestBuildPayload_RunError_IncludesErrorKind`（31行目）を、`got.Text` に `emojiFailure`（`"❌"`）と `errorKind` 文字列を含み、`got.Attachments` が1件で `Color == colorDanger`、`Fields` が空であることを検証するテストに書き換える（AC-02, AC-07）。このケースは、失敗一覧の有無によらず attachment が常に生成・色付けされることの回帰テストを兼ねる（設計書 3.3節の設計判断そのものの検証）。
- [ ] `TestBuildPayload_PartialFailure_IncludesFailedRKeysAndErrorKind`（38行目）を、`got.Text` に `emojiFailure` を含むが `rkey1`/`rkey2` などの個別詳細は含まないこと（AC-03）、`got.Attachments[0].Color == colorDanger`（AC-07）、`got.Attachments[0].Fields[0].Value` に `rkey1`・`"atproto http error: com.atproto.repo.deleteRecord status=500"`・`rkey2`・`"atproto http error: com.atproto.repo.deleteRecord status=429"` がいずれも含まれること（AC-04）を検証するテストに書き換える。
- [ ] `TestBuildPayload_ExcludesPostBody_OnlyIncludesStructuredFields`（57行目）を、`got.Attachments[0].Fields[0]` が `slackField{Title: "Failed posts", Value: "rkey1: atproto http error: com.atproto.repo.deleteRecord status=500"}` と完全一致すること（`assert.Equal`、部分一致ではなく完全一致にすることで `atproto.Post` に将来 body 相当のフィールドが追加された場合の回帰を検知する）を検証するテストに書き換える（AC-04, AC-12）。
- [ ] `TestBuildPayload_EscapesMentionSyntaxInFailedRKey`（76行目）を、`got.Attachments[0].Fields[0].Value` が `<!channel>` を含まず `&lt;!channel&gt;` を含むことを検証するテストに書き換える（AC-09）。
- [ ] `TestBuildPayload_SanitizesANSIEscapeInFailedRKey`（91行目）を、`got.Attachments[0].Fields[0].Value` が `\x1b` を含まないことを検証するテストに書き換える（AC-09）。
- [ ] `TestBuildPayload_SanitizesNewlineInFailedRKey`（105行目）を、`got.Attachments[0].Fields[0].Value` が `"evil\nFAKE LOG LINE"` をそのままの形では含まない（サニタイズ済みである）ことを検証するテストに書き換える（AC-09）。
- [ ] `TestBuildPayload_RunError_EscapesMentionSyntaxInSSRFErrorEndpoint`（127行目）を、`got.Text` が `<!channel>` を含まず `&lt;!channel&gt;` を含むことを検証するテストに書き換える（AC-09、`text` 側のサニタイズの回帰確認）。
- [ ] `TestBuildPayload_TruncatesWhenExceedsLimit_AppendsTruncatedMarker`（141行目）の名称を `TestBuildPayload_FailureFieldTruncatesWhenExceedsLimit_AppendsTruncatedMarker` に変更し、200件の失敗を与えた場合に `got.Attachments[0].Fields[0].Value` が `maxPayloadLength` 以内に切り詰められ `truncatedMarker` で終わることを検証する（AC-10、設計書 3.4節2番目の適用箇所の回帰テスト）。
- [ ] `TestBuildPayload_TruncationIsUTF8Safe`（163行目）を同様に `got.Attachments[0].Fields[0].Value` を対象にし、`utf8.ValidString` であることを検証するよう書き換える（AC-10）。
- [ ] 新規テスト `TestBuildPayload_TextTruncatesWhenExceedsLimit_AppendsTruncatedMarker` を追加する: `outcome.Err` に、`errorKind` が `atproto.HTTPError.ErrorName` を経由して極端に長い文字列（例: 5000文字の英数字列）を返すエラー値（`&atproto.HTTPError{Method: "com.atproto.repo.deleteRecord", StatusCode: 500, ErrorName: strings.Repeat("x", 5000)}`）を与え、`got.Text` が `maxPayloadLength` 以内に切り詰められ `truncatedMarker` で終わることを検証する（AC-10、設計書 3.4節1番目の適用箇所 — `text` 自体への切り詰め — の新規テスト）。
- [ ] 新規テスト `TestIsFailure_FourOutcomePatterns` を追加する: `Outcome` の4パターン（完全成功／`Err != nil`／`Result == nil` かつ `Err == nil`／部分失敗）それぞれで `isFailure()` の戻り値が `false`/`true`/`false`/`true` であることを表形式テスト（`t.Run` サブテスト）で検証する（AC-08）。`Result == nil` かつ `Err == nil` のケースにコメントを付し、`internal/runner.Run` の契約上この組み合わせは生成されない想定であるが `isFailure` は防御的に `false` を返す旨を明記する（設計書 7節）。

### フェーズ7: `notifypreview` の表示整形（設計書 3.5節）

**対象ファイル**: `internal/notify/notifypreview/main.go`

- [ ] `printScenarios()`（68行目）を、`notify.BuildPayloadPreview(s.outcome)` の戻り値（`webhookPayload`、`Text`/`Attachments` フィールドを公開型経由で参照可能）を人間可読な形に整形して出力するよう変更する。出力フォーマット例: `"=== %s ===\ntext: %s\ncolor: %s\nfields:\n  %s: %s\n"`（`fields` が空の場合は `"  (none)"` 等を表示する）。JSON マーシャルした生ペイロードをそのまま表示する案は採らない（設計書 3.5節: 改行や絵文字がエスケープされて読みにくくなるため）。
- [ ] `sendScenarios()`（77行目）は `notify.BuildPayloadPreview`/`webhookPayload` を経由しない独立経路であるため、変更不要であることを実装時に再確認する（設計書 3.5節「`make notify-preview-send` への影響」）。

### フェーズ8: `make notify-preview-send` による実送信確認（設計書 3.5節・7節）

**対象ファイル**: なし（手動確認のみ）

- [ ] `BSKY_SLACK_WEBHOOK_URL_TEST` にテスト用 Slack チャンネルの Incoming Webhook URL を設定したうえで `make notify-preview-send` を実行し、`success-empty`/`success-apply`/`partial-failure`/`run-error`/`truncation` の5シナリオすべてが、実際の Slack クライアント（デスクトップアプリまたはブラウザのいずれか1種類）上で絵文字・色付き attachment・フィールド分離を含めて意図通りに描画されることを目視で確認する（NF-005、設計書 3.4節で指摘された「`maxPayloadLength` の新しい適用箇所の未検証リスク」の解消）。

### フェーズ9: ドキュメント更新

**対象ファイル**: `docs/dev/developer_guide/package_reference.md`

- [ ] `docs/dev/developer_guide/package_reference.md:54` の `internal/notify` の説明にある `builds a Slack Incoming Webhook payload (`text` field only) from a report.Result/error outcome` を `builds a Slack Incoming Webhook payload (`text` summary plus a color-coded `attachments` block for failure detail) from a report.Result/error outcome` に変更する（変更後の文言は英語で記述する — 同ファイルはパッケージ構成の英語説明であるため、既存の文体に合わせる）。

### フェーズ10: 品質確認

- [ ] `make fmt` を実行する。
- [ ] `make test` を実行し、`internal/notify` パッケージおよびリポジトリ全体のテストが緑であることを確認する。
- [ ] `make lint` を実行し、警告・エラーがないことを確認する。
- [ ] `make deadcode` を実行し、`buildPayload`/`isFailure`/`colorFor`/`truncate` 等の新規シンボルに到達不能コードがないことを確認する。

## 3. 実装順序とマイルストーン

| マイルストーン | 内容 | 完了基準 |
|---|---|---|
| M1 | 型定義とロジック抽出（フェーズ1〜2） | `webhookPayload`/`slackAttachment`/`slackField` が定義され、`isFailure()` への置き換え後も既存の `TestSend_ChannelRouting_*` 系が緑 |
| M2 | ペイロード構築ロジックの再設計（フェーズ3〜4） | `buildPayload` が `webhookPayload` を返し、`text`/失敗一覧フィールドそれぞれに独立した切り詰めが適用される |
| M3 | テスト・プレビューツールの追従（フェーズ5〜7） | `payload_test.go` の全テストが新構造に対応し緑、`make notify-preview` が新フォーマットで表示される |
| M4 | 実運用確認とドキュメント（フェーズ8〜9） | `make notify-preview-send` による実送信確認が完了し、`package_reference.md` が更新済み |
| M5 | 品質確認（フェーズ10） | `make fmt && make test && make lint && make deadcode` が全て成功 |

## 4. テスト戦略

- **ユニットテスト**: `internal/notify/payload_test.go` に `isFailure()`・`buildPayload()`（絵文字・色・フィールド分離・切り詰めの各観点）のテストを配置する（フェーズ6で詳述、設計書 7節）。新規ヘルパー `truncate()`・`colorFor()` は `buildPayload()` 経由の既存のテストケースで間接的に検証し、独立した単体テストは追加しない（`buildPayload` の全分岐がこれらの関数の全パターンをすでに網羅しているため、重複テストを避ける — YAGNI）。
- **回帰テスト**: `internal/notify/notify_test.go` の既存 `TestSend_*` 群（16件）は無変更のまま実行し、`isFailure()` への置き換えと `buildPayload` の戻り値型変更が送信先振り分け・リトライ・タイムアウト・秘密情報非露出の既存の振る舞いを変えないことを確認する。
- **手動確認**: `make notify-preview`（表示整形の目視確認）と `make notify-preview-send`（実際の Slack チャンネルへの送信確認、フェーズ8）。
- **新規テスト・test_helpers**: 新しいテストヘルパーファイルは不要と判断した。`internal/notify/test_helpers.go`（B1、`//go:build test`）と `internal/notify/test_helpers_test.go`（B2、無タグ）は既存のまま流用し、前者は戻り値型のみ変更する（[test_organization.md](../../dev/developer_guide/test_organization.md)）。

## 5. リスク管理

| リスク | 影響 | 対策 |
|---|---|---|
| `maxPayloadLength`（4000）を `text` と attachment field `Value` それぞれ単体の上限として転用することが、Slack の実際の受理上限を検証しないままの決定である（設計書 3.4節） | 実際の Slack 受理拒否・表示崩れ | フェーズ8で `make notify-preview-send` による実送信確認を実装完了前の必須ステップとする |
| `buildPayload` の戻り値型変更に伴い、フェーズ1〜4の間は一時的にコンパイルが通らない期間が生じる | 中間コミットでの `make test`/`make lint` 失敗 | フェーズ1〜6を1つの作業単位として連続実施し、コンパイルが通り `make test` が緑になるフェーズ6完了時点で初めて `make lint`/コミットを行う（設計書 8節の順序をそのまま踏襲） |
| `notifypreview/main.go` の表示整形処理を新規に書き起こすため、意図しない情報欠落（例: `Fields` が複数件になった場合の表示崩れ）が起こりうる | 開発者向けプレビュー表示の可読性低下（本番動作には影響しない） | フェーズ7完了後に `make notify-preview` を実行し、5シナリオ全てで `text`・色・フィールドが欠落なく表示されることを目視確認する |

## 6. 実装チェックリスト

- [ ] フェーズ1: ペイロード型の追加
- [ ] フェーズ2: `isFailure()` の抽出
- [ ] フェーズ3: `buildPayload()` の再設計
- [ ] フェーズ4: 切り詰めヘルパーの抽出と適用
- [ ] フェーズ5: `BuildPayloadPreview` の戻り値型追従
- [ ] フェーズ6: `payload_test.go` の更新
- [ ] フェーズ7: `notifypreview` の表示整形
- [ ] フェーズ8: `make notify-preview-send` による実送信確認
- [ ] フェーズ9: ドキュメント更新
- [ ] フェーズ10: 品質確認

## 7. 受け入れ基準検証（Acceptance Criteria Verification）

| AC | 内容(要約) | 検証手段 | 種別 |
|---|---|---|---|
| AC-01 | 正常終了時、見出しに成功絵文字（✅） | `internal/notify/payload_test.go::TestBuildPayload_SuccessOutcome_IncludesDeleteCountAndStatus` | test |
| AC-02 | 異常終了時、見出しに失敗絵文字（❌） | `internal/notify/payload_test.go::TestBuildPayload_RunError_IncludesErrorKind` | test |
| AC-03 | `text` に失敗投稿ごとの個別詳細を含まない | `internal/notify/payload_test.go::TestBuildPayload_PartialFailure_IncludesFailedRKeysAndErrorKind` | test |
| AC-04 | 失敗投稿の rkey・エラー種別が `fields` に構造化格納 | `internal/notify/payload_test.go::TestBuildPayload_PartialFailure_IncludesFailedRKeysAndErrorKind`、`internal/notify/payload_test.go::TestBuildPayload_ExcludesPostBody_OnlyIncludesStructuredFields` | test |
| AC-05 | 完全成功時、詳細 attachment の `fields` が空 | `internal/notify/payload_test.go::TestBuildPayload_SuccessOutcome_IncludesDeleteCountAndStatus` | test |
| AC-06 | 正常終了時、`color` が `good` | `internal/notify/payload_test.go::TestBuildPayload_SuccessOutcome_IncludesDeleteCountAndStatus` | test |
| AC-07 | 異常終了時（部分失敗含む）、`color` が `danger` | `internal/notify/payload_test.go::TestBuildPayload_RunError_IncludesErrorKind`、`internal/notify/payload_test.go::TestBuildPayload_PartialFailure_IncludesFailedRKeysAndErrorKind` | test |
| AC-08 | 色分けとチャンネル振り分けが同一の判定式（`isFailure`）を使う | `internal/notify/payload_test.go::TestIsFailure_FourOutcomePatterns`（4パターンの真偽値を検証）に加え、`internal/notify/notify_test.go::TestSend_ChannelRouting_AllSucceeded_UsesSuccessURL`・`TestSend_ChannelRouting_RunError_UsesFailureURL`・`TestSend_ChannelRouting_PartialFailure_UsesFailureURL`（`send()` が `isFailure()` 呼び出しに置き換わった後も同じ送信先を選ぶことを確認）。静的確認として `rg -n "outcome\.Err != nil \|\| \(outcome\.Result" internal/notify/notify.go` を実行し、`notify.go` にインライン判定式が残っていない（0件）ことを確認する | test + static |
| AC-09 | `fields` の rkey・エラー種別に `sanitizeForPayload` が適用される | `internal/notify/payload_test.go::TestBuildPayload_EscapesMentionSyntaxInFailedRKey`、`TestBuildPayload_SanitizesANSIEscapeInFailedRKey`、`TestBuildPayload_SanitizesNewlineInFailedRKey` | test |
| AC-10 | `text`・失敗一覧フィールド `Value` それぞれが独立に切り詰められ、マーカーが付与される | `internal/notify/payload_test.go::TestBuildPayload_TextTruncatesWhenExceedsLimit_AppendsTruncatedMarker`（`text` 側、新規）、`TestBuildPayload_FailureFieldTruncatesWhenExceedsLimit_AppendsTruncatedMarker`（フィールド側、既存改名）、`TestBuildPayload_TruncationIsUTF8Safe`（UTF-8安全性） | test |
| AC-11 | 秘密情報（app パスワード・セッション JWT・`Authorization`・Webhook URL）がペイロードに含まれない | `internal/notify/notify_test.go::TestSendError_Error_NeverContainsWebhookURL`、`TestSend_RetryLog_UsesRedactedURL_NotRawWebhookURL`（Send経路全体での非露出を確認）に加え、静的確認として `rg -n "cfg\.(SuccessWebhookURL|FailureWebhookURL)|SecretString" internal/notify/payload.go` を実行し、`payload.go`（`buildPayload`/`webhookPayload` の構築経路）が `Config`/`SecretString`型を一切参照していない（0件）ことを確認する | test + static |
| AC-12 | 投稿本文がペイロードのいずれのフィールドにも含まれない | `internal/notify/payload_test.go::TestBuildPayload_ExcludesPostBody_OnlyIncludesStructuredFields`（`slackField` の完全一致検証）に加え、静的確認として `rg -n "type Post struct" -A 6 internal/atproto/posts.go` を実行し、`Post` 構造体に `Body` 相当のフィールドが存在しない（`RKey`/`Type`/`CreatedAt`/`Pinned` の4フィールドのみ）ことを確認する | test + static |

## 8. 成功基準

- 上表 AC-01〜AC-12 の検証手段がすべて緑（test は成功、static は期待される結果と一致）。
- `make fmt && make test && make lint && make deadcode` がすべて成功する（NF-001）。
- `internal/notify` パッケージの `buildPayload` 構築ロジックが、Webhook への実ネットワーク通信なしに単体テストできる（NF-002、フェーズ6のテストがいずれもモック/スタブなしの純粋関数呼び出しであることで満たす）。
- 悪意あるペイロード（メンション記法・ANSI エスケープ・改行混入）に対するセキュリティテストが `fields` を含む新構造でも維持される（NF-003、フェーズ6）。
- Go 1.26.2 以上でビルドできる（NF-004、`go.mod` のバージョン指定を変更しないことで維持）。
- `make notify-preview` が新しいペイロード構造でも動作し、開発者が新フォーマットをローカルで確認できる（NF-005、フェーズ7・8）。

## 9. 次のステップ

- 本計画書のエンジニアリングレビュー（`.claude/commands/_lib/review-subagent-pattern.md` の手順）と日本語プロセスレビュー（`japrose`）を完了し、Critical/Major の指摘を解消したうえで `approved` への更新を依頼する。
- 承認後、フェーズ1から順に実装に着手する。
