# リトライ・実行タイムアウト — 実装計画書

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

`internal/retry` パッケージを新設し、`internal/atproto.HTTPDoer` を透過的にラップする指数バックオフ付きリトライ機構（F-001）を実装する。実行タイムアウト（F-002）・タイムアウトと削除処理の境界制御（F-003）は [アーキテクチャ設計書](02_architecture.md) 1.1節・3.4節・3.5節が確認する通り既存実装（[0004_cli_entrypoint](../0004_cli_entrypoint/01_requirements.md)・[0001_config](../0001_config/01_requirements.md)）で充足済みであり、本タスクはこれらに新しい型・フィールドを追加しない。本タスクが新たに満たすべきなのは「リトライのバックオフ待機中も deadline 到達を遅延なく検知すること」（設計書 3.5節）のみである。

### 1.2 実装方針

[アーキテクチャ設計書](02_architecture.md) 8節の実装優先順位（フェーズ1: `internal/retry` パッケージ → フェーズ2: `internal/atproto` への組み込み → フェーズ3: ドキュメント更新）にそのまま従う。本計画書ではこれに加えて、[0004_cli_entrypoint](../0004_cli_entrypoint/03_implementation_plan.md) と同じ構成で、品質確認をフェーズ4として独立させる。

### 1.3 既存コード調査結果

- `internal/retry` パッケージは存在しない（`find internal -maxdepth 1 -type d` で確認済み。既存は `internal/atproto`・`internal/cleanup`・`internal/config`・`internal/report`・`internal/runner` の5つ）。本タスクはこのパッケージを新規作成する。
- `internal/atproto/client.go` の `NewClient` は現在、呼び出し元から渡された `httpDoer` をそのまま `resolveHandleToDID`/`resolveDIDDocument` に渡しており、リトライ・ホスト安全性再検証のいずれのラップも経ていない（`internal/atproto/client.go:46-51`）。PDS 検証成功後の `httpDoer` の組み立ては `newPDSDoer`（パッケージ変数、デフォルトは `newRestrictedDoer(verifiedAddrs, host)` を返すのみ、`internal/atproto/client.go:37-39`）が担っており、ここにもリトライのラップは存在しない。
- `internal/atproto/did.go` の `resolveHandleToDID`・`resolveDIDDocument` は、それぞれリクエスト組み立て前に一度だけ `checkRequestHostSafety(ctx, reqURL)` を呼び出している（`did.go:47-49`・`did.go:104-106`）。設計書 2.3.1節はこの呼び出し位置を「リクエスト組み立て前に1回」から「`HTTPDoer.Do` が呼ばれるたびに1回」へ移すことを求めており、`newHostSafetyCheckedDoer(inner HTTPDoer) HTTPDoer` という新規ラッパー関数（`did.go` に追加）を経由させる設計になっている。この変更は `resolveHandleToDID`/`resolveDIDDocument` 自身から `checkRequestHostSafety` の呼び出しを削除し、呼び出し元（`NewClient`）が渡す `HTTPDoer` 側にこの責務を移すことを意味する。
- `internal/atproto/errors.go` の `*SSRFError` は `Error()`/`Unwrap()` のみを実装しており、`Permanent() bool` は未実装（`errors.go` 全文確認済み）。設計書 2.3節・4節が要求する `Permanent() bool { return true }` を追加する。
- **既存テストへの影響（設計書 2.4節ではカバーされていない箇所を含めて洗い出す）**:
  - `did_test.go:223-236` の `TestResolveDIDDocument_RejectsUnsafeDidWebHost` は `resolveDIDDocument(context.Background(), mock, "did:web:127.0.0.1")` を `NewClient` を経由せず直接呼び出しており、`mock.CallCount() == 0`（ホスト安全性検証によって拒否され、実際のリクエストが送られないこと）を検証している。設計書 2.3.1節の通り `checkRequestHostSafety` の呼び出しを `resolveDIDDocument` 本体から削除すると、このテストが直接渡す生の `mock` にはホスト安全性検証が一切適用されなくなり、`mock.CallCount() == 0` の期待が崩れる（`mock.Do` が実際に呼ばれてしまう）。設計書 2.4節は `atproto.NewClient` を経由する `did_test.go` のテスト（`TestNewClient_RejectsUntrustedHost_NoFurtherRequest` 等）への影響のみを扱っている。`resolveDIDDocument`/`resolveHandleToDID` を直接呼び出すこのテストへの影響は明記されていない。本計画はこれを設計書のカバー漏れと判断し、フェーズ2で `mock` を `newHostSafetyCheckedDoer(mock)` でラップしてから渡すようテストを更新するタスクを追加する（2.2節フェーズ2参照）。
  - `did_test.go:238-263` の `TestResolveHandleToDID_RejectsMalformedHandle` は `resolveHandleToDID` 冒頭の `invalidHandleChars` 検証（`checkRequestHostSafety` より前に行われる、別の検証）のみに依拠しており、`checkRequestHostSafety` の呼び出し位置移動の影響を受けない（確認済み: `mock.CallCount() == 0` はハンドル文字列の構文検証時点で確定する）。変更不要。
  - `did_test.go:265-276` の `TestResolveHandleToDID_RejectsNonDIDResponse` は安全なホスト（`publicIPLiteral`）に対する呼び出しが成功したうえで DID 形式でないレスポンスを拒否するテストであり、`checkRequestHostSafety` の成否には依存しない（安全性検証が呼ばれてもホストが安全なため通過するのみ）。変更不要。
  - `session_test.go`（`TestClient_Login_InvalidCredentials_NoFurtherCalls`）・`posts_test.go`（`TestClient_ListPosts_PaginationStalled`）・`delete_test.go` はいずれも `newTestClient`（`test_helpers.go`、`//go:build test`）で `*Client` の非公開フィールドを直接設定しており、`NewClient` を経由しない（＝リトライラップ・ホスト安全性再検証ラップのいずれも通過しない）。設計書 2.4節の記載通り、変更不要。
  - `errors_test.go`・`http_test.go` は `HTTPError`/`SSRFError`/`restrictedDoer`/`DialContext` を直接テストしており無関係（設計書 2.4節と同じ）。
  - `runner_integration_test.go` は `atproto.NewClient` 経由で `*Client` を構築するが、`atproto.StubPassthroughPDSDoer(t)` を使って PDS 側のみリトライラップを外している（設計書 3.3節）。DID 解決フェーズは `StubPassthroughPDSDoer` の対象外のためリトライ・ホスト安全性再検証ラップを通過するが、モックが DID 解決を1回で成功させるため挙動は変わらない。変更不要。
