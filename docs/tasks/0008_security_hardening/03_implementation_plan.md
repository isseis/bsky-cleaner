# セキュリティ強化・仕上げ — 実装計画書

## Document Status

| Item | Value |
|---|---|
| Status | `approved` |
| Created | 2026-07-06 |
| Review date | 2026-07-06 |
| Reviewer | isseis |
| Comments | - |

## 1. 実装概要

### 1.1 目的

本計画は [02_architecture.md](./02_architecture.md) に基づき、[01_requirements.md](./01_requirements.md) の AC-01〜AC-06 を満たすための作業を、検証可能な単位に分解する。作業は性質の異なる 3 種類に分かれる。

- **DoS 系防御の新規実装（AC-04）**: [セキュリティ設計](../../design/security.md) が「防御が抜けている」と明記する 3 項目（`doXRPC` の応答サイズ上限・`listAllRecords` のページネーション上限・XRPC `http.Client` のリクエスト全体タイムアウト）を `internal/atproto` に実装する。本タスク唯一の新規プロダクションコードである（設計は [02_architecture.md 3 章](./02_architecture.md#3-コンポーネント設計)）。
- **横断的な結合テスト（AC-01・AC-02・AC-05・AC-06）**: エンドツーエンドの秘密情報非漏洩と、削除処理全体の冪等性・異常系を、既存のテストハーネスを再利用した結合テストで固定する。新しいプロダクションコードは追加しない。
- **棚卸しと決定の記録（AC-03・AC-04）**: [セキュリティ設計](../../design/security.md) の全リスクカテゴリのトレーサビリティ一覧と、未対応項目の対応可否判断を、本計画書の [7 章](#7-セキュリティ設計の棚卸しac-03ac-04) に静的成果物として記載する。

### 1.2 実装方針

- **設計の再掲を避ける**: 上限値の設計根拠・エラー契約・脅威モデルは [02_architecture.md](./02_architecture.md) を参照し、本計画では「どのファイルに何を追加し、どう検証するか」のみを書く。
- **既存資産の再利用**: 結合テストは既存の `atprototestutil.MockHTTPDoer` とネットワーク遮断パターン、および既存テストファイルのヘルパー（[1.3 節](#13-既存コード調査結果)）を再利用する。
- **Go ソースは英語**: 追加する識別子・コメント・文字列リテラルはすべて英語で記述する。
- **各編集後に `make fmt` → `make test` → `make lint` を実行**し、緑を維持する（NF-001）。

### 1.3 既存コード調査結果

各対象について「既存の状態 / 不足 / 変更内容」を示す。変更不要な領域は省く。

**プロダクションコード（Phase 1 対象）**

- `internal/atproto/http.go`
  - 既存: `doXRPC` は成功応答を `json.NewDecoder(resp.Body).Decode(out)` で無制限に読む（77 行目）。非 2xx 応答は `xrpcErrorName(resp.Body)` を上限なしで読む（73 行目）。`newRestrictedDoer`（121 行目）が構築する `http.Client` は `dialTimeout`（接続確立のみ）を持ち、`http.Client.Timeout`（全体）を持たない。定数 `dialTimeout` は 21 行目に既存。
  - 不足: 応答サイズ上限・リクエスト全体タイムアウト。
  - 変更: 定数 `maxXRPCResponseBytes`・`xrpcRequestTimeout` を追加し、`doXRPC` の成功／エラー両ボディ読み取りに上限を課す。`newRestrictedDoer` に `timeout time.Duration` 引数を追加して `http.Client.Timeout` に設定し、本番の呼び出し元 `newPDSDoer` は定数 `xrpcRequestTimeout` を渡す（[02_architecture.md 3.1・3.3 節](./02_architecture.md#3-コンポーネント設計)）。パッケージ変数の save/restore は使わない（並列テストで壊れないようにするため）。
- `internal/atproto/posts.go`
  - 既存: `listAllRecords`（169 行目）は `resp.Cursor == cursor` の停滞のみ検知し `ErrPaginationStalled` を返す（190-192 行目）。各ページの `resp.Records`（`listRecord.Value` は `json.RawMessage`）を `all` に蓄積する。ページ内レコード数上限は既存定数 `listRecordsPageLimit`（`"100"`、32 行目）。
  - 不足: 累積バイト数・総ページ数・総レコード数の上限。
  - 変更: 定数 `maxListTotalBytes`・`maxListPages`・`maxListRecords` を追加し、`listAllRecords` のループで各上限を検査して `ErrPaginationLimitExceeded` を返す（[02_architecture.md 3.2 節](./02_architecture.md#32-累積バイトページレコード数上限 listallrecords)）。
- `internal/atproto/errors.go`
  - 既存: センチネル群（`ErrPaginationStalled` 等、11-18 行目）、`HTTPError`（`Method`/`StatusCode`/`ErrorName`/`Err`、56-61 行目）。
  - 変更: センチネル `ErrResponseTooLarge`・`ErrPaginationLimitExceeded` を追加（追加のみ、[02_architecture.md 3.4 節](./02_architecture.md#34-追加する型定数インターフェイス定義)）。
- `internal/atproto/test_helpers.go`（`//go:build test`, `package atproto`）
  - 既存: `newTestClient`・`StubPassthroughPDSDoer`。
  - 変更: なし。`xrpcRequestTimeout` の差し替えはパッケージ変数の save/restore ではなく `newRestrictedDoer` への引数渡しで行うため、本ファイルへの追加は不要（[Phase 1](#phase-1-dos-系防御の実装と単体テストac-04)）。

**変更不要だが Phase 1 が再利用する既存資産**

- `internal/retry/doer.go`: `drainAndClose`（179 行目、`io.CopyN(dst, body, maxDrainBytes+1)`、44 行目）が「上限＋1 まで読んで超過を判定する」既存パターンの参照実装。中間（リトライ対象）応答ボディは `maxDrainBytes`（64 KiB）で既に上限済み（[02_architecture.md 3.1 節の注記](./02_architecture.md#31-応答サイズ上限 doxrpc)）。`http.Client.Timeout` 発火時のエラーは既存の `ErrTransportFailure` 経路と `retry` の一時的エラー分類（`classify`、145 行目）をそのまま通り、新しい分岐は不要。
- `internal/atproto/did.go`: `maxDIDResponseBytes`（18 行目）・`maxDIDDocumentResponseBytes`（23 行目）は `io.LimitReader` ベースの別契約。本タスクでは変更しない（[02_architecture.md 3.1 節の差分説明](./02_architecture.md#31-応答サイズ上限 doxrpc)）。

**結合テストが再利用する既存資産（Phase 2・3 対象）**

- `internal/atproto/testutil/mocks.go`（`//go:build test`）: `MockHTTPDoer`（`Handler`/`Requests()`/`CallCount()`）、`JSONResponse`。`Requests()` は記録済みリクエストのボディ（`RecordedRequest.Body`）を返すため、Slack POST ボディの捕捉に追加実装は不要。
- `internal/atproto/testutil/fixtures.go`（`//go:build test`）: `CreateSessionResponseJSON`・`ListRecordsResponseJSON`・`DeleteRecordResponseJSON`。
- `internal/atproto/runner_integration_test.go`（`package atproto_test`）: `publicIPLiteral`（25 行目）・`integrationAppPassword`（27 行目）・`StubPassthroughPDSDoer` を使った実 `*atproto.Client` ＋モックの配線パターン。**Phase 2 の新規ファイルは同一 `package atproto_test` に置くため、これらを再定義せず直接再利用する。**
- `cmd/main_test.go`（`package main`）: `publicIPLiteral`・`testDID`・`validConfigPath`・`hermeticHandler`・`setEnvCredentials`・`listRecordsHandler`・`deleteRecordHandler`・`slackWebhookHandler`・`postPageResponse`・`emptyPageResponse`。**Phase 3 の新規ファイルは同一 `package main` に置くため、これらを再定義せず直接再利用する。** 既存の `TestRun_ExecutionTimeoutExceeded_ReturnsExitCode1`（154 行目）は実行タイムアウト到達の配線パターンの参照。

**AC ↔ 実装コードの現状対応（棚卸しの下地、[7 章](#7-セキュリティ設計の棚卸しac-03ac-04) で完成させる）**

- 秘密マスキング機構は各パッケージに実装済み（`config.SecretString`／`atproto` の `HTTPError`・`SSRFError` の秘密非包含／`notify.errorKind`・`redactWebhookURL`）。AC-01・AC-02 はこれらを結合状態で検証するのみで、変更しない。
- `DeleteRecord`（`delete.go`）は既存の冪等性（2xx を成功として扱う）を持つ。AC-05・AC-06 はこれを結合状態で検証するのみ。

## 2. 実装ステップ

各フェーズは [02_architecture.md 8 章](./02_architecture.md#8-実装優先順位) のフェーズ定義と順序に一致する。本計画書はステップを `X-Y` 形式の番号ではなく Phase 単位で管理しているため、以降の PR 作成ポイントの `**対象ステップ**` 欄は Phase 番号（Phase 1〜4）を単位として記載する。各 Phase は対応する PR に 1:1 で対応する。

### Phase 1: DoS 系防御の実装と単体テスト（AC-04）

**対象ファイル**: `internal/atproto/errors.go`, `internal/atproto/http.go`, `internal/atproto/posts.go`, `internal/atproto/client.go`, `internal/atproto/test_helpers.go`, `internal/atproto/http_test.go`, `internal/atproto/posts_test.go`

- [x] `errors.go`: センチネル `ErrResponseTooLarge = errors.New("XRPC response exceeds size limit")` を追加する。
- [x] `errors.go`: センチネル `ErrPaginationLimitExceeded = errors.New("pagination byte/page/record limit exceeded")` を追加する。
- [x] `http.go`: 定数 `maxXRPCResponseBytes`（8 MiB = `8 << 20`）を追加する。1 ページ最大 100 レコードの正当な応答が数 MB に収まる前提の余裕値（[02_architecture.md 3.1 節](./02_architecture.md#31-応答サイズ上限 doxrpc)）。
- [x] `http.go`: リクエスト全体タイムアウトを表す定数 `xrpcRequestTimeout`（`const xrpcRequestTimeout = 30 * time.Second`）を追加する。`dialTimeout`（10 秒）を上回る値。
- [x] `http.go`: `newRestrictedDoer` のシグネチャに `timeout time.Duration` 引数を追加し（`newRestrictedDoer(verifiedAddrs []net.IP, host string, timeout time.Duration)`）、構築する `http.Client` に `Timeout: timeout` を設定する。パッケージ変数を使わず引数で渡すのは、`t.Cleanup` によるグローバル状態の save/restore（`newPDSDoer` の既存パターン）だとこのパッケージのテストが将来 `t.Parallel()` を使った際にデータ競合になり得るためである。本番の唯一の呼び出し元 `newPDSDoer`（`client.go`）は定数 `xrpcRequestTimeout` を渡すよう更新する。テストは `newRestrictedDoer` を短い `timeout` で直接呼び出すことで、遅延応答サーバーに対するタイムアウト発火を実時間を待たずに確認できる（[02_architecture.md 3.3 節](./02_architecture.md#33-リクエスト全体タイムアウト restricteddoer)）。
- [x] `http.go`: `doXRPC` の成功（2xx）応答処理を変更する。既存の `if out != nil` ガード（`DeleteRecord` は `out=nil` を渡すため、`http.go:76`）を維持したまま、`out != nil` の場合のみ `io.ReadAll(io.LimitReader(resp.Body, maxXRPCResponseBytes+1))` で読み取り、読み取り長が `maxXRPCResponseBytes` を超えた場合はデコードせず `&HTTPError{Method: xrpcMethod, StatusCode: resp.StatusCode, ErrorName: <marker>, Err: ErrResponseTooLarge}` を返す。上限内なら読み取ったバイト列を `json.Unmarshal` で `out` にデコードする。`out == nil`（削除など本文を読まない経路）の挙動は従来どおり変えない。
- [x] `http.go`: 応答サイズ超過マーカー用の定数（例: `const responseTooLargeErrorName = "ResponseTooLarge"`）を追加し、上記 `HTTPError.ErrorName` に設定する。これは PDS 応答由来ではなく本パッケージが決め打つ固定値であり、成功応答（`status=200`）由来の超過を `errorKind` 上で正常な 2xx と区別可能にする（[02_architecture.md 4.1・4.2 節](./02_architecture.md#41-エラー型)）。
- [x] `http.go`: 非 2xx 応答の `xrpcErrorName` 呼び出しを `xrpcErrorName(io.LimitReader(resp.Body, maxXRPCResponseBytes))` に変更し、エラーボディ経由の枯渇も塞ぐ。
- [x] `posts.go`: 定数 `maxListTotalBytes`（累積バイト上限、常駐メモリが数百 MiB 程度に収まる値、例: `256 << 20`）・`maxListPages`（総ページ数上限、例: `10000`）・`maxListRecords`（総レコード数上限、例: `1_000_000`）を追加する。正当な大規模アカウントが到達しない値とする（[02_architecture.md 3.2 節](./02_architecture.md#32-累積バイトページレコード数上限 listallrecords)）。
- [x] `posts.go`: `listAllRecords` のループで、ページ取得ごとに (1) 総ページ数、(2) 蓄積した各 `listRecord.Value` の長さの累積和、(3) 総レコード数を集計し、いずれかが対応する上限を超えた時点で `fmt.Errorf("list posts: list %s: %w", collection, ErrPaginationLimitExceeded)` を返す（既存の `ErrPaginationStalled` と同じラップ形式・同じフェイルクローズ）。
- [x] `http_test.go`: 応答サイズ上限の単体テストを追加する（[4.1 節](#41-単体テスト dos-系防御 phase-1)）。
- [x] `http_test.go`: リクエスト全体タイムアウトの単体テストを追加する。`newRestrictedDoer` を短い `timeout` 引数で直接呼び出す（グローバル変数の上書きは行わない）（[4.1 節](#41-単体テスト dos-系防御 phase-1)）。
- [x] `posts_test.go`: 累積バイト・総ページ・総レコード各上限の単体テストを追加する（[4.1 節](#41-単体テスト dos-系防御 phase-1)）。

**完了基準**: `make test`・`make lint` が緑。新規センチネルと上限を検査する単体テストが、超過時に対応するセンチネルを返し、上限内では従来どおり全件取得・デコードされることを確認する。

### PR-1 作成ポイント：internal/atproto DoS defenses

**対象ステップ**: Phase 1

**推奨タイトル**: `feat(0008-security-hardening): add XRPC response size, request timeout, and pagination limits`

**レビュー観点**: `doXRPC` の成功／エラー両方のボディ読み取りに `maxXRPCResponseBytes` 上限が正しく適用され、境界値（上限ちょうど＝成功、上限＋1＝超過）が設計書 3.1 節と一致していること / `newRestrictedDoer` への `timeout` 引数追加がパッケージ変数の save/restore を使わず、`newPDSDoer`・`newTestRestrictedDoer` の呼び出し元をすべて更新済みであること（将来の `t.Parallel()` 導入時のデータ競合回避という設計意図どおりであること） / `listAllRecords` の累積バイト・総ページ・総レコード上限が既存の `ErrPaginationStalled` と同じラップ形式で `ErrPaginationLimitExceeded` を返すこと / 上限内の既存ケース（`TestClient_ListPosts_Pagination` 等）が無退行であること

- [x] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [x] PR を作成した (https://github.com/isseis/bsky-cleaner/pull/59)
- [x] PR がマージされた
- [x] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

### Phase 2: 冪等性・異常系の結合テスト（AC-05・AC-06）

**対象ファイル**: `internal/atproto/idempotency_integration_test.go`（新規, `package atproto_test`）

- [x] AC-05 のテスト関数（`TestRunnerRun_AllTargetsAlreadyDeleted_TreatedAsSuccess`）を追加する。実 `*atproto.Client` ＋ `MockHTTPDoer` を `runner.Run`（apply=true）で駆動し、**複数**の対象 rkey すべての `deleteRecord` に 2xx を返すことで、一覧取得後に対象が消えていてもクラッシュせず正常系（全件 `Deleted` に入り `Failed` は空）として扱われることを検証する。配線は `runner_integration_test.go` の `TestRunnerRun_WithRealAtprotoClient` を踏襲し、`publicIPLiteral`・`integrationAppPassword`・`StubPassthroughPDSDoer` を再利用する（[02_architecture.md 6.2 節 AC-05](./02_architecture.md#62-冪等性異常系の結合テストac-05ac-06)）。**既存テストとの差別化**: 単一投稿・2xx→成功の基本経路は既存 `TestRunnerRun_WithRealAtprotoClient` が既に担保するため、本テストは重複を避け「複数対象がすべて既削除でも全件成功として扱われる」という AC-05 固有のシナリオ（一覧取得後に対象が消えているケース）を明示的に検証する点で区別する。テストの doc コメントで既存テストとの重なりを明記し、本テストの独自表明が「複数の既削除対象の一括成功」であることを述べる。
- [x] AC-06 のテスト関数（`TestRunnerRun_CancelMidDelete_RemainingFailedThenReRunSafe`）を追加する。複数削除対象を用意し、`context.WithCancel`（`WithTimeout` ではない）で得た `ctx` を `runner.Run` に渡す。`MockHTTPDoer.Handler` の `deleteRecord` 分岐で削除呼び出しを計数し、(a) 入口で `req.Context().Err()` が非 nil なら即座にそのエラーを返す（net/http が canceled ctx に対して返す挙動の再現）、(b) N 件目までは 2xx、(c) N+1 件目でテストの `cancel()` を呼んでから `ctx.Err()` を返す。
  - **なぜ決定的か**: `WithCancel` を使い `cancel()` のみを中断契機とすることで、実時間に一切依存しない決定的な中断を作る（`WithTimeout` の短いタイマーは低速ランナー上で N 未満の時点で発火し N を非決定にするため採用しない）。
  - **エラー種別の扱い**: この経路の `ctx.Err()` は `context.Canceled` であり `context.DeadlineExceeded` ではない。`runner.Run` は両者を同じ「削除失敗」として `Failed` に振り分ける（`runner.go` は `ctx` の種別を区別しない）ため、テストは `Canceled`／`DeadlineExceeded` の種別を表明せず、「残り対象が `Failed` に入る」ことのみを表明する。実行タイムアウトの強行中断（deadline 到達）も本番では同一経路を通るため、この決定的モデルで AC-06 の趣旨を満たす（[02_architecture.md 6.2 節 AC-06](./02_architecture.md#62-冪等性異常系の結合テストac-05ac-06)）。
- [x] 同テストで、終端状態が `Deleted`（N 件）と `Failed`（残り全件）に分かれること、および `Deleted` の rkey 集合と `Failed` の rkey 集合が重複せず判別可能であることを表明する（AC-06 の「削除済みと未処理が判別可能」）。実装は `ctx` を削除ループ内で明示確認しないため未着手バケットは存在しない、という実挙動に合わせる。
- [x] 同テストで、続けて同じ対象集合に対し 2 回目の `runner.Run`（`ctx` は未キャンセルの新規）を駆動し、既削除 rkey への再削除がすべて 2xx を返して `Failed` が空になる（重複削除がエラーにならない）ことを検証する（AC-06 の「次回実行時に重複削除の試行でエラーにならない」）。

**完了基準**: `make test -tags test` 相当（`make test`）が緑。AC-05・AC-06 の両関数が [02_architecture.md 6.2 節](./02_architecture.md#62-冪等性異常系の結合テストac-05ac-06) の実挙動どおりに通る。

### PR-2 作成ポイント：idempotency and cancellation integration tests

**対象ステップ**: Phase 2

**推奨タイトル**: `test(0008-security-hardening): add idempotency and mid-run cancellation integration tests`

**レビュー観点**: 複数の既削除対象がすべて `Deleted` として扱われクラッシュしないこと（AC-05）、既存 `TestRunnerRun_WithRealAtprotoClient` との重複範囲がテストの doc コメントで明示されていること / `context.WithCancel` のみを用いた中断がフレーキーでない決定的な設計になっていること（`WithTimeout` を避けた理由が妥当であること） / 中断後の `Deleted`／`Failed` の rkey 集合が重複せず判別可能であること、2 回目の `Run` で重複削除がすべて 2xx として扱われエラーにならないこと（AC-06） / 新規テストが `runner_integration_test.go` の既存ヘルパー（`publicIPLiteral`・`integrationAppPassword`・`StubPassthroughPDSDoer`）を再定義せず再利用していること

- [x] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [x] PR を作成した
- [x] PR がマージされた
- [x] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

### Phase 3: 秘密非漏洩の結合テスト（AC-01・AC-02）

**対象ファイル**: `cmd/secret_leak_integration_test.go`（新規, `package main`）

- [x] **識別性の高い秘密リテラルを定義する**。app パスワード・`AccessJWT`・成功／失敗 Webhook URL には、部分文字列一致が偶然当たらない固定リテラル（例: `secretAppPassword = "SECRET-APP-PW-xyz789"`、`secretAccessJWT = "SECRET-ACCESS-JWT-abc123"`、成功／失敗 Webhook URL も `hooks.slack.com` 配下の識別性の高いパス）を本ファイル内の定数として定義する。既存 `setEnvCredentials`（`"app-password"`・`"access-jwt"` 相当）の値は短く弱いため**本 Phase では使用しない**。
- [x] **単一の Phase-3 セットアップヘルパー**（例: `setupSecretLeakEnv(t)`）を用意し、上記の識別性の高い app パスワードと両 Webhook URL を環境変数／TOML に注入する。**AC-01・AC-02 の全 `run()` 駆動テストがこのヘルパーを必ず呼ぶ**こととし、いずれのテストも `setEnvCredentials` にフォールバックしない。フォールバックすると、禁止文字列集合が実際には一度も設定されていない値を探すことになり、テストが常に通ってしまう（漏洩を見逃す偽陰性になる）ためである。
- [x] **`createSession` が識別性の高い `AccessJWT` を返す Phase-3 ローカルのハンドラ**を用意する。既存 `listRecordsHandler`（`cmd/main_test.go`）は `createSession` 応答を `CreateSessionResponseJSON(testDID, "access-jwt")` と決め打つため、その `createSession` 分岐を**そのままは再利用しない**。本 Phase のハンドラは `createSession` 分岐で `CreateSessionResponseJSON(testDID, secretAccessJWT)` を返し、`listRecords`／`getRecord`／`deleteRecord`／Slack POST の各分岐は `hermeticHandler`・`postPageResponse`・`emptyPageResponse`・`slackWebhookHandler`・`JSONResponse` を再利用して構成する（JWT を注入する `createSession` 分岐のみ独自実装する）。
- [x] 禁止文字列集合 **{`secretAppPassword`, `secretAccessJWT`, `"Bearer " + secretAccessJWT`, 成功 Webhook URL, 失敗 Webhook URL}** を組み立てるヘルパーを用意する（[02_architecture.md 6.1 節の禁止文字列集合](./02_architecture.md#61-秘密非漏洩の結合テストac-01ac-02)）。集合の各値は上記セットアップヘルパー／`createSession` ハンドラが注入した実値と一致させる。
- [x] 3 出力面（stdout バッファ・stderr バッファ・捕捉した Slack ペイロード）を集める共通検証ヘルパーを用意する。Slack ペイロードは `mock.Requests()` を `req.URL` の host が `hooks.slack.com` のものに絞って `RecordedRequest.Body` から取得する（`MockHTTPDoer` の既存記録機構を再利用）。各面に対し禁止文字列集合の各要素が部分文字列として現れないことを表明する。
- [x] AC-01（正常系）のテストを追加する。ログイン成功→一覧取得→削除→Slack 通知まで通し、秘密が現れないことを検証する。副作用契約（[02_architecture.md 5.3 節](./02_architecture.md#53-副作用契約 dry-run-と---apply)）に従い **dry-run と `--apply` の両方**を対象とする（dry-run では Slack 通知が抑止されるため検証面は stdout・stderr、`--apply` では 3 面すべて）。
- [x] AC-02 (a) 認証失敗のテストを追加する。`createSession` に非 2xx を返し、適用面に秘密が現れないことを検証する。
- [x] AC-02 (b) ネットワークエラーのテストを追加する。モックの `Do` が通信エラー（`return nil, errors.New(...)`）を返す。
- [x] AC-02 (c) DID/PDS エンドポイント解決エラーのテストを追加する。`.well-known/atproto-did` または `.well-known/did.json` 応答を不正値／非 2xx にする（`hermeticHandler` を使わず DID 解決段で失敗させる）。**この経路は `createSession` に到達しないため `AccessJWT`／Bearer に対する非漏洩表明は空振り（vacuous）**であり、実効的な検証は app パスワード・Webhook URL の非漏洩に対して働く。テストコメントにこの点を明記する。
- [x] AC-02 (d) 削除呼び出し失敗のテストを追加する。`deleteRecord` に非 2xx を返す（`--apply`）。
- [x] AC-02 (e) Slack 通知送信失敗のテストを追加する。`hooks.slack.com` への POST に非 2xx を返す（`--apply`）。retry の実時間待機を避けるため非 429 の 4xx を用いる（既存 `TestRun_Apply_SlackNotifyFails_*` と同じ配慮）。
- [x] AC-02 (f) 実行タイムアウト到達のテストを追加する。`execution_timeout_seconds = 1`（`LoadAppConfig` の最小値）の設定と、`ctx` を尊重して `<-req.Context().Done()` で待機するモックで deadline 到達を発生させる（既存 `TestRun_ExecutionTimeoutExceeded_ReturnsExitCode1` の配線を踏襲）。DID 解決段で中断するため **(c) と同様に `AccessJWT`／Bearer への表明は空振り**であり、実効検証は app パスワード・Webhook URL に働く。検証面は stdout・stderr。テストコメントにこの点を明記する。

**完了基準**: `make test` が緑。AC-01（両モード）・AC-02 (a)〜(f) の各テストが、意図した失敗経路を通過したうえで 3 面（または適用面）に秘密文字列を含まないことを確認する。

### PR-3 作成ポイント：secret non-leakage integration tests

**対象ステップ**: Phase 3

**推奨タイトル**: `test(0008-security-hardening): add secret non-leakage integration tests`

**レビュー観点**: 識別性の高い秘密リテラル（app password・`AccessJWT`・両 Webhook URL）を注入する専用セットアップヘルパーを全テストが必ず呼び、既存の弱い `setEnvCredentials` にフォールバックしていないこと（偽陰性回避） / 禁止文字列集合が stdout・stderr・Slack ペイロードの 3 面（dry-run では 2 面）で漏洩していないことを一貫して検証していること / AC-02 (c)・(f) は DID 解決段で中断するため `AccessJWT`／Bearer への非漏洩表明が空振り（vacuous）である旨がテストコメントに明記されていること / AC-01 が dry-run と `--apply` の両方を対象とし副作用契約（Slack 通知の抑止有無）と整合していること

- [x] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [x] PR を作成した (https://github.com/isseis/bsky-cleaner/pull/62)
- [x] PR がマージされた
- [x] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

### Phase 4: 棚卸し一覧・決定記録（AC-03・AC-04）

**対象ファイル**: 本計画書 [7 章](#7-セキュリティ設計の棚卸しac-03ac-04)

- [x] AC-03: [セキュリティ設計](../../design/security.md) に列挙された全リスクカテゴリのトレーサビリティ一覧を [7.1 節](#71-トレーサビリティ一覧ac-03) に記載する。列挙は同文書を正とする（[02_architecture.md 7.3 節](./02_architecture.md#73-棚卸しの検証ac-03ac-04)）。
- [x] AC-04: 未対応・不十分と判明した項目の対応可否判断を [7.2 節](#72-対応可否判断ac-04) に記載する。DoS 系 3 項目は本タスクで対応済みとし元タスクへ差し戻さない。DID 解決経路の独立タイムアウトとファイルシステム権限管理は本タスク非対応とその理由を記録する。

**完了基準**: Phase 1〜3 の結果（実装・見送りの確定）を反映し、[7 章](#7-セキュリティ設計の棚卸しac-03ac-04) が [8 章の AC 検証](#8-受け入れ基準の検証) の `static` チェックを満たす。

### PR-4 作成ポイント：security design traceability documentation

**対象ステップ**: Phase 4

**推奨タイトル**: `docs(0008-security-hardening): record security design traceability and disposition`

**レビュー観点**: [7.1 節](#71-トレーサビリティ一覧ac-03) のトレーサビリティ一覧が [セキュリティ設計](../../design/security.md) の 9 カテゴリすべてを網羅していること / [7.2 節](#72-対応可否判断ac-04) の対応可否判断が PR-1〜PR-3 実装後の実際のファイル・シンボルと矛盾しないこと（Phase 1〜3 マージ後に `rg` で再確認済みであること） / DID 解決経路の独立タイムアウトとファイルシステム権限管理を「本タスク非対応」とした理由が要件定義書のスコープ外規定と整合していること

- [x] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [x] PR を作成した (https://github.com/isseis/bsky-cleaner/pull/63)
- [ ] PR がマージされた
- [ ] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

## 3. 実装順序とマイルストーン

| マイルストーン | 内容 | 完了の定義 |
|---|---|---|
| M1 | Phase 1 完了 | DoS 系 3 防御が実装され、単体テストが緑。`make test`・`make lint` が緑 |
| M2 | Phase 2 完了 | AC-05・AC-06 の結合テストが緑 |
| M3 | Phase 3 完了 | AC-01・AC-02 の結合テストが緑 |
| M4 | Phase 4 完了 | 棚卸し一覧・決定記録が [7 章](#7-セキュリティ設計の棚卸しac-03ac-04) に揃い、[8 章](#8-受け入れ基準の検証) の全 AC が緑 |

Phase 1 を先行させる理由、Phase 4 を最後に置く理由は [02_architecture.md 8 章](./02_architecture.md#8-実装優先順位) を参照。

### 3.2 PR 構成

| PR | 対象ステップ | 主な変更内容 |
|---|---|---|
| PR-1 | Phase 1 | `internal/atproto` に XRPC 応答サイズ上限・リクエスト全体タイムアウト・`listAllRecords` の累積バイト/総ページ/総レコード上限を実装し、単体テストを追加する |
| PR-2 | Phase 2 | `internal/atproto/idempotency_integration_test.go` を新規追加し、AC-05（既削除対象の正常系扱い）・AC-06（中断後の判別可能性・再実行安全性）を結合テストで検証する |
| PR-3 | Phase 3 | `cmd/secret_leak_integration_test.go` を新規追加し、AC-01（正常系）・AC-02 (a)〜(f)（代表的なエラー経路）で秘密情報が 3 出力面に現れないことを検証する |
| PR-4 | Phase 4 | 本計画書 [7 章](#7-セキュリティ設計の棚卸しac-03ac-04) に、セキュリティ設計の全リスクカテゴリのトレーサビリティ一覧と未対応項目の対応可否判断を確定する |

## 4. テスト戦略

### 4.1 単体テスト（DoS 系防御、Phase 1）

配置は [02_architecture.md 7.1 節](./02_architecture.md#71-単体テスト dos-系防御) のとおり。既存の `newPostsTestClient`・`newListPostsHandler`・`buildListRecordsBody`（`posts_test.go`）と `newTestRestrictedDoer`（`http_test.go`）を再利用する。

| 対象 | 検証内容 | 配置ファイル |
|---|---|---|
| 応答サイズ上限 | `maxXRPCResponseBytes` を超える応答ボディで `doXRPC` が `*HTTPError`（`errors.Is` で `ErrResponseTooLarge`、`ErrorName` が超過マーカー）を返し、上限内の応答は従来どおりデコードされること（境界：上限ちょうどは成功、上限＋1 で超過） | `internal/atproto/http_test.go` |
| リクエスト全体タイムアウト | `xrpcRequestTimeout` を短縮したうえで、応答を遅延させる `httptest` サーバーに対し `http.Client.Timeout` が実際に発火し `Do` がタイムアウトエラーを返すこと。定数値を突き合わせるだけの検証はしない（[02_architecture.md 7.1 節](./02_architecture.md#71-単体テスト dos-系防御)） | `internal/atproto/http_test.go` |
| 累積バイト上限 | 巨大 `value` を持つページを積み上げる応答で `ErrPaginationLimitExceeded` が返ること | `internal/atproto/posts_test.go` |
| 総ページ数上限 | 異なるカーソルの空ページ（レコードなし）を返し続ける応答で `ErrPaginationLimitExceeded` が返ること | `internal/atproto/posts_test.go` |
| 総レコード数上限 | 極小レコードを大量に返す応答で `ErrPaginationLimitExceeded` が返ること | `internal/atproto/posts_test.go` |
| 上限内の全件取得 | 3 上限のいずれにも達しない複数ページ応答が従来どおり全件取得されること（既存 `TestClient_ListPosts_Pagination` が担保しているため、新規追加は上限超過ケースのみ） | 既存 `internal/atproto/posts_test.go` |

`newRestrictedDoer` へのシグネチャ変更（`timeout time.Duration` 引数追加）に伴い、既存 `newTestRestrictedDoer`（`http_test.go:71`、内部で `newRestrictedDoer(dialContextAddrs, host)` を呼ぶ）も `timeout time.Duration` 引数を追加してそのまま転送するよう更新する（既存の呼び出し元 `TestRestrictedDoer_Do` 等は本番相当の `xrpcRequestTimeout` を渡せばよい）。タイムアウト単体テストでは `newTestRestrictedDoer` に短いタイムアウト値を渡して `restrictedDoer` を構築する。パッケージ変数の上書きは行わないため、構築順序に関する制約はない。`httptest` サーバーはそのタイムアウト値より長く応答を遅らせ（本番既定の 30 秒には依存しない）、`t.Cleanup(server.Close)` でサーバーを閉じてゴルーチン滞留を防ぐ。

### 4.2 結合テスト（横断的検証、Phase 2・3）

- 冪等性・異常系（AC-05・AC-06）: `internal/atproto/idempotency_integration_test.go`。詳細は [Phase 2](#phase-2-冪等性異常系の結合テストac-05ac-06) と [02_architecture.md 6.2 節](./02_architecture.md#62-冪等性異常系の結合テストac-05ac-06)。
- 秘密非漏洩（AC-01・AC-02）: `cmd/secret_leak_integration_test.go`。詳細は [Phase 3](#phase-3-秘密非漏洩の結合テストac-01ac-02) と [02_architecture.md 6.1 節](./02_architecture.md#61-秘密非漏洩の結合テストac-01ac-02)。
- いずれも実 Bluesky API に依存せず、`atprototestutil.MockHTTPDoer` によりネットワークを遮断する（NF-002）。

### 4.3 テストヘルパー方針

- 新規のクロスパッケージヘルパー・モックは不要（既存の `atprototestutil` で足りる）。
- Phase 1 はタイムアウトの差し替えを `newRestrictedDoer`／`newTestRestrictedDoer` への引数渡しで行うため、`test_helpers.go` への追加は不要（パッケージ変数の save/restore ヘルパーを設けない）。
- Phase 2・3 の新規テストファイルは `_test.go` であり、`//go:build test` タグは付けない（テストバイナリで常にコンパイルされる。既存 `runner_integration_test.go`・`main_test.go` と同じ扱い）。

## 5. リスク管理

| リスク | 影響 | 緩和策 |
|---|---|---|
| 上限値が正当な大規模アカウントに対して過小 | 正常系で `ErrResponseTooLarge`／`ErrPaginationLimitExceeded` が誤発火 | [02_architecture.md 3.1・3.2 節](./02_architecture.md#3-コンポーネント設計) の根拠（1 ページ 100 件・数 MB、常駐メモリ数百 MiB）に沿って余裕値を設定。将来のチューニングは [02_architecture.md 9 章](./02_architecture.md#9-将来の拡張性) のとおり定数調整で対応可能 |
| AC-06 テストの非決定性（実時間依存） | フレーキーテスト | モックにテストの `cancel()` を呼ばせ、`ctx.Err()` を即座に返すことで deadline 到達を決定的に再現（[Phase 2](#phase-2-冪等性異常系の結合テストac-05ac-06)） |
| タイムアウト単体テストのゴルーチン滞留 | テストプロセスのリーク | 遅延サーバーを `t.Cleanup(server.Close)` で確実に閉じ、`xrpcRequestTimeout` を短縮して即座に発火させる |
| 秘密検出の偽陰性（短い/弱い禁止文字列が偶然マッチしない） | 実際の漏洩を検出できない | 識別性の高い固定リテラルを禁止文字列に使う（[Phase 3](#phase-3-秘密非漏洩の結合テストac-01ac-02)） |

## 6. 実装チェックリスト

- [x] PR-1 マージ済み（対象ステップ：Phase 1。`errors.go` のセンチネル 2 種、`http.go` の `maxXRPCResponseBytes`・`xrpcRequestTimeout`・超過マーカー定数、`doXRPC` の応答サイズ上限、`newRestrictedDoer` の `timeout` 引数、`posts.go` の 3 上限定数と `listAllRecords` の検査、`http_test.go`・`posts_test.go` の単体テスト追加）
- [x] PR-2 マージ済み（対象ステップ：Phase 2。`internal/atproto/idempotency_integration_test.go` を新規作成し AC-05・AC-06 を検証）
- [x] PR-3 マージ済み（対象ステップ：Phase 3。`cmd/secret_leak_integration_test.go` を新規作成し AC-01・AC-02 を検証。PR: https://github.com/isseis/bsky-cleaner/pull/62）
- [ ] PR-4 マージ済み（対象ステップ：Phase 4。本計画書 [7 章](#7-セキュリティ設計の棚卸しac-03ac-04) の棚卸し一覧・決定記録を確定）
- [x] 全体：`make fmt`・`make test`・`make lint` が緑（NF-001）

## 7. セキュリティ設計の棚卸し（AC-03・AC-04）

本章は AC-03・AC-04 の静的成果物である。列挙は [セキュリティ設計](../../design/security.md) の記載を正とする。

### 7.1 トレーサビリティ一覧（AC-03）

| # | リスクカテゴリ（security.md） | 対応する実装 | 対応するテスト |
|---|---|---|---|
| 1 | 投稿本文経由の間接的なインジェクション（メンション拡散・ANSI・ログ改行） | `internal/notify/sanitize.go`（`notify.Sanitize`）、`cmd/main.go`（stdout 出力を `notify.Sanitize` でサニタイズ） | `internal/notify/sanitize_test.go`、`cmd/main_test.go::TestRun_ApplyPartialFailure_ConsoleOutputSanitizesMaliciousRKey` |
| 2 | AT Protocol フェデレーション構造起因の SSRF | `internal/atproto/did.go`（`validatePDSEndpoint`・`checkRequestHostSafety`・`isUnsafeIP`）、`internal/atproto/http.go`（`restrictedDoer`・`newRestrictedDialContext`・`rejectRedirect`） | `internal/atproto/did_test.go`、`internal/atproto/http_test.go`（`TestRestrictedDialContext_*`・`TestCheckRedirect_AlwaysRejects`・`TestRestrictedDoer_Do`） |
| 3 | 壊れた・攻撃者管理下の PDS 起因のリソース枯渇（DoS） | **本タスク Phase 1**: `http.go`（`maxXRPCResponseBytes`・`xrpcRequestTimeout`）、`posts.go`（`maxListTotalBytes`・`maxListPages`・`maxListRecords`）。既存: `did.go`（`maxDIDResponseBytes`・`maxDIDDocumentResponseBytes`）、`retry/doer.go`（`maxDrainBytes`） | **本タスク Phase 1**: `http_test.go`・`posts_test.go`（[4.1 節](#41-単体テスト dos-系防御 phase-1)） |
| 4 | エラーメッセージ・スタックトレース経由の秘密情報漏洩 | `internal/atproto/errors.go`（`HTTPError`・`SSRFError` の秘密非包含）、`internal/notify/errorkind.go`（`errorKind` のフェイルクローズ）、`internal/notify/notify.go`（`redactWebhookURL`）、`internal/config`（`SecretString`） | **本タスク Phase 3**: `cmd/secret_leak_integration_test.go`（[6.1 節](./02_architecture.md#61-秘密非漏洩の結合テストac-01ac-02)）。既存: `internal/notify/errorkind_test.go` 等の各コンポーネント単体テスト |
| 5 | リトライ・cron 起因の自滅的アクセス制限（リトライ過多） | `internal/retry/doer.go`（`Policy`・`backoffDelay` の指数バックオフ上限）、`internal/atproto/client.go`（`defaultRetryPolicy`） | `internal/retry/doer_test.go` |
| 6 | 多重起動によるリソース枯渇 | `cmd/main.go`（`context.WithTimeout(ctx, cfg.ExecutionTimeout)` による実行タイムアウト） | `cmd/main_test.go::TestRun_ExecutionTimeoutExceeded_ReturnsExitCode1` |
| 7 | 設定ファイルの改ざん・権限 | `internal/config/validate.go`（`retention_days` 等のバリデーション）。ファイルシステム権限管理は**未実装**（[7.2 節](#72-対応可否判断ac-04)） | `internal/config` の各バリデーションテスト |
| 8 | 削除処理の冪等性 | `internal/atproto/delete.go`（`DeleteRecord` が 2xx を成功として扱う）、`internal/runner/runner.go`（個別失敗を継続して `Failed` に集約） | **本タスク Phase 2**: `internal/atproto/idempotency_integration_test.go`（[6.2 節](./02_architecture.md#62-冪等性異常系の結合テストac-05ac-06)）。既存: `internal/atproto/delete_test.go` |
| 9 | Docker イメージのサプライチェーンリスク | ベースイメージの digest ピン留めはタスク 0007（Docker 配布）の責務。本タスク時点では `Dockerfile` は未作成であり、対策の実装・検証はタスク 0007 のスコープ（本タスクの対象外） | タスク 0007 の検証（本タスクの対象外） |

> 実装列・テスト列に挙げた既存ファイルのパス・シンボルは、Phase 4 の確定時に実在を再確認する（[8 章](#8-受け入れ基準の検証) の AC-03 静的チェックで担保）。

### 7.2 対応可否判断（AC-04）

| 項目 | 判断 | 理由 |
|---|---|---|
| `doXRPC` の応答サイズ上限 | **本タスクで対応** | security.md が未対策と明記。Phase 1 で `maxXRPCResponseBytes` を実装。0002 へ差し戻さない（AC-04） |
| `listAllRecords` の総ページ／総レコード上限 | **本タスクで対応** | 同上。Phase 1 で `maxListTotalBytes`・`maxListPages`・`maxListRecords` を実装。0002 へ差し戻さない |
| XRPC `http.Client` のリクエスト全体タイムアウト | **本タスクで対応** | 同上。Phase 1 で `xrpcRequestTimeout` を実装。0005 へ差し戻さない |
| DID 解決経路（`http.DefaultClient`）の独立タイムアウト | **本タスク非対応** | AC-04 が名指しするのは XRPC 経路の `http.Client`。DID 解決経路は `did.go` の応答サイズ上限と実行タイムアウトの deadline 伝播でハングが有界に収まる（[02_architecture.md 3.3 節の残余注記](./02_architecture.md#33-リクエスト全体タイムアウト restricteddoer)）。将来対応は [02_architecture.md 9 章](./02_architecture.md#9-将来の拡張性) |
| 設定ファイルのファイルシステム権限管理・改ざん検知 | **本タスク非対応** | 要件定義書 Out of Scope。初期リリースの完了の定義に含まれない（[01_requirements.md 2・5 章](./01_requirements.md#2-スコープ)）。棚卸しのみ |
| Slack Webhook ホスト検証 | **対応済み（差し戻し不要）** | `slack_allowed_host` 許可リスト導入済み（[02_architecture.md 付録](./02_architecture.md#付録 - 決定履歴)） |

## 8. 受け入れ基準の検証

各 AC を `test`（実行可能・誤動作で失敗）／`static`（rg/grep/コンパイル）／`manual` に分類する。テスト関数名は実装時に確定する暫定名（本計画のフェーズ記述に対応）であり、実装後に実名へ更新する。

| AC | 種別 | 検証内容 |
|---|---|---|
| AC-01 | test | `cmd/secret_leak_integration_test.go::TestRun_SecretNonLeak_HappyPath`（dry-run・apply の両モード）。stdout・stderr・Slack ペイロードの各面に禁止文字列集合の要素が現れないことを表明 |
| AC-02 | test | `cmd/secret_leak_integration_test.go` の (a)〜(f) 各テスト（`..._AuthFailure` / `..._NetworkError` / `..._DIDResolutionError` / `..._DeleteFailure` / `..._SlackSendFailure` / `..._ExecutionTimeout`）。各失敗経路で AC-01 と同じ非漏洩表明 |
| AC-03 | static | 本計画書 [7.1 節](#71-トレーサビリティ一覧ac-03) に 9 カテゴリが揃うこと。検証コマンド：`rg -c '^\| [0-9]' docs/tasks/0008_security_hardening/03_implementation_plan.md` が [7.1 節](#71-トレーサビリティ一覧ac-03) の行数 9 を含む区間で 9 を返す。加えて `rg -n -e '投稿本文' -e 'SSRF' -e 'DoS' -e '秘密情報漏洩' -e 'リトライ過多' -e '多重起動' -e '設定ファイルの改ざん' -e '削除処理の冪等性' -e 'サプライチェーン' docs/tasks/0008_security_hardening/03_implementation_plan.md` が全 9 語にマッチ（security.md の全カテゴリが記載されていること） |
| AC-04 | static | 本計画書 [7.2 節](#72-対応可否判断ac-04) に DoS 系 3 項目の「本タスクで対応」、DID 解決経路の独立タイムアウトとファイルシステム権限管理の「本タスク非対応」＋理由が揃うこと。検証コマンド：`rg -n -e 'maxXRPCResponseBytes' -e 'maxListPages' -e 'xrpcRequestTimeout' -e 'DID 解決経路' -e 'ファイルシステム権限' docs/tasks/0008_security_hardening/03_implementation_plan.md` が [7.2 節](#72-対応可否判断ac-04) の各行にマッチ |
| AC-05 | test | `internal/atproto/idempotency_integration_test.go::TestRunnerRun_AllTargetsAlreadyDeleted_TreatedAsSuccess`（複数の既削除対象がすべて `Deleted` に入り `Failed` が空になることを表明）。基本経路の 2xx→成功は既存 `internal/atproto/runner_integration_test.go::TestRunnerRun_WithRealAtprotoClient` も担保する |
| AC-06 | test | `internal/atproto/idempotency_integration_test.go::TestRunnerRun_CancelMidDelete_RemainingFailedThenReRunSafe`。中断後に `Deleted`（N 件）と `Failed`（残り）が判別可能で、2 回目実行が全件 2xx で `Failed` 空になることを表明 |

> AC-04 の DoS 系実装そのもの（`maxXRPCResponseBytes` 等が実際に発火する挙動）は Phase 1 の単体テスト（[4.1 節](#41-単体テスト dos-系防御 phase-1)）が `test` として担保する。[7.2 節](#72-対応可否判断ac-04) の `static` チェックは「判断が計画書に記録されていること」を担保する補完である。

## 9. 成功基準

- AC-01〜AC-06 が [8 章](#8-受け入れ基準の検証) の `test`／`static` で緑（[01_requirements.md 6 章](./01_requirements.md#6-成功基準要約)）。
- [セキュリティ設計](../../design/security.md) の全リスクカテゴリが [7.1 節](#71-トレーサビリティ一覧ac-03) でトレース可能。
- `make fmt`・`make test`・`make lint` が緑（NF-001）、Go 1.26.2 以上でビルド可能（NF-003）。
- 結合テストが実 Bluesky API に依存しない（NF-002）。

## 10. 横断検索チェックリスト

`make lint`・`make test` が検出しない項目のみを挙げる（追加中心のため最小限）。

- [x] 新規センチネル名（`ErrResponseTooLarge`・`ErrPaginationLimitExceeded`）が既存名と衝突しないこと：`rg -n 'ErrResponseTooLarge|ErrPaginationLimitExceeded' internal/` が `errors.go` の定義と利用箇所のみを返す。
- [x] 超過マーカー文字列（例 `"ResponseTooLarge"`）が `errorKind` の出力と整合すること（[02_architecture.md 4.2 節](./02_architecture.md#42-errorkind-との連携と可観測性)）。`errorkind.go` は `HTTPError.ErrorName` を `error=%s` として汎用に埋め込むだけで、マーカー文字列リテラルを持たない（`rg -n 'ResponseTooLarge' internal/notify/` は 0 件が正しい）。したがって検証は `rg` の突き合わせではなく、`http.go` が設定するマーカー値が `errorkind.go:33` の `error=%s` 経由で `"atproto http error: <method> status=200 error=ResponseTooLarge"` として現れることを目視確認する（マーカー定義箇所は `rg -n 'ResponseTooLarge' internal/atproto/` で 1 件確認）。

## 11. 次のステップ

- すべてのフェーズが完了した。PR-4 を作成しマージする。
