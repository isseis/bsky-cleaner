# Slack 通知への実行コンテキスト情報・統計情報の追加 — 実装計画書

## Document Status

| Item | Value |
|---|---|
| Status | `approved` |
| Created | 2026-07-10 |
| Review date | 2026-07-10 |
| Reviewer | isseis |
| Comments | - |

関連ドキュメント: [要件定義書](01_requirements.md)・[アーキテクチャ設計書](02_architecture.md)

## 1. 実装概要

### 1.1 目的

[要件定義書](01_requirements.md) の F-001〜F-004 を実装する。Slack 通知の attachment に実行ホスト名・bsky アカウントハンドルを正常系・異常系を問わず含め（F-001）、削除処理に到達した実行では対象件数・削除件数・処理時間を統計情報として含める（F-002）。また、`text` の一行サマリから件数表現を除去し（F-003）、通知配信が at-least-once ではないという既知の制限をドキュメントに明記する（F-004）。

### 1.2 実装方針

[アーキテクチャ設計書](02_architecture.md) 1.1節の設計原則（既存コンポーネントの再利用・単一責任の踏襲・YAGNI・既存パターンの継続利用）をそのまま踏襲する。本計画書では設計判断を繰り返さず、各フェーズで参照する設計書の節番号のみを示す。

### 1.3 既存コード調査結果

- **`internal/config`**（`config.go`/`validate.go`/`errors.go`）: `Config`/`rawConfig` は `SlackAllowedHost`（任意項目、キー欠如と空文字列を区別しない）という既存パターンをすでに持つ（`config.go:26-31,40`、`validate.go:56`）。`Hostname` フィールドはこのパターンをそのまま複製すればよく、新しいバリデーション方式（`FieldError`/エラーセンチネル）の追加は不要（アーキテクチャ設計書 3.1節）。`validateConfig`（`validate.go:19-58`）に1行追加するだけで済む。
- **`internal/config` のテストヘルパー**: `internal/config/test_helpers.go` に `writeTempTOML(t, content) string`（`//go:build test`、22行目）が既にあり、`config_test.go` の `TestLoad_SlackAllowedHostField_ParsesOptionalTOMLKey`（`config_test.go:160-196`）が `Hostname` と全く同型の「キー在り/キー無し」2パターンテストの手本になる。新規 `hostname_test.go` はこのヘルパーをそのまま再利用し、新しいテストヘルパーは追加しない。
- **`internal/notify/payload.go`**: `Outcome`（16-19行目）・`buildPayload`（157-209行目）・`isFailure`（88-90行目、変更なし）・`sanitizeForPayload`（110-112行目、変更なし）・`truncate`（119-124行目、変更なし）を確認した。`buildPayload` は現在「完全成功時は `attachments` を作らない」（197-205行目のコメント）方針であり、これをアーキテクチャ設計書 3.5節の例外に従い「常に1件の attachment を生成する」方針に変更する。
- **`internal/notify/payload_test.go`**: 既存14件のテスト関数を確認した。`TestEscapeSlackMarkup_EscapesAmpersandLtGt`・`TestIsFailure_FourOutcomePatterns` の2件は `buildPayload`/`Fields` を直接検証しないため無変更でよい。残り12件は `got.Attachments[0].Fields` に対して `require.Len`/`assert.Len` による**完全一致の件数チェック**を行っており、Host/Account フィールドが先頭に追加されると全件破壊される（部分一致ではない点を誤認しないこと）。内訳は次の通り。
  - `TestBuildPayload_SuccessOutcome_IncludesDeleteCountAndStatus`（20-36行目）・`TestBuildPayload_ResultAndErrNil_HasNoAttachment`（248-251行目）: `assert.Empty(t, got.Attachments)` が前提で、アーキテクチャ設計書 3.5節の例外により破壊される（フェーズ3で更新）。
  - `TestBuildPayload_RunError_IncludesErrorKind`（48行目）・`TestBuildPayload_RunError_EscapesMentionSyntaxInSSRFErrorEndpoint`（167行目）・`TestBuildPayload_ErrorFieldTruncatesWhenExceedsLimit_AppendsTruncatedMarker`（236行目）: いずれも `outcome.Result == nil` で `require.Len(t, got.Attachments[0].Fields, 1)` を使っており、Host/Account 追加後は3件（Host, Account, Error）になるため破壊される。`outcome.Result == nil` なのでフェーズ5の統計フィールドは追加されず、以降のフェーズで再破壊されることはない。
  - `TestBuildPayload_PartialFailure_IncludesFailedRKeysAndErrorKind`（73行目）・`TestBuildPayload_EscapesMentionSyntaxInFailedRKey`（110行目）・`TestBuildPayload_SanitizesANSIEscapeInFailedRKey`（127行目）・`TestBuildPayload_SanitizesNewlineInFailedRKey`（143行目）・`TestBuildPayload_FailureFieldTruncatesWhenExceedsLimit_AppendsTruncatedMarker`（186行目）・`TestBuildPayload_TruncationIsUTF8Safe`（212行目）: いずれも `outcome.Result != nil`（`Failed` を持つ）で `require.Len(t, got.Attachments[0].Fields, 1)` を使っている。フェーズ3で Host/Account 追加により3件になり、さらにフェーズ5で Targets/Deleted/Duration が追加されて6件になる — **2段階で件数が変わる**ため、固定件数へのアサーション変更では二度手直しが必要になる。この6件は「特定フィールドの値がサニタイズ・切り詰めされていること」の検証が目的であり総件数の検証が目的ではないため、フェーズ3で `Title` によるフィールド検索ヘルパーへ書き換え、以降のフェーズで再修正が不要な形にする（詳細はフェーズ3参照）。
  - `TestBuildPayload_ExcludesPostBody_OnlyIncludesStructuredFields`（80-96行目）: `outcome.Result != nil` で `require.Len(t, got.Attachments[0].Fields, 1)` および `Fields[0]` への完全一致アサーションを使っている。このテストの目的自体が「余計なフィールドが紛れ込まないこと」の総件数検証であるため、上記6件とは異なり `Title` 検索への書き換えでは代替できない。フェーズ3で3件（Host, Account, Failed posts）への更新、フェーズ5で再度6件（Host, Account, Targets, Deleted, Duration, Failed posts）への更新が必要であり、両フェーズの該当チェックボックスに明記する。
- **`internal/notify/notifypreview/fixtures.go`**: `scenarios()`（43-83行目）の5シナリオすべてが `notify.Outcome{Result: ..., Err: ...}` のみを設定しており、`Host`/`Account`/`Elapsed` の設定が無い。`//go:build test` 済み（1行目）。
- **`cmd/main.go`**: `run()`（334-384行目）は `runner.Run()` を355行目で1回呼び出し、371行目で `sendNotification(cfg, httpDoer, result, runErr)` を呼ぶ。`sendNotification`（391-400行目）は `result *report.Result, runErr error` を受け取り `notify.Outcome{Result: result, Err: runErr}` を組み立てている。この2箇所を変更する。`config.ResolveHostname` はまだ存在しない（新規追加）。
- **`cmd/main_test.go`**: `TestRun_ApplyAllSucceed_ReturnsExitCode0AndPrintsResult`（468-484行目）ほか、`slackWebhookHandler`（459-466行目）・`assertOnlySlackRequestURL`（518-528行目）・`validConfigPath`（34-39行目、`slack_allowed_host` は設定するが `hostname` は未設定）・`setEnvCredentials`（63-70行目、`BSKY_HANDLE` に `publicIPLiteral` を設定）という既存ヘルパーがある。`atprototestutil.MockHTTPDoer.Requests()`（`internal/atproto/testutil/mocks.go:65-69`）は `RecordedRequest{Method, URL, Body []byte}`（20-24行目）を保持しており、Slack へ送信された JSON ボディをテストで直接検証できる。新しいクロスパッケージテストヘルパーの追加は不要。
- **ドキュメント**: `docs/design/configuration.md`/`docs/design/configuration.ja.md` は英日対訳ペア（アーキテクチャ設計書のファイル一覧は `configuration.md` のみを挙げているが、翻訳ガイドライン（CLAUDE.md）に従い対訳を維持するため `configuration.ja.md` も本計画に含める。設計判断を変更するものではなく、既存の対訳整合ルールの適用ともいえるため、再承認は不要と判断する）。`docs/dev/developer_guide/package_reference.md` は日本語版が存在しない単一ファイル。`README.md`/`README.ja.md`・`docs/overview.md`/`docs/overview.ja.md` はいずれも対訳ペアで、アーキテクチャ設計書のファイル一覧（2.1節）が既に4ファイルすべてを挙げている。