- `internal/atproto/test_helpers.go` の `StubPassthroughPDSDoer` は `newPDSDoer` を差し替えるのみであり、本タスクでの変更対象ではない（設計書 3.3節: 差し替え後の関数はリトライラップを経ない、という契約は `newPDSDoer` のデフォルト実装側にリトライラップを実装することで自然に保たれる）。
- `docs/design/configuration.md` の `execution_timeout_seconds` の記述例（`docs/design/configuration.md:26` 付近の TOML コードブロック）の直後、`## 環境変数` 見出しの直前に、設計書 3.6節が定める追記内容を挿入する。
- `docs/dev/developer_guide/package_reference.md` はまだ `internal/retry` パッケージについて一切記載していない（`rg -n "internal/retry" docs/dev/developer_guide/package_reference.md` は0件）。CLAUDE.md の方針（「Package Reference を `cmd/`・`internal/` の変更に追従させる」）に従い、フェーズ3で追記する。
- **AC-05・AC-06 のテストカバレッジ上のギャップ**: 設計書 3.4節は AC-05・AC-06 が既存実装（`cmd/main.go` の `context.WithTimeout`、`internal/config` の検証）で充足済みであると述べているが、`rg -n "WithTimeout|WithDeadline|WithCancel" cmd/main_test.go internal/runner internal/atproto` を実行した結果、実際に実行タイムアウトを発火させて `run` の終了コードを確認する既存テストは存在しないことを確認した。（`internal/config` 側のテストは「設定値が `time.Duration` として正しく読み込まれること」のみを検証しており、`cmd/main.go` 側で実際にタイムアウトが処理を中断させることは未検証である。）AC-05 はテキストの静的な存在確認だけでは検証できない振る舞い要件であるため（mkplan の AC 検証ルール）、フェーズ2で `cmd/main_test.go` に実際に短い `execution_timeout_seconds` を設定し、ハングする `HTTPDoer` に対して `run` が有限時間内にタイムアウトエラーで終了することを検証する回帰テストを1本追加する。これは新しい型・実装を追加するものではなく、既存の `context.WithTimeout` 配線に対する検証テストの追加である。
- **AC-08・AC-09 の検証方針**: 設計書 3.5節・7.2節は、「リトライ待機中の ctx キャンセルが遅延なく検知されること」を `internal/retry/doer_test.go` の単体テスト（`fakeClock` を用いる、7.1節）で、「その結果生じたエラーが `Result.Failed` に分類され、他の投稿の削除処理が継続されること」を `internal/runner/runner_test.go::TestRun_Apply_PartialFailure_ContinuesAndRecordsFailure`（[0004_cli_entrypoint](../0004_cli_entrypoint/03_implementation_plan.md) で実装済み、`runner.Run` はエラーの原因を区別せず `Result.Failed` に振り分ける）で検証する方針を示している。この方針の下では、新しい結合テストは追加しない。しかし、この2つの既存/単体テストの間には「`retry.Doer` が返す `ctx.Err()` が `doXRPC`（`internal/atproto/http.go`）の `*HTTPError` ラップを経て `DeleteRecord` の戻り値までそのまま伝わること」自体を確認するテストが存在しない（`internal/retry` 単体テストは `atproto` を一切経由せず、`runner_test.go` の `fakeClient` は `internal/atproto`/`internal/retry` を一切経由しない、`internal/runner/test_helpers.go` 確認済み）。この中間層の結合は個々のコードを読めば妥当だと判断できる。しかし、実装ミス（例: `doXRPC` が `ctx.Err()` を別のセンチネルエラーで上書きしてしまう）を検出するテストがないまま「検証済み」とするのは AC トレーサビリティ上のギャップである。したがって、本計画はフェーズ2に `internal/atproto/client_test.go::TestClient_DeleteRecord_CtxDeadlineDuringRetry_ReturnsCtxErrWithoutFullBackoff`（新規）を追加し、`retry.Doer` → `doXRPC` → `DeleteRecord` の3層を実際に通して「ctx 期限切れが `errors.Is` で追跡可能な形で伝わり、かつ遅延なく（`Policy.BaseDelay` の実待機なしに）返ること」を検証する（2.2節フェーズ2参照）。

## 2. 実装ステップ

### フェーズ1: `internal/retry` パッケージの実装（F-001、AC-01〜AC-04、NF-002）

- [x] **対象ファイル**: `internal/retry/clock.go`（新規作成）
  - **作業内容**:
    - `package retry` を宣言する。
    - 設計書 3.1節の `Clock` インターフェースをそのまま定義する: `Sleep(ctx context.Context, d time.Duration) error`。
    - `RealClock` 構造体（フィールドなし）を定義し、`Sleep` を実装する。`time.NewTimer(d)` と `ctx.Done()` の両方を `select` で待ち受け、いずれか早い方で返る。タイマー経由で返る場合は `nil`、`ctx.Done()` 経由で返る場合は `ctx.Err()` を返す。`d <= 0` の場合は `select` の競合に頼らず、まず `ctx.Err()` を確認してから（非 `nil` ならそれを返し）、そうでなければ待機なしで `nil` を返す（設計書 3.1節の防御的な扱い）。タイマーは `defer timer.Stop()` で確実に解放する。
  - **完了基準**: `go build ./...` が成功する。

- [x] **対象ファイル**: `internal/retry/doer.go`（新規作成）
  - **作業内容**:
    - `package retry` を宣言する。
    - 設計書 3.1節の `HTTPDoer` インターフェース、`Policy` 構造体（`MaxRetries int`・`BaseDelay time.Duration`・`MaxDelay time.Duration`）をそのまま定義する。
    - 設計書 2.3節の非公開インターフェース `permanentError`（`Permanent() bool`）を定義する。
    - `Doer` 構造体（非公開フィールド: `inner HTTPDoer`・`policy Policy`・`clock Clock`）と `NewDoer(inner HTTPDoer, policy Policy, clock Clock) *Doer` を実装する。
    - `(d *Doer) Do(req *http.Request) (*http.Response, error)` を実装する。設計書 2.2節のシーケンス図・3.1節の要点・3.2節の分類表の通り、次の判定順序で処理する。
      1. `req.GetBody` が非 `nil` の場合、2回目以降の送信で毎回これを呼び出し新しい `req.Body` を持つクローンを送信する（`req.Clone(req.Context())` を用い、クローンの `Body` のみ差し替える）。1回目は元の `req` をそのまま送信する。
      2. `d.inner.Do` がエラーを返した場合: エラーが `permanentError` を満たし `Permanent()==true` を返す場合は、リトライせず即座にそのエラーを返す（AC-03、2.3節）。それ以外のエラーは一時的な通信エラーとして扱い、手順4（待機・再試行）に進む（AC-01）。
      3. `d.inner.Do` が応答を返した場合: ステータスコードが 401、または 429 以外の 4xx の場合はリトライせず即座にその応答を返す（AC-03、3.2節）。ステータスコードが 429 または 5xx の場合は一時的なエラーとして扱い、応答ボディを読み捨てて `Close()` してから（3.1節の3パターン、後述）、手順4（待機・再試行）に進む（AC-01）。それ以外（2xx、429 以外の 4xx を除く応答）は即座にその応答を返す。
      4. 現在の試行回数が `Policy.MaxRetries` に達していれば、直前のエラーまたは応答をそのまま返す（AC-02）。達していなければ待機時間を計算する: 429 応答の `Retry-After` ヘッダーが解釈可能かつ正の値であればそれを参考値とし、そうでなければ `Policy.BaseDelay` を初項とする指数バックオフ（試行回数ごとに2倍）を用いる。いずれの場合も `Policy.MaxDelay` で頭打ちにする（AC-04、3.1節の `Retry-After` 異常値対策）。
      5. `d.clock.Sleep(ctx, wait)` を呼び出す（`ctx` は `req.Context()`）。`Sleep` が非 `nil` のエラー（`ctx.Err()`）を返した場合、それ以上リトライせず即座にそのエラーを返す（AC-08 の前提となる性質、設計書 3.5節）。`nil` が返れば手順1に戻り再試行する。
    - 429/5xx と判定した中間応答の `resp.Body` の読み捨て処理は、設計書 3.1節の3パターンで実装する。(a) `io.Copy(io.Discard, resp.Body)` で EOF まで読み切ってから `Close()` する。(b) 読み捨てたバイト数が `maxDrainBytes`（`const maxDrainBytes = 64 * 1024`、設計書 3.1節の「数十 KB」の具体値として64KiBを採用）を超えた場合は、EOF を待たずに `Close()` する。(c) 読み捨て中に `req` の `ctx` がキャンセル/デッドライン到達した場合（`io.Copy` がエラーを返す）は、`Close()` した上でリトライをスケジュールせず ctx 由来のエラーとして即座に返す（手順5と同じ「即座に打ち切り」の扱い）。
    - 再試行が発生するたび（打ち切りも含む）に `log/slog` のデフォルトロガーへ1行出力する: 試行回数・待機時間・HTTP メソッド・URL を含む（設計書 3.1節「リトライの可観測性」）。
  - **完了基準**: `go build ./...` が成功する。

