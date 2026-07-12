# ラップされた永続エラーのリトライ抑止 — 実装計画書

## Document Status

| Item | Value |
|---|---|
| Status | `approved` |
| Created | 2026-07-12 |
| Review date | 2026-07-12 |
| Reviewer | isseis |
| Comments | - |

## 1. 実装概要

### 1.1 目的

`internal/retry` の永続エラー判定を、エラーチェーンの**トップレベル**のみを検査する素の型アサーションから、**チェーン全体を走査**する方式（`errors.As` にインターフェース型 `permanentError` をターゲットとして渡す）へ変更する。これにより、`*url.Error` 等でラップされた `*atproto.SSRFError` が無駄なリトライ予算を消費せず、初回試行で永続と判定されて即座に呼び出し元へ伝播する。設計の詳細は [02_architecture.md](02_architecture.md) を参照。

> **設計書との差異（要 architecture 修正）**: [02_architecture.md](02_architecture.md) §3.1 等は走査手段を `errors.AsType[permanentError]` と記述しているが、これはコンパイルできない。`errors.AsType[E error]` の型パラメータ `E` は `error` 制約を持ち、`permanentError`（`Permanent() bool` のみで `Error()` を持たない）は `error` を満たさないため `permanentError does not satisfy error (missing method Error)` となる（Go 1.26.2 で確認）。非ジェネリックな `errors.As` はインターフェース型（`error` を埋め込まないものを含む）をターゲットにでき、チェーンを走査して代入可能な要素を検出できる。したがって本実装は `errors.As` を用いる。`permanentError` に `error` を埋め込む案は要件 2章 Out of Scope・NF-004 に反するため採らない。設計書側の当該記述（§2.1 図・§3.1・§3.4・付録B）は本タスク完了時に `errors.As` へ訂正し再承認する必要がある（§9 申し送り事項）。

### 1.2 実装方針

- **最小差分（YAGNI）**: 変更対象は `internal/retry/doer.go` の `classify` 内の永続判定1箇所と、それを説明する既存コメント3箇所、および `internal/retry/doer_test.go` へのテスト追加のみ。インターフェース契約・リトライ方針・他パッケージには手を入れない（要件 NF-004、[02_architecture.md](02_architecture.md) §1.1）。
- **既存挙動の非回帰**: トップレベル永続エラー（AC-01）、非永続エラーのリトライ（AC-03）、`nil` エラー時のレスポンス分類（AC-04）は変更しない。
- **Go ソースは英語**: 追加・変更するコメント、識別子、文字列リテラルはすべて英語で記述する（本計画書の散文は日本語）。

### 1.3 既存コード調査結果

| 対象 | 現状 | 不足・変更点 |
|---|---|---|
| `internal/retry/doer.go` の `classify`（154行目） | `if permErr, ok := doErr.(permanentError); ok && permErr.Permanent()` というトップレベル型アサーションで永続判定している。 | `errors.As(doErr, &permErr)`（`var permErr permanentError`）によるチェーン走査へ変更する。`errors` は既にインポート済み（5行目）のため追加インポート不要。 |
| `permanentError` インターフェースのコメント（32〜36行目） | 「Doer checks for this via a **plain type assertion**」と記述。 | 「チェーン全体を走査して判定する」旨へ更新（AC-06）。構造的型付けの利点を述べた末尾の一文（呼び出し元がインポート不要でオプトアウトできる）はそのまま残す。 |
| `Doer` 型のコメント（53〜56行目） | 「never retrying an error satisfying the unexported permanentError interface」と記述。 | ラップされていてもチェーン内に永続エラーがあればリトライしない旨へ整合（AC-06）。 |
| `classify` のコメント（147〜151行目） | 「never retried permanent errors」と記述。 | 同上、チェーン走査で永続エラーを検出する旨へ整合（AC-06）。 |
| `internal/retry/doer_test.go` の `permanentTestError`（231〜234行目） | `Permanent() bool` を実装する既存のテスト専用エラー型。 | 再利用する。ラップして返すケースを追加（AC-02）。 |
| `internal/retry/doer_test.go` の `TestDoer_Do_PermanentFailures_NotRetried`（236行目） | `permanent_error` ケースでトップレベル永続エラーの初回中断を検証済み（AC-01）。 | 変更不要。変更後も緑を維持する（回帰確認）。 |
| `internal/retry/doer_test.go` の `TestDoer_Do_TransientFailures_RetriesThenSucceeds`（118行目） | `transport_error` ケースで非永続失敗のリトライを検証済み（AC-03）。 | 変更不要。永続判定の誤検知がないことを担保。 |
| `internal/retry/doer_test.go` のステータス別テスト群 | 401・403（`TestDoer_Do_PermanentFailures_NotRetried`）、429・5xx（`TestDoer_Do_TransientFailures_RetriesThenSucceeds` ほか）で `nil` エラー時の分類を検証済み（AC-04）。 | 変更不要。 |
| `internal/retry/test_helpers_test.go` の `fakeClock` | `Sleep` 呼び出しを `SleepCalls` に記録する既存ヘルパー。 | 再利用する。AC-02 で `SleepCalls` が空であることを assert する。 |
| `internal/atproto/http_test.go`（127行目付近） | 素の `*url.Error` でラップされた `*SSRFError` を `errors.AsType` が透過検出できることを既に担保（ただし `retry.Doer` を経由しない）。 | 変更不要。AC-02 は別途 `retry.Doer` 経由で検証する。 |