## 2. ファイル一覧

| フェーズ | ファイル | 区分 |
|---|---|---|
| 1 | `internal/config/config.go` | 変更 |
| 1 | `internal/config/validate.go` | 変更 |
| 1 | `internal/config/hostname.go` | 新規 |
| 1 | `internal/config/hostname_test.go` | 新規 |
| 2 | `internal/notify/payload.go`（`Outcome` 拡張のみ） | 変更 |
| 3 | `internal/notify/payload.go`（`buildPayload` 変更: Host/Account フィールド） | 変更 |
| 3 | `internal/notify/payload_test.go` | 変更 |
| 4 | `internal/notify/notifypreview/fixtures.go`（Host/Account サンプル値） | 変更 |
| 5 | `internal/notify/payload.go`（`buildPayload` 変更: 統計フィールド） | 変更 |
| 5 | `internal/notify/payload_test.go` | 変更 |
| 6 | `internal/notify/payload.go`（`text` テンプレート変更） | 変更 |
| 6 | `internal/notify/payload_test.go` | 変更 |
| 7 | `cmd/main.go` | 変更 |
| 7 | `cmd/main_test.go` | 変更 |
| 8 | `internal/notify/notifypreview/fixtures.go`（Elapsed サンプル値） | 変更 |
| 11 | `docs/design/configuration.md` | 変更 |
| 11 | `docs/design/configuration.ja.md` | 変更 |
| 11 | `docs/dev/developer_guide/package_reference.md` | 変更 |
| 12 | `README.md` | 変更 |
| 12 | `README.ja.md` | 変更 |
| 12 | `docs/overview.md` | 変更 |
| 12 | `docs/overview.ja.md` | 変更 |

## 3. 実装ステップ

### 3.1 PR 構成
| PR | 対象ステップ | 主な変更内容 |
|---|---|---|
| PR-1 | フェーズ1 | `internal/config` へのホスト名解決（`Config.Hostname`/`ResolveHostname`）追加 |
| PR-2 | フェーズ2 / フェーズ3 | `Outcome` への `Host`/`Account`/`Elapsed` 追加、`buildPayload` が常に Host/Account を含む attachment を生成するよう変更 |
| PR-3 | フェーズ4 | `notifypreview` フィクスチャへの Host/Account 追加と、Mattermost を含む実クライアントでの早期の実送信描画確認 |
| PR-4 | フェーズ5 / フェーズ6 | `buildPayload` への統計フィールド（Targets/Deleted/Duration）追加、`text` からの件数表現除去 |
| PR-5 | フェーズ7 / フェーズ8 / フェーズ9 / フェーズ10 | `cmd/main.go` の時間計測・`Outcome` 構築、`notifypreview` の Elapsed 追加、全体テスト実行、最終実送信確認 |
| PR-6 | フェーズ11 | `docs/design/configuration.md`/`.ja.md`・`docs/dev/developer_guide/package_reference.md` の更新 |
| PR-7 | フェーズ12 | `README.md`/`.ja.md`・`docs/overview.md`/`.ja.md` への配信保証の限界（F-004）の明記 |

### フェーズ1: `internal/config` へのホスト名解決の追加

対応: F-001（AC-03, AC-04, AC-05）。設計: アーキテクチャ設計書 3.1節。

- [x] `internal/config/config.go` の `Config` 構造体に `Hostname string` フィールドを追加する。コメントは3.1節のコード例（186-190行目）に準じ、英語で記述する。
- [x] `internal/config/config.go` の `rawConfig` 構造体に `` Hostname string `toml:"hostname"` `` を追加する（`SlackAllowedHost string` と同じ非ポインタパターン、`config.go:40` 相当の位置）。
- [x] `internal/config/validate.go` の `validateConfig`（19-58行目）の戻り値構築（52-57行目）に `Hostname: raw.Hostname,` を1行追加する。
- [x] `internal/config/hostname.go` を新規作成し、`ResolveHostname(cfg Config) string` を実装する。`cfg.Hostname` が空でなければそれを返す（AC-03）。空なら `os.Hostname()` を呼び、成功すればその値を返す（AC-04）。`os.Hostname()` がエラーを返す場合は空文字列を返す（AC-05、エラーを外に伝播しない）。関数コメントはアーキテクチャ設計書 3.1節のコード例（196-200行目）に準じる。
- [x] `internal/config/hostname_test.go` を新規作成し、次の4テストケースを実装する。
  - [x] `TestResolveHostname_TOMLValueSet_ReturnsTOMLValue`: `ResolveHostname` に `Config{Hostname: "worker-1"}` を直接渡すと `"worker-1"` が返ることを確認する（AC-03、`ResolveHostname` 単体のフォールバック順序の検証）。
  - [x] `TestResolveHostname_TOMLValueEmpty_ReturnsOSHostname`: `ResolveHostname` に `Config{Hostname: ""}` を直接渡すと、返り値が空文字列でないことのみを確認する（アーキテクチャ設計書7節: 実際の `os.Hostname()` は環境依存のため、値そのものの一致は検証しない。AC-04）。
  - [x] `TestLoad_HostnameField_ParsesOptionalTOMLKey`: 既存の `writeTempTOML`（`internal/config/test_helpers.go:22`）と `Load()` を使い、`config_test.go` の `TestLoad_SlackAllowedHostField_ParsesOptionalTOMLKey` と同型の「キー在り/キー無し」2パターンで、TOML `hostname` キーが `rawConfig`/`validateConfig` を経て `Config.Hostname` に正しく届くことを確認する（AC-03/AC-04 のTOML読み込み経路。上記2件は `ResolveHostname` のフォールバック順序のみを検証し `Load()` を経由しないため、この経路のテストが別途必要）。
  - [x] （AC-05 の `os.Hostname()` 失敗分岐はアーキテクチャ設計書7節の判断により関数を差し替え可能にせず、テスト不可能な防御的分岐として扱う。実装レビューでの確認にとどめ、新規テストケースは追加しない。）
- [x] **（AC-17 追加分、要件定義書 2026-07-10 追記）** `internal/config/hostname.go` の `ResolveHostname` のシグネチャを `ResolveHostname(cfg Config) string` から `ResolveHostname(cfg Config) (string, error)` に変更する。`cfg.Hostname` が空でない場合、または `os.Hostname()` が成功した場合は `(値, nil)` を返す（AC-03/AC-04 の挙動は変更しない）。`os.Hostname()` がエラーを返す場合は `("", err)` を返す（AC-05: 通知フィールドには引き続き空文字列を使う best-effort 方針は維持しつつ、失敗した事実自体は呼び出し元に伝播できるようにする）。関数コメントを新しいシグネチャに合わせて更新する。
- [x] **（AC-17 追加分）** `internal/config/hostname_test.go` の `TestResolveHostname_TOMLValueSet_ReturnsTOMLValue`・`TestResolveHostname_TOMLValueEmpty_ReturnsOSHostname` を新しい2値シグネチャに追従させる（`got, err := ResolveHostname(cfg)` に変更し、いずれも `require.NoError(t, err)` を追加。TOML優先/`os.Hostname()`成功時はエラーが無いことを確認するのが目的）。`os.Hostname()` 失敗時のエラー伝播（`("", err)`）自体は既存の方針どおり関数差し替え不可のため自動テスト対象外とし、コードレビューで確認する（フェーズ1既存の注記を継続）。
- [x] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した（AC-17 追加分の反映後に再確認が必要）
- [x] PR を作成した
- [x] PR がマージされた
- [x] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