- [x] **対象ファイル**: `internal/retry/test_helpers.go`（新規作成、`//go:build test`）
  - **作業内容**: `package retry` を宣言し、`//go:build test` タグを付与する。`fakeClock` 構造体を実装する: `Sleep(ctx context.Context, d time.Duration) error` は実際の待機を行わず、まず `ctx.Err()` を確認して非 `nil` ならそれを即座に返し、そうでなければ呼び出された `d` を記録用フィールド `SleepCalls []time.Duration` に追記して `nil` を返す（NF-002: 実待機なしで `Policy.MaxDelay` 頭打ちの検証・ctx キャンセルの即時検知の両方を同じ実装でシミュレートできる）。
  - **完了基準**: `internal/retry` パッケージ配下のテストからのみ参照され、`//go:build test` タグにより本番ビルドに含まれないことを `go build ./...`（タグなし）でも確認する。

- [x] **対象ファイル**: `internal/retry/doer_test.go`（新規作成）
  - **作業内容**: 設計書 7.1節が要求するシナリオを、`fakeClock` と、`retry.HTTPDoer` を満たす軽量なテスト専用モック（本ファイル内に定義する非公開の関数型アダプタ、例: `type mockDoerFunc func(*http.Request) (*http.Response, error)` とそのレシーバ `Do`。`internal/retry` 自身のテストでのみ使用し他パッケージから参照されないため、`testutil/` サブディレクトリは不要）を用いて検証する。応答ボディは `io.ReadCloser` の `Close()` 呼び出し回数を数えるラッパー（同じく本ファイル内に定義）で包む。
    - [x] `TestDoer_Do_SuccessOnFirstAttempt_NoRetry`: 初回で2xxが返る場合、リトライせずそのまま応答を返すこと（回帰確認、`fakeClock.SleepCalls` が空であること）。
    - [x] `TestDoer_Do_TransientFailures_RetriesThenSucceeds`: 通信エラー・429・5xxの3パターンをテーブル駆動で検証し、いずれも指数バックオフで再試行した後、成功応答が得られ次第それを返すこと（AC-01）。
    - [x] `TestDoer_Do_MaxRetriesExceeded_ReturnsLastFailure`: `Policy.MaxRetries` 到達後は再試行を打ち切り、最後の失敗（エラーまたは非2xx応答）をそのまま返すこと（AC-02）。
    - [x] `TestDoer_Do_PermanentFailures_NotRetried`: 401、429以外の4xx（例: 403）、および `Permanent() bool` を実装するテスト用エラー型の3パターンをテーブル駆動で検証し、いずれも再試行せず即座に返ること（AC-03）。
    - [x] `TestDoer_Do_ExponentialBackoffCappedAtMaxDelay`: 連続する一時的エラーに対する待機時間が指数的に増加しつつ `Policy.MaxDelay` を超えないこと（`fakeClock.SleepCalls` の各要素をアサートする、AC-04）。
    - [x] `TestDoer_Do_LargeRetryAfterCappedAtMaxDelay`: 429応答が `Policy.MaxDelay` を大幅に超える `Retry-After` を返した場合も、実際の待機時間が `Policy.MaxDelay` で頭打ちになること（AC-04）。
    - [x] `TestDoer_Do_NonPositiveRetryAfterFallsBackToExponential`: 429応答の `Retry-After` が負の秒数、または過去日時の場合、待機なしでの再試行にはならず指数バックオフの計算値にフォールバックすること（設計書 3.1節）。
    - [x] `TestDoer_Do_DrainsAndClosesIntermediateBody_OnNormalEOF`: 再試行対象と判定した中間応答の `Body` が、次の試行に進む前に EOF まで読み切られたうえで `Close()` されること（`Close()` 呼び出し回数をアサート、設計書 3.1節パターン(a)）。
    - [x] `TestDoer_Do_DiscardCapExceeded_ClosesWithoutFullDrain`: 中間応答の `Body` が `maxDrainBytes` を超える場合、EOF を待たずに読み捨てを打ち切って `Close()` すること（設計書 3.1節パターン(b)）。
    - [x] `TestDoer_Do_CtxCanceledDuringBodyDrain_ReturnsCtxErrImmediately`: 中間応答の読み捨て中に `req` の `ctx` がキャンセルされた場合、読み捨てエラーを無視せず、以降のリトライをスケジュールせずに ctx 由来のエラーを即座に返すこと（設計書 3.1節パターン(c)、AC-08 の前提）。
    - [x] `TestDoer_Do_RetriesResendFreshBodyFromGetBody`: `req.GetBody` を設定したPOSTリクエストで再試行が発生した場合、2回目以降の送信でも `req.GetBody()` から取得した新しいボディが送信されること（1回目の送信でボディが消費済みでも空にならないこと）。
    - [x] `TestDoer_Do_CtxCanceledDuringSleep_ReturnsImmediately`: `fakeClock.Sleep` に渡す `ctx` を事前にキャンセルしておき、`Doer.Do` が実際の待機（`fakeClock.SleepCalls` への記録）を行わずに即座に `ctx.Err()` を返すこと（AC-08 の前提となる 3.5節の性質）。
    - [x] `TestDoer_Do_LogsRetryAttempt`: `slog.SetDefault` を `slog.NewTextHandler` でバッファ書き込みするロガーに差し替え（`t.Cleanup` で元に戻す）、再試行が発生した場合にログ出力へ試行回数・待機時間・HTTPメソッド・URLに相当する情報が含まれることを検証する（設計書 3.1節「リトライの可観測性」）。
  - **完了基準**: `make test` で `internal/retry` パッケージの全テストが成功する。各テストは `assert`/`require`（`github.com/stretchr/testify`）でアサーションを記述する（CLAUDE.md テスト方針）。すべてのテストが `fakeClock` を用い、実際の `time.Sleep` を行わないため、パッケージ全体の実行時間が数百ミリ秒以内に収まることを目視確認する（NF-002）。

