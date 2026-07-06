# Slack 通知 — 実装計画書

## Document Status

| Item | Value |
|---|---|
| Status | `approved` |
| Created | 2026-07-05 |
| Review date | 2026-07-05 |
| Reviewer | isseis |
| Comments | - |

関連ドキュメント: [要件定義書](01_requirements.md) / [アーキテクチャ設計書](02_architecture.md)

## 1. 実装概要

### 1.1 目的

実行結果（成功/失敗、削除件数、失敗した投稿の識別子、エラー種別）を Slack Incoming Webhook 経由で通知する新規パッケージ `internal/notify` を実装し、`cmd/main.go` に統合する（F-001〜F-007）。あわせて、この通知機能が安全に動作するための2つの既存パッケージの拡張を行う: `internal/config` への Webhook URL 許可ホスト検証（F-005）、`internal/retry` への Webhook URL 秘匿用ログ抑制機構（3.6.1節）。

### 1.2 実装方針

[アーキテクチャ設計書](02_architecture.md) 8節の実装優先順位にそのまま従う。本計画書ではこれに加えて、[0005_retry_timeout](../0005_retry_timeout/03_implementation_plan.md) と同じ構成で、品質確認を独立したフェーズとして最後に置く。

- フェーズ1: `internal/retry` — Webhook URL 秘匿のための拡張（設計書 3.6.1節）
- フェーズ2: `internal/config` — Webhook URL 許可ホスト検証（設計書 3.1節）
- フェーズ3: `internal/notify` — 型定義・サニタイズ・エラーカテゴリ化（設計書 3.2節・4節）
- フェーズ4: `internal/notify` — ペイロード構築・切り詰め（設計書 3.3節）
- フェーズ5: `internal/notify` — `Send`（HTTP送信・リトライ統合、設計書 3.4節〜3.6節）
- フェーズ6: `cmd/main.go` への統合（設計書 2.2節・3.5節）
- フェーズ7: ドキュメント更新（設計書 3.5節・8節の7番目の項目）
- フェーズ8: 品質確認

### 1.3 既存コード調査結果

- **`internal/notify` パッケージは存在しない**（`find internal -maxdepth 1 -type d` で確認済み）。本タスクはこのパッケージを新規作成する。設計書 2.1節の新規・変更ファイル一覧の通り、`notify.go`・`payload.go`・`sanitize.go`・`errorkind.go` の4ファイルを新設する。
- **`internal/retry.NewDoer(inner HTTPDoer, policy Policy, clock Clock) *Doer`**（`internal/retry/doer.go:58`）は現在オプション引数を受け取らない。設計書 3.6.1節が要求する `Option` 可変長引数の追加は Go の可変長引数の後方互換性により、既存の呼び出し箇所を変更せずに追加できる。既存の呼び出し箇所は3箇所（`rg -n "retry\.NewDoer\(" internal cmd` で確認済み: `internal/atproto/client.go:54`・`internal/atproto/client.go:63`・`internal/atproto/client_test.go:51`）であり、いずれも `opts` を渡さないままで良い（テストコード側の呼び出し箇所も含め無変更でビルド・テストが通ることをフェーズ1の完了基準で確認する）。
- **`internal/retry/doer.go` の `logRetrying`/`logGivingUp`**（214〜240行目）は現在、非公開の自由関数として `req.URL.String()` を直接 `slog` に出力しており、`Doer` のどのフィールドも参照していない。設計書 3.6.1節の `WithURLRedactor` を実装するには、`Doer` 構造体に非公開フィールド `redact func(*http.Request) string`（未設定なら `nil`）を追加し、この2関数を `redact` を参照できる形（`(d *Doer)` のメソッド化、または `redact` を引数として渡す形のいずれか、実装時に選択する）に変更する必要がある。
- **`internal/config` の既存構成**を確認した（`config.go`・`validate.go`・`errors.go`・`credentials.go`・`app_config.go` を全文確認済み）。
  - `Config`（`config.go:18`）・`rawConfig`（`config.go:26`）に `slack_allowed_host` に対応するフィールドが存在しない。`RetentionDays`/`ExecutionTimeoutSeconds` とは異なり、TOML キーの省略とテスト対象文字列としての空文字列を区別する必要がない（3.1節ルール1: 両方の Webhook URL が未設定なら `slack_allowed_host` の設定有無を問わずスキップする、かつ空文字列と未設定は同じ「未設定」として扱ってよい）ため、`rawConfig.SlackAllowedHost` はポインタではなく `string` 型（`toml:"slack_allowed_host"`）で追加する。
  - `validateConfig`（`validate.go:18`）は必須フィールドの存在確認と数値範囲検証のみを行っており、`slack_allowed_host` の検証は行わない（そもそも `Credentials` を受け取らないため単独では検証できない）。設計書 3.1節の通り、新しい検証関数 `validateSlackAllowedHost(cfg Config, creds Credentials) error` を追加し、`LoadAppConfig`（`app_config.go:13`）が `Load`・`LoadCredentials` の両方を成功させた後に呼び出す。なお設計書 2.1節「新規・変更ファイル一覧」は `internal/config/validate.go`・`errors.go`・`credentials_test.go` の3ファイルのみを挙げており、`config.go`（`Config`/`rawConfig` へのフィールド追加）・`app_config.go`（`validateSlackAllowedHost` の呼び出し追加）を挙げていない。これは設計書のファイル一覧の抜け漏れであり、本計画はこの2ファイルへの変更を独自に追加する（credentials_test.go に関する1.3節の訂正と同様、設計書の記載を鵜呑みにせずコード調査結果を優先する）。
  - `errors.go` の既存センチネルエラー（`ErrFileNotFound`・`ErrParseFailed`・`ErrMissingField`・`ErrInvalidValue`・`ErrMissingEnv`）に、設計書 3.1節が定義する `ErrWebhookHostMismatch`・`ErrSlackAllowedHostMissing` を追加する。
  - `FieldError`（`errors.go:25`）の既存の使い分け（`Value` は TOML 由来の非秘匿フィールドのみに設定し、環境変数由来の秘匿フィールドでは空のまま、というコメント上の契約、`errors.go:24`）に従い、ホスト不一致エラーの `FieldError.Field` には該当する環境変数名（`BSKY_SLACK_WEBHOOK_URL_SUCCESS`/`BSKY_SLACK_WEBHOOK_URL_FAILURE`）を設定し、`Value` は空のままとする。`slack_allowed_host` 未設定エラーの `Field` は `"slack_allowed_host"` とする。
- **設計書 3.1節「影響を受ける既存テスト」の記載には誤りがある**: 設計書は `internal/config/credentials_test.go::TestLoadCredentials_Success`・`TestLoadCredentials_SlackWebhookURLOnlyOneSet` のフィクスチャに `slack_allowed_host` の追加が必要だとしているが、実際にこの2つのテストが呼び出すのは `LoadCredentials()` のみであり、TOML（`Config`）を一切読み込まない。`validateSlackAllowedHost` は `LoadAppConfig` からのみ呼び出される（3.1節設計判断）ため、`LoadCredentials()` 単体を呼ぶテストはこの検証を一切経由せず、影響を受けない。**`internal/config/credentials_test.go` は変更不要**と判断する（本計画のフェーズ2ではこの2テストを変更しない）。
- **`internal/config/app_config_test.go::TestLoadAppConfig_Success`（既存、設計書が見落としている箇所）**: このテストは `LoadAppConfig` を呼び出し、`setAllCredentialEnv` で正常系・異常系の両方の Webhook URL を `https://hooks.slack.com/services/...` に設定しているが、TOML フィクスチャに `slack_allowed_host` を含んでいない。`validateSlackAllowedHost` が `LoadAppConfig` に組み込まれると、このテストは AC-21 前段（Webhook URL 設定時に `slack_allowed_host` 必須）により失敗するようになる。フェーズ2でこのテストのフィクスチャに `slack_allowed_host = "hooks.slack.com"` を追加する必要がある。
- **`cmd/main_test.go` の既存テストへの広範な影響（設計書 2.1節が明示していない箇所）**: `setEnvCredentials`（`cmd/main_test.go:62`）は本ファイルの全テストで正常系・異常系の両方の Webhook URL を `hooks.slack.com` に設定しているが、`validConfigPath`（`cmd/main_test.go:33`）が書き込む TOML には `slack_allowed_host` が含まれない。`validateSlackAllowedHost` の導入後は、`setEnvCredentials` と `validConfigPath` を組み合わせて使う既存テストすべてが、Slack 通知そのものとは無関係な理由（AC-21 前段の起動失敗）で `config.LoadAppConfig` の時点から失敗するようになる。該当する既存テストは `TestRun_ClientInitFailure_ReturnsExitCode1`・`TestRun_LoginFailure_ReturnsExitCode1`・`TestRun_DryRunWithTargets_ReturnsExitCode0AndPrintsTargets`・`TestRun_DryRunNoTargets_ReturnsExitCode0AndPrintsNoTargetsMessage`・`TestRun_ApplyAllSucceed_ReturnsExitCode0AndPrintsResult`・`TestRun_ApplyPartialFailure_ReturnsExitCode3AndPrintsFailures` の6件（`validConfigPath` 経由）と、独自に TOML 文字列を組み立てる `TestRun_ExecutionTimeoutExceeded_ReturnsExitCode1`（`cmd/main_test.go:157`）の計7件である。これは「CLI/バイナリが複数回起動され、それぞれが同じ起動時検証を再実行する」場合に該当し、`validConfigPath` の共有 TOML 本文と `TestRun_ExecutionTimeoutExceeded_ReturnsExitCode1` 独自の TOML 本文の両方に `slack_allowed_host = "hooks.slack.com"` を追加する必要がある（フェーズ6で対応、2箇所とも漏れなく修正する）。`TestRun_ConfigLoadFailure_ReturnsExitCode1` は存在しないパスを渡すため `Load` 自体が失敗し、この変更の影響を受けない。
- **AC-04（dry-run では通知しない）の回帰テストは新規追加が不要**: `TestRun_DryRunWithTargets_ReturnsExitCode0AndPrintsTargets`・`TestRun_DryRunNoTargets_ReturnsExitCode0AndPrintsNoTargetsMessage`・`TestRun_LoginFailure_ReturnsExitCode1`（いずれも `apply=false` で `run` を呼ぶ）が使う `hermeticHandler`/`listRecordsHandler` は、`com.atproto.*` 系以外の未知のリクエストに対して既に `t.Fatalf("unexpected request: ...")` で失敗する作りになっている（`cmd/main_test.go:224`）。`cmd/main.go` の統合実装が dry-run 時に誤って Slack へ POST してしまった場合、この3テストは（上記のフィクスチャ修正後）そのまま回帰検出として機能する。したがって新規のテスト関数は追加せず、フィクスチャ修正のみで足りる。
- **`atproto.Post`（`internal/atproto/posts.go:51`）には投稿本文を保持するフィールドが一切存在しない**（`RKey`・`Type`・`CreatedAt`・`Pinned` の4フィールドのみ、全文確認済み）。したがって AC-14（通知ペイロードに投稿本文を含めない）は、`internal/notify` が `report.DeleteFailure.Post` から読み取れる情報自体に本文が存在しないという型レベルの保証によって、構造的に満たされる。ペイロード構築テストは「`RKey` とエラー種別以外の想定外の内容を含まない」ことを確認する形とする。
- **`internal/retry` の `Clock`/`fakeClock` パターン**（`internal/retry/clock.go`・`internal/retry/test_helpers.go`）は公開インターフェース `Clock` と、`//go:build test` 配下の非公開 `fakeClock` からなる。設計書 3.6節の通り `internal/notify.Send` も `clock retry.Clock` を引数に取るため、`internal/notify` 自身のテストは `retry.Clock` を実装する専用の `fakeClock` 相当の型を必要とする。[test_organization.md](../../dev/developer_guide/test_organization.md) の判断基準（公開APIのみを使うか／パッケージ内部専用か）に従うと、この型は `retry.Clock`（公開インターフェース）のみを実装する点で `internal/retry` 自身の `fakeClock` と全く同じ性質を持つ。`internal/retry` 側の `fakeClock` は利用テストファイルが `doer_test.go` の1つだけであるにもかかわらず `test_helpers.go`（Classification B、`//go:build test`）に置かれている（既存の前例）ため、本タスクもこれに合わせ `internal/notify/test_helpers.go`（`//go:build test`）に `fakeClock` を定義する。「複数テストファイルにまたがるかどうか」は Classification A/B の判断基準ではない。
- **`internal/notify` の HTTP モックには `httptest.Server` を用い、`atprototestutil.MockHTTPDoer` は使わない**: 設計書 7節は「HTTP挙動...は `net/http/httptest` によるモックサーバで検証する」と明記しており、`internal/notify` は `internal/atproto` に依存しない独立したテスト環境を持つ（設計書 2.1節: `internal/notify` は `internal/atproto` の型定義にのみ依存し `HTTPDoer` 実装には依存しない）。したがって新規の `testutil/` サブディレクトリは不要。
- **Webhook 単発 HTTP 呼び出しタイムアウト（3秒、設計書 3.6節）をテストで検証する際の速度上の懸念**: 3秒という値をハードコードされた定数のまま使うと、タイムアウトを実際に発生させるテストが3秒以上かかる。既存コードベースは `internal/retry` のバックオフ待機を `retry.Clock` の注入によってテストから排除しているが、HTTP 呼び出し自体のタイムアウト（`context.WithTimeout` の期間）は `retry.Clock` の対象外である。本計画はこれを非公開パッケージ変数（`var requestTimeout = 3 * time.Second`）として定義し、`internal/notify` 自身のテストからのみ短い値に上書きできるようにする（`internal/retry` の `Clock` 注入と同じ「テスト容易性のための小さな非公開の可変ポイント」という設計判断であり、公開 API・`Config` 構造体は変更しない）。
- **`docs/design/configuration.md` に、本タスクの実装を前提とした「未実装（計画中）」の記述が既に存在する**（19〜20行目の `slack_allowed_host` 行、44〜46行目の環境変数節）。これはアーキテクチャ設計時に将来の実装を見越して先行して書かれた記述であり、本タスクの実装完了後はこれらの「未実装」という注記が事実と矛盾するようになる。フェーズ7でこれらの注記を削除し、実装済みの記述に置き換える。また、31行目の `execution_timeout_seconds` の運用ガイダンス（0005 が追記したリトライ最悪ケース時間の説明）に、設計書 3.5節が定める通知処理の最悪ケース所要時間（約12秒: 3秒タイムアウト×最大3試行=9秒 + バックオフ1秒+2秒=3秒）を加味する旨を追記する必要がある。
- **`docs/dev/developer_guide/package_reference.md` はまだ `internal/notify` について記載していない**（`rg -n "internal/notify" docs/dev/developer_guide/package_reference.md` は0件）。また `internal/report` の既存の説明文（48行目）は「Reused as-is by a future Slack formatter」（将来形）となっており、本タスク完了後は事実と異なる。フェーズ7で `internal/notify` の新規セクションを追加し、`internal/report`・`internal/retry`・`internal/config` の説明文を実装済みの内容に更新する。