### PR-1 作成ポイント: internal/config hostname resolution

**対象ステップ**: フェーズ1

**推奨タイトル**: `feat(0013): add TOML hostname field and ResolveHostname`

**レビュー観点**: `ResolveHostname` のフォールバック順序（TOML優先→`os.Hostname()`→空文字列）が要件AC-03〜AC-05と一致しているか / `Config`/`rawConfig` への `Hostname` 追加が既存の `SlackAllowedHost` の非ポインタパターンと一貫しているか / `hostname_test.go` が新しいテストヘルパーを増やさず既存の `writeTempTOML` を再利用しているか（`TestLoad_HostnameField_ParsesOptionalTOMLKey`）、かつ `ResolveHostname` のフォールバック順序単体の検証（`writeTempTOML`/`Load()` を経由しない直接呼び出し2件）とTOML読み込み経路の検証が両方揃っているか / `ResolveHostname` の戻り値を `(string, error)` に変更した AC-17 追加分が、AC-03〜AC-05 の既存挙動（TOML優先・`os.Hostname()`フォールバック・空文字列の best-effort 方針）を後退させていないか

- [x] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した（AC-17 追加分の反映後に再確認が必要）
- [x] PR を作成した
- [x] PR がマージされた
- [x] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

### フェーズ2: `Outcome` 型への `Host`/`Account`/`Elapsed` フィールド追加

対応: F-001・F-002 の型的土台。設計: アーキテクチャ設計書 3.2節。

- [x] `internal/notify/payload.go` の `Outcome` 構造体（16-19行目）に `Host string`・`Account string`・`Elapsed time.Duration` を追加する。コメントはアーキテクチャ設計書 3.2節のコード例（209-228行目）に準じる。
- [x] `import "time"` を `internal/notify/payload.go` に追加する。
- [x] `go build ./...` がこの時点で成功することを確認する（`buildPayload` はまだ新フィールドを読まないため、コンパイルのみが目的）。

### フェーズ3: `buildPayload` に Host/Account フィールドを追加し、常に attachment を生成する

対応: F-001（AC-01, AC-02, AC-06）。設計: アーキテクチャ設計書 3.3節（手順2・5）・3.5節。

- [x] `internal/notify/payload.go` の `buildPayload`（157-209行目）を変更する。
  - [x] `fields` スライスを新設し、`sanitizeForPayload(outcome.Host)` を `slackField{Title: "Host", Value: ...}` として先頭に追加する。
  - [x] 続けて `sanitizeForPayload(outcome.Account)` を `slackField{Title: "Account", Value: ...}` として追加する。
  - [x] 既存の `isFailure(outcome)` 分岐（176-206行目）が構築する `attachment.Fields`（"Error" または "Failed posts"）を、この `fields` に対する追記（`append`）に変更する。
  - [x] `attachments` の生成条件を「`isFailure(outcome)` かつ `len(attachment.Fields) > 0`」から「常に1件生成する」に変更する。`color` は `isFailure(outcome)` が真なら `colorDanger`、偽なら空文字列（`omitempty` によりJSON上は省略）とする（アーキテクチャ設計書 3.3節手順5）。**追記（フェーズ4）**: この後の実送信確認で色なし attachment が可視化されない事象が判明したため、成功時の `color` は後続フェーズで `colorGood`（`"good"`）に変更された。詳細はフェーズ4の記述と付録「決定履歴」を参照。
  - [x] 197-202行目にある「color-only attachment を送らない」ための防御コメント・分岐を削除する（Host/Account により `fields` が常に非空になるため、この防御は不要になる。アーキテクチャ設計書 3.5節）。
- [x] `internal/notify/payload.go` の `buildPayload` の関数コメント（145-156行目）を、「成功時は attachment を生成しない」という記述から「常に1件の attachment を生成する」という記述に更新する。
- [x] `internal/notify/payload.go` の `webhookPayload`・`slackAttachment` の型コメント（32-59行目）を、「失敗時のみ attachment を生成する」という記述から実態（常に1件生成、`Fields` に Host/Account を含む）に合わせて更新する。
- [x] `internal/notify/test_helpers_test.go`（既存ファイル、B2: 非公開シンボルのみ、ビルドタグ不要。`docs/dev/developer_guide/test_organization.md` の分類B2に該当）に、`slackField` を `Title` で検索するテストヘルパー `findField(t *testing.T, fields []slackField, title string) slackField` を追加する。`title` に一致する要素が無ければ `t.Fatalf` で即座に失敗させる。以降のフェーズで `Fields` の総件数がフェーズを追うごとに変化する（フェーズ3で+2, フェーズ5で+最大3）ため、個々のフィールド値の検証をインデックス依存にしないための共通ヘルパーとして使う。
- [x] `internal/notify/payload_test.go` の `TestBuildPayload_SuccessOutcome_IncludesDeleteCountAndStatus`（20-36行目）を更新する。
  - [x] `outcome` に `Host: "worker-1"`・`Account: "alice.bsky.social"` を追加する。
  - [x] `assert.Empty(t, got.Attachments)`（35行目）を、`assert.Len(t, got.Attachments, 1)` および `assert.Equal(t, "", got.Attachments[0].Color)`（danger色でないこと）に置き換える。
  - [x] `findField(t, got.Attachments[0].Fields, "Host").Value` が `"worker-1"`、`findField(t, got.Attachments[0].Fields, "Account").Value` が `"alice.bsky.social"` であることを検証するアサーションを追加する。
- [x] `internal/notify/payload_test.go` の `TestBuildPayload_ResultAndErrNil_HasNoAttachment`（248-251行目）を更新する。
  - [x] テスト名を `TestBuildPayload_ResultAndErrNil_AttachmentHasOnlyHostAccountFields` に変更する（実態に合わせる。アーキテクチャ設計書 3.5節の更新指示）。
  - [x] `Outcome{}`（Host/Account ともゼロ値）を渡し、`assert.Len(t, got.Attachments, 1)` および `assert.Equal(t, []slackField{{Title: "Host", Value: ""}, {Title: "Account", Value: ""}}, got.Attachments[0].Fields)` で、Fields が Host/Account の2件のみであることを検証するよう変更する（このケースは `outcome.Err == nil && outcome.Result == nil` で `isFailure` は真だが Error/Failed posts フィールドは追加されず、`outcome.Result == nil` のためフェーズ5の統計フィールドも追加されないため、Fields はこの2件で以降のフェーズを通じて変化しない）。
- [x] `internal/notify/payload_test.go` の `TestBuildPayload_ExcludesPostBody_OnlyIncludesStructuredFields`（80-96行目、既存コード調査結果参照）を更新する。**この関数のみフェーズ5でも再度更新が必要（既存コード調査結果参照）。**
  - [x] `require.Len(t, got.Attachments[0].Fields, 1)`（92行目）を `require.Len(t, got.Attachments[0].Fields, 3)` に変更する。
  - [x] `Fields[0]` への完全一致アサーション（94-95行目）を `Fields[2]`（Host, Account に続く3番目の要素）への完全一致アサーションに変更する。
- [x] `internal/notify/payload_test.go` の次の6テストを、`Fields` の総件数チェックからフィールド個別の値チェックへ書き換える（`outcome.Result != nil` でフェーズ5により総件数がさらに変化するため、`findField` ヘルパーを使い件数に依存しない形にする。各テストで `require.Len(t, got.Attachments[0].Fields, 1)` を削除し、該当フィールドの参照を `got.Attachments[0].Fields[0]` から `findField(t, got.Attachments[0].Fields, "<Title>")` に置き換える。既存の `Value` に対するアサーション自体は変更しない）。
  - [x] `TestBuildPayload_PartialFailure_IncludesFailedRKeysAndErrorKind`（73行目、`Title: "Failed posts"`）
  - [x] `TestBuildPayload_EscapesMentionSyntaxInFailedRKey`（110行目、`Title: "Failed posts"`）
  - [x] `TestBuildPayload_SanitizesANSIEscapeInFailedRKey`（127行目、`Title: "Failed posts"`）
  - [x] `TestBuildPayload_SanitizesNewlineInFailedRKey`（143行目、`Title: "Failed posts"`）
  - [x] `TestBuildPayload_FailureFieldTruncatesWhenExceedsLimit_AppendsTruncatedMarker`（186行目、`Title: "Failed posts"`）
  - [x] `TestBuildPayload_TruncationIsUTF8Safe`（212行目、`Title: "Failed posts"`）