新規パッケージ・新規ファイル・新規テストヘルパーの追加は不要。

## 2. 実装ステップ

小さな単一変更のためフェーズ分割は行わず、以下の順で実施する（[02_architecture.md](02_architecture.md) §8 の優先順位に対応）。

### ステップ1: 永続判定のチェーン走査化（F-001 / AC-01・AC-02・AC-03・AC-05）

**対象ファイル**: `internal/retry/doer.go`

- [x] `classify` の `doErr != nil` 分岐（現154行目）の永続判定を、トップレベル型アサーション `doErr.(permanentError)` からチェーン走査へ変更する。具体的には `var permErr permanentError` を宣言し `if errors.As(doErr, &permErr) && permErr.Permanent()` で判定する（`errors.As` はポインタ経由でインターフェース型ターゲットを受け付け、チェーンを走査して最初に代入可能な要素を検出する）。判定が真のときは従来どおり `return false, 0, nil, doErr`（受け取ったエラーをそのまま返す）とし、戻り値の形（`(nil, err)`）は変更しない。

**完了基準**: `make test` で AC-01〜AC-05 の関連テストが緑。トップレベル永続・ラップ永続の双方が初回試行のみで中断し、非永続はリトライされる。

### ステップ2: コメント整合（AC-06）

**対象ファイル**: `internal/retry/doer.go`

- [x] `permanentError` インターフェースのコメント（32〜36行目）の「plain type assertion」の記述を、チェーン全体を走査して判定する旨の英語表現へ更新する。構造的型付けの利点（呼び出し元がこのパッケージの型をインポートせずにオプトアウトできる）を述べた末尾の一文はそのまま残す。
- [x] `Doer` 型のコメント（53〜56行目）の永続エラー判定の説明を、ラップされていてもチェーン内に永続エラーがあればリトライしない旨へ英語で整合させる。
- [x] `classify` のコメント（147〜151行目）の永続エラー判定の説明を、チェーン走査で永続エラーを検出する旨へ英語で整合させる。

**完了基準**: `doer.go` 内に「plain type assertion」等のトップレベル判定を示す記述が残っていない（後述 §7 の静的検証で確認）。

### ステップ3: ラップされた永続エラーのテスト追加（AC-02 / AC-05）

**対象ファイル**: `internal/retry/doer_test.go`

- [x] ラップされた永続エラーが初回試行のみで中断することを検証する新規テスト関数 `TestDoer_Do_WrappedPermanentError_NotRetried` を追加する。既存の `permanentTestError`（231行目）・`fakeClock`・`mockDoerFunc` を再利用する。以下2ケースをテーブル駆動で検証する。
  - `fmt.Errorf("...: %w", &permanentTestError{...})` で1段ラップしたケース（単一の `Unwrap() error` を持つラップの代表例）。
  - 実運用の発生源に合わせた `&url.Error{Op: "Get", URL: "...", Err: &permanentTestError{...}}` でラップしたケース（`retry.Doer` 経由の検出を直接検証）。