## 2. 実装ステップ

### フェーズ1: `internal/retry` — Webhook URL 秘匿のための拡張（設計書 3.6.1節）

- [x] **対象ファイル**: `internal/retry/doer.go`（既存ファイルの変更）
  - **作業内容**:
    - 設計書 3.6.1節の型定義通り、`Option func(*Doer)` 型と `WithURLRedactor(redact func(*http.Request) string) Option` 関数を追加する。
    - `Doer` 構造体に非公開フィールド `redact func(*http.Request) string` を追加する。
    - `NewDoer(inner HTTPDoer, policy Policy, clock Clock, opts ...Option) *Doer` に可変長引数 `opts` を追加し、構築時に順に適用する。
    - `logRetrying`・`logGivingUp`（214〜240行目）を変更し、`d.redact` が非 `nil` の場合はログの `url` フィールドに `d.redact(req)` の戻り値を、`nil` の場合は従来通り `req.URL.String()` を使う。
  - **完了基準**: `go build ./...` が成功する。`internal/atproto/client.go` の既存2箇所・`internal/atproto/client_test.go` の既存1箇所（計3箇所、1.3節参照）の `retry.NewDoer(...)` 呼び出し（`opts` を渡さない）が無変更のままビルド・テストが通ることを確認する。

- [x] **対象ファイル**: `internal/retry/doer_test.go`（既存ファイルの変更）
  - **作業内容**:
    - [x] `TestDoer_Do_WithURLRedactor_RetryLogUsesRedactedURL`（新規）: `WithURLRedactor` を指定して構築した `Doer` が再試行ログ（`logRetrying`）を出力する際、`url` フィールドの値が `redact` 関数の戻り値になり、`req.URL.String()`（生のURL）が含まれないことを検証する（既存の `TestDoer_Do_LogsRetryAttempt` と同じ `slog.SetDefault`/`bytes.Buffer` パターンを用いる）。
    - [x] `TestDoer_Do_WithURLRedactor_GivingUpLogUsesRedactedURL`（新規）: `Policy.MaxRetries` 到達により `logGivingUp` が出力される際も同様に `redact` の戻り値が使われ、生のURLが含まれないことを検証する。
    - [x] `TestDoer_Do_NoRedactor_GivingUpLogUsesRawURL`（新規）: `WithURLRedactor` を指定しない場合、`logGivingUp` が従来通り `req.URL.String()`（生のURL）をログに出力すること。既存の `TestDoer_Do_LogsRetryAttempt` は `logRetrying`（再試行ログ）の非redactorケースの回帰確認として既に機能しているが、`logGivingUp`（打ち切りログ)には同等の既存テストが存在しないため、`WithURLRedactor` 導入によってこの経路の未redactor時の挙動が変化していないことを保証する新規のベースラインテストとして追加する。
    - 既存の `TestDoer_Do_LogsRetryAttempt`（`WithURLRedactor` を指定しないケース）は無変更のまま、`redact` 未設定時に従来通り `req.URL.String()` が再試行ログに出力されることの回帰確認として機能する。
  - **完了基準**: `make test` で `internal/retry` パッケージの全テストが成功する。

### フェーズ2: `internal/config` — Webhook URL 許可ホスト検証（設計書 3.1節、F-005: AC-11〜AC-13, AC-21）

- [x] **対象ファイル**: `internal/config/errors.go`（既存ファイルの変更）
  - **作業内容**: 設計書 3.1節のコード例通り、`ErrWebhookHostMismatch`・`ErrSlackAllowedHostMissing` の2つのセンチネルエラーを追加する。
  - **完了基準**: `go build ./...` が成功する。

- [x] **対象ファイル**: `internal/config/config.go`（既存ファイルの変更）
  - **作業内容**: `rawConfig`（26行目）に `SlackAllowedHost string \`toml:"slack_allowed_host"\`` を追加する。`Config`（18行目）に `SlackAllowedHost string` を追加する。`validateConfig`（`validate.go`）の戻り値構築部分に `SlackAllowedHost: raw.SlackAllowedHost` を追加する（1.3節の通り、他の必須フィールドと異なりポインタ型にせず、欠落時のゼロ値 `""` をそのまま「未設定」として扱う）。
  - **完了基準**: `go build ./...` が成功する。

- [x] **対象ファイル**: `internal/config/validate.go`（既存ファイルの変更）
  - **作業内容**: 設計書 3.1節「検証内容」の3ルールに従い `validateSlackAllowedHost(cfg Config, creds Credentials) error` を追加する。
    1. `creds.SlackSuccessWebhookURL.Reveal()` と `creds.SlackFailureWebhookURL.Reveal()` が両方とも空文字列なら、`cfg.SlackAllowedHost` の値を問わず `nil` を返す（AC-21後段）。
    2. 上記に該当せず（いずれか一方でも設定されており）、かつ `cfg.SlackAllowedHost == ""` なら `&FieldError{Field: "slack_allowed_host", Err: ErrSlackAllowedHostMissing}` を返す（AC-21前段）。
    3. 上記のいずれにも該当しない場合、設定されている Webhook URL（`BSKY_SLACK_WEBHOOK_URL_SUCCESS`・`BSKY_SLACK_WEBHOOK_URL_FAILURE`）それぞれについて、`net/url.Parse` 済みの `Hostname()`（ポート番号を除く）を `strings.EqualFold` で `cfg.SlackAllowedHost` と比較し、一致しなければ該当する環境変数名を `Field` に持つ `&FieldError{Field: <該当envvar名>, Err: ErrWebhookHostMismatch}` を返す（AC-12, AC-13）。両方とも設定されている場合、成功/失敗のどちらの照合が先に不一致になってもよい（順序は要件で規定されていない）。未設定（空文字列）の側はスキップする。
  - **完了基準**: `go build ./...` が成功する。この関数は `internal/config` パッケージ内でのみ使用され、公開しない。

- [x] **対象ファイル**: `internal/config/app_config.go`（既存ファイルの変更）
  - **作業内容**: `LoadAppConfig`（13行目）が `Load`・`LoadCredentials` の両方を成功させた後、`AppConfig` を組み立てて返す前に `validateSlackAllowedHost(*cfg, *creds)` を呼び出し、エラーがあればそれをそのまま返す。
  - **完了基準**: `go build ./...` が成功する。