- [x] `internal/notify/payload_test.go` の次の3テストも同様に `findField` へ書き換える（`outcome.Result == nil` のためフェーズ5の影響は受けないが、フェーズ3の Host/Account 追加で `require.Len(..., 1)` は破壊されるため書き換えが必須）。
  - [x] `TestBuildPayload_RunError_IncludesErrorKind`（48行目、`Title: "Error"`）
  - [x] `TestBuildPayload_RunError_EscapesMentionSyntaxInSSRFErrorEndpoint`（167行目、`Title: "Error"`）
  - [x] `TestBuildPayload_ErrorFieldTruncatesWhenExceedsLimit_AppendsTruncatedMarker`（236行目、`Title: "Error"`）
- [x] `internal/notify/payload_test.go` に、Host/Account のサニタイズを検証する新規テスト `TestBuildPayload_SanitizesMentionSyntaxAndControlCharsInHostAndAccount` を追加する。`outcome.Host`/`outcome.Account` に `"<!channel>"`・ANSI エスケープ・改行を含む値を与え、`findField` で取得した対応する `Fields` の値がエスケープ・除去済みであることを検証する（`TestBuildPayload_EscapesMentionSyntaxInFailedRKey` 等の既存パターンを流用、NF-003）。
- [x] `internal/notify/payload_test.go` に、正常終了時・異常終了時の双方で `Fields` に `Title: "Host"`・`Title: "Account"` が含まれることを確認する新規テスト `TestBuildPayload_ErrOutcome_IncludesHostAndAccountFields` を追加する（AC-01, AC-02 の異常系側。`TestBuildPayload_RunError_IncludesErrorKind` の `outcome` に `Host`/`Account` を追加した派生ケース）。
- [x] `make notify-preview` を実行し、コンパイルが通ることを確認する（`notifypreview` の `Outcome` フィールドはまだ未設定だが、`buildPayload` は空文字列の `Host`/`Account` でもパニックしないため、この時点でも実行は成功するはずである）。

### PR-2 作成ポイント: always-attachment host/account fields

**対象ステップ**: フェーズ2 / フェーズ3

**推奨タイトル**: `feat(0013): always include host/account fields in Slack attachment`

**レビュー観点**: 完全成功時にも attachment を常時生成する変更が、0012 で確立した「`Fields` が空の attachment は一部クライアントで不可視になる」問題を再発させる懸念に対し、コードレベルでどう説明できるか（実際のクライアント描画確認は PR-3 のフェーズ4で行うため、このPRの時点では設計上の妥当性のみをレビューする） / Host/Account が `sanitizeForPayload` を経由してから格納されているか（NF-003） / 12件の既存テスト更新（`findField` へ書き換えた9件、期待値そのものを更新した3件）が漏れなく行われているか（既存コード調査結果参照）

- [x] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [x] PR を作成した
- [x] PR がマージされた
- [x] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

### フェーズ4: `notifypreview` フィクスチャへの Host/Account 追加と実送信確認

対応: アーキテクチャ設計書 3.5節・5.3節（Mattermost 互換性の早期確認）。

- [x] `internal/notify/notifypreview/fixtures.go` の `scenarios()`（43-83行目）内、5シナリオすべての `notify.Outcome` に `Host: "worker-1"`・`Account: "alice.bsky.social"` を追加する（`success-empty`・`success-apply`・`partial-failure`・`run-error`・`truncation` の5箇所すべて。フェーズ8で `Elapsed` を追加するまでは統計フィールドは表示されない）。
- [x] `make notify-preview` を実行し、全5シナリオの出力に `Host`/`Account` フィールドが表示されることを目視確認する。
- [x] `make notify-preview-send` を実行し、Mattermost を含む実際の Slack Incoming Webhook 互換クライアントに送信し、完全成功シナリオ（`Fields` が2件、danger色ではない attachment）が可視のブロックとして描画されることを確認する（アーキテクチャ設計書 5.3節）。
  - **結果**: 描画に問題があった（attachment 自体は可視のブロックとして描画されたが、`Color` 未設定のため詳細を囲むボックスの縦線が既定の青色で表示され、意図した緑色にならない）。
  - [x] 描画に問題がある場合、アーキテクチャ設計書 5.3節のフォールバック（成功時に `Color: "good"` を設定する）を適用し、3.3節手順5・付録「決定履歴」の更新が必要になる旨をこの計画書のコメント欄に記録した上で、アーキテクチャ設計書自体の改訂を先に行う（フェーズ順序を崩さない。フォールバックが不要だった場合は、実装完了時にこの注記を「対象外」であったと明示する）。
    - **コメント欄**: 上記確認結果を受け、アーキテクチャ設計書の 3.3節手順5・5.3節・付録「決定履歴」を改訂し（`color` を成功時 `colorGood`（`"good"`）に変更）、続けて `internal/notify/payload.go`（`colorGood` 定数追加・`buildPayload` の色選択ロジック変更）と `internal/notify/payload_test.go`（`Color` の期待値を `""` から `colorGood` に更新）に反映する。フェーズ順序（設計書改訂 → 実装）を維持する。
- [x] `internal/notify/payload.go` に `colorGood = "good"` 定数を追加し（`colorDanger` と対で定義）、`buildPayload` の色選択ロジックを `color := colorDanger; if !isFailure(outcome) { color = colorGood }` 相当に変更する（アーキテクチャ設計書 3.3節手順5フォールバック）。
- [x] `internal/notify/payload_test.go` の `TestBuildPayload_SuccessOutcome_IncludesDeleteCountAndStatus`（35行目）の `assert.Equal(t, "", got.Attachments[0].Color)` を `assert.Equal(t, colorGood, got.Attachments[0].Color)` に更新する。
- [x] `internal/notify/payload_test.go` の `TestBuildPayload_ResultAndErrNil_AttachmentHasOnlyHostAccountFields`（フェーズ3で改名済み）に `Color` が `colorGood` であることのアサーションを追加する（このケースは `isFailure` が真だが Result/Err が両方 nil という防御的分岐であり、完全成功ではないため確認が必要 — 実際には `outcome.Err == nil && outcome.Result == nil` は `isFailure` が真になるため `colorDanger` のままであることを確認し、コメントで理由を明記する）。
- [x] `make notify-preview-send` を再実行し、完全成功シナリオの attachment に緑色のバーが表示されることを確認する。

### PR-3 作成ポイント: notifypreview host/account fixtures and real-send verification

**対象ステップ**: フェーズ4

**推奨タイトル**: `chore(0013): verify Slack attachment rendering with host/account fields`

**レビュー観点**: PR-2 で `buildPayload` が完全成功時にも常に attachment を生成するようになった設計判断が、Mattermost を含む実際の Incoming Webhook 互換クライアントで意図通り描画されることを実送信結果から確認できるか（0012 で発生した「空 attachment 不可視化」問題の再発が無いこと） / 描画に問題があった場合のフォールバック適用有無とその根拠が明記されているか

- [x] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [x] PR を作成した
- [x] PR がマージされた
- [x] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

### フェーズ5: `buildPayload` への統計フィールド追加

対応: F-002（AC-07, AC-08, AC-09, AC-10）。設計: アーキテクチャ設計書 3.3節手順3。

- [x] `internal/notify/payload.go` の `import` に `"strconv"` を追加する。
- [x] `buildPayload` の `fields` 構築（フェーズ3で追加した Host/Account の直後）に、`outcome.Result != nil` の場合のみ次の3フィールドを追加する分岐を実装する。
  - [x] `slackField{Title: "Targets", Value: strconv.Itoa(len(outcome.Result.Targets))}`
  - [x] `slackField{Title: "Deleted", Value: strconv.Itoa(len(outcome.Result.Deleted))}`
  - [x] `slackField{Title: "Duration", Value: outcome.Elapsed.String()}`