### フェーズ2: `internal/atproto` への組み込み（F-001の適用、既存テストの回帰確認）

- [x] **対象ファイル**: `internal/atproto/errors.go`（既存ファイルの変更）
  - **作業内容**: 設計書 4節のコード例の通り、`*SSRFError` に `Permanent() bool { return true }` を追加する。`atproto` パッケージは `internal/retry` を import しない（構造的部分型による暗黙適合、設計書 2.3節）。
  - **完了基準**: `go build ./...` が成功する。

- [x] **対象ファイル**: `internal/atproto/did.go`（既存ファイルの変更）
  - **作業内容**:
    - 設計書 2.3.1節のシグネチャの通り `newHostSafetyCheckedDoer(inner HTTPDoer) HTTPDoer` を追加する。返す `HTTPDoer` の `Do(req *http.Request)` は、`checkRequestHostSafety(req.Context(), req.URL.String())` を呼び出し、エラーがあればそれをそのまま返し、なければ `inner.Do(req)` に委譲する。
    - `resolveHandleToDID` から `checkRequestHostSafety(ctx, reqURL)` の呼び出し（現行 `did.go:47-49` 相当）を削除する。ハンドル文字列の構文検証（`invalidHandleChars` チェック）はそのまま残す。
    - `resolveDIDDocument` から `checkRequestHostSafety(ctx, docURL)` の呼び出し（現行 `did.go:104-106` 相当）を削除する。
  - **完了基準**: `go build ./...` が成功する（この時点では `internal/atproto` の呼び出し元がまだ `newHostSafetyCheckedDoer` を組み込んでいないため、以降の `client.go` の変更と合わせて初めてホスト安全性検証が有効になる。次のタスクと同一コミット内で完結させ、中間状態でホスト安全性検証が欠落したビルドを残さないこと）。

- [x] **対象ファイル**: `internal/atproto/client.go`（既存ファイルの変更）
  - **作業内容**:
    - `internal/retry` パッケージを import する。
    - `defaultRetryPolicy`（`retry.Policy{MaxRetries: 5, BaseDelay: time.Second, MaxDelay: 30 * time.Second}`、設計書 3.4節の既定値）をパッケージレベルの変数として定義する。
    - `NewClient` 内で、DID 解決に使う `HTTPDoer` を `retry.NewDoer(newHostSafetyCheckedDoer(httpDoer), defaultRetryPolicy, retry.RealClock{})` として組み立て、`resolveHandleToDID`/`resolveDIDDocument` の呼び出しにはこの組み立て済み `HTTPDoer` を渡す（現行はどちらも生の `httpDoer` を渡している）。
    - `newPDSDoer` のデフォルト実装（現行 `client.go:37-39`）を、`newRestrictedDoer(verifiedAddrs, host)` の戻り値を `retry.NewDoer(..., defaultRetryPolicy, retry.RealClock{})` でラップして返すよう変更する（設計書 2.1節の凡例「`newPDSDoer` 内で戻り値をラップされる」、3.3節）。`newPDSDoer` 変数のシグネチャ自体は変更しない。
  - **完了基準**: `go build ./...` が成功する。`internal/atproto` のうち `did_test.go`・`session_test.go`・`posts_test.go`・`delete_test.go`・`errors_test.go`・`http_test.go`・`runner_integration_test.go` の既存テストが、次のタスク（`did_test.go` の更新）を除いて無変更のまま `make test` で成功する（1.3節の既存コード調査結果の通り）。

- [x] **対象ファイル**: `internal/atproto/did_test.go`（既存ファイルの変更・追加）
  - **作業内容**:
    - [x] `TestResolveDIDDocument_RejectsUnsafeDidWebHost`（既存）を修正する: `resolveDIDDocument(context.Background(), mock, "did:web:127.0.0.1")` の呼び出しを `resolveDIDDocument(context.Background(), newHostSafetyCheckedDoer(mock), "did:web:127.0.0.1")` に変更し、`mock.CallCount() == 0` の期待が `newHostSafetyCheckedDoer` 経由でも成立することを確認する（1.3節で洗い出した回帰ギャップの解消）。
    - [x] `TestNewHostSafetyCheckedDoer_RevalidatesOnEveryCall`（新規）: `newHostSafetyCheckedDoer` でラップした `HTTPDoer` の `Do` を複数回呼び出すモックを用意し、1回目の呼び出し時は安全なホスト（`publicIPLiteral`）への `req`、2回目の呼び出し時は不正なアドレス（例: `10.0.0.1`）への `req` を渡す。1回目は `mock` に到達し成功応答が返ること、2回目は `mock` に到達せず `*SSRFError` が返ることを検証し、`Do` が呼ばれるたびに独立して `checkRequestHostSafety` が再実行されることを確認する（設計書 2.3.1節、DNS リバインディング対策とリトライの両立）。
  - **完了基準**: `make test` で `internal/atproto` パッケージの全テストが成功する。

- [x] **対象ファイル**: `internal/atproto/client_test.go`（新規作成）
  - **作業内容**:
    - [x] `TestNewClient_WrapsHTTPDoerWithRetry`（新規）を実装する。`stubSymbolicHostLookup`（`did_test.go`、既存）と `handleResolutionHandler`（`did_test.go`、既存）を再利用して `NewClient` を成功させ、返った `*Client` の非公開フィールド `httpDoer` を `_, ok := client.httpDoer.(*retry.Doer)` で型アサーションし、`ok` が `true` であることを検証する（設計書 7.1節「`NewClient` が返す `*Client` の `httpDoer` がリトライでラップされていることの確認」）。
    - [x] `TestClient_DeleteRecord_CtxDeadlineDuringRetry_ReturnsCtxErrWithoutFullBackoff`（新規）を実装する。AC-08・AC-09 が要求する「実行タイムアウトの deadline が個々の削除 API 呼び出しの `context.Context` に伝播し、リトライ待機中もその deadline 到達を遅延なく検知する」という一連の経路を、`internal/retry`（フェーズ1で単体テスト済み）・`doXRPC` によるエラーラップ（`internal/atproto/http.go`、既存）・`DeleteRecord` の3層を実際に通して確認する、`internal/retry` 単体テストと `internal/runner` の既存テストだけでは検証できない結合的な性質を埋めるテストである（1.3節参照）。`newTestClient`（既存）を用い、`httpDoer` に `retry.NewDoer(mock, defaultRetryPolicy, retry.RealClock{})` を設定する（`RealClock` を用いる。`fakeClock` は `internal/retry` 自身の非公開ヘルパーであり `internal/atproto` からは参照できないため、`internal/atproto` 側のこの結合確認では実クロックを使う）。`mock.Handler` は呼び出しごとに `<-req.Context().Done()` を待ってから `nil, req.Context().Err()` を返すブロッキングハンドラとする。`context.WithTimeout(context.Background(), 100*time.Millisecond)` で短い deadline を持つ `ctx` を作り、`client.DeleteRecord(ctx, "abc123")` を呼び出す。戻り値のエラーが `errors.Is(err, context.DeadlineExceeded)` を満たすこと（`retry.Doer` が返す `ctx.Err()` が `doXRPC` の `*HTTPError` ラップを経ても `errors.Is` で追跡できること）、かつテストの実行時間が `defaultRetryPolicy.BaseDelay`（1秒）を大きく下回る一定時間（例: 500ミリ秒未満）に収まることを検証する。

    後者が満たされない場合、`retry.Doer` がリトライ待機中の ctx 期限切れを遅延なく検知できていないことを意味する（`cmd/main_test.go::TestRun_ExecutionTimeoutExceeded_ReturnsExitCode1` と同じ理由づけ、フェーズ2の当該タスク参照）。
  - **完了基準**: `make test` で本ファイルの全テストが成功する。`go test -tags test -run TestClient_DeleteRecord_CtxDeadlineDuringRetry_ReturnsCtxErrWithoutFullBackoff ./internal/atproto -v` の実行時間が1秒未満であることを確認する。