- [x] **対象ファイル**: `internal/config/app_config_test.go`（既存ファイルの変更・追加）
  - **作業内容**:
    - [x] `TestLoadAppConfig_Success`（既存）のTOMLフィクスチャに `slack_allowed_host = "hooks.slack.com"` を追加する（1.3節の通り、このテストが正常系・異常系の両方の Webhook URL を `hooks.slack.com` に設定しているため、この追加なしでは AC-21 前段により失敗するようになる）。
    - [x] `TestLoadAppConfig_SlackAllowedHost_BothURLsMatchAllowedHost_Succeeds`（新規）: 正常系・異常系の両方の Webhook URL のホスト部が `slack_allowed_host` と一致する場合、`LoadAppConfig` が成功すること（AC-11）。
    - [x] `TestLoadAppConfig_SlackAllowedHost_OnlySuccessURLSet_MatchesAllowedHost_Succeeds`（新規）: 正常系 Webhook URL のみ設定され、そのホスト部が `slack_allowed_host` と一致する場合に成功すること（AC-11、片方のみ設定という既存の正当な構成が壊れないことの確認）。
    - [x] `TestLoadAppConfig_SlackAllowedHost_SuccessURLHostMismatch_ReturnsError`（新規）: 正常系 Webhook URL のホスト部が `slack_allowed_host` と異なる場合、`LoadAppConfig` が `ErrWebhookHostMismatch` を返し、`errors.AsType[*FieldError]` で得られる `Field` が `"BSKY_SLACK_WEBHOOK_URL_SUCCESS"` であること（AC-12）。
    - [x] `TestLoadAppConfig_SlackAllowedHost_FailureURLHostMismatch_ReturnsError`（新規）: 異常系 Webhook URL のホスト部が異なる場合も同様に `ErrWebhookHostMismatch`・`Field == "BSKY_SLACK_WEBHOOK_URL_FAILURE"` であること（AC-12）。
    - [x] `TestLoadAppConfig_SlackAllowedHost_PortAndCaseIgnoredInComparison_Succeeds`（新規）: Webhook URL が `https://Hooks.Slack.com:443/services/x` のようにポート番号・大文字小文字の異なる表記であっても、`slack_allowed_host = "hooks.slack.com"` と一致するとみなされ成功すること（AC-13）。
    - [x] `TestLoadAppConfig_SlackAllowedHost_MissingWhileWebhookURLSet_ReturnsError`（新規）: 正常系 Webhook URL のみ設定され `slack_allowed_host` が未設定（TOMLにキー自体がない）の場合、`ErrSlackAllowedHostMissing` を返し、`Field == "slack_allowed_host"` であること（AC-21前段）。
    - [x] `TestLoadAppConfig_SlackAllowedHost_NotRequiredWhenBothWebhookURLsUnset_Succeeds`（新規）: 正常系・異常系の両方の Webhook URL が未設定の場合、`slack_allowed_host` が未設定であっても `LoadAppConfig` が成功すること（AC-21後段、Slack 通知を使わない運用との整合）。
  - **完了基準**: `make test` で `internal/config` パッケージの全テストが成功する。

- [x] **対象ファイル**: `internal/config/config_test.go`（既存ファイルの変更）
  - **作業内容**: `TestLoad_SlackAllowedHostField_ParsesOptionalTOMLKey`（新規）を追加する。`slack_allowed_host = "hooks.slack.com"` を含む TOML から `Load` を呼んだ場合に `cfg.SlackAllowedHost == "hooks.slack.com"` となること、キー自体を含まない TOML では `cfg.SlackAllowedHost == ""` のままエラーにならないことの2パターンをテーブル駆動で検証する（新規に追加した TOML フィールドが実際にパースされることの基礎確認。`validateSlackAllowedHost` 側のテストとは独立して、パース自体が正しいことを確認する）。
  - **完了基準**: `make test` で本テストが成功する。

- [x] **フェーズ2完了時の前倒し修正（計画からの分岐）**: `validateSlackAllowedHost` を `LoadAppConfig` に組み込んだ時点で、`cmd/main_test.go` の `validConfigPath`（フェーズ6項目、当初計画は本行をフェーズ6でのみ修正する想定だった）が書き込む TOML に `slack_allowed_host` がないため、`setEnvCredentials` が両 Webhook URL を `hooks.slack.com` に設定している既存テスト群が AC-21 前段によりテスト失敗するようになった。フェーズ6の本格的な `cmd/main.go` 統合（`notify.Send` 呼び出し等）を待たず、このフェーズ2の完了時点で `validConfigPath` と `TestRun_ExecutionTimeoutExceeded_ReturnsExitCode1` 独自TOMLの双方に `slack_allowed_host = "hooks.slack.com"` を追加済み（`make test` を継続的に green に保つため）。フェーズ6では、この行の再追加は不要（既に完了済み）である点に注意し、Slack POST 応答モックの追加など残りの統合作業のみを行う。

### フェーズ3: `internal/notify` — 型定義・サニタイズ・エラーカテゴリ化（設計書 3.2節・4節）

- [x] **対象ファイル**: `internal/notify/sanitize.go`（新規作成）
  - **作業内容**: 設計書 3.2節のシグネチャ通り `Sanitize(s string) string` を実装する。C0制御文字（ESC (0x1B) を含む）と改行 (`\n`/`\r`) を除去する。
  - **完了基準**: `go build ./...` が成功する。

- [x] **対象ファイル**: `internal/notify/sanitize_test.go`（新規作成）
  - **作業内容**:
    - [x] `TestSanitize_RemovesANSIEscapeSequence`: ESC (0x1B) から始まるANSIエスケープシーケンスを含む文字列が無害化されること（AC-16）。
    - [x] `TestSanitize_RemovesNewlinesAndCarriageReturns`: `\n`・`\r` を含む文字列からこれらが除去されること（AC-17）。
    - [x] `TestSanitize_LeavesOrdinaryTextUnchanged`: 制御文字を含まない通常の文字列（マルチバイト文字を含む）が変化しないこと（回帰確認）。
  - **完了基準**: `make test` で本ファイルの全テストが成功する。

- [x] **対象ファイル**: `internal/notify/errorkind.go`（新規作成）
  - **作業内容**: 設計書 4節の通り `errorKind(err error) string` を実装する。`errors.AsType[*config.FieldError]`・`errors.AsType[*atproto.HTTPError]`・`errors.AsType[*atproto.SSRFError]` の順に判定し、該当する型のうち秘匿情報を含まないフィールド（`Field`／`Method`+`StatusCode`+`ErrorName`／`Endpoint`+`Stage`）から分類文字列を組み立てる。いずれにも一致しない場合は固定文字列 `"unknown error"` を返す。`err.Error()` や `%v`/`%+v` によるエラー全体の展開は行わない。
  - **完了基準**: `go build ./...` が成功する。

- [x] **対象ファイル**: `internal/notify/errorkind_test.go`（新規作成）
  - **作業内容**:
    - [x] `TestErrorKind_ConfigFieldError_ReturnsFieldBasedCategory`: `*config.FieldError` を渡した場合、`Field` に基づく分類文字列が返ること。
    - [x] `TestErrorKind_AtprotoHTTPError_ReturnsMethodAndStatusBasedCategory`: `*atproto.HTTPError` を渡した場合、`Method`/`StatusCode`/`ErrorName` に基づく分類文字列が返ること。
    - [x] `TestErrorKind_AtprotoSSRFError_ReturnsEndpointStageBasedCategory`: `*atproto.SSRFError` を渡した場合、`Endpoint`/`Stage` に基づく分類文字列が返ること。
    - [x] `TestErrorKind_UnknownErrorType_ReturnsUnknownErrorFallback`: 上記いずれの型にも一致しない `errors.New("some error")` を渡した場合、固定文字列 `"unknown error"` が返ること（AC-20、4節「運用上の意味」）。
    - [x] `TestErrorKind_NeverIncludesRawErrorStringOrSecrets`: `fmt.Errorf("...: %w", ...)` で `"Authorization: Bearer secret-token"` のような秘匿情報らしき文字列を埋め込んだ未知のエラー型を渡した場合でも、戻り値が固定の `"unknown error"` のみであり、埋め込んだ文字列を一切含まないこと（AC-19, AC-20, NF-003）。
  - **完了基準**: `make test` で本ファイルの全テストが成功する。

### フェーズ4: `internal/notify` — ペイロード構築・切り詰め（設計書 3.3節）

- [x] **対象ファイル**: `internal/notify/payload.go`（新規作成）
  - **作業内容**:
    - パッケージ非公開の `escapeSlackMarkup(s string) string` を実装する。`&`→`&amp;`、`<`→`&lt;`、`>`→`&gt;` の順に置換する（設計書 3.3節、Slack公式のmrkdwnエスケープ規則）。
    - パッケージ非公開の `buildPayload(outcome Outcome) string` を実装する。設計書 3.3節の表の通り、実行結果（成功/失敗）・削除件数・失敗した投稿の識別子（`DeleteFailure.Post.RKey`）・エラー種別（`errorKind(err)`）のみを組み立てる。投稿本文は一切参照しない（1.3節の通り `atproto.Post` に本文フィールドが存在しないため構造的に不可能）。識別子・エラー種別には `Sanitize()` を適用した後に `escapeSlackMarkup()` を適用する（設計書 3.3節「サニタイズの二段階」の順序）。`outcome.Result` が `nil`（ログイン/一覧取得失敗等、`runner.Run` が結果を返せなかった場合）でもパニックせず、削除件数を0件として扱い `outcome.Err` 由来のエラー種別のみを含むテキストを構築する（`report.FormatText` の "Never panics" という既存の設計方針と同様の配慮をする）。
    - 全体のテキストが上限（`const maxPayloadLength = 4000`）を超える場合、末尾を切り詰め `"...(truncated)"` を付与する（設計書 3.3節）。
  - **実装時の分岐（計画からの逸脱）**: 設計書 3.2節の型定義のうち `Outcome`（`buildPayload` が直接引数に取る）だけは、フェーズ5の `notify.go` ではなくこのフェーズ4の `payload.go` 側に定義した。`HTTPDoer`/`Config`/`SendError` はフェーズ5の計画通り `notify.go` に定義している。
  - **完了基準**: `go build ./...` が成功する。

- [x] **対象ファイル**: `internal/notify/payload_test.go`（新規作成）
  - **作業内容**:
    - [x] `TestEscapeSlackMarkup_EscapesAmpersandLtGt`: `&`・`<`・`>` を含む文字列がそれぞれ `&amp;`・`&lt;`・`&gt;` に置換されること（AC-15の前提となる白箱テスト）。
    - [x] `TestBuildPayload_SuccessOutcome_IncludesDeleteCountAndStatus`: `Outcome{Result: &report.Result{Mode: report.ModeApply, Deleted: [...]}}`（`Err` が `nil`、`Failed` が空）から、成功を示す文言と削除件数を含むテキストが構築されること（AC-01）。
    - [x] `TestBuildPayload_RunError_IncludesErrorKind`: `Outcome{Result: nil, Err: someErr}`（ログイン失敗等を模した実行時エラー）から、`errorKind(someErr)` の戻り値を含むテキストが構築されること（AC-01）。
    - [x] `TestBuildPayload_PartialFailure_IncludesFailedRKeysAndErrorKind`: `Result.Failed` に複数の `DeleteFailure` を含む `Outcome` から、それぞれの `RKey` と `errorKind` の分類文字列を含むテキストが構築されること（AC-01）。
    - [x] `TestBuildPayload_ExcludesPostBody_OnlyIncludesStructuredFields`: 構築されたテキストが `RKey`・エラー種別・件数以外の想定外の内容を含まないこと（AC-14。1.3節の通り `atproto.Post` に本文フィールドがないため、このテストは主に「意図せぬフィールドを追加していないこと」の回帰確認として機能する）。
    - [x] `TestBuildPayload_EscapesMentionSyntaxInFailedRKey`: 失敗した投稿の `RKey` に `<!channel>` を含むテスト用の値を与えた場合、構築されたテキストにおいて `<` が `&lt;` にエスケープされ、Slackのメンション記法として解釈されない形になること（AC-15）。
    - [x] `TestBuildPayload_SanitizesANSIEscapeInFailedRKey`: `RKey` にANSIエスケープシーケンスを含む値を与えた場合、構築されたテキストにエスケープシーケンスがそのまま残らないこと（AC-16）。
    - [x] `TestBuildPayload_SanitizesNewlineInFailedRKey`: `RKey` に改行を含む値を与えた場合、構築されたテキストに生の改行が残らないこと（AC-16、ログ偽装対策の一部としてペイロード側でも確認）。
    - [x] `TestBuildPayload_TruncatesWhenExceedsLimit_AppendsTruncatedMarker`: `Result.Failed` に大量の `DeleteFailure` を含め全体が4000文字を超える `Outcome` を与えた場合、構築されたテキストの長さが上限以下に切り詰められ、末尾に `"...(truncated)"` が付与されること（AC-18）。
    - [x] `TestBuildPayload_TruncationIsUTF8Safe`（コードレビューで発見したバグの回帰防止として新規追加）: マルチバイト文字（日本語）の `RKey` を大量に含め切り詰めが発生する `Outcome` を与えた場合、切り詰め後のテキストが `utf8.ValidString` を満たすこと。当初の実装は `text[:maxPayloadLength-len(truncatedMarker)]` という生のバイトオフセットでスライスしており、マルチバイト文字の途中で切断され不正なUTF-8を生成しうるバグがあった。`truncationCutPoint`（新規のヘルパー関数、`unicode/utf8.RuneStart` でルーン境界まで後退する）を導入して修正した。
  - **完了基準**: `make test` で本ファイルの全テストが成功する。