- [x] 各ケースで次を assert する。
  - `inner.Do` の呼び出し回数 == 1（初回試行のみ）。
  - `fakeClock.SleepCalls` が空（バックオフ待機が発生しない、AC-02）。
  - 戻り値が `(nil, err)` すなわち `resp == nil` かつ `err != nil`（戻り値契約、AC-05）。
  - `errors.AsType[*permanentTestError](err)` が真、または `errors.Is` で元の永続エラーがチェーン内に保持されている（エラーがそのまま返る）。

**完了基準**: 追加テストが緑。`url.Error` を用いるため `net/url` のインポートを追加する。

### ステップ4: ビルドチェック（NF-001・NF-005）

- [x] `make fmt` を実行しフォーマットを整える。
- [x] `make test` を実行し全テストが緑であることを確認する。
- [x] `make lint` を実行し警告がないことを確認する。

## 3. 実装順序とマイルストーン

単一の小変更のため単一マイルストーンとする。

- **M1（本タスクの完了）**: ステップ1〜4 完了。ラップされた `*atproto.SSRFError` が `retry.Doer` によって初回試行で永続と判定され、バックオフ待機なしに呼び出し元へ伝播する。`make test && make lint` が緑。

## 4. テスト戦略

詳細は [02_architecture.md](02_architecture.md) §7 を参照。本計画で新規に追加するのは以下の1テスト関数のみで、他は既存テストの緑維持（回帰確認）で担保する。

- **新規追加（`internal/retry/doer_test.go`）**: `TestDoer_Do_WrappedPermanentError_NotRetried`（ステップ3）— AC-02・AC-05 を検証。
- **既存で担保（変更なし）**:
  - AC-01: `TestDoer_Do_PermanentFailures_NotRetried` の `permanent_error` ケース。
  - AC-03: `TestDoer_Do_TransientFailures_RetriesThenSucceeds` の `transport_error` ケース。
  - AC-04: 上記および `TestDoer_Do_PermanentFailures_NotRetried` の 401/403 ケース、429/5xx を扱う各テスト。
- **統合テスト・セキュリティテスト**: 本変更は `internal/retry` 単体に閉じるため新規追加なし（[02_architecture.md](02_architecture.md) §7.2・§7.3）。`internal/atproto` 側の既存 SSRF テストは現状のまま緑を維持する。

境界値・エラー経路の考慮: 永続判定は「チェーン内に永続エラーが有る／無い」の二分岐であり、有る側（AC-01 トップレベル・AC-02 ラップ）と無い側（AC-03）の双方をテストで網羅する。ラップ段数は「1段以上」を代表して1段で検証する（`errors.As` の走査は段数に依存しないため）。

## 5. リスク管理

| リスク | 内容 | 緩和策 |
|---|---|---|
| 誤検知（本来リトライ可能な失敗を永続と誤判定） | チェーン走査は内側にラップされた永続エラーも検出するため、リトライ可能な失敗が永続エラーをラップ／`Join` していると全体が永続扱いになる。 | 現状 `permanentError` 実装は `*atproto.SSRFError`（`Permanent()` 常に `true`）のみで、リトライ可能な失敗が永続エラーをラップ／`Join` する経路は存在しない（[02_architecture.md](02_architecture.md) §3.1 注記）。AC-03 のテストで非永続失敗が誤検知されないことを担保。 |
| `errors.As` のインターフェース対象利用 | 既存の `errors.AsType` 利用は具体型（`*SSRFError` 等、いずれも `error` を満たす）が対象で、インターフェース型 `permanentError` を対象にする用法は本コードベース初。加えて `permanentError` は `Error()` を持たないため `errors.AsType[permanentError]` はコンパイルできない（§1.1 の差異注記）。 | 非ジェネリックな `errors.As(doErr, &permErr)` を用いる。`errors.As` は `error` を埋め込まないインターフェース型でもポインタ経由でターゲットにでき、チェーンを走査する。`fmt.Errorf` ラップ・`*url.Error` ラップの双方で走査・検出が成立することを、本実装とは独立に事前検証済み。AC-01・AC-02 のテストで実挙動を最終確認する。 |

スケジュールリスク: 単一ファイルの小変更のため特筆事項なし。

## 6. 実装チェックリスト

- [x] ステップ1: `classify` の永続判定をチェーン走査へ変更（`internal/retry/doer.go`）
- [x] ステップ2a: `permanentError` インターフェースのコメント整合
- [x] ステップ2b: `Doer` 型のコメント整合
- [x] ステップ2c: `classify` のコメント整合
- [x] ステップ3: `TestDoer_Do_WrappedPermanentError_NotRetried` を追加（`internal/retry/doer_test.go`）
- [x] ステップ4: `make fmt` → `make test` → `make lint` が緑