- [x] **対象ファイル**: `cmd/main_test.go`（既存ファイルの変更）
  - **作業内容**: `TestRun_ExecutionTimeoutExceeded_ReturnsExitCode1`（新規）を追加する。1.3節で洗い出した通り、AC-05（実行タイムアウト超過時に処理を中断し非0の終了コードで終了すること）を実際に検証する既存テストが存在しないための追加である。`execution_timeout_seconds = 1`（`internal/config` が許容する最小値）を含む設定ファイルを用意し、DID解決用のレスポンス（`.well-known/atproto-did`）を返すハンドラの代わりに、`<-req.Context().Done()` を待ってから `nil, req.Context().Err()` を返すブロッキングハンドラを設定した `MockHTTPDoer` を用いる。`run(configPath, false, time.Now(), mock, io.Discard, io.Discard)` を呼び出し、戻り値が `exitSetupOrRunFail`（`1`）であること、かつテスト自体が数秒以内（`execution_timeout_seconds` 到達までの1秒 + `internal/retry` の初回バックオフによる余分な待機が発生しないこと、後述）に完了することを検証する。`retry.Doer` はこのブロッキングハンドラが返す `ctx.Err()` を一時的な通信エラーとして扱い、一度は再試行を試みる。しかし `Clock.Sleep`（`RealClock`）に渡す `ctx` が既にキャンセル済みのため、`select` が即座に `ctx.Done()` 側で復帰し、実際の1秒間の追加バックオフ待機は発生しない（`internal/retry/clock.go` の実装、フェーズ1参照）。この性質により、テスト全体の実行時間は `execution_timeout_seconds` の1秒程度に収まる。
  - **完了基準**: `make test` で本テストが成功し、かつ `go test -tags test -run TestRun_ExecutionTimeoutExceeded_ReturnsExitCode1 ./cmd -v` の実行時間が2秒未満であることを確認する（実際の `RealClock`・実行タイムアウトを用いる数少ないテストであるため、想定通り高速に完了することを明示的に確認する）。

### PR-1 作成ポイント: internal/retry package and atproto integration

**対象ステップ**: フェーズ1 / フェーズ2

**推奨タイトル**: `feat(0005-retry-timeout): add internal/retry and wrap atproto HTTPDoer`

**レビュー観点**: `Doer.Do` の再試行対象分類（429/5xx/通信エラー vs 401/429以外の4xx/`Permanent()`）が設計書 3.2節の表と一致していること / バックオフ待機時間が常に `Policy.MaxDelay` で頭打ちになっていること（`Retry-After` 由来・指数計算由来のどちらも） / `Clock.Sleep` の ctx キャンセルが実際の待機を待たずに即座に検知されること / `newHostSafetyCheckedDoer` の導入によって DID 解決の既存テスト（`did_test.go`）が退行していないこと（特に `TestResolveDIDDocument_RejectsUnsafeDidWebHost` の更新箇所） / `StubPassthroughPDSDoer` を使う既存の結合テストが本タスクの変更後も無修正のまま成功すること

- [ ] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [ ] PR を作成した
- [ ] PR がマージされた
- [ ] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

### フェーズ3: ドキュメント更新（AC-07、CLAUDE.md のパッケージ参照更新方針）

- [ ] **対象ファイル**: `docs/design/configuration.md`
  - **作業内容**: `execution_timeout_seconds` の記述例（TOMLコードブロック）の直後、`## 環境変数` 見出しの直前に、設計書 3.6節に記載された引用ブロックの文面をそのまま追記する（本文中の「本アーキテクチャ設計書 3.4節」という参照は、コピー先が `docs/design/` 直下であることを踏まえ `[0005_retry_timeout アーキテクチャ設計書](../tasks/0005_retry_timeout/02_architecture.md#34-f-002実行タイムアウトの充足状況とリトライポリシーの数値ac-05ac-07)` のような相対リンクに変換して追記する）。
  - **完了基準**: `rg -n "約31秒" docs/design/configuration.md` が1件以上ヒットする（設計書 3.6節の引用文面が転記されていることの確認）。
- [ ] **対象ファイル**: `docs/dev/developer_guide/package_reference.md`
  - **作業内容**:
    1. 「Directory Structure」の `internal/` 一覧に、`atproto/` の説明に続けて次の1行を追加する。
       ```
       - `retry/`: generic `HTTPDoer` decorator that retries transient failures (transport errors, 429, 5xx) with bounded exponential backoff, with no dependency on internal/atproto (see docs/tasks/0005_retry_timeout)
       ```
    2. 「Package Responsibilities」の「AT Protocol Client」の項にある既存の説明文（`docs/dev/developer_guide/package_reference.md:31`）は「`retries, dry-run/apply switching, and post-age/type filtering are out of scope and left to other packages`」と記載しており、本タスク以後は `internal/atproto` 自身が `internal/retry` を組み込んでリトライを行うため、この一文は事実と矛盾する。文末の `retries, dry-run/apply switching, and post-age/type filtering are out of scope and left to other packages (see docs/tasks/0002_atproto_client/01_requirements.md).` を `dry-run/apply switching and post-age/type filtering are out of scope and left to other packages (see docs/tasks/0002_atproto_client/01_requirements.md); retrying transient HTTP failures is delegated to internal/retry, which NewClient wraps every outbound HTTPDoer (DID resolution and the post-validation PDS client) in before use (see docs/tasks/0005_retry_timeout/01_requirements.md).` に変更する（「リトライは対象外」という誤った記述を残さないよう、追記ではなく既存文の書き換えとして行う）。
    3. 「Package Responsibilities」に新しい見出し「**Retry**」を追加し、次の説明を記載する。
       ```
       **Retry**

       - `internal/retry`: a generic `HTTPDoer` decorator (`Doer`) that retries transient failures (transport errors, HTTP 429, HTTP 5xx) with bounded exponential backoff, honoring a server's `Retry-After` header when positive and always capping the wait at `Policy.MaxDelay`. Never retries an error satisfying the unexported `permanentError` interface or a non-429 4xx status. Depends only on the standard library, so `internal/atproto` is the only consumer that imports it (see docs/tasks/0005_retry_timeout/01_requirements.md).
       ```
  - **完了基準**: 以下の `rg` コマンドがそれぞれ期待通りの結果になる。
    - `rg -n "internal/retry" docs/dev/developer_guide/package_reference.md` — 3件以上ヒットする（Directory Structure の1行 + AT Protocol Client の項の書き換え後の1行 + Package Responsibilities「Retry」見出し以降の記述1行以上）。
    - `rg -n "retries, dry-run/apply switching, and post-age/type filtering are out of scope" docs/dev/developer_guide/package_reference.md` — 0件（「リトライは対象外」という書き換え前の文言が残っていないことの確認）。