- [x] **テスト品質監査での追加修正**（2026-07-06、PR-2 マージ前に対応。優先度の高い順）
  1. **（優先度: 高、セキュリティ）** `buildPayload` の `outcome.Err` 経路（`SSRFError` 等）に対するSlackインジェクション対策の検証が存在しなかった: `atproto.SSRFError.Endpoint` はDID/PDS解決由来で攻撃者が影響しうる値（`docs/design/security.md` の脅威モデル対象）であり、`errorKind` 経由で `buildPayload` の出力に埋め込まれるが、既存のインジェクション系テスト（`TestBuildPayload_EscapesMentionSyntaxInFailedRKey` 等）は `Result.Failed[].Post.RKey` 経路しかカバーしていなかった。**実装時の分岐**: `errorKind` は `SSRFError.Endpoint` を `%q`（Go文字列リテラル形式）で埋め込んでおり、これが改行・ANSIエスケープ等の制御文字を既に文字表現へエスケープ済みにしてしまうため、`Sanitize()` によるANSI/改行除去は本経路では実質的な検証対象にならない（`%q` が先に無害化してしまうため）一方、`<`/`>` はそのまま素通りするため `escapeSlackMarkup()` によるmrkdwnメンション構文の無害化のみが本経路で意味のある検証対象である。この実態に合わせ、当初案の `TestBuildPayload_SanitizesSlackInjectionInSSRFErrorEndpoint`（仮称、ANSI/改行/メンション構文すべてを検証する想定）ではなく、`TestBuildPayload_RunError_EscapesMentionSyntaxInSSRFErrorEndpoint`（メンション構文の無害化のみを検証）を追加した。
  2. **（優先度: 中）** `TestBuildPayload_RunError_IncludesErrorKind`・`TestBuildPayload_PartialFailure_IncludesFailedRKeysAndErrorKind` は期待値を検証対象自身の `errorKind(...)` 呼び出し結果から生成しており、`errorKind` 自体にバグがあっても両辺が同じ値になり検出できなかった。期待値を既知の入力から手書きしたリテラル文字列（`"atproto http error: com.atproto.server.createSession status=401"` 等）に置き換えた。
  3. **（優先度: 低）** `TestBuildPayload_ExcludesPostBody_OnlyIncludesStructuredFields` は `assert.Contains` を使っており、想定外フィールドが追加で混入しても検出できなかった（「他に何が含まれていても」通ってしまうため）。`assert.Equal` による完全一致比較に変更した。
  4. **（優先度: 低、フェーズ5 `notify_test.go` 分の前倒し対応）** `TestSend_Success_PostsToSelectedWebhook`（フェーズ5）も同種の自己参照問題（期待値を `buildPayload(...)` 呼び出し結果から生成）があったため、本項目とあわせてリテラル部分文字列比較に修正した（詳細はフェーズ5側のチェックリストに記載）。
  - **完了基準**: 追加・修正した全テストを含め `make test`・`make lint` で `internal/notify` パッケージの全テストが成功する（確認済み: 2026-07-06）。

### フェーズ5: `internal/notify` — `Send`（HTTP送信・リトライ統合、設計書 3.4節〜3.6節）

- [x] **対象ファイル**: `internal/notify/notify.go`（新規作成）
  - **作業内容**:
    - 設計書 3.2節の型定義通り、`HTTPDoer`・`Config`（`SuccessWebhookURL`/`FailureWebhookURL` とも `config.SecretString`）・`Outcome`（`Result *report.Result`・`Err error`）・`SendError`（`StatusCode int`・`Err error`、`Error()`/`Unwrap()`）を定義する。
    - 非公開パッケージ変数 `var requestTimeout = 3 * time.Second`（1.3節のテスト容易性に関する決定）と、`defaultRetryPolicy = retry.Policy{MaxRetries: 2, BaseDelay: time.Second, MaxDelay: 4 * time.Second}`（設計書 3.6節の既定値）を定義する。
    - `Send(ctx context.Context, cfg Config, doer HTTPDoer, clock retry.Clock, outcome Outcome) error` を実装する。
      1. 設計書 3.4節の表の優先順位で送信先 `SecretString` を選択する: `outcome.Err != nil` または `len(outcome.Result.Failed) > 0` なら `cfg.FailureWebhookURL`、それ以外は `cfg.SuccessWebhookURL`。
      2. 選択した URL の `Reveal()` が空文字列なら、送信をスキップし `nil` を返す（設計書 3.5節の表3行目）。
      3. `buildPayload(outcome)`（フェーズ4）の結果を Slack Incoming Webhook の JSON ボディ `{"text": "..."}` に組み立てる。
      4. `retry.NewDoer(doer, defaultRetryPolicy, clock, retry.WithURLRedactor(...))` を構築する。渡す redactor は選択した Webhook URL のホスト名のみを返す関数とする（例: `"https://" + host + "/services/[REDACTED]"`、設計書 3.6.1節）。
      5. `http.NewRequestWithContext` で選択したURLへの POST リクエストを組み立て（この時点の ctx には requestTimeout を適用しない ── 理由は次項の分岐メモを参照）、構築した `retry.Doer` 経由で送信する。
      6. 通信エラー・非2xxレスポンスのいずれの場合も、生の `net/http`/`net/url` エラー文字列やレスポンスボディを一切含めず `&SendError{StatusCode: <0または実際のステータス>, Err: <元エラー>}` を返す（設計書 4節・5.2節）。`SendError.Error()` は `StatusCode` と固定の分類文字列のみから組み立て、`Unwrap()` は内部の `err` を返すが、呼び出し元は `Unwrap()` の結果を直接出力してはならない（設計書 3.2節のコメント契約）。
      7. 成功（2xx）の場合は `nil` を返す。
  - **実装からの分岐（コードレビューで発見）**: 当初の実装は手順5を「`context.WithTimeout(ctx, requestTimeout)` で単一呼び出し用の ctx を作り、`http.NewRequestWithContext` でリクエストを組み立てる」としていたが、これは `internal/retry.Doer`（`cloneForAttempt`）が再試行のたびに元のリクエストの ctx をそのまま使い回す実装であるため、1回の低速な応答だけでこの共有デッドラインが尽きてしまうと、以降の全ての再試行・バックオフ待機が即座に失敗し、設計書 3.6節が想定する「requestTimeout × 最大試行回数」という最悪ケースモデル（再試行ごとに新しいタイムアウト予算を得る）が成立しないバグがあった。修正として、`doer`（`HTTPDoer`）を `perAttemptTimeoutDoer`（`Do` 呼び出しのたびに `req.Context()` から新しい `context.WithTimeout` を導出するラッパー）でラップしてから `retry.NewDoer` に渡す形に変更した。`Send` 自身は呼び出し元から渡された `ctx` をそのまま `http.NewRequestWithContext` に渡し、requestTimeout の適用は `perAttemptTimeoutDoer` 側に一任する。回帰防止として `TestSend_EachRetryAttemptGetsFreshTimeout`（新規）と `TestSend_HTTPTimeout_ReturnsSendError` への試行回数アサーション追加で検証した。
  - **完了基準**: `go build ./...` が成功する。

- [x] **対象ファイル**: `internal/notify/test_helpers.go`（新規作成、`//go:build test`）
  - **作業内容**: `package notify` を宣言し、`//go:build test` タグを付与する。`retry.Clock` を実装する非公開の `fakeClock` 構造体（実待機なしで `SleepCalls []time.Duration` に記録し、`nil` を返す）を定義する。`internal/retry/test_helpers.go` の `fakeClock` と同じ設計であり、`retry.Clock`（公開インターフェース）のみを実装する（1.3節。`internal/retry` 自身の `fakeClock` が利用テストファイル1つのみにもかかわらず `test_helpers.go` に置かれている前例に合わせる）。
  - **完了基準**: `internal/notify` パッケージ配下のテストからのみ参照され、`//go:build test` タグにより本番ビルドに含まれないことを `go build ./...`（タグなし）でも確認する。