## 7. 受け入れ基準の検証

| AC | 内容 | 種別 | 検証タスク・場所 |
|---|---|---|---|
| AC-01 | トップレベル永続エラーが従来どおり初回試行のみで中断 | test | `internal/retry/doer_test.go::TestDoer_Do_PermanentFailures_NotRetried`（`permanent_error` ケース、既存・回帰確認） |
| AC-02 | ラップされた永続エラーが初回試行のみで中断し `Clock.Sleep` が発生しない | test | `internal/retry/doer_test.go::TestDoer_Do_WrappedPermanentError_NotRetried`（新規、`inner.Do` 呼び出し回数 == 1 かつ `SleepCalls` が空を assert） |
| AC-03 | 非永続失敗が従来どおりリトライされる（誤検知なし） | test | `internal/retry/doer_test.go::TestDoer_Do_TransientFailures_RetriesThenSucceeds`（`transport_error` ケース、既存） |
| AC-04 | `nil` エラー時のレスポンス分類（2xx・401・非429 4xx・429・5xx）が不変 | test | `internal/retry/doer_test.go::TestDoer_Do_PermanentFailures_NotRetried`（401・403）、`TestDoer_Do_TransientFailures_RetriesThenSucceeds`（429・5xx）、`TestDoer_Do_SuccessOnFirstAttempt_NoRetry`（2xx）（いずれも既存） |
| AC-05 | `Do` がレスポンスとエラーを同時に非 `nil` で返さない | test | `internal/retry/doer_test.go::TestDoer_Do_WrappedPermanentError_NotRetried`（永続判定経路で `resp == nil` かつ `err != nil` を assert） |
| AC-06 | 判定機構を説明する `doer.go` のコメントがチェーン走査の実態と整合 | static | (1) `rg -n "plain type assertion" internal/retry/doer.go` の結果が0件（トップレベル判定を示す旧記述が残っていないこと）。(2) `rg -ni "error chain\|chain\|Unwrap" internal/retry/doer.go` が1件以上（`permanentError`／`Doer`／`classify` のいずれのコメントもチェーン走査を述べる新記述を含むこと。想定表現例: "checks the whole error chain"）。加えて3箇所すべての更新を PR 差分で目視確認（`manual` 補足）。 |

## 8. 成功基準

- AC-01〜AC-06 が test/static で緑。
- `make test && make lint` が緑（NF-001）。Go 1.26.2 でビルド可能（NF-005）。
- `internal/retry` が標準ライブラリのみに依存する状態を維持（NF-002・NF-003）— 新規外部依存を追加しない。
- 変更は永続判定の走査範囲拡大とそれに伴うコメント整合、および AC-02 のテスト追加に限定され、無関係なリファクタリングを含まない（NF-004）。

## 9. 次のステップ（本タスクの受け入れ基準外）

- **0016 設計書（`02_architecture.md`）の API 記述訂正**: §2.1 図・§3.1・§3.4・付録B が走査手段を `errors.AsType[permanentError]` と記述しているが、これはコンパイルできない（§1.1 の差異注記）。当該記述を `errors.As`（インターフェース型ターゲット）へ訂正し、再承認を受ける必要がある。実装は本計画に従い `errors.As` で行う。
- **0015 設計書の追随更新**: 本タスク完了後、[0015_security_review_fixes](../0015_security_review_fixes/02_architecture.md) §3.2・§6.1・付録B の「リダイレクト拒否は `MaxRetries` 回リトライされてから浮上する」旨の記述を「初回試行で永続と判定され即座に中断する」に更新する（要件 6章・[02_architecture.md](02_architecture.md) §8 申し送り事項）。本タスクのコード受け入れ基準には含めない。

## 付録: クロス検索チェックリスト

`make lint`・`make test` では検出できない、削除・変更した記述の残存確認。

- [x] `rg -n "plain type assertion" internal/retry/` — 0件（旧コメント記述が残っていないこと。AC-06 と同一のため §7 の検証で兼ねる）。
- [x] `rg -n "doErr\.\(permanentError\)" internal/retry/` — 0件（旧トップレベル型アサーションが残っていないこと）。