### PR-2 作成ポイント: documentation updates

**対象ステップ**: フェーズ3

**推奨タイトル**: `docs(0005-retry-timeout): document retry worst-case timing and add package reference entry`

**レビュー観点**: `docs/design/configuration.md` への追記内容が設計書 3.6節の文面と一致していること（数値のコピーミスがないこと） / `package_reference.md` の追記が既存の `internal/atproto`・`internal/cleanup` の記述粒度・文体と一致していること

- [ ] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [ ] PR を作成した
- [ ] PR がマージされた
- [ ] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

### フェーズ4: 品質確認

- [ ] **対象コマンド**: `make fmt` / `make test` / `make lint`
  - **作業内容**: 3コマンドを順に実行し、いずれもエラーなく完了することを確認する（NF-001）。
  - **完了基準**: 3コマンドすべてが正常終了する。
- [ ] **対象コマンド**: `make deadcode`
  - **作業内容**: `internal/retry` 新設および `internal/atproto` の変更に起因する新規の未使用コードが検出されないことを確認する。
  - **完了基準**: 本タスクに起因する新規の未使用コードが検出されない（既存の未解消項目がある場合はその理由を実行結果として明記する）。

### PR-3 作成ポイント: quality gates

**対象ステップ**: フェーズ4

**推奨タイトル**: `chore(0005-retry-timeout): confirm quality gates`

**レビュー観点**: `make fmt`/`make test`/`make lint`/`make deadcode` の実行結果がすべて記録されていること

- [ ] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [ ] PR を作成した
- [ ] PR がマージされた

## 3. 実装順序とマイルストーン

### 3.1 マイルストーン

| マイルストーン | 内容 | 成果物 |
|---|---|---|
| M1 | `internal/retry` パッケージの実装と単体テストが完了し `make test` が緑になる | `internal/retry/doer.go`・`internal/retry/clock.go`・`internal/retry/test_helpers.go`・`internal/retry/doer_test.go` |
| M2 | `internal/atproto` への組み込みが完了し、AC-01〜AC-04・AC-05（回帰テスト追加分）が test で緑になり、既存テストが無退行であることを確認する | `internal/atproto/client.go`・`internal/atproto/errors.go`・`internal/atproto/did.go`・`internal/atproto/did_test.go`・`internal/atproto/client_test.go`・`cmd/main_test.go` |
| M3 | ドキュメント更新が完了し AC-07 が静的検証で緑になる | 更新済み `docs/design/configuration.md`・`docs/dev/developer_guide/package_reference.md` |
| M4 | `make fmt`/`make test`/`make lint`/`make deadcode` の完走を確認する | 実行結果の記録（実装チェックリスト） |

### 3.2 PR 構成

| PR | 対応フェーズ | 概要 |
|---|---|---|
| PR-1 | フェーズ1 / フェーズ2 | `internal/retry` パッケージの新設と `internal/atproto` への組み込み |
| PR-2 | フェーズ3 | `docs/design/configuration.md`・`package_reference.md` の更新 |
| PR-3 | フェーズ4 | 品質確認完了の記録 |

## 4. テスト戦略

### 4.1 単体テスト

- `internal/retry`: `doer_test.go` にて、一時的エラーの再試行（AC-01）・上限到達時の打ち切り（AC-02）・恒久的エラーの即時返却（AC-03）・バックオフ上限（AC-04、`Retry-After` の正常値・異常値の両方）・中間応答のボディ読み捨て（3パターン）・`GetBody` による再送・ctx キャンセルの即時検知・可観測性ログを検証する（フェーズ1、`fakeClock` を用いて実待機なしで完結させる、NF-002）。
- `internal/atproto`: `did_test.go` にて `newHostSafetyCheckedDoer` が呼び出しごとに再検証を行うことを検証し（フェーズ2）、`client_test.go` にて `NewClient` が返す `*Client` の `httpDoer` が `retry.Doer` でラップされていることを検証する（フェーズ2）。

### 4.2 統合テスト

- 本タスクは `internal/atproto/runner_integration_test.go` に新規のケースを追加しない。1.3節・2.2節フェーズ2の通り、この既存の結合テストは本タスクの変更後も無修正のまま回帰テストとして機能する（設計書 7.2節の決定を踏襲）。
- `cmd/main_test.go::TestRun_ExecutionTimeoutExceeded_ReturnsExitCode1`（新規）は、実際の `context.WithTimeout`・`internal/retry.RealClock` を用いた end-to-end 検証であり、AC-05 のテストカバレッジ上のギャップ（1.3節）を埋める。
- AC-08・AC-09 は、`internal/retry/doer_test.go::TestDoer_Do_CtxCanceledDuringSleep_ReturnsImmediately`（`retry.Doer` 単体でのctx キャンセルの即時検知）、`internal/atproto/client_test.go::TestClient_DeleteRecord_CtxDeadlineDuringRetry_ReturnsCtxErrWithoutFullBackoff`（新規。`retry.Doer` → `doXRPC` → `DeleteRecord` の3層を実際に通した結合確認、1.3節参照）、`internal/runner/runner_test.go::TestRun_Apply_PartialFailure_ContinuesAndRecordsFailure`（既存、[0004_cli_entrypoint](../0004_cli_entrypoint/03_implementation_plan.md)。エラー原因を問わず `Result.Failed` に振り分けられ、処理が継続すること）の3つを組み合わせて検証する。

### 4.3 後方互換テスト

- `internal/atproto` の既存テスト（`did_test.go`・`session_test.go`・`posts_test.go`・`delete_test.go`・`errors_test.go`・`http_test.go`・`runner_integration_test.go`）が、`TestResolveDIDDocument_RejectsUnsafeDidWebHost` の1件を除き無変更のまま `make test` で成功することを確認する（1.3節・フェーズ2）。

### 4.4 テストヘルパ方針

[test_organization.md](../../dev/developer_guide/test_organization.md) の分類に従う。

- `internal/retry/test_helpers.go`: `fakeClock` は `internal/retry` の公開型 `Clock` を実装するのみで、`internal/retry` 自身のテストでのみ使用される。他パッケージ（`internal/atproto` 等）は `fakeClock` を直接使わず、本番同様 `RealClock` を使う（`internal/atproto` 側のテストは配線の確認のみで、時間制御ロジック自体は `internal/retry` 側で検証済みのため。設計書 3.3節・付録決定履歴）。したがって Classification B（`test_helpers.go`、`//go:build test`）に置く。
- `internal/retry/doer_test.go` 内の軽量モック（`mockDoerFunc` とボディの `Close()` 呼び出し回数を数えるラッパー）は `internal/retry` 公開APIのみに依存するが、`internal/retry` 自身のテストからしか参照されないため、独立した `testutil/` サブディレクトリを新設する必要はない（同一テストファイル内のヘルパーで足りる）。
- `internal/atproto/client_test.go` は既存の `did_test.go` のヘルパー（`stubSymbolicHostLookup`・`handleResolutionHandler`、いずれも `package atproto` 内の非公開関数）をそのまま再利用し、新規ヘルパーを追加しない。
- `cmd/main_test.go` に追加する `TestRun_ExecutionTimeoutExceeded_ReturnsExitCode1` は、既存の `cmd/main_test.go` 内のヘルパー（1.3節・[0004_cli_entrypoint](../0004_cli_entrypoint/03_implementation_plan.md) で実装済みの `hermeticHandler` 等)を必要な範囲で再利用し、新規の共有テストヘルパーファイルは追加しない。