- [x] **対象ファイル**: `internal/notify/notify_test.go`（新規作成）
  - **作業内容**: `net/http/httptest.Server` を用いて実際のHTTPラウンドトリップを検証する。各テストで構築した `httptest.Server` は、構築直後に `t.Cleanup(server.Close)` を登録し、テスト終了時に必ず停止する（すべてのテストケースで一貫して適用し、リスニングソケット・goroutineのリークを防ぐ）。
    - [x] `TestSend_Success_PostsToSelectedWebhook`: 正常系 `Outcome`（成功、失敗なし）で `Send` を呼び、`httptest.Server` が正常系URL宛のPOSTを受信し、そのボディが `buildPayload` の出力を含む妥当なJSONであること（AC-01）。
    - [x] `TestSend_ChannelRouting_AllSucceeded_UsesSuccessURL`: 全件成功の `Outcome` で正常系URLにのみ送信されること（AC-05）。
    - [x] `TestSend_ChannelRouting_RunError_UsesFailureURL`: `Outcome.Err != nil`（`Result` が `nil`）の場合に異常系URLに送信されること（AC-06）。
    - [x] `TestSend_ChannelRouting_PartialFailure_UsesFailureURL`: `Result.Failed` が1件以上の場合に異常系URLに送信されること（AC-07）。
    - [x] `TestSend_SameWebhookURLForBothChannels_RoutesCorrectlyInBothOutcomes`: `Config.SuccessWebhookURL == Config.FailureWebhookURL` の場合、成功・失敗いずれの `Outcome` でも同一URLに正しく送信されること（AC-08）。
    - [x] `TestSend_SelectedWebhookURLEmpty_SkipsSendReturnsNil`: 選択された送信先の `SecretString` が空の場合、HTTPリクエストを一切発行せず `nil` を返すこと（設計書 3.5節の回帰防止テスト、対応するACはないが明示的に規定されている挙動）。
    - [x] `TestSend_HTTPTimeout_ReturnsSendError`: `requestTimeout` をテスト用に短い値（例: 50ミリ秒。実行環境の負荷によるスケジューリング遅延で誤って早期タイムアウト/未タイムアウトにならない程度の余裕を持たせる）に上書きし、`httptest.Server` のハンドラを意図的にブロックさせ、`Send` が `*SendError`（`StatusCode == 0`）を返すこと（AC-09）。テスト終了後に `requestTimeout` を元の値に `t.Cleanup` で復元する。
    - [x] `TestSend_NonRetryableStatus_ReturnsSendErrorWithStatusCode`: `httptest.Server` が400を返す場合、リトライされず1回のリクエストで `*SendError{StatusCode: 400}` が返ること。
    - [x] `TestSend_RetriesTransientFailureThenSucceeds_UsesFakeClock`: 1回目500・2回目200を返す `httptest.Server` に対し、`fakeClock` を使って実待機なしで2回目の成功応答が返ること（F-004）。
    - [x] `TestSend_MaxRetriesExceeded_ReturnsSendError_BoundedAttempts`: 常に500を返す `httptest.Server` に対し、`fakeClock` を使って実待機なしで検証し、リクエスト回数が `defaultRetryPolicy.MaxRetries + 1`（= 3回）で頭打ちになり、最終的に `*SendError` が返ること（AC-10、リトライが有界であることの確認）。
    - [x] `TestSendError_Error_NeverContainsWebhookURL`: 送信先URLのパスにトークン文字列を含む `httptest.Server` の URL を用い、通信エラー（例: サーバーを即座にクローズする）を発生させ、`SendError.Error()` の戻り値にその URL/トークン文字列が含まれないこと（AC-19）。
    - [x] `TestSend_RetryLog_UsesRedactedURL_NotRawWebhookURL`: `slog.SetDefault` をバッファ書き込みハンドラに差し替え、500応答によるリトライを発生させたうえで、ログ出力に Webhook URL のパス（トークン相当）が含まれず、ホスト名相当の文字列のみが含まれること（AC-19、`WithURLRedactor` の実際の配線確認）。
    - [x] `TestNotifyWorstCaseTime_BoundedBelowExecutionTimeoutGuidance`: 設計書 3.6節が示す約12秒という最悪ケース所要時間の見積もりが、[0005_retry_timeout](../0005_retry_timeout/01_requirements.md) が定める推奨実行タイムアウト値のオーダー（数十秒〜）より十分小さいことを確認する（AC-10）。**実装からの分岐（2026-07-06、テスト品質監査で発見）**: 当初の実装は `requestTimeout`・`defaultRetryPolicy` の値のみから最悪ケース所要時間を独立した算術式で再計算し、設計書の約12秒という数値と比較するものだった。これは `backoffDelay`（本番のバックオフ計算式、`internal/retry/doer.go`）を一切呼び出さず同じ式をテスト側で再実装していたため、本番の計算式自体にバグがあっても両者が同じ値になり検出できないという欠陥があった。修正として、常に500を返す `httptest.Server` に対して実際に `Send`（→ 実際の `retry.Doer.Do`）を実行し尽くし、`fakeClock` が記録した実際の `SleepCalls` の合計値から最悪ケース所要時間を導出する形に変更した（`TestSend_MaxRetriesExceeded_ReturnsSendError_BoundedAttempts` と同じ `httptest.Server`/`fakeClock` の組み合わせを流用）。これにより本番のリトライループ・バックオフ計算式そのものを経由した検証になった。
  - **完了基準**: `make test` で本ファイルの全テストが成功する。`TestSend_HTTPTimeout_ReturnsSendError` を除く全テストが `fakeClock` を使い実待機を行わないため、`go test -tags test -run TestSend ./internal/notify -v` の実行時間が1秒未満であることを目視確認する（NF-002）。

- [x] **テスト品質監査での追加修正**（2026-07-06、PR-2 マージ前に対応。優先度: 低）
  - `TestSend_Success_PostsToSelectedWebhook` は期待値の一部を検証対象と同じ経路の `buildPayload(...)` 呼び出し結果から生成しており、`buildPayload` 自体のバグはこのテストでは検出できなかった（`payload_test.go` 側で別途カバーされるため実害は小さいが、多重防御のため対応した）。`assert.Contains` をハードコードしたリテラル部分文字列（`"bsky-cleaner run succeeded: deleted 0 post(s)."`）による検証に置き換えた。
  - **完了基準**: 変更後も `make test` で `internal/notify` パッケージの全テストが成功する（確認済み: 2026-07-06）。

### フェーズ6: `cmd/main.go` への統合（設計書 2.2節・3.5節）

- [x] **対象ファイル**: `cmd/main.go`（既存ファイルの変更）
  - **作業内容**:
    - `internal/notify`・`internal/retry` を import する。
    - `notifyTimeout`（新規の非公開定数、例: `const notifyTimeout = 15 * time.Second`）を定義する。設計書 3.6節が示す通知処理の最悪ケース所要時間（約12秒）に安全マージンを加えた値とし、その根拠をコメントに明記する（設計書 3.5節がこの値を明示していないため、本計画で採用する値として決定する）。
    - `run` 関数（75〜107行目）を次の構造に変更する。
      1. `result, runErr := runner.Run(...)` の戻り値を、従来のように即座に分岐せず一旦両方とも保持する。
      2. `runErr == nil` の場合のみ、`fmt.Fprint(stdout, notify.Sanitize(report.FormatText(*result)))` を出力する（AC-17、既存の `report.FormatText(*result)` の呼び出しを `notify.Sanitize(...)` でラップする）。`runErr != nil` の場合は、従来通り `fmt.Fprintln(stderr, runErr.Error())` を出力する。この2箇所とも、既存の5箇所の `fmt.Fprint*` 呼び出し（80・92・98・102・112行目）と同じ `//nolint:gosec // stdout/stderr is a CLI stream, not an HTTP response body; G705's XSS concern does not apply` を付与する（`make lint` がこの新規箇所だけ異なる扱いにならないようにする）。
      3. `apply` が `true` の場合、`context.WithTimeout(context.Background(), notifyTimeout)` で `runner.Run` 用の `ctx` から独立した `notifyCtx` を作り（設計書 3.5節「通知用タイムアウトの独立性」）、`notify.Config{SuccessWebhookURL: cfg.SlackSuccessWebhookURL, FailureWebhookURL: cfg.SlackFailureWebhookURL}` を組み立て、`notify.Send(notifyCtx, notifyCfg, httpDoer, retry.RealClock{}, notify.Outcome{Result: result, Err: runErr})` を呼び出す。戻り値が非 `nil` の場合、`fmt.Fprintln(stderr, sendErr.Error())` に同じ `//nolint:gosec` コメント付きで出力する（AC-02・AC-03、終了コードには一切影響させない）。`apply` が `false` の場合は `Send` を呼ばない（AC-04）。
      4. 最後に終了コードを決定する: `runErr != nil` なら `exitSetupOrRunFail`、`len(result.Failed) > 0` なら `exitPartialFailure`、それ以外は `exitOK`。
    - `client, err := atproto.NewClient(...)` 以前の設定読み込み・クライアント初期化の失敗パス（既存の早期 `return exitSetupOrRunFail`）は変更しない（1.3節の通り、これらの失敗は `notify.Send` を呼ばない）。
  - **完了基準**: `go build ./...` が成功する。

- [x] **対象ファイル**: `cmd/main_test.go`（既存ファイルの変更・追加）
  - **作業内容**:
    - [x] `validConfigPath`（33行目）が書き込む TOML本文に `slack_allowed_host = "hooks.slack.com"` の行を追加する（1.3節の通り、これを行わないと `TestRun_ClientInitFailure_ReturnsExitCode1`・`TestRun_LoginFailure_ReturnsExitCode1`・`TestRun_DryRunWithTargets_ReturnsExitCode0AndPrintsTargets`・`TestRun_DryRunNoTargets_ReturnsExitCode0AndPrintsNoTargetsMessage`・`TestRun_ApplyAllSucceed_ReturnsExitCode0AndPrintsResult`・`TestRun_ApplyPartialFailure_ReturnsExitCode3AndPrintsFailures` の6件が Slack 通知と無関係な理由で失敗するようになる）。**実装時点で確認**: フェーズ2完了時の前倒し修正（フェーズ2の実装チェックリスト参照）により既に追加済みだった。
    - [x] `TestRun_ExecutionTimeoutExceeded_ReturnsExitCode1`（157行目）が独自に組み立てる TOML本文にも同様に `slack_allowed_host = "hooks.slack.com"` の行を追加する（1.3節、`validConfigPath` とは別に組み立てられているため個別の修正が必要）。**実装時点で確認**: こちらもフェーズ2の前倒し修正で既に追加済みだった。
    - [x] `TestRun_ApplyAllSucceed_ReturnsExitCode0AndPrintsResult`（既存）のモックハンドラ（`deleteRecordHandler`）に、`hooks.slack.com/services/success` 宛のPOSTリクエストに200を返す分岐を追加し、既存のアサーション（終了コード・stdout内容）が無変更のまま成功することを確認する（AC-05の結合確認を兼ねる）。新規の `slackWebhookHandler` ラッパーで実装した。
    - [x] `TestRun_ApplyPartialFailure_ReturnsExitCode3AndPrintsFailures`（既存）のモックハンドラに、`hooks.slack.com/services/failure` 宛のPOSTリクエストに200を返す分岐を追加し、既存のアサーションが無変更のまま成功することを確認する（AC-07の結合確認を兼ねる）。
    - [x] `TestRun_ApplyLoginFailure_SendsFailureNotification`（新規）: `apply=true` でログイン失敗（`com.atproto.server.createSession` が401を返す）を発生させ、`hooks.slack.com/services/failure` 宛のPOSTリクエストが1回発生すること、かつ終了コードが `exitSetupOrRunFail`（従来通り）のままであることを検証する（AC-06。既存の `TestRun_LoginFailure_ReturnsExitCode1` は `apply=false` であり、apply モードでの異常系ルーティングを検証する既存テストがないため新規に追加する）。
    - [x] `TestRun_Apply_SlackNotifyFails_ExitCodeUnaffected`（新規）: 削除は全件成功させつつ、`hooks.slack.com/services/success` 宛のPOSTに400（非429、再試行対象外のステータス）を返すモックを用意し、終了コードが `exitOK`（Slack通知の失敗に影響されない）のままであることを検証する（AC-02）。モック応答を非429の4xxにすることで、`retry.RealClock` による実際のバックオフ待機を発生させず高速に完了させる（1.3節の設計判断）。
    - [x] `TestRun_Apply_SlackNotifyFails_StderrContainsMaskedFailureMessage`（新規）: 上記と同じ400応答の設定で、stderr に通知失敗を示す文言が出力されること、かつ `hooks.slack.com/services/success` のパス（Webhook URLそのもの）が出力に含まれないことを検証する（AC-03, AC-19）。
    - [x] `TestRun_Apply_NoWebhookConfigured_SkipsNotifyWithoutError`（新規）: `BSKY_SLACK_WEBHOOK_URL_SUCCESS`/`BSKY_SLACK_WEBHOOK_URL_FAILURE` を両方とも空文字列に上書き（`slack_allowed_host` の検証も自動的にスキップされる、AC-21後段）した状態で `apply=true`・全件成功のシナリオを実行し、`hooks.slack.com` 宛のリクエストが一切発生せず（モックハンドラが該当ホストへのリクエストで `t.Fatalf` する作りのまま）、終了コードが `exitOK` のままであることを検証する（設計書 3.5節の回帰防止テスト）。
    - [x] `TestRun_ApplyPartialFailure_ConsoleOutputSanitizesMaliciousRKey`（新規）: 削除失敗する投稿の `rkey` に改行文字を含む値（例: `"evil\nFAKE LOG LINE"`）を設定し、`apply=true` で実行した際、stdout に生の改行を含む行注入が発生しないこと（`notify.Sanitize` でラップされた出力の改行数が期待通りであること）を検証する（AC-17）。**実装時の分岐**: `notify.Sanitize` は全ての C0 制御文字（=改行含む）を無条件に除去する実装（フェーズ3で確定済み）であるため、`report.FormatText` 自身が本来持つ改行（サマリ行・失敗行の区切り）も含めて全て除去される。したがって本テストの検証内容は「改行数が期待通り(2)」ではなく「生の改行が一切残らない(0件)」に補正し、代わりに内容（件数・rkey文字列）が引き続き含まれることを確認する形にした。
  - **完了基準**: `make test` で `cmd` パッケージの全テストが成功する。実行結果: 2026-07-06、`make fmt && make test && make lint && make deadcode` すべて成功。