- [x] `internal/notify/payload_test.go` に新規テスト `TestBuildPayload_ResultNotNil_IncludesTargetsDeletedDurationFields` を追加する。`outcome.Result` に `Targets`（3件）・`Deleted`（2件）を設定し、`outcome.Elapsed` に `2*time.Second` 相当を設定して、`Fields` に `Title: "Targets", Value: "3"`・`Title: "Deleted", Value: "2"`・`Title: "Duration", Value: "2s"` が含まれることを検証する（AC-07, AC-08, AC-09）。
- [x] `internal/notify/payload_test.go` に新規テスト `TestBuildPayload_ResultNil_ExcludesTargetsDeletedDurationFields` を追加する。`outcome.Result == nil`（`outcome.Err` にログイン失敗相当のエラーを設定）のケースで、`Fields` のいずれの `Title` も `"Targets"`・`"Deleted"`・`"Duration"` と一致しないことを検証する（AC-10）。
- [x] `internal/notify/payload_test.go` の `TestBuildPayload_ExcludesPostBody_OnlyIncludesStructuredFields`（フェーズ3で更新済み）を再度更新する。このテストの `outcome.Result` は `Failed` のみを持ち `Targets`/`Deleted` を明示的に設定していないため、本フェーズの変更後は `Fields` が6件（Host, Account, Targets（値 `"0"`）, Deleted（値 `"0"`）, Duration（値 `outcome.Elapsed.String()`、ゼロ値なら `"0s"`）, Failed posts）になる。`require.Len(t, got.Attachments[0].Fields, 3)` を `require.Len(t, got.Attachments[0].Fields, 6)` に、`Fields[2]` への完全一致アサーションを `Fields[5]` への完全一致アサーションに更新する（既存コード調査結果・フェーズ3参照。このテストのみ `findField` に書き換えず、総件数検証という本来の目的を維持する）。

### フェーズ6: `text` からの件数表現の除去

対応: F-003（AC-12, AC-13, AC-14）。設計: アーキテクチャ設計書 3.3節手順1・3.4節。

- [x] `internal/notify/payload.go` の `buildPayload` 内、`text` を構築する `switch`（158-172行目）を次のように変更する。
  - [x] `default` 節（164-171行目）の完全成功時の文言を `fmt.Sprintf("%s bsky-cleaner run succeeded: deleted %d post(s).", emojiSuccess, deleted)` から `fmt.Sprintf("%s bsky-cleaner run succeeded.", emojiSuccess)` に変更する（件数プレースホルダ `%d`・`deleted` 変数参照を削除)。
  - [x] 同じく `default` 節の部分失敗時の文言を `fmt.Sprintf("%s bsky-cleaner run completed with failures: deleted %d post(s), %d failure(s).", emojiFailure, deleted, failedCount)` から `fmt.Sprintf("%s bsky-cleaner run completed with failures.", emojiFailure)` に変更する。
  - [x] `deleted := len(outcome.Result.Deleted)` の変数宣言（165行目）を、`failedCount == 0` の分岐判定にのみ必要な形に整理する（`deleted` 変数はもはや文言に使われないため、`failedCount := len(outcome.Result.Failed)` の判定のみ残し、未使用変数エラーを避ける）。
  - [x] `outcome.Err != nil`（160-161行目）・`outcome.Result == nil`（162-163行目）の2分岐は既存要件どおり変更しない（件数を含んでいないため）。
- [x] `internal/notify/payload_test.go` の `TestBuildPayload_SuccessOutcome_IncludesDeleteCountAndStatus` から `assert.Contains(t, got.Text, "2")`（29行目）を削除する（削除件数が `text` に含まれなくなったため。AC-12）。実装時は削除ではなく `assert.NotContains(t, got.Text, "2")` に置き換え、件数不在を積極的に検証する形にした（AC-12 の意図はより明確に満たされる）。
- [x] `internal/notify/payload_test.go` のテスト名 `TestBuildPayload_SuccessOutcome_IncludesDeleteCountAndStatus` を `TestBuildPayload_SuccessOutcome_TextHasNoDeleteCount` に変更する（実態に合わせる）。
- [x] `internal/notify/payload_test.go` に新規テスト `TestBuildPayload_PartialFailure_TextHasNoFailureCount` を追加する。部分失敗の `outcome`（`TestBuildPayload_PartialFailure_IncludesFailedRKeysAndErrorKind` と同様の fixture）に対し、`got.Text` に失敗件数を示す数値（例えば `"2"`）が含まれないことを検証する（AC-13）。
- [x] `internal/notify/payload_test.go` に新規テスト `TestBuildPayload_TextRetainsEmojiForSuccessAndFailure` を追加する。完全成功・部分失敗・実行エラーの3ケースそれぞれで、`got.Text` に `emojiSuccess`（成功時のみ）または `emojiFailure`（それ以外）が含まれることを再確認する（AC-14。0012 AC-01/AC-02 の絵文字判別要件が後退していないことの明示的な回帰テスト）。
- [x] `internal/notify/notify_test.go` の `TestSend_Success_PostsToSelectedWebhook`（本フェーズの実装計画には無い派生影響。`text` の固定リテラル文言 `"bsky-cleaner run succeeded: deleted 0 post(s)."` を検証していたため、件数除去後の新しい固定文言 `"bsky-cleaner run succeeded."` に更新した）。

### PR-4 作成ポイント: statistics fields and text simplification

**対象ステップ**: フェーズ5 / フェーズ6

**推奨タイトル**: `feat(0013): add delete statistics fields and simplify text summary`

**レビュー観点**: `Targets`/`Deleted`/`Duration` フィールドが `outcome.Result != nil` の場合のみ追加されているか（AC-07〜AC-10） / `text` から件数表現が完全に除去されつつ、絵文字による正常系/異常系判別（0012 由来の保証）が後退していないか（AC-12〜AC-14） / フェーズ3で更新済みの `TestBuildPayload_ExcludesPostBody_OnlyIncludesStructuredFields` がフィールド追加後の6件構成に正しく再更新されているか

- [x] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [x] PR を作成した（https://github.com/isseis/bsky-cleaner/pull/132）
- [x] PR がマージされた
- [x] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

### フェーズ7: `cmd/main.go` の時間計測・`Outcome` 構築

対応: F-001・F-002 の実行時の値供給（AC-11）・AC-17（要件定義書 2026-07-10 追記分）。設計: アーキテクチャ設計書 3.6節。

- [ ] `internal/config/hostname.go`（フェーズ1）が確定した `ResolveHostname` を `cmd/main.go` から呼び出せるよう、`cmd/main.go` の `import` は既存の `"github.com/isseis/bsky-cleaner/internal/config"` をそのまま使う(追加不要)。`"log/slog"` の `import` を新規追加する（AC-17、`cmd/main.go` にとって新規パターンだが `internal/config/secret.go` 等ですでに `log/slog` は採用済み）。
- [ ] `cmd/main.go` の `run()`（334-384行目）を変更する。
  - [ ] 355行目 `result, runErr := runner.Run(ctx, client, cfg.AppPassword, cfg.RetentionDays, apply, now)` の直前に `start := time.Now()` を追加する。
  - [ ] 同行の直後に `elapsed := time.Since(start)` を追加する。
  - [ ] 366-369行目のコメントの直後、370行目の `if apply {` の直前で、`host, hostErr := config.ResolveHostname(cfg.Config)` を呼び出す（AC-17: フェーズ1で `(string, error)` に変更済みの新シグネチャ）。`hostErr != nil` の場合、`slog.Warn("failed to resolve hostname, using empty value", "error", hostErr)` で警告ログを出力する（AC-17。`host` は `hostErr != nil` でも空文字列のまま利用し、AC-05 の best-effort 方針どおり通知処理自体は継続する。Slack への警告ポストは行わない — 要件定義書 Comments 欄参照）。続けて `outcome := notify.Outcome{Result: result, Err: runErr, Host: host, Account: cfg.Handle, Elapsed: elapsed}` を構築する（アーキテクチャ設計書 3.6節: `apply` の値によらず常に構築する）。
  - [ ] 371行目 `sendNotification(cfg, httpDoer, result, runErr)` の呼び出しを `sendNotification(cfg, httpDoer, outcome)` に変更する。