## 5. リスク管理

| リスク | 影響 | 対策 |
|---|---|---|
| `Doer.Do` の再試行対象分類を誤り、SSRF拒否や認証エラーまで再試行してしまう | 恒久的に失敗するはずの呼び出しに無駄な待機が発生し、レート制限誘発リスクが増す（設計書 5節） | フェーズ1の `TestDoer_Do_PermanentFailures_NotRetried` で 401・429以外の4xx・`Permanent()` の3パターンを個別に検証し、PR-1 のレビュー観点で分類表との一致を確認する |
| `checkRequestHostSafety` の呼び出し位置移動により、DNSリバインディング対策が意図せず弱まる、またはリトライのたびに再検証が行われず穴が残る | SSRF/DNSリバインディング対策の後退（セキュリティ設計 参照） | フェーズ2の `TestNewHostSafetyCheckedDoer_RevalidatesOnEveryCall` で「呼び出しごとに再検証される」ことを明示的に検証し、`TestResolveDIDDocument_RejectsUnsafeDidWebHost` の更新漏れ（1.3節で発見済み）を解消する |
| `Clock.Sleep` の実装が `ctx` キャンセルを即座に検知できず、実行タイムアウト到達後もリトライ待機が続いてしまう | AC-08 の「強制中断が遅延なく発生する」という要件が満たされず、cron の多重起動対策が弱まる | フェーズ1の `TestDoer_Do_CtxCanceledDuringSleep_ReturnsImmediately`（`fakeClock` によるロジック検証）と `cmd/main_test.go::TestRun_ExecutionTimeoutExceeded_ReturnsExitCode1`（`RealClock` による end-to-end 検証）の両方でこの性質を確認する |
| `internal/retry` 導入によって既存の `internal/atproto` テストの呼び出し回数アサーションが意図せず変化する | 既存テストが赤くなり、原因調査に時間を要する | フェーズ2完了時に `make test` で `internal/atproto` パッケージ全体を実行し、1.3節で洗い出した「変更不要」なテストが実際に無変更のまま成功することを確認する |
| スケジュールリスク: `checkRequestHostSafety` の呼び出し位置移動（did.go）と `internal/retry` パッケージ新設（新規パッケージ）を同一PRに含めると、レビュー範囲が広くなり見落としが増える | 軽微〜中程度 | フェーズ1・フェーズ2を1つのPR（PR-1）にまとめる代わりに、レビュー観点で「分類ロジック」と「ホスト安全性再検証」を別々の観点として明記し、レビュー時にそれぞれ独立して確認できるようにする |

## 6. 実装チェックリスト

- [ ] PR-1 マージ済み（対象ステップ: フェーズ1 / フェーズ2。`internal/retry` パッケージ新設、`internal/atproto` への組み込み、AC-01〜AC-04・AC-05（回帰テスト）・AC-08/AC-09（ctxキャンセル検知の単体・結合テスト）関連のテスト追加）
- [ ] PR-2 マージ済み（対象ステップ: フェーズ3。`docs/design/configuration.md`・`package_reference.md` 更新、AC-07 静的検証）
- [ ] PR-3 マージ済み（対象ステップ: フェーズ4。品質確認完了の記録）
- [ ] `make fmt` / `make test` / `make lint` がすべて通過
- [ ] `make deadcode` で本タスクに起因する新規の未使用コードがないことを確認

## 7. 受け入れ基準の検証（Acceptance Criteria Verification）

| AC | 検証種別 |
|---|---|
| AC-01〜AC-04 | test |
| AC-05 | test |
| AC-06 | test（既存） |
| AC-07 | static |
| AC-08〜AC-09 | test（新規結合テスト+既存テストの組み合わせ） |

**AC-01: API 呼び出しが一時的なエラー（429、5xx、タイムアウト等）で失敗した場合、指数バックオフで待機した上で再試行する**
- Test location: `internal/retry/doer_test.go::TestDoer_Do_TransientFailures_RetriesThenSucceeds`
- Implementation: `internal/retry/doer.go`（`Doer.Do`）
- Verification method: test（通信エラー・429・5xxの3パターンをテーブル駆動で検証し、いずれも `fakeClock` に記録された待機を経て成功応答を返すことをアサートする）

**AC-02: リトライ回数が設定された上限に達した場合、それ以上リトライせずエラーとして呼び出し元に返す**
- Test location: `internal/retry/doer_test.go::TestDoer_Do_MaxRetriesExceeded_ReturnsLastFailure`
- Implementation: `internal/retry/doer.go`（`Doer.Do` の試行回数チェック）
- Verification method: test（`Policy.MaxRetries` を超える回数だけ一時的エラーを返すモックを用意し、`Doer.Do` が最後の失敗をそのまま返し、モックの呼び出し回数が `MaxRetries+1` 回で頭打ちになることをアサートする）

**AC-03: 認証エラー（401 等）のような再試行しても解決しないエラーに対しては、リトライせず即座にエラーを返す**
- Test location: `internal/retry/doer_test.go::TestDoer_Do_PermanentFailures_NotRetried`
- Implementation: `internal/retry/doer.go`（`Doer.Do` の分類ロジック、`permanentError` インターフェース）、`internal/atproto/errors.go`（`SSRFError.Permanent()`）
- Verification method: test（401、429以外の4xx、`Permanent()==true` を実装するテスト用エラー型の3パターンで、モックの呼び出し回数が1回のまま即座にエラー・応答が返ることをアサートする）

**AC-04: リトライ間の待機時間には上限（最大バックオフ時間）が設けられており、無制限に増加しない。429 応答が `Retry-After` ヘッダーを含む場合、その値を待機時間の参考にしてよいが、最大バックオフ時間の上限を超えて待機しない**
- Test location: `internal/retry/doer_test.go::TestDoer_Do_ExponentialBackoffCappedAtMaxDelay`、`internal/retry/doer_test.go::TestDoer_Do_LargeRetryAfterCappedAtMaxDelay`、`internal/retry/doer_test.go::TestDoer_Do_NonPositiveRetryAfterFallsBackToExponential`
- Implementation: `internal/retry/doer.go`（`Doer.Do` の待機時間計算）
- Verification method: test（指数バックオフ・極端に大きい `Retry-After`・0以下の `Retry-After` の3パターンで、`fakeClock.SleepCalls` に記録された待機時間がいずれも `Policy.MaxDelay` を超えないことをアサートする）