### フェーズ7: ドキュメント更新（設計書 3.5節・8節7番目の項目）

- [x] **対象ファイル**: `docs/design/configuration.md`
  - **作業内容**:
    1. 20行目の `slack_allowed_host` 行から、末尾の「**未実装（計画中）**: ...」という一文を削除する（本タスクの実装完了によりこの注記は事実と矛盾するため）。
    2. 44〜46行目の「Slack Webhook URL のホスト部が TOML `slack_allowed_host` と一致することの検証は...」の直後にある「> **未実装（計画中）**: ...」の引用ブロック（46行目）を削除する。
    3. 31行目の `execution_timeout_seconds` 運用ガイダンスの段落の末尾に、次の趣旨の一文を追記する: Slack通知処理（[0006_slack_notification アーキテクチャ設計書](../tasks/0006_slack_notification/02_architecture.md#35-副作用契約dry-run--apply-とネットワーク送信) 3.5節）が、削除処理とは独立したタイムアウト予算（既定で約12秒: 単発HTTPタイムアウト3秒 × 最大3試行 + バックオフ1秒+2秒）を追加で消費するため、`execution_timeout_seconds` とスケジュール間隔の余裕を検討する際はこの追加分も考慮すること。
  - **完了基準**: `rg -n "未実装（計画中）" docs/design/configuration.md` の実行結果が0件であること。`rg -n "約12秒" docs/design/configuration.md` の実行結果が1件以上であること。

- [x] **対象ファイル**: `docs/dev/developer_guide/package_reference.md`
  - **作業内容**:
    1. 「Directory Structure」の `internal/` 一覧（17行目 `retry/` の次）に次の1行を追加する。
       ```
       - `notify/`: builds a Slack Incoming Webhook payload from a report.Result/error outcome, sanitizes/escapes externally-sourced identifiers and error text, and sends it via internal/retry with a URL-redacting retry log (see docs/tasks/0006_slack_notification)
       ```
    2. 「Package Responsibilities」の `internal/config` の説明文（28行目）の末尾に次の一文を追加する: ` \`LoadAppConfig\` also validates that any configured Slack webhook URL's host matches the TOML \`slack_allowed_host\` allowlist, failing closed if it is missing or does not match (see docs/tasks/0006_slack_notification/01_requirements.md).`
    3. 「Package Responsibilities」の `internal/retry` の説明文（36行目）の末尾に次の一文を追加する: ` \`WithURLRedactor\` lets a caller whose request URL itself carries a secret (e.g. a Slack Incoming Webhook token) override the URL text emitted by the retry/give-up log, without affecting internal/atproto's existing unredacted logging (see docs/tasks/0006_slack_notification/01_requirements.md).`
    4. 「Package Responsibilities」に新しい見出し「**Notification**」を追加し（`internal/report` の項の直後）、次の説明を記載する。
       ```
       **Notification**

       - `internal/notify`: builds a Slack Incoming Webhook payload (`text` field only) from a `report.Result`/error outcome, routing to a success or failure webhook URL based on whether the run errored or had partial delete failures (`Send`). Sanitizes control characters/newlines and escapes Slack mrkdwn mention syntax in externally-sourced identifiers and error text before including them, and never includes post body content. Delegates HTTP timeout/retry to `internal/retry`, redacting the webhook URL from its retry/give-up logging. A delivery failure never affects the CLI's own exit code (see docs/tasks/0006_slack_notification/01_requirements.md).
       ```
    5. `internal/report` の説明文（48行目）の「Reused as-is by a future Slack formatter (see docs/tasks/0006_slack_notification/01_requirements.md).」を「Reused as-is by \`internal/notify\`'s Slack payload construction (see docs/tasks/0006_slack_notification/01_requirements.md).」に変更する（「将来」という表現を、実装済みの事実を反映した表現に修正する）。
  - **完了基準**: 以下の `rg` コマンドがそれぞれ期待通りの結果になる。
    - `rg -n "internal/notify" docs/dev/developer_guide/package_reference.md` — 3件以上ヒットする（Directory Structure の1行 + Package Responsibilities「Notification」見出し以降の記述1行以上 + `internal/report` 項からの参照1行）。
    - `rg -n "Reused as-is by a future Slack formatter" docs/dev/developer_guide/package_reference.md` — 0件（書き換え前の文言が残っていないことの確認）。

### フェーズ8: 品質確認

- [ ] **対象コマンド**: `make fmt` / `make test` / `make lint`
  - **作業内容**: 3コマンドを順に実行し、いずれもエラーなく完了することを確認する（NF-001）。
  - **完了基準**: 3コマンドすべてが正常終了する。実行結果をここに記録する。
- [ ] **対象コマンド**: `make deadcode`
  - **作業内容**: `internal/notify` 新設・`internal/retry`/`internal/config`/`cmd/main.go` の変更に起因する新規の未使用コードが検出されないことを確認する。
  - **完了基準**: 本タスクに起因する新規の未使用コードが検出されない（既存の未解消項目がある場合はその理由を実行結果として明記する）。

## 3. 実装順序とマイルストーン

### 3.1 マイルストーン

| マイルストーン | 内容 | 成果物 |
|---|---|---|
| M1 | `internal/retry` の `WithURLRedactor` 拡張が完了し `make test` が緑になる | `internal/retry/doer.go`・`internal/retry/doer_test.go` |
| M2 | `internal/config` の許可ホスト検証（AC-11〜AC-13, AC-21）が完了し test で緑になる | `internal/config/config.go`・`validate.go`・`errors.go`・`app_config.go`・`app_config_test.go`・`config_test.go` |
| M3 | `internal/notify` パッケージ（型・サニタイズ・エラーカテゴリ化・ペイロード構築・`Send`）が完了し、AC-01・AC-05〜AC-10・AC-14〜AC-16・AC-18〜AC-20 が test で緑になる | `internal/notify/` 配下の全ファイル |
| M4 | `cmd/main.go` への統合が完了し、AC-02〜AC-04・AC-06・AC-07・AC-17 が結合テストで緑になり、既存テストが無退行であることを確認する | `cmd/main.go`・`cmd/main_test.go` |
| M5 | ドキュメント更新が完了し、フェーズ7の `rg` コマンドがすべて期待通りの結果になる | 更新済み `docs/design/configuration.md`・`package_reference.md` |
| M6 | `make fmt`/`make test`/`make lint`/`make deadcode` の完走を確認する | 実行結果の記録（実装チェックリスト） |

### 3.2 PR 構成

| PR | 対応フェーズ | 概要 |
|---|---|---|
| PR-1 | フェーズ1 / フェーズ2 | `internal/retry` のURL秘匿ログ拡張、`internal/config` の許可ホスト検証 |
| PR-2 | フェーズ3 / フェーズ4 / フェーズ5 | `internal/notify` パッケージの新設（型・サニタイズ・エラーカテゴリ化・ペイロード構築・`Send`） |
| PR-3 | フェーズ6 | `cmd/main.go` への統合、既存テストの回帰修正 |
| PR-4 | フェーズ7 | ドキュメント更新 |
| PR-5 | フェーズ8 | 品質確認完了の記録 |

### PR-1 作成ポイント: internal/retry URL redaction and internal/config allowlist validation

**対象ステップ**: フェーズ1 / フェーズ2

**推奨タイトル**: `feat(0006-slack-notification): add retry URL redaction and config allowlist host validation`

**レビュー観点**: `WithURLRedactor` 未指定時に `internal/atproto` の既存ログ出力が無変更であること / `validateSlackAllowedHost` の3ルール（両方未設定でスキップ・片方設定時の必須化・ホスト比較）が設計書 3.1節と一致していること / `internal/config/credentials_test.go` を変更しない判断（1.3節）が正しいこと

- [x] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [x] PR を作成した（https://github.com/isseis/bsky-cleaner/pull/49）
- [x] PR がマージされた
- [x] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

### PR-2 作成ポイント: internal/notify package

**対象ステップ**: フェーズ3 / フェーズ4 / フェーズ5

**推奨タイトル**: `feat(0006-slack-notification): add internal/notify package`

**レビュー観点**: `buildPayload` が投稿本文を一切参照していないこと（AC-14） / サニタイズ→mrkdwnエスケープの適用順序が識別子・エラー種別のすべてに一貫していること（AC-15, AC-16） / `SendError.Error()`・リトライログのいずれからもWebhook URLが漏洩しないこと（AC-19） / チャンネル振り分けの優先順位が設計書 3.4節の表と一致していること（AC-05〜AC-08） / `requestTimeout`・`defaultRetryPolicy` の値が設計書 3.6節の既定値と一致していること

- [x] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [x] PR を作成した（https://github.com/isseis/bsky-cleaner/pull/50、base: `issei/0006-slack-notification-04`）
- [x] PR がマージされた
- [x] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

### PR-3 作成ポイント: cmd/main.go integration

**対象ステップ**: フェーズ6

**推奨タイトル**: `feat(0006-slack-notification): wire Slack notification into cmd/main.go`

**レビュー観点**: dry-run時に `notify.Send` が一切呼ばれないこと（AC-04） / 通知の送信失敗が終了コードに影響しないこと（AC-02） / stdout の書き込みが `notify.Send` 呼び出しより先に行われること（設計書 3.5節の順序要件） / `notifyCtx` が `runner.Run` 用の `ctx` から独立していること（設計書 3.5節） / 既存の `cmd/main_test.go` のテスト（`validConfigPath`・`TestRun_ExecutionTimeoutExceeded_ReturnsExitCode1` 双方のTOMLフィクスチャ修正を含む）がすべて無退行で成功すること

- [x] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [x] PR を作成した（https://github.com/isseis/bsky-cleaner/pull/51）
- [x] PR がマージされた
- [x] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

### PR-4 作成ポイント: documentation updates

**対象ステップ**: フェーズ7

**推奨タイトル**: `docs(0006-slack-notification): update configuration reference and package reference`

**レビュー観点**: `docs/design/configuration.md` から「未実装（計画中）」の注記が過不足なく削除されていること / 追記した約12秒の説明が設計書 3.6節の数値計算と一致していること / `package_reference.md` の追記が既存の記述粒度・文体と一致していること

- [x] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [x] PR を作成した
- [x] PR がマージされた
- [x] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

### PR-5 作成ポイント: quality gates

**対象ステップ**: フェーズ8

**推奨タイトル**: `chore(0006-slack-notification): confirm quality gates`

**レビュー観点**: `make fmt`/`make test`/`make lint`/`make deadcode` の実行結果がすべて記録されていること

- [ ] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [ ] PR を作成した
- [ ] PR がマージされた

## 4. テスト戦略

### 4.1 単体テスト

- `internal/retry`: `WithURLRedactor` 指定時にリトライ・打ち切りログが redactor の戻り値を使うこと、未指定時は既存の `req.URL.String()` のままであることを検証する（フェーズ1）。
- `internal/config`: `slack_allowed_host` の一致・不一致（正常系/異常系それぞれ）・片方のみ設定・ポート/大文字小文字の正規化・未設定（両方未設定でスキップ/片方設定で必須）の各パターンを `app_config_test.go` で検証する（フェーズ2、AC-11〜AC-13, AC-21）。
- `internal/notify`: `Sanitize`・`escapeSlackMarkup`・`buildPayload`（識別子・エラー種別のサニタイズ順序、切り詰め）・`errorKind`（型付きエラーの分類、未知エラーのフォールバック）を、実際のネットワーク通信なしに検証する（フェーズ3・4、NF-002）。
- `internal/notify.Send`: `httptest.Server` を用いてHTTPタイムアウト・リトライ・チャンネル振り分け・`SendError` の内容を検証する。リトライを伴うテストは `fakeClock` を用いて実待機なしで完結させる（フェーズ5、NF-002）。

### 4.2 統合テスト

- `cmd/main.go`: `--apply` 時のみ通知が発生すること（AC-04、既存の dry-run テスト3件を回帰確認として再利用）、正常系/異常系（実行時エラー・部分失敗）のチャンネル振り分け（AC-05〜AC-07、既存の apply テスト2件の拡張 + ログイン失敗時の新規テスト1件）、通知失敗時に終了コードが変化しないこと（AC-02）と標準エラー出力にマスク済みの文言が出ること（AC-03）、標準出力のサニタイズ（AC-17）を、モックHTTPサーバ（`atprototestutil.MockHTTPDoer`）を用いて検証する（フェーズ6）。
- Webhook URL が未設定の場合に通知をスキップして正常終了することの回帰テストを1本追加する（対応するACはないが、設計書 3.5節が明示する挙動であるため）。

### 4.3 後方互換テスト

- `internal/atproto/client.go` の既存2箇所の `retry.NewDoer(...)` 呼び出しが `Option` 追加後も無変更でビルド・テストが通ることを確認する（フェーズ1、1.3節）。
- `internal/config/credentials_test.go` が本タスクの影響を受けず無変更のまま成功することを確認する（フェーズ2、1.3節の設計書記載の誤りの訂正）。
- `cmd/main_test.go` の既存テスト（`TestRun_ClientInitFailure_ReturnsExitCode1` 等7件）が、TOMLフィクスチャへの `slack_allowed_host` 追加後、既存のアサーション内容を変えずに成功することを確認する（フェーズ6、1.3節）。

### 4.4 テストヘルパ方針

[test_organization.md](../../dev/developer_guide/test_organization.md) の分類に従う。

- `internal/notify` 自身のテストが必要とする `retry.Clock` のフェイク実装（`fakeClock`）は、`retry.Clock`（公開インターフェース）のみを実装しパッケージ内部専用であるという性質が `internal/retry` 自身の `fakeClock` と同じであるため、その前例（`internal/retry/test_helpers.go`）に倣い `internal/notify/test_helpers.go`（Classification B、`//go:build test`）に置く（1.3節参照。利用テストファイルの数は Classification A/B の判断基準ではない）。
- `internal/notify` のHTTPモックには `net/http/httptest.Server` を用い、`internal/atproto/testutil` の `MockHTTPDoer` は使わない（`internal/notify` は `internal/atproto` の `HTTPDoer` 実装ではなく型定義のみに依存するため、`internal/atproto/testutil` を import する理由がない）。したがって `internal/notify/testutil/` サブディレクトリは新設しない。
- `cmd/main_test.go` は既存の `atprototestutil.MockHTTPDoer`・`hermeticHandler`・`listRecordsHandler`・`deleteRecordHandler` をそのまま再利用し、Slack Webhook 宛のリクエストに対する応答分岐をこれらの既存ハンドラに追加する形で対応する。新規の共有テストヘルパーファイルは追加しない。

## 5. リスク管理

| リスク | 影響 | 対策 |
|---|---|---|
| `cmd/main.go` の `run` 関数の制御フロー変更（早期 `return` から終了コード判定の一元化への書き換え）により、既存の設定読み込み・クライアント初期化失敗時の挙動が意図せず変化する | 既存テスト（`TestRun_ConfigLoadFailure_ReturnsExitCode1` 等）が退行する | フェーズ6の完了基準として、変更前に早期returnしていた2パスは無変更のまま維持し、`make test` で当該テストの継続成功を確認する |
| `validateSlackAllowedHost` の導入により、Slack通知を使わない既存の運用（`slack_allowed_host` 未設定・Webhook URL 未設定）が誤って起動失敗になる | 既存ユーザーの設定が壊れる（fail-closedの過剰適用） | フェーズ2の `TestLoadAppConfig_SlackAllowedHost_NotRequiredWhenBothWebhookURLsUnset_Succeeds` で「両方未設定なら検証しない」ことを明示的に確認する |
| `cmd/main_test.go` のTOMLフィクスチャ修正漏れ（`validConfigPath` と `TestRun_ExecutionTimeoutExceeded_ReturnsExitCode1` の2箇所のうち片方のみ修正） | 修正漏れした側のテストが Slack 通知と無関係な理由で失敗し続ける | フェーズ6のチェックリストで2箇所を明示的に別項目として列挙し、`make test` の全緑化をもって漏れがないことを確認する |
| `internal/notify.Send` の単発HTTPタイムアウトテスト（`TestSend_HTTPTimeout_ReturnsSendError`）が `requestTimeout` の書き換えを `t.Cleanup` で戻し忘れ、後続テストに影響する | パッケージ内の他のテストが不安定になる（テスト間の状態漏れ） | フェーズ5の当該タスクで `t.Cleanup` による復元を明記し、コードレビュー（PR-2）で確認する |
| リトライによるメッセージ重複（設計書 3.6節「許容するトレードオフ」）が運用上見逃される | オンコール担当者が同一実行結果の重複通知に混乱する | 本タスクのスコープでは対策しない設計判断（設計書 3.6節）を踏襲し、7節の成功基準にこの制約を明記する |

## 6. 実装チェックリスト

- [ ] PR-1 マージ済み（対象ステップ: フェーズ1 / フェーズ2。`internal/retry` のURL秘匿ログ拡張、`internal/config` の許可ホスト検証、AC-11〜AC-13・AC-21関連のテスト追加）
- [ ] PR-2 マージ済み（対象ステップ: フェーズ3 / フェーズ4 / フェーズ5。`internal/notify` パッケージ新設、AC-01・AC-05〜AC-10・AC-14〜AC-16・AC-18〜AC-20関連のテスト追加）
- [ ] PR-3 マージ済み（対象ステップ: フェーズ6。`cmd/main.go` への統合、AC-02〜AC-04・AC-06・AC-07・AC-17関連のテスト追加、既存テストの回帰修正）
- [ ] PR-4 マージ済み（対象ステップ: フェーズ7。`docs/design/configuration.md`・`package_reference.md` 更新）
- [ ] PR-5 マージ済み（対象ステップ: フェーズ8。品質確認完了の記録）
- [ ] `make fmt` / `make test` / `make lint` がすべて通過
- [ ] `make deadcode` で本タスクに起因する新規の未使用コードがないことを確認

## 7. 受け入れ基準の検証（Acceptance Criteria Verification）

| AC | 検証種別 |
|---|---|
| AC-01 | test |
| AC-02 | test |
| AC-03 | test |
| AC-04 | test（既存テストの再利用） |
| AC-05 | test |
| AC-06 | test |
| AC-07 | test |
| AC-08 | test |
| AC-09 | test |
| AC-10 | test |
| AC-11 | test |
| AC-12 | test |
| AC-13 | test |
| AC-14 | test |
| AC-15 | test |
| AC-16 | test |
| AC-17 | test |
| AC-18 | test |
| AC-19 | test |
| AC-20 | test |
| AC-21 | test |

**AC-01: 実行結果（成功/失敗、削除件数、失敗した投稿の識別子、エラー種別）を Slack Incoming Webhook 形式のペイロードとして送信できる**
- Test location: `internal/notify/payload_test.go::TestBuildPayload_SuccessOutcome_IncludesDeleteCountAndStatus`、`TestBuildPayload_RunError_IncludesErrorKind`、`TestBuildPayload_PartialFailure_IncludesFailedRKeysAndErrorKind`、`internal/notify/notify_test.go::TestSend_Success_PostsToSelectedWebhook`
- Implementation: `internal/notify/payload.go`（`buildPayload`）、`internal/notify/notify.go`（`Send`）
- Verification method: test（成功・実行時エラー・部分失敗の3パターンで、構築されたテキストに件数・識別子・エラー種別が含まれることと、実際にHTTP POSTされることをアサートする）

**AC-02: Webhook への送信が失敗した場合、CLI 本体の終了コードは削除処理自体の成否のみを反映し、通知の送信失敗によって変化しない**
- Test location: `cmd/main_test.go::TestRun_Apply_SlackNotifyFails_ExitCodeUnaffected`
- Implementation: `cmd/main.go`（`run` の終了コード判定が `notify.Send` の戻り値を参照しないこと）
- Verification method: test（削除全件成功・Slack通知400失敗の組み合わせで、終了コードが `exitOK` のままであることをアサートする）

**AC-03: 通知の送信に失敗した場合、その事実は標準エラー出力に出力される。出力時に Webhook URL はマスクする**
- Test location: `cmd/main_test.go::TestRun_Apply_SlackNotifyFails_StderrContainsMaskedFailureMessage`
- Implementation: `cmd/main.go`（`run` の `sendErr.Error()` 出力）、`internal/notify/notify.go`（`SendError.Error()`）
- Verification method: test（stderrに失敗を示す文言が含まれ、Webhook URLのパス文字列が含まれないことをアサートする）

**AC-04: `--apply` 実行時のみ Slack 通知を送信する。デフォルト（dry-run）実行では送信しない**
- Test location: `cmd/main_test.go::TestRun_DryRunWithTargets_ReturnsExitCode0AndPrintsTargets`、`TestRun_DryRunNoTargets_ReturnsExitCode0AndPrintsNoTargetsMessage`、`TestRun_LoginFailure_ReturnsExitCode1`（いずれも既存、フィクスチャ修正のみで回帰確認として機能する。1.3節参照）
- Implementation: `cmd/main.go`（`run` の `apply` 分岐）
- Verification method: test（既存のモックハンドラが未知のリクエスト（Slackへの誤POSTを含む）に `t.Fatalf` する作りになっているため、これらのテストの継続成功自体がAC-04の検証となる）

**AC-05: 実行が正常終了した場合、正常系用の Webhook URL に通知が送信される**
- Test location: `internal/notify/notify_test.go::TestSend_ChannelRouting_AllSucceeded_UsesSuccessURL`、`cmd/main_test.go::TestRun_ApplyAllSucceed_ReturnsExitCode0AndPrintsResult`（既存、モックハンドラ拡張）
- Implementation: `internal/notify/notify.go`（`Send` のチャンネル選択ロジック）
- Verification method: test（`internal/notify` レベルでの単体テストと、`cmd/main.go` 経由の結合テストの両方で、正常系URLへのPOSTを確認する）

**AC-06: 実行がエラー終了した場合、異常系用の Webhook URL に通知が送信される**
- Test location: `internal/notify/notify_test.go::TestSend_ChannelRouting_RunError_UsesFailureURL`、`cmd/main_test.go::TestRun_ApplyLoginFailure_SendsFailureNotification`（新規）
- Implementation: `internal/notify/notify.go`（`Send` のチャンネル選択ロジック）
- Verification method: test（`Outcome.Err != nil` のケースを単体・結合の両方で検証する。既存の `TestRun_LoginFailure_ReturnsExitCode1` は `apply=false` のため、apply モードでのルーティングを検証する新規結合テストを追加する）

**AC-07: 削除対象の一部のみ失敗した場合（部分失敗）は異常系として扱い、異常系用の Webhook URL に送信する**
- Test location: `internal/notify/notify_test.go::TestSend_ChannelRouting_PartialFailure_UsesFailureURL`、`cmd/main_test.go::TestRun_ApplyPartialFailure_ReturnsExitCode3AndPrintsFailures`（既存、モックハンドラ拡張）
- Implementation: `internal/notify/notify.go`（`Send` のチャンネル選択ロジック、`len(Result.Failed) > 0` の判定）
- Verification method: test

**AC-08: 正常系・異常系に同一の Webhook URL を設定した運用でも、正しく動作する**
- Test location: `internal/notify/notify_test.go::TestSend_SameWebhookURLForBothChannels_RoutesCorrectlyInBothOutcomes`
- Implementation: `internal/notify/notify.go`（`Config` が両フィールドに同一値を保持するだけで特別な分岐を要しないこと）
- Verification method: test

**AC-09: Slack Webhook への HTTP POST には HTTP タイムアウトを設ける**
- Test location: `internal/notify/notify_test.go::TestSend_HTTPTimeout_ReturnsSendError`
- Implementation: `internal/notify/notify.go`（`requestTimeout` を用いた `context.WithTimeout`）
- Verification method: test（`requestTimeout` をテスト用に短縮し、意図的にブロックするハンドラに対してタイムアウトエラーが返ることをアサートする）

**AC-10: 通知処理全体（リトライ含む）の最悪ケース所要時間は有界であり、実行タイムアウトより十分短い**
- Test location: `internal/notify/notify_test.go::TestSend_MaxRetriesExceeded_ReturnsSendError_BoundedAttempts`、`internal/notify/notify_test.go::TestNotifyWorstCaseTime_BoundedBelowExecutionTimeoutGuidance`
- Implementation: `internal/notify/notify.go`（`requestTimeout`・`defaultRetryPolicy`、設計書 3.6節の既定値）
- Verification method: test（前者はリクエスト回数が `MaxRetries + 1` 回で頭打ちになる=有界であることをアサートし、後者は `requestTimeout`・`defaultRetryPolicy` の実際の値から計算した最悪ケース所要時間が設計書 3.6節の約12秒と一致し、かつ実行タイムアウトの推奨値より十分小さいことを定数の算術チェックとしてアサートする。両者の組み合わせでAC-10の「有界」と「十分短い」の両方を自動テストで検証する）

**AC-11: `slack_allowed_host` が設定されており、設定されている Webhook URL すべてのホスト部が一致する場合、設定は正常に読み込まれる**
- Test location: `internal/config/app_config_test.go::TestLoadAppConfig_SlackAllowedHost_BothURLsMatchAllowedHost_Succeeds`、`TestLoadAppConfig_SlackAllowedHost_OnlySuccessURLSet_MatchesAllowedHost_Succeeds`
- Implementation: `internal/config/validate.go`（`validateSlackAllowedHost`）
- Verification method: test

**AC-12: いずれかの Webhook URL のホスト部が `slack_allowed_host` と一致しない場合、fail-closed で起動が失敗する**
- Test location: `internal/config/app_config_test.go::TestLoadAppConfig_SlackAllowedHost_SuccessURLHostMismatch_ReturnsError`、`TestLoadAppConfig_SlackAllowedHost_FailureURLHostMismatch_ReturnsError`
- Implementation: `internal/config/validate.go`（`validateSlackAllowedHost`）、`internal/config/errors.go`（`ErrWebhookHostMismatch`）
- Verification method: test

**AC-13: ホスト部の照合はポート番号を除いた形で行い、大文字/小文字を区別しない完全一致とする**
- Test location: `internal/config/app_config_test.go::TestLoadAppConfig_SlackAllowedHost_PortAndCaseIgnoredInComparison_Succeeds`
- Implementation: `internal/config/validate.go`（`validateSlackAllowedHost` の `url.URL.Hostname()` + `strings.EqualFold` による比較）
- Verification method: test

**AC-14: 通知ペイロードに投稿本文を含めない。含めるのは削除件数・成否・識別子・エラー種別に限る**
- Test location: `internal/notify/payload_test.go::TestBuildPayload_ExcludesPostBody_OnlyIncludesStructuredFields`
- Implementation: `internal/notify/payload.go`（`buildPayload`）
- Verification method: test（1.3節の通り `atproto.Post` に本文フィールドが存在しないため構造的に保証されるが、意図せぬフィールド追加がないことを確認する回帰テストとして機能する）

**AC-15: 通知に含める外部由来の文字列は、Slackのメンション記法が展開されない形式で送信される**
- Test location: `internal/notify/payload_test.go::TestEscapeSlackMarkup_EscapesAmpersandLtGt`、`TestBuildPayload_EscapesMentionSyntaxInFailedRKey`
- Implementation: `internal/notify/payload.go`（`escapeSlackMarkup`）
- Verification method: test

**AC-16: 通知に含める外部由来の文字列は、ANSI エスケープシーケンスが除去またはエスケープされる**
- Test location: `internal/notify/sanitize_test.go::TestSanitize_RemovesANSIEscapeSequence`、`internal/notify/payload_test.go::TestBuildPayload_SanitizesANSIEscapeInFailedRKey`
- Implementation: `internal/notify/sanitize.go`（`Sanitize`）
- Verification method: test

**AC-17: コンソール出力についても、外部由来の文字列にログ偽装が起きないよう制御文字が除去またはエスケープされる。サニタイズ処理は一箇所に集約される**
- Test location: `cmd/main_test.go::TestRun_ApplyPartialFailure_ConsoleOutputSanitizesMaliciousRKey`
- Implementation: `internal/notify/sanitize.go`（`Sanitize`）、`cmd/main.go`（`notify.Sanitize(report.FormatText(*result))` によるラップ）
- Verification method: test（`internal/notify.Sanitize` が `internal/notify` 自身のペイロード構築（AC-16）と `cmd/main.go` の標準出力（AC-17）の両方から呼ばれる同一実装であることは、`internal/notify/sanitize.go` が単一の公開関数であることによって構造的に保証される）

**AC-18: 通知ペイロードが上限長を超える場合、末尾を切り詰め、切り詰められたことが分かるマーカーを付与する**
- Test location: `internal/notify/payload_test.go::TestBuildPayload_TruncatesWhenExceedsLimit_AppendsTruncatedMarker`
- Implementation: `internal/notify/payload.go`（`buildPayload` の切り詰めロジック、`maxPayloadLength`）
- Verification method: test

**AC-19: 通知ペイロードおよび stderr 出力の構築過程で、app パスワード・セッション JWT・`Authorization` ヘッダー・Slack Webhook URL の値が含まれない**
- Test location: `internal/notify/notify_test.go::TestSendError_Error_NeverContainsWebhookURL`、`TestSend_RetryLog_UsesRedactedURL_NotRawWebhookURL`、`internal/notify/errorkind_test.go::TestErrorKind_NeverIncludesRawErrorStringOrSecrets`、`cmd/main_test.go::TestRun_Apply_SlackNotifyFails_StderrContainsMaskedFailureMessage`
- Implementation: `internal/notify/notify.go`（`SendError.Error()`）、`internal/retry/doer.go`（`WithURLRedactor` 経由のログ）、`internal/notify/errorkind.go`（`errorKind` の明示的フィールド選択）
- Verification method: test（`SendError.Error()`・リトライログ・`errorKind` の未知エラーフォールバックの3経路それぞれで秘匿情報が含まれないことを個別にアサートする）

**AC-20: エラーオブジェクトをそのまま文字列化せず、通知に含めてよいフィールドを明示的に選択する形で構築される**
- Test location: `internal/notify/errorkind_test.go::TestErrorKind_UnknownErrorType_ReturnsUnknownErrorFallback`、`TestErrorKind_NeverIncludesRawErrorStringOrSecrets`
- Implementation: `internal/notify/errorkind.go`（`errorKind`）
- Verification method: test

**AC-21: Webhook URL が設定されているにもかかわらず `slack_allowed_host` が未設定の場合、fail-closed で起動が失敗する。両方とも未設定の場合はエラーにしない**
- Test location: `internal/config/app_config_test.go::TestLoadAppConfig_SlackAllowedHost_MissingWhileWebhookURLSet_ReturnsError`、`TestLoadAppConfig_SlackAllowedHost_NotRequiredWhenBothWebhookURLsUnset_Succeeds`
- Implementation: `internal/config/validate.go`（`validateSlackAllowedHost`）、`internal/config/errors.go`（`ErrSlackAllowedHostMissing`）
- Verification method: test

## 8. 成功基準

- **機能的完全性**: AC-01〜AC-21 がすべて7節のテストで検証され成功する。
- **品質指標**: `make fmt` / `make test` / `make lint` / `make deadcode` がすべてエラーなく完了する（NF-001）。
- **時間のモック化（NF-002）**: `internal/retry`・`internal/notify` のリトライを伴うテストがすべて `fakeClock` を用いて実際の待機なしに完結すること。実待機を伴うテストは `internal/notify/notify_test.go::TestSend_HTTPTimeout_ReturnsSendError`（短縮した `requestTimeout` を使うため数十ミリ秒程度）のみであることを確認する。
- **セキュリティ検証（NF-003）**: 設計書 5節が挙げるリスク（メンション拡散・ANSIエスケープ注入・ログ偽装・Webhook URL漏洩・エラーオブジェクトの丸ごとシリアライズ）それぞれに対応するテスト（7節のAC-15・AC-16・AC-17・AC-19・AC-20の各テスト）が緑であることをもって検証する。
- **既知の残存リスク**: 設計書 3.6節「リトライによるメッセージ重複の可能性」および 5.2節「通知失敗の可観測性（0007のスコープ依存）」は、本タスクが設計判断として受け入れたトレードオフであり、本タスクの完了基準には含めない。
- **ドキュメント整備**: `docs/design/configuration.md`・`docs/dev/developer_guide/package_reference.md` に、`slack_allowed_host`・`internal/notify`・通知処理の最悪ケース時間が実装済みの内容として反映されている（フェーズ7、7節参照ではなくフェーズ7の完了基準の `rg` コマンドで確認）。

## 9. 次のステップ

- 設計書 9節の通り、メッセージフォーマット（色・絵文字・Run ID・Block Kit化）は将来必要になった場合、`internal/notify/payload.go` の `buildPayload` 内部実装のみを変更すればよく、`Send`/`Config`/`Outcome` の型は変更不要である。
- 設計書 9節の通り、現行の `slack_allowed_host` は単一ホストのみを許可する。複数ホストの許可が必要になった場合は [0008_security_hardening](../0008_security_hardening/) で対応する想定。
- 設計書 5.2節の通り、AC-03のstderr出力が実際に永続化されるかどうかは [0007_docker_distribution](../0007_docker_distribution/) のcron/Dockerログ収集構成に依存する。本タスクはstderrへの出力までを担い、その先の収集・永続化が実際に構成されていることの確認は0007側のスコープとする。