- [ ] `cmd/main.go` の `sendNotification`（391-400行目）のシグネチャを `func sendNotification(cfg *config.AppConfig, httpDoer atproto.HTTPDoer, result *report.Result, runErr error) error` から `func sendNotification(cfg *config.AppConfig, httpDoer atproto.HTTPDoer, outcome notify.Outcome) error` に変更する。関数コメントはアーキテクチャ設計書 3.6節のコード例（283-289行目）に準じる。
- [ ] `sendNotification` 内 399行目の `notify.Send(notifyCtx, notifyCfg, httpDoer, retry.RealClock{}, notify.Outcome{Result: result, Err: runErr})` を `notify.Send(notifyCtx, notifyCfg, httpDoer, retry.RealClock{}, outcome)` に変更する（`Outcome` はすでに呼び出し元 `run()` で構築済みのため、ここでは再構築しない）。
- [ ] `report.Result` を直接参照していた `sendNotification` のシグネチャから `"github.com/isseis/bsky-cleaner/internal/report"` の import が不要にならないことを確認する（`run()` 内の `report.FormatText(*result)`（363行目）が引き続き使用するため、import 自体は残る）。
- [ ] `cmd/main_test.go` の以下の既存テストが `run()` のシグネチャ変更を経ずにコンパイル可能であることを確認する（`sendNotification` は非公開関数であり `run()` 経由でのみ呼ばれるため、テスト自体の呼び出しコードは変更不要。次のチェックリストで内容面の追従を行う）。
  - [ ] `TestRun_ApplyAllSucceed_ReturnsExitCode0AndPrintsResult`（468-484行目）
  - [ ] `TestRun_ApplyPartialFailure_ReturnsExitCode3AndPrintsFailures`（486-512行目）
  - [ ] `TestRun_ApplyLoginFailure_SendsFailureNotification`（530-548行目）
  - [ ] `TestRun_Apply_SlackNotifyFails_ExitCodeUnaffected`（550-566行目）
  - [ ] `TestRun_Apply_SlackNotifyFails_StderrContainsMaskedFailureMessage`（568-582行目）
  - [ ] `TestRun_Apply_NoWebhookConfigured_SkipsNotifyWithoutError`（584-599行目）
- [ ] `cmd/main_test.go` に新規テスト `TestRun_Apply_SendsHostAndAccountFromConfigAndCredentials` を追加する。`validConfigPath` が生成する TOML に `hostname = "ci-runner-1"` を追記した設定ファイルを用意し（`validConfigPath`（34-39行目）の `body` 定数に倣い、テスト専用のヘルパーまたはインライン文字列で `hostname` 行を含む TOML を書く）、`setEnvCredentials`（`BSKY_HANDLE` に `publicIPLiteral` を設定）で実行後、`mock.Requests()`（`atprototestutil.MockHTTPDoer.Requests()`）から `hooks.slack.com` 宛のリクエストを特定し、その `Body` を `json.Unmarshal` してペイロードの `attachments[0].fields` に `Title: "Host", Value: "ci-runner-1"` と `Title: "Account", Value: <publicIPLiteral の値>` が含まれることを検証する。これにより Host/Account の取り違え（アーキテクチャ設計書 付録「決定履歴」で言及されているリスク）を検出できる。
- [ ] `cmd/main_test.go` の `TestRun_ApplyAllSucceed_ReturnsExitCode0AndPrintsResult`（468-484行目）に、送信された Slack payload の `attachments[0].fields` に `Title: "Duration"` が含まれ、その `Value` が `time.ParseDuration` で解釈可能な非負の値であることを検証するアサーションを追加する（AC-09・AC-11 の統合確認。`mock.Requests()` から `Body` を取得し `json.Unmarshal` する点は直前のテストと同じパターンを再利用する）。

### フェーズ8: `notifypreview` フィクスチャへの Elapsed 追加

対応: NF-007（`make notify-preview` の継続動作）。

- [ ] `internal/notify/notifypreview/fixtures.go` の `scenarios()` のうち、`outcome.Result != nil` である4シナリオ（`success-empty`・`success-apply`・`partial-failure`・`truncation`。`run-error` は `Result == nil` のため対象外）に `Elapsed: 3 * time.Second` 相当のサンプル値を追加する。
- [ ] `make notify-preview` を実行し、対象4シナリオの出力に `Targets`/`Deleted`/`Duration` フィールドが表示されることを目視確認する。

### フェーズ9: テストの追加・更新の総仕上げ

対応: 7節のテスト戦略の総括確認。

- [ ] `make test` を実行し、`internal/config`・`internal/notify`・`cmd` パッケージのテストがすべて成功することを確認する。
- [ ] `make fmt` を実行し、フェーズ1〜8で変更した全ファイルに差分が出ないことを確認する（フォーマット崩れがあれば修正する）。

### フェーズ10: `make notify-preview-send` による最終実送信確認

対応: アーキテクチャ設計書 7節・8節手順10。

- [ ] `make notify-preview-send` を実行し、統計フィールド（Targets/Deleted/Duration）を含む全シナリオが Mattermost を含む実クライアントで問題なく描画されることを確認する。

### PR-5 作成ポイント: cmd wiring and final verification

**対象ステップ**: フェーズ7 / フェーズ8 / フェーズ9 / フェーズ10

**推奨タイトル**: `feat(0013): wire elapsed time, host, and account into cmd runtime`

**レビュー観点**: 処理時間の計測区間が `runner.Run()` 呼び出しの直前・直後のみに限定され、設定読み込みや DID/PDS 解決の時間を含んでいないか（AC-11） / Host/Account が TOML `hostname`・`BSKY_HANDLE` の実際の値から正しく供給されており、両者の取り違えが無いか（`TestRun_Apply_SendsHostAndAccountFromConfigAndCredentials` で検証） / `config.ResolveHostname` が返すエラーを握りつぶさず `slog.Warn` で警告ログを出しつつ、通知処理自体は空文字列のホストで継続しているか（AC-17） / `make test`・`make notify-preview-send` の両方が全シナリオで green であることを確認したうえで次フェーズ（ドキュメント更新）に進んでいるか

- [ ] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [ ] PR を作成した
- [ ] PR がマージされた
- [ ] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

### フェーズ11: ドキュメント更新（設定リファレンス・パッケージリファレンス）

対応: NF-007 に付随するドキュメント整合。AC番号に直接紐づく要件ではないが、既存コード調査結果で述べた通り翻訳対訳の維持のため実施する。

- [ ] `docs/design/configuration.md` の TOML フィールド一覧表（22-27行目）に `hostname` の行を追加する。列は既存の `slack_allowed_host` 行（27行目）と同じ形式（Item/Type/Required-Optional/Default Value/Format-Constraints）に揃え、「required」ではなく「optional」、デフォルト値は `os.Hostname()` の結果、説明は「TOML で明示指定しない場合 `os.Hostname()` にフォールバックする」旨を記載する。
- [ ] `docs/design/configuration.md` の Example セクション（29-36行目）の TOML コード例に `hostname = "worker-1"` の行を追加する。
- [ ] `docs/design/configuration.ja.md` に、上記2件の対訳（日本語版のフィールド一覧表の行・Example のコード例）を追加する。
- [ ] `docs/dev/developer_guide/package_reference.md` の `internal/notify` の説明（54行目）を、「`text` 一行サマリ + 失敗時のみの `attachments`」という記述から、「`text` 一行サマリ（件数を含まない） + 常に生成される `attachments`（実行ホスト名・アカウントハンドル、削除処理に到達した実行では対象件数・削除件数・処理時間を含む）」に更新する。
- [ ] `docs/dev/developer_guide/package_reference.md` の `internal/config` の説明（30行目）に、`ResolveHostname`（TOML `hostname` フィールド優先、未設定時は `os.Hostname()` にフォールバックしてホスト名を解決する）についての一文を追記する。
- [ ] 上記4件の記述内容がフェーズ1・3・5で実装した実際の挙動（`hostname` が任意項目であること、`ResolveHostname` のフォールバック順序、`buildPayload` が常に attachment を生成し統計フィールドを条件付きで含めること）と一致していることを、担当者自身が実装コードと本文を突き合わせてレビューする（実装コードを正とする整合性レビュー）。静的確認として `rg -n "hostname" docs/design/configuration.md docs/design/configuration.ja.md` を実行し、両ファイルに追加した `hostname` 行・コード例がそれぞれ1件以上マッチすることを確認する。`package_reference.md` の更新は単なる説明文の書き換えでありユニークな検索語を持たないため、この静的確認の対象外とし、上記のコードとの突き合わせレビューのみで担保する。