**AC-05: 設定された実行タイムアウトを超過した場合、処理を中断し、明確なタイムアウトエラーとして終了する**
- Test location: `cmd/main_test.go::TestRun_ExecutionTimeoutExceeded_ReturnsExitCode1`
- Implementation: `cmd/main.go`（`run` の `context.WithTimeout`、既存実装、[0004_cli_entrypoint](../0004_cli_entrypoint/01_requirements.md)）
- Verification method: test（1.3節で洗い出した既存カバレッジのギャップを埋める新規テスト。`execution_timeout_seconds=1` とハングする `HTTPDoer` を用いて `run` を実行し、戻り値が `exitSetupOrRunFail` であること、かつテストが数秒以内に完了することをアサートする）

**AC-06: 実行タイムアウトは [0001_config](../0001_config/01_requirements.md) で読み込まれる設定値から取得される**
- Test location: `internal/config/config_test.go::TestLoad_ExecutionTimeoutValidation`、`internal/config/app_config_test.go`（既存、0001_config で実装済み）
- Implementation: `internal/config/validate.go`（`ExecutionTimeout` の算出）、`cmd/main.go`（`cfg.ExecutionTimeout` の参照、既存実装）
- Verification method: test（既存テストが TOML の `execution_timeout_seconds` から `time.Duration` への変換を検証済みであり、本タスクはこの経路を変更しないため、既存テストの継続的な成功をもって検証済みとする）

**AC-07: タイムアウトの累積時間見積もりに、リトライの最悪ケース（最大リトライ回数 × 最大バックオフ時間）が考慮されていることがドキュメント上明記されている**
- Test location: 該当なし（ドキュメントの存在確認のため static 検証のみで十分。テキストの存在有無を検証する要件であり、振る舞いを伴わないため）
- Implementation: `docs/design/configuration.md`（3.6節の追記内容）
- Verification method: static（`rg -n "約31秒" docs/design/configuration.md` を実行し、1件以上ヒットすることを確認する）

**AC-08: 実行タイムアウトの deadline は個々の削除 API 呼び出しの `context.Context` にも伝播し、呼び出し中にタイムアウトへ達した場合はその呼び出しを強制中断してよい。ただし強制中断された呼び出しの投稿は「削除完了」として扱わず、削除の成否が確認できない投稿として扱う**
- Test location: `internal/retry/doer_test.go::TestDoer_Do_CtxCanceledDuringSleep_ReturnsImmediately`（`retry.Doer` 単体でのリトライ待機の遅延なき打ち切り）、`internal/atproto/client_test.go::TestClient_DeleteRecord_CtxDeadlineDuringRetry_ReturnsCtxErrWithoutFullBackoff`（新規。`retry.Doer` → `doXRPC` → `DeleteRecord` を実際に通した結合確認）、`internal/runner/runner_test.go::TestRun_Apply_PartialFailure_ContinuesAndRecordsFailure`（既存、[0004_cli_entrypoint](../0004_cli_entrypoint/01_requirements.md)。`DeleteRecord` のエラー原因を問わず `Result.Failed` に分類され「削除完了」に含まれないこと）
- Implementation: `internal/retry/clock.go`（`RealClock.Sleep` の ctx 監視）、`internal/atproto/http.go`（`doXRPC` のエラーラップ、既存）、`internal/runner/runner.go`（`Run` の `Result.Deleted`/`Result.Failed` 振り分け、既存実装）
- Verification method: test（deadline 伝播自体は `http.NewRequestWithContext`（既存、[0002_atproto_client](../0002_atproto_client/01_requirements.md)）が担い変更がないため新規テスト不要。本タスクが新たに満たすべき「リトライ待機中の deadline 到達を遅延なく検知し、そのエラーが `DeleteRecord` の戻り値まで正しく伝わる」性質を `doer_test.go`（`retry.Doer` 単体）と `client_test.go`（`retry.Doer` から `DeleteRecord` までの結合）の2段階で検証し、その結果生じるエラーが既存の分類ロジックでどう扱われるかを `runner_test.go` の既存テストで検証する、という3つのテストの組み合わせで全体を検証する）

**AC-09: タイムアウトにより処理が中断した場合、それまでに削除完了が確認できた投稿と、削除が未完了・成否不明なまま残った投稿（強制中断されたものを含む）が判別できる形で結果が返る**
- Test location: `internal/runner/runner_test.go::TestRun_Apply_PartialFailure_ContinuesAndRecordsFailure`（既存）、`internal/report/report_test.go::TestFormatText_Apply_WithFailures`（既存、[0004_cli_entrypoint](../0004_cli_entrypoint/01_requirements.md)）
- Implementation: `internal/report/report.go`（`Result.Deleted`/`Result.Failed`、`FormatText`、既存実装）
- Verification method: test（`Result.Deleted`/`Result.Failed` の構築・表示ロジックは0004で実装・検証済みであり、本タスクは分類の原因（強制中断か通常のエラーか）を区別しない既存設計をそのまま用いるため、これらの既存テストの継続的な成功をもって検証済みとする）

## 8. 成功基準

- **機能的完全性**: AC-01〜AC-09 がすべて7節のテストで検証され成功する。
- **品質指標**: `make fmt` / `make test` / `make lint` / `make deadcode` がすべてエラーなく完了する（NF-001）。
- **セキュリティ検証**: 設計書 5節の通り、本タスク固有のリスクは「リトライ過多による自滅的なアクセス制限」「DNSリバインディング対策とリトライの両立」の2点であり、前者はフェーズ1の `TestDoer_Do_MaxRetriesExceeded_ReturnsLastFailure`・`TestDoer_Do_ExponentialBackoffCappedAtMaxDelay`・`TestDoer_Do_LargeRetryAfterCappedAtMaxDelay`、後者はフェーズ2の `TestNewHostSafetyCheckedDoer_RevalidatesOnEveryCall` で検証する。
- **時間のモック化（NF-002）**: `internal/retry` パッケージの全テストが `fakeClock` を用いて実際の待機なしに完結すること（フェーズ1完了基準）。実際の `RealClock` を用いるテストは `cmd/main_test.go::TestRun_ExecutionTimeoutExceeded_ReturnsExitCode1` の1本のみであり、その実行時間が2秒未満であることを確認する。
- **ドキュメント整備**: `docs/design/configuration.md`・`docs/dev/developer_guide/package_reference.md` に、リトライの最悪ケース時間・`internal/retry` パッケージの責務が反映されている（フェーズ3、7節の `rg` コマンドで確認）。

## 9. 次のステップ

- 設計書 9節の通り、`internal/retry.Policy` の数値を将来 TOML から設定可能にする場合も、`internal/retry` パッケージ自体の型・インターフェースは変更せず `internal/atproto` 側の値取得元を変えるだけで対応できる。
- 設計書 9節の通り、[0006_slack_notification](../0006_slack_notification/01_requirements.md) が Slack Webhook 送信に独自のリトライを必要とする場合、本タスクが新設する `internal/retry` パッケージをそのまま再利用できる可能性がある（0006 側の要件定義で判断する）。
- 本タスクでは、リトライポリシーの数値（`MaxRetries`/`BaseDelay`/`MaxDelay`）が実際の Bluesky 側のレート制限ポリシーと比較して妥当かどうかは検証しない（設計書 5.2節「検出限界」）。運用開始後、実際のレート制限誘発が観測された場合は、既定値の見直しを別タスクとして起票する。