### PR-6 作成ポイント: configuration and package reference docs

**対象ステップ**: フェーズ11

**推奨タイトル**: `docs(0013): document hostname config field and package reference`

**レビュー観点**: `docs/design/configuration.md`/`.ja.md` の英日対訳が内容・書式ともに一致しているか / `package_reference.md` の `internal/notify`/`internal/config` の説明が PR-1〜PR-5 で実装した実際の挙動と乖離していないか

- [ ] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [ ] PR を作成した
- [ ] PR がマージされた
- [ ] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

### フェーズ12: ドキュメント更新（配信保証の限界の明記、F-004）

対応: F-004（AC-15, AC-16）。設計: アーキテクチャ設計書 5.4節。

- [ ] `README.md` の「Safety」節（241-250行目）の箇条書きに、既存の4項目（243-248行目）と同じ **太字見出し + em dash** の書式で次の1項目を追加する。
  ```
  - **No at-least-once delivery guarantee for Slack notifications** — if notification
    sending fails for any reason (including a process crash), the run's result,
    especially an already-completed deletion, may never be reported
  ```
- [ ] `README.ja.md` の「安全性」節（236-244行目）に、既存4項目（238-243行目）と同じ書式で次の対訳を追加する。
  ```
  - **Slack 通知は at-least-once 配信を保証しない** — 何らかの理由（プロセスクラッシュを含む）で
    通知の送信に失敗した場合、実行結果、特に完了済みの削除結果が通知されないまま
    失われることがある
  ```
- [ ] `docs/overview.md` の「Execution Result Notification」節（103-109行目）の末尾に、次の1段落を追加する。
  ```
  Notification delivery is best-effort and does not guarantee at-least-once
  delivery. If notification sending fails for any reason (including a process
  crash after runner.Run() completes), the run's result -- in particular an
  already-completed deletion -- may never be reported via Slack.
  ```
- [ ] `docs/overview.ja.md` の「実行結果の通知」節（103-109行目付近）の末尾に、次の対訳を追加する。
  ```
  通知の送信はベストエフォートであり、at-least-once の配信を保証しない。runner.Run() の
  完了後、何らかの理由（プロセスクラッシュを含む）で通知の送信が失敗した場合、実行結果、
  特に完了済みの削除結果が Slack に通知されないまま失われることがある。
  ```

### PR-7 作成ポイント: delivery guarantee documentation

**対象ステップ**: フェーズ12

**推奨タイトル**: `docs(0013): document Slack notification delivery guarantee limits`

**レビュー観点**: README/overview の英日対訳の文言が一致しているか / at-least-once 配信を保証しないという記述が、誇張・過小表現なく正確に事実（0006 由来の既知の制限であること）を反映しているか / AC-15・AC-16 の静的検証コマンドが検索するリテラル文字列と、実際に追加した本文が一字一句一致しているか

- [ ] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [ ] PR を作成した
- [ ] PR がマージされた
- [ ] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

## 4. Acceptance Criteria Verification

| AC | 内容概要 | 種別 | 検証方法 |
|---|---|---|---|
| AC-01 | 正常/異常終了いずれも `fields` に Host フィールドを含む | test | `internal/notify/payload_test.go::TestBuildPayload_SuccessOutcome_TextHasNoDeleteCount`（正常系）・`internal/notify/payload_test.go::TestBuildPayload_ErrOutcome_IncludesHostAndAccountFields`（異常系） |
| AC-02 | 正常/異常終了いずれも `fields` に Account フィールドを含む | test | 同上2テスト（Host と同一テスト内でアサーション） |
| AC-03 | TOML `hostname` 指定時はその値を使う | test | `internal/config/hostname_test.go::TestResolveHostname_TOMLValueSet_ReturnsTOMLValue`（`ResolveHostname` 単体のフォールバック順序）・`internal/config/hostname_test.go::TestLoad_HostnameField_ParsesOptionalTOMLKey`（TOML読み込み経路） |
| AC-04 | TOML `hostname` 未指定時は `os.Hostname()` を使う | test | `internal/config/hostname_test.go::TestResolveHostname_TOMLValueEmpty_ReturnsOSHostname`（`ResolveHostname` 単体のフォールバック順序）・`internal/config/hostname_test.go::TestLoad_HostnameField_ParsesOptionalTOMLKey`（TOML読み込み経路、キー無し時に空文字列がそのまま `Config.Hostname` へ届くことの確認） |
| AC-05 | `os.Hostname()` 失敗時も通知送信自体は失敗しない | static, manual | static: `rg -n "^func ResolveHostname\(cfg Config\) \(string, error\) \{$" internal/config/hostname.go` を実行し、1件マッチすること（AC-17 追加分により `ResolveHostname` は `(string, error)` を返すシグネチャに変更されたが、`os.Hostname()` 失敗時に通知フィールド用の値としては引き続き空文字列を返す設計であることの確認）。manual: `os.Hostname()` のエラーを空文字列にフォールバックしていること、および `run()` が `hostErr` を通知送信の失敗として扱わないことをコードレビューで確認する（`os.Hostname()` 失敗分岐自体はアーキテクチャ設計書7節の判断により自動テスト対象外） |
| AC-06 | 完全成功時にも attachment を生成する | test | `internal/notify/payload_test.go::TestBuildPayload_SuccessOutcome_TextHasNoDeleteCount`（`assert.Len(t, got.Attachments, 1)` のアサーション） |
| AC-07 | `Result != nil` の場合、削除対象件数フィールドを含む | test | `internal/notify/payload_test.go::TestBuildPayload_ResultNotNil_IncludesTargetsDeletedDurationFields` |
| AC-08 | `Result != nil` の場合、実削除件数フィールドを含む | test | 同上 |
| AC-09 | `Result != nil` の場合、処理時間フィールドを含む | test | `internal/notify/payload_test.go::TestBuildPayload_ResultNotNil_IncludesTargetsDeletedDurationFields`（`buildPayload` 単体） / `cmd/main_test.go::TestRun_ApplyAllSucceed_ReturnsExitCode0AndPrintsResult`（`run()` 経由の統合確認） |
| AC-10 | `Result == nil` の場合、統計フィールドを含まない | test | `internal/notify/payload_test.go::TestBuildPayload_ResultNil_ExcludesTargetsDeletedDurationFields` |
| AC-11 | 処理時間の計測区間が `runner.Run()` 呼び出しのみ | test | `cmd/main_test.go::TestRun_ApplyAllSucceed_ReturnsExitCode0AndPrintsResult`（Duration フィールドが非負の妥当な値であることを確認）に加え、`cmd/main.go` の `start`/`elapsed` の配置（`runner.Run()` 呼び出しの直前・直後）をコードレビューで確認する |
| AC-12 | 正常終了時の `text` に削除件数の数値を含まない | test | `internal/notify/payload_test.go::TestBuildPayload_SuccessOutcome_TextHasNoDeleteCount` |
| AC-13 | 異常終了時の `text` に削除件数・失敗件数の数値を含まない | test | `internal/notify/payload_test.go::TestBuildPayload_PartialFailure_TextHasNoFailureCount` |
| AC-14 | `text` は引き続き絵文字と短い状態文言を含む | test | `internal/notify/payload_test.go::TestBuildPayload_TextRetainsEmojiForSuccessAndFailure` |
| AC-15 | README(.ja).md Safety 節に配信保証なしの記述がある | static | `rg -n "No at-least-once delivery guarantee for Slack notifications" README.md` が1件マッチすること、かつ `rg -n "Slack 通知は at-least-once 配信を保証しない" README.ja.md` が1件マッチすること（フェーズ12で確定した文言そのものを検索するため、英語ファイルと日本語ファイルを別々のコマンドで検証する） |
| AC-16 | overview(.ja).md Execution Result Notification 節に配信保証なしの記述がある | static | `rg -n "does not guarantee at-least-once" docs/overview.md` が1件マッチすること、かつ `rg -n "at-least-once の配信を保証しない" docs/overview.ja.md` が1件マッチすること（フェーズ12で確定した文言そのものを検索するため、英語ファイルと日本語ファイルを別々のコマンドで検証する） |
| AC-17 | `os.Hostname()` 失敗時に `slog` で警告ログを出力する | static, manual | static: `rg -n 'slog\.Warn\(' cmd/main.go` が1件以上マッチすること。manual: `hostErr != nil` の場合にのみ `slog.Warn` が呼ばれ、`host`（フィールド値）は引き続き空文字列で通知処理が継続すること（AC-05 の best-effort 方針を後退させないこと）をコードレビューで確認する（`os.Hostname()` 失敗分岐自体はフェーズ1既存の判断により自動テスト対象外） |

NF-001（`make fmt`/`make test`/`make lint` の成功）はフェーズ9・下記実装チェックリストの `make lint`/`make test` 実行タスクで検証する。NF-002（ネットワーク通信なしの単体テスト）はフェーズ1・3・5・6で追加する全テストがモック/フィクスチャのみで完結することで満たされる（新規ネットワーク呼び出しを追加しない）。NF-003（Host/Account のサニタイズ）は `internal/notify/payload_test.go::TestBuildPayload_SanitizesMentionSyntaxAndControlCharsInHostAndAccount` で検証する。NF-004（秘密情報の非混入）は `Outcome`/`webhookPayload` のいずれのフィールドにも `config.SecretString` 型の値を代入していないことをコードレビューで確認する（フェーズ2・7の型変更が `SecretString` を一切参照しないことは `internal/notify/payload.go`・`cmd/main.go` の diff で確認できる）。NF-005（投稿本文の非含有）は既存の `TestBuildPayload_ExcludesPostBody_OnlyIncludesStructuredFields`（フェーズ3で更新済み）が継続して検証する。NF-006（Go 1.26.2 以上）は `go.mod` を変更しないため対象外。NF-007（`make notify-preview` の継続動作）はフェーズ4・8で確認する。

## 5. 実装順序とマイルストーン

アーキテクチャ設計書 8節の実装優先順位に対応する。フェーズ番号は本計画書独自の番号だが、順序と依存関係は8節と一致させている。

1. **マイルストーン A（フェーズ1 / PR-1）**: `internal/config` にホスト名解決ロジックが追加され、単体で完結してテスト可能になる。他フェーズと独立して着手できる。
2. **マイルストーン B（フェーズ2〜3 / PR-2）**: `Outcome` 型が拡張され、`buildPayload` が常に Host/Account を含む attachment を生成するようになる。この時点で既存テスト12件（既存コード調査結果参照）が破壊され、更新される。
3. **マイルストーン C（フェーズ4 / PR-3）**: Mattermost を含む実クライアントでの描画確認が完了し、5.3節のリスクが早期に解消される（後続フェーズより先に手戻りリスクを潰す）。
4. **マイルストーン D（フェーズ5〜6 / PR-4）**: 統計フィールドの追加、`text` からの件数除去が完了し、F-002・F-003 のコア実装が完了する。
5. **マイルストーン E（フェーズ7〜8 / PR-5）**: `cmd/main.go` が実際の処理時間・ホスト名・アカウントハンドルを計測・供給するようになり、`notifypreview` のフィクスチャが全フィールドを網羅する。
6. **マイルストーン F（フェーズ9〜10 / PR-5）**: 全テスト・実送信確認が完了し、機能実装が完了する。
7. **マイルストーン G（フェーズ11〜12 / PR-6・PR-7）**: ドキュメント更新が完了し、AC-15/AC-16 を含む全 AC が満たされる。

## 6. テスト戦略

- **単体テスト**: アーキテクチャ設計書 7節に定義された `internal/config/hostname_test.go`（新規）・`internal/notify/payload_test.go`（更新・新規）のテストケースを、フェーズ1・3・5・6 で実装する（3節参照）。
- **統合テスト**: `cmd/main_test.go` の既存6テスト（フェーズ7参照）のコンパイル追従に加え、`TestRun_Apply_SendsHostAndAccountFromConfigAndCredentials`（新規）・`TestRun_ApplyAllSucceed_ReturnsExitCode0AndPrintsResult` への Duration アサーション追加により、`cmd/main.go` が実際に正しい値を `notify.Outcome` に供給していることを検証する。
- **手動確認**: `make notify-preview`（フェーズ4・8）・`make notify-preview-send`（フェーズ4・10）による目視・実送信確認。
- **後方互換性の観点**: 本タスクは 0012 が確立した `isFailure`・`sanitizeForPayload`・`truncate`・チャンネル振り分けロジックを変更しないため、これらを検証する既存テスト（`TestIsFailure_FourOutcomePatterns` 等）はそのまま残し、新規の重複テストは追加しない。

## 7. リスク管理

- **技術リスク**: アーキテクチャ設計書 5.3節で述べた通り、完全成功時に danger色でない attachment が一部の Incoming Webhook 互換クライアント（Mattermost 等）で描画されない可能性がある。フェーズ4で早期に実送信確認を行い、問題があれば5.3節のフォールバック（成功時色の導入）を適用する。フォールバックが必要になった場合はアーキテクチャ設計書の該当節（3.3節手順5・付録）を先に改訂してから実装を進める。
- **スケジュールリスク**: 本タスクはドキュメント更新（フェーズ11・12）を含む。実装ステップの完了後にまとめて実施可能であり、他フェーズの妨げにならない。

## 8. 実装チェックリスト

- [x] PR-1 マージ済み（対象ステップ: フェーズ1）
- [x] PR-2 マージ済み（対象ステップ: フェーズ2 / フェーズ3）
- [x] PR-3 マージ済み（対象ステップ: フェーズ4）
- [x] PR-4 マージ済み（対象ステップ: フェーズ5 / フェーズ6）
- [ ] PR-5 マージ済み（対象ステップ: フェーズ7 / フェーズ8 / フェーズ9 / フェーズ10）
- [ ] PR-6 マージ済み（対象ステップ: フェーズ11）
- [ ] PR-7 マージ済み（対象ステップ: フェーズ12）
- [ ] `make lint` が成功することを確認する
- [ ] 4節の Acceptance Criteria Verification 表の全項目が緑であることを確認する

## 9. 完了基準

- **機能的完全性**: AC-01〜AC-16 がすべて4節の検証方法で緑であること。
- **品質指標**: `make fmt`・`make test`・`make lint` がすべて成功すること（NF-001）。フェーズ1・3・5・6で追加した新規テストがすべて実行され、失敗しないこと。
- **セキュリティ検証**: NF-003（Host/Account のサニタイズ）・NF-004（秘密情報の非混入）・NF-005（投稿本文の非含有）が4節の記載通り検証済みであること。
- **ドキュメント完全性**: フェーズ11・12で更新した全ドキュメント（`docs/design/configuration.md`/`.ja.md`・`docs/dev/developer_guide/package_reference.md`・`README.md`/`README.ja.md`・`docs/overview.md`/`docs/overview.ja.md`）に、AC-15・AC-16 で要求される記述が含まれていること。

## 10. 次のステップ

- 本実装計画書が `approved` であることを確認する。
- フェーズ1から順に実装に着手する（各フェーズ完了時に該当するチェックボックスを更新する）。
