# DNS TXT によるハンドル解決 — 実装計画書

## Document Status

| Item | Value |
|---|---|
| Status | `approved` |
| Created | 2026-07-07 |
| Review date | 2026-07-07 |
| Reviewer | isseis |
| Comments | - |

## 1. 実装概要

### 1.1 目的

[02_architecture.md](02_architecture.md) で承認された設計に基づき、`internal/atproto` にハンドル解決の DNS TXT 方式（F-001）と、DNS TXT → HTTPS well-known の優先順位・フォールバック制御（F-002）、DNS 応答の妥当性検証（F-003）を実装する。既存の HTTPS well-known 方式の挙動（成功パス・エラーパス双方）は変更しない。

### 1.2 実装原則

- 新規ロジックはすべて既存パッケージ `internal/atproto` の既存ファイル `did.go`/`errors.go`/`client.go`/`test_helpers.go` に追記する（[02_architecture.md](02_architecture.md) 2.1 節、新規パッケージは起こさない）。
- DNS リゾルバ抽象は既存の `lookupIPAddr` と同じパターン（本番はパッケージ変数に標準ライブラリの関数値を束縛し、テストはその変数を差し替える）を踏襲する（同 3.1 節）。
- `resolveHandleToDID`（HTTPS well-known 方式）はシグネチャ・実装とも変更しない。
- 各フェーズの完了条件として `make fmt` → `make test` → `make lint` を実行する。

### 1.3 既存コード調査結果

**`internal/atproto/did.go`**
- 既存: `resolveHandleToDID`（HTTPS well-known 方式、42-76 行目）、`invalidHandleChars`（29-34 行目）、`lookupIPAddr` パッケージ変数（179-184 行目、`net.DefaultResolver.LookupIPAddr` を束縛する既存パターン）。いずれも変更不要。`resolveHandleToDIDViaDNS`/`resolveHandle` はこのファイルに追記する。
- 追加: `txtLookuper` インターフェース、`lookupTXT` パッケージ変数、`dnsTXTLookupTimeout` 定数、`resolveHandleToDIDViaDNS`、`resolveHandle`（[02_architecture.md](02_architecture.md) 3.1/3.2 節）。

**`internal/atproto/errors.go`**
- 既存: `ErrDIDResolutionFailed`（12 行目）。両方式失敗時に引き続き使う（変更不要）。
- 追加: `ErrDNSHandleResolutionFailed`（DNS TXT 方式の解決失敗を示すセンチネルエラー）。

**`internal/atproto/client.go`**
- 既存: `NewClient`（62-93 行目）が `resolveHandleToDID(ctx, didResolutionDoer, handle)` を直接呼んでいる（65 行目）。
- 変更: この呼び出しを `resolveHandle(ctx, didResolutionDoer, handle)` に置き換える（引数リストは変わらない、[02_architecture.md](02_architecture.md) 3.1 節の通り新しい引数を配線する必要はない）。

**`internal/atproto/test_helpers.go`**
- 既存: `StubPassthroughPDSDoer`（27-48 行目、`newPDSDoer` パッケージ変数を差し替えるエクスポート済みテストヘルパーのパターン）。
- 追加: 同じパターンで `lookupTXT` を「レコード 0 件」のフェイクに差し替える `StubDNSTXTLookup(t *testing.T)` を追加する。

**既存テストへの影響（`NewClient` 経由で DNS TXT 方式が先に試行されるようになる箇所）**

[02_architecture.md](02_architecture.md) 3.3 節は影響対象として `client_test.go`・`runner_integration_test.go`・`idempotency_integration_test.go` の 3 ファイルを挙げているが、実際に `NewClient` を呼び出す（＝新設の DNS TXT 問い合わせを踏む）テストを網羅的に洗い出すと、以下の通りさらに広い範囲に及ぶ。対応しない場合、テスト実行時に `lookupTXT`（本番は `net.DefaultResolver`）が実際の DNS へ問い合わせてしまい、CI 環境でのハング・ネットワーク依存による不安定化を招く。

- `internal/atproto/did_test.go`（`package atproto`）: `stubSymbolicHostLookup`（29-39 行目）を呼んでいる `NewClient` 経由テストは 3 件 —  `TestNewClient_ResolvesHandleToDIDAndPDSEndpoint`（65 行目）、`TestNewClient_DIDResolutionFailure`（134 行目）、`TestNewClient_RejectsUntrustedHost_NoFurtherRequest`（149 行目）。
- `internal/atproto/client_test.go`（`package atproto`）: `stubSymbolicHostLookup` を呼んでいる `TestNewClient_WrapsHTTPDoerWithRetry`（20 行目）。
- `internal/atproto/runner_integration_test.go`（`package atproto_test`）: `TestRunnerRun_WithRealAtprotoClient`（45 行目）。
- `internal/atproto/idempotency_integration_test.go`（`package atproto_test`）: `TestRunnerRun_AllTargetsAlreadyDeleted_TreatedAsSuccess`（32 行目、`NewClient` 呼び出しは 78 行目）、`TestRunnerRun_CancelMidDelete_RemainingFailedThenReRunSafe`（116 行目、`NewClient` 呼び出しは 177 行目・245 行目の 2 回だが同一テスト内であり、スタブ 1 回で両方カバーされる）。
- `cmd/main_test.go`（`package main`）: `setEnvCredentials`（63-69 行目）を呼んでいるテストは 13 件 — `TestRun_ClientInitFailure_ReturnsExitCode1`（170 行目）、`TestRun_ExecutionTimeoutExceeded_ReturnsExitCode1`（200 行目）、`TestRun_LoginFailure_ReturnsExitCode1`（220 行目）、`TestRun_LoginFailure_StderrSanitizesMaliciousErrorName`（248 行目）、`TestRun_DryRunWithTargets_ReturnsExitCode0AndPrintsTargets`（319 行目）、`TestRun_DryRunNoTargets_ReturnsExitCode0AndPrintsNoTargetsMessage`（336 行目）、`TestRun_ApplyAllSucceed_ReturnsExitCode0AndPrintsResult`（409 行目）、`TestRun_ApplyPartialFailure_ReturnsExitCode3AndPrintsFailures`（427 行目）、`TestRun_ApplyLoginFailure_SendsFailureNotification`（471 行目）、`TestRun_Apply_SlackNotifyFails_ExitCodeUnaffected`（491 行目）、`TestRun_Apply_SlackNotifyFails_StderrContainsMaskedFailureMessage`（509 行目）、`TestRun_Apply_NoWebhookConfigured_SkipsNotifyWithoutError`（525 行目）、`TestRun_ApplyPartialFailure_ConsoleOutputSanitizesMaliciousRKey`（758 行目）。このうち `TestRun_ClientInitFailure_ReturnsExitCode1` は `StubPassthroughPDSDoer` を呼んでいない（HTTPS well-known 側で 404 を返し `NewClient` がそこで失敗するため）が、DNS TXT 方式は `NewClient` 内で HTTPS より先に試行されるため、この呼び出しだけは避けられず同様に対応が必要である。
- `cmd/secret_leak_integration_test.go`（`package main`）: `setupSecretLeakEnv`（38-44 行目）を呼んでいるテストは 8 件すべて — `TestRun_SecretNonLeak_HappyPath_DryRun`（139 行目）、`TestRun_SecretNonLeak_HappyPath_Apply`（155 行目）、`TestRun_SecretNonLeak_AuthFailure`（176 行目）、`TestRun_SecretNonLeak_NetworkError`（201 行目）、`TestRun_SecretNonLeak_DIDResolutionError`（220 行目）、`TestRun_SecretNonLeak_DeleteFailure`（240 行目）、`TestRun_SecretNonLeak_SlackSendFailure`（259 行目）、`TestRun_SecretNonLeak_ExecutionTimeout`（279 行目）。

**対応方針（DRY — 個々のテスト関数を編集せず、共通セットアップ箇所へ集約する）**: 上記はいずれも少数の共通セットアップ関数の呼び出し元であるため、各テスト関数を個別に編集する代わりに、次の 6 箇所を編集することで全 28 件のテスト関数をカバーする。
1. `internal/atproto/did_test.go` の `stubSymbolicHostLookup`（29-39 行目）内で `StubDNSTXTLookup(t)` を呼ぶ — `did_test.go` の 3 件 + `client_test.go` の 1 件（計 4 件）をカバー。
2. `internal/atproto/runner_integration_test.go` の `TestRunnerRun_WithRealAtprotoClient`（46 行目 `atproto.StubPassthroughPDSDoer(t)` の直後）に `atproto.StubDNSTXTLookup(t)` を追加。
3. `internal/atproto/idempotency_integration_test.go` の `TestRunnerRun_AllTargetsAlreadyDeleted_TreatedAsSuccess`（33 行目）に同様に追加。
4. 同ファイルの `TestRunnerRun_CancelMidDelete_RemainingFailedThenReRunSafe`（117 行目）に同様に追加。
5. `cmd/main_test.go` の `setEnvCredentials`（63-69 行目）内で `atproto.StubDNSTXTLookup(t)` を呼ぶ — 13 件をカバー。
6. `cmd/secret_leak_integration_test.go` の `setupSecretLeakEnv`（38-44 行目）内で `atproto.StubDNSTXTLookup(t)` を呼ぶ — 8 件をカバー。

いずれのファイルも `atproto` パッケージを既にインポート済みであり、新規 import 追加は不要。

## 2. 実装ステップ

### Phase 1: エラー型の追加

**対象ファイル**: `internal/atproto/errors.go`

- [x] `ErrDNSHandleResolutionFailed`（センチネルエラー、[02_architecture.md](02_architecture.md) 4.1 節のコード例のとおり）を、既存の `var (...)` ブロック（`ErrDIDResolutionFailed` などが定義されている 11-25 行目）に追加する。

**成功基準**: `make fmt && make test && make lint` が成功する。

### Phase 2: DNS TXT 方式の解決関数の追加

**対象ファイル**: `internal/atproto/did.go`、`internal/atproto/did_test.go`

- [x] `txtLookuper` インターフェース（`LookupTXT(ctx context.Context, name string) ([]string, error)`）を追加する。
- [x] `lookupTXT` パッケージ変数（`net.DefaultResolver` を束縛、既存の `lookupIPAddr` と同じパターン）を追加する。
- [x] `dnsTXTLookupTimeout = 3 * time.Second` 定数を追加する（`did.go` に `time` の import 追加が必要）。
- [x] `resolveHandleToDIDViaDNS(ctx context.Context, handle string) (string, error)` を追加する。実装内容（[02_architecture.md](02_architecture.md) 3.1 節）:
  - `context.WithTimeout(ctx, dnsTXTLookupTimeout)` で親 ctx から派生させたタイムアウト付き ctx を使う。
  - `lookupTXT.LookupTXT(ctx, "_atproto."+handle)` を呼ぶ。
  - 返ってきたレコードのうち `did=` プレフィックスを持ち、かつその値が `did:` で始まる（DID として妥当な形式である）ものだけを候補とする（AC-01, AC-03）。この DID 形式チェックは新設のパッケージ関数 `looksLikeDID(s string) bool`（`strings.HasPrefix(s, "did:")`）に切り出し、既存の `resolveHandleToDID`（HTTPS well-known 方式、`did.go:72` 付近）が行っていた同等のインラインチェックもこのヘルパー呼び出しに置き換える（DRY、レビュー指摘対応: `did=` の値そのものを検証せず受理すると HTTPS 側と挙動が非対称になるため）。
  - 候補が 0 件・複数件・リゾルバエラーのいずれの場合も `ErrDNSHandleResolutionFailed` でラップして返す（AC-02, AC-07, AC-08）。リゾルバエラーは `%w` で元のエラーを保持する（AC-08、`errors.AsType[*net.DNSError]` 等で判別可能にする）。
  - 候補が一意なら DID を返す（AC-01）。
- [x] `did_test.go` に `fakeTXTLookuper`（`records []string`・`err error` を保持し `txtLookuper` を満たすフェイク、`testing` パッケージへの依存を持たない単純な構造体）を追加する。ネットワーク I/O を伴わないテスト容易性のための最小実装とする（NF-003）。
- [x] `TestResolveHandleToDIDViaDNS_SingleDIDRecord_Success`: `did=did:plc:xxxx` が 1 件 → 成功（AC-01）。
- [x] `TestResolveHandleToDIDViaDNS_NoRecords_ReturnsDNSHandleResolutionFailed`: レコード 0 件 → `ErrDNSHandleResolutionFailed`（AC-02）。
- [x] `TestResolveHandleToDIDViaDNS_RecordsWithoutDIDPrefix_Ignored`: `did=` プレフィックスを持たないレコードのみ → 無視されて解決失敗（AC-03）。
- [x] `TestResolveHandleToDIDViaDNS_MalformedDIDValue_Ignored`（レビュー指摘対応で追加）: `did=` プレフィックスは持つが値が `did:` で始まらない（`did=notadid`・`did=` 空値）レコードのみ → 無視されて解決失敗扱い（AC-01 の「`did=did:plc:xxxx` 形式」という定義に対する境界値、AC-03 の趣旨の延長）。
- [x] `TestResolveHandleToDIDViaDNS_MultipleDIDRecords_ReturnsDNSHandleResolutionFailed`: `did=` プレフィックスを持つレコードが複数件 → `ErrDNSHandleResolutionFailed`（AC-07）。
- [x] `TestResolveHandleToDIDViaDNS_ResolverError_ReturnsTypedError`: フェイクの `err` フィールドに `&net.DNSError{Err: "no such host", Name: "_atproto.alice.test", IsNotFound: true}` のような具体的な `*net.DNSError` 値を設定し、戻り値エラーに対して `errors.Is(err, ErrDNSHandleResolutionFailed)` が真であることと、`errors.AsType[*net.DNSError](err)` で元の `*net.DNSError` が実際に取り出せることの両方を確認する（AC-08）。単なる `errors.New(...)` を使うと `AsType` による型抽出側の検証にならないため、必ず具体的な型を持つエラーを使う。

**成功基準**: `make fmt && make test && make lint` が成功し、上記5テストすべてが成功する。

### PR-1 作成ポイント: DNS TXT resolution primitive

**対象ステップ**: Phase 1 / Phase 2

**推奨タイトル**: `feat(0009-dns-handle-resolution): add DNS TXT handle resolution primitive`

**レビュー観点**: `did=` プレフィックス抽出とレコード0件/複数件/リゾルバエラーの fail-closed 判定（AC-02/AC-03/AC-07）が正しいか / `errors.AsType[*net.DNSError]` で元のリゾルバエラーが取り出せるか（AC-08） / この時点で `resolveHandleToDIDViaDNS` は `NewClient` からまだ呼ばれておらず、既存の HTTPS well-known 経路の挙動に影響しないこと

（注: `make deadcode` はこの PR 単体では `resolveHandleToDIDViaDNS`/`ErrDNSHandleResolutionFailed` を「未使用」として検出しうる — `_test.go` からのみ参照され、本番コードから呼ばれるのは PR-3 の Phase 5 でのため。これは意図した一時的な状態であり、この PR で対処すべき不具合ではない。）

- [x] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [ ] PR を作成した
- [ ] PR がマージされた
- [ ] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

### Phase 3: 優先順位・フォールバック制御の追加

**対象ファイル**: `internal/atproto/did.go`、`internal/atproto/did_test.go`

- [ ] `resolveHandle(ctx context.Context, httpDoer HTTPDoer, handle string) (string, error)` を追加する。ログ出力に `slog.Default()` を使うため、`did.go` に `"log/slog"` の import 追加が必要（Phase 2 の `time` 追加と同様）。実装内容（[02_architecture.md](02_architecture.md) 3.1/4.3 節、処理フローは同 6 章のシーケンス図参照）:
  - 既存の `invalidHandleChars` チェックを一度だけ適用する（DNS・HTTPS いずれの方式を試す前）。なお、フォールバック時に呼び出す `resolveHandleToDID` 自身もこのチェックを内部で再度行う（`did.go:43`）ため、フォールバック経路では同じ検証が2回実行される。ただし検証自体は冪等であり、かつハンドルは1回目のチェックで既に検証済みであるため、2回目の判定結果は常に「不正文字なし（false）」となる。意図した軽微な重複であり、修正不要。
  - `resolveHandleToDIDViaDNS` を試行し、成功すれば `slog.Default().Info("resolved handle via DNS TXT", "handle", handle)` を出力して即座に DID を返す（HTTPS への問い合わせは行わない、AC-04）。
  - `resolveHandleToDIDViaDNS` が失敗すれば `slog.Default().Warn("DNS TXT handle resolution failed, falling back to HTTPS well-known", "handle", handle, "error", <DNS 側のエラー>)` を出力し、`resolveHandleToDID(ctx, httpDoer, handle)` にフォールバックする（AC-05）。
  - HTTPS 側も失敗すれば、DNS 側・HTTPS 側それぞれの失敗理由を `errors.Join` で保持しつつ `ErrDIDResolutionFailed` でラップして返す（AC-06、[02_architecture.md](02_architecture.md) 4.1 節）。
- [ ] `TestResolveHandle_DNSSucceeds_DoesNotCallHTTPS`: DNS TXT 方式が成功する場合、フェイク `httpDoer`（`atprototestutil.MockHTTPDoer`）への呼び出し回数が 0 であることを `CallCount()` で確認する（AC-04）。
- [ ] `TestResolveHandle_DNSFails_FallsBackToHTTPS`: DNS TXT 方式が失敗する場合に HTTPS well-known 方式へフォールバックし、成功パス・エラーパス双方で既存の [0002_atproto_client](../0002_atproto_client/01_requirements.md) AC-01〜AC-03 相当の挙動（`did_test.go` の `TestNewClient_ResolvesHandleToDIDAndPDSEndpoint`・`TestNewClient_DIDResolutionFailure` が検証済みの範囲と同じ判断基準）が維持されることを確認する（AC-05）。
- [ ] `TestResolveHandle_BothFail_ReturnsErrDIDResolutionFailed`: DNS TXT・HTTPS 双方が失敗する場合、`errors.Is(err, ErrDIDResolutionFailed)` が真になり処理が中断されることを確認する（AC-06）。

**成功基準**: `make fmt && make test && make lint` が成功し、上記3テストすべてが成功する。

### PR-2 作成ポイント: priority and fallback orchestration

**対象ステップ**: Phase 3

**推奨タイトル**: `feat(0009-dns-handle-resolution): add DNS-then-HTTPS handle resolution fallback`

**レビュー観点**: `invalidHandleChars` チェックが DNS・HTTPS 双方を試す前に一度だけ適用されているか（2回目の重複チェックが意図通り無害であることの理解を含む） / DNS 成功時に HTTPS へ一切問い合わせないこと（AC-04、`CallCount()` アサーションの妥当性） / 両方式失敗時に `errors.Join` で双方の失敗理由が保持され `ErrDIDResolutionFailed` として判別可能であること（AC-06） / ログ出力（`slog.Info`/`slog.Warn`）の内容がオンコール調査に十分な情報（`handle`、失敗理由）を含むか

（注: `make deadcode` はこの PR 単体では `resolveHandle` を「未使用」として検出しうる — `_test.go` からのみ参照され、`NewClient` から呼ばれるのは PR-3 の Phase 5 でのため。これは意図した一時的な状態であり、この PR で対処すべき不具合ではない。）

- [ ] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [ ] PR を作成した
- [ ] PR がマージされた
- [ ] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

### Phase 4: テストヘルパーの追加

**対象ファイル**: `internal/atproto/test_helpers.go`

- [ ] `StubDNSTXTLookup(t *testing.T)` を追加する。`StubPassthroughPDSDoer`（27-48 行目）と同じパターン（`prev := lookupTXT` → `t.Cleanup` で復元 → `lookupTXT` を「常に空レコードを返すフェイク」に差し替え）で実装する。`atproto_test` などの外部パッケージからも呼べるようエクスポートする。

**成功基準**: `make fmt && make test && make lint` が成功する。`test_helpers.go` は `//go:build test` タグ付きファイルであり、通常の `make build`（`go build ./cmd`、`-tags test` なし）ではコンパイル対象に入らないが、`make test`（`go test -tags test ./...`）は `-tags test` を指定するため、コンパイル確認も兼ねる。

### Phase 5: `NewClient` の切り替えと既存テストの回帰対応

**対象ファイル**: `internal/atproto/client.go`、`internal/atproto/did_test.go`、`internal/atproto/client_test.go`、`internal/atproto/runner_integration_test.go`、`internal/atproto/idempotency_integration_test.go`、`cmd/main_test.go`、`cmd/secret_leak_integration_test.go`

- [ ] `client.go` の `NewClient`（65 行目）で `resolveHandleToDID(ctx, didResolutionDoer, handle)` の呼び出しを `resolveHandle(ctx, didResolutionDoer, handle)` に置き換える。
- [ ] `internal/atproto/did_test.go` の `stubSymbolicHostLookup`（29-39 行目）内に `StubDNSTXTLookup(t)` の呼び出しを追加する（1.3 節の対応方針 1）。
- [ ] `internal/atproto/runner_integration_test.go` の `TestRunnerRun_WithRealAtprotoClient`（46 行目付近）に `atproto.StubDNSTXTLookup(t)` の呼び出しを追加する（同 2）。
- [ ] `internal/atproto/idempotency_integration_test.go` の `TestRunnerRun_AllTargetsAlreadyDeleted_TreatedAsSuccess`（33 行目付近）に `atproto.StubDNSTXTLookup(t)` の呼び出しを追加する（同 3）。
- [ ] 同ファイルの `TestRunnerRun_CancelMidDelete_RemainingFailedThenReRunSafe`（117 行目付近）に `atproto.StubDNSTXTLookup(t)` の呼び出しを追加する（同 4）。
- [ ] `cmd/main_test.go` の `setEnvCredentials`（63-69 行目）内に `atproto.StubDNSTXTLookup(t)` の呼び出しを追加する（同 5）。
- [ ] `cmd/secret_leak_integration_test.go` の `setupSecretLeakEnv`（38-44 行目）内に `atproto.StubDNSTXTLookup(t)` の呼び出しを追加する（同 6）。
- [ ] `did_test.go` に `TestNewClient_DNSTXTSuccess_StillGoesThroughDownstreamPipeline` を追加する（AC-09 の動的検証）。この 1 件だけは他の `NewClient` テストと異なり `stubSymbolicHostLookup`/`StubDNSTXTLookup` の「レコード 0 件」フェイクを使わず、`lookupTXT` を直接 `did=did:plc:test123` を1件返すフェイクに差し替える。`atprototestutil.MockHTTPDoer` は `.well-known/atproto-did`（HTTPS 側の呼び出し）へのリクエストが来たら `t.Fatalf` で失敗させ、`plc.directory/did:plc:test123` への `resolveDIDDocument` リクエストと、その先の `validatePDSEndpoint` が使う DNS（`stubSymbolicHostLookup` 相当の `lookupIPAddr` スタブ）にのみ応答する。これにより、DNS TXT 方式で得た DID が実際に既存の `resolveDIDDocument`/`validatePDSEndpoint` を経由し、`NewClient` が成功裏に `*Client` を構築することを直接確認する（AC-09 の「迂回経路が生まれない」ことのふるまいレベルでの証明。静的検証だけでは、この経路が実際に最後まで動作することまでは確認できない）。
- [ ] `make test` を実行し、1.3 節で列挙した 28 件のテストを含む全テストが実ネットワーク I/O なしで成功することを確認する（回帰確認、AC-05 の「既存挙動を壊さない」ことの直接検証）。

**成功基準**: `make fmt && make test && make lint` が成功する。`go test -tags test -run 'TestNewClient|TestRunnerRun|TestRun_' ./... -v` の出力で DNS 関連のネットワークエラー・タイムアウトが発生しないことを確認する。

### PR-3 作成ポイント: NewClient cutover and regression coverage

**対象ステップ**: Phase 4 / Phase 5

**推奨タイトル**: `feat(0009-dns-handle-resolution): wire DNS TXT resolution into NewClient`

**レビュー観点**: `StubDNSTXTLookup` を注入した6箇所（1.3 節の対応方針1〜6）が過不足なく全 28 件の `NewClient` 経由テストをカバーしているか / `client.go` の変更が「`resolveHandleToDID` 呼び出しを `resolveHandle` に置き換えるだけ」に留まり、`resolveDIDDocument`/`validatePDSEndpoint` 以降の経路に新しい分岐を持ち込んでいないこと（AC-09、`rg -n "ViaDNS" internal/atproto/client.go` がマッチなしであること） / `TestNewClient_DNSTXTSuccess_StillGoesThroughDownstreamPipeline` が HTTPS well-known 側へのリクエストを `t.Fatalf` で確実に検知する構成になっているか / 全既存テストが実 DNS I/O なしで成功し CI でハングしないこと

（注: この PR は本タスクで最大の diff になる。PR 説明では、1.3 節の対応方針1〜6にあたる「機械的な `StubDNSTXTLookup(t)` 呼び出し追加」6箇所と、「実質的な変更」（`client.go` の1行差し替え、および新規追加の `TestNewClient_DNSTXTSuccess_StillGoesThroughDownstreamPipeline`）を明示的に区別して記載し、レビュアーが後者に注意を集中できるようにする。）

- [ ] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [ ] PR を作成した
- [ ] PR がマージされた
- [ ] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

## 3. 実装順序とマイルストーン

### 3.1 マイルストーン

[02_architecture.md](02_architecture.md) 8 章の実装優先順位（Phase 1〜5）をそのまま採用する。各フェーズは前フェーズの成果物に依存するため、次の順序で直列に進める。

| マイルストーン | 内容 | 完了条件 |
|---|---|---|
| M1 | Phase 1 完了 | `ErrDNSHandleResolutionFailed` が定義され、ビルドが通る |
| M2 | Phase 2 完了 | `resolveHandleToDIDViaDNS` が単体で AC-01/02/03/07/08 を満たす |
| M3 | Phase 3 完了 | `resolveHandle` が単体で AC-04/05/06 を満たす |
| M4 | Phase 4 完了 | `StubDNSTXTLookup` が実装される |
| M5 | Phase 5 完了 | `NewClient` が新経路に切り替わり、既存テスト全件が回帰なく成功する（機能完成） |

### 3.2 PR 構成

| PR | 対象ステップ | 主な変更内容 |
|---|---|---|
| PR-1 | Phase 1 / Phase 2 | `ErrDNSHandleResolutionFailed` センチネルエラー、`txtLookuper`/`lookupTXT`/`dnsTXTLookupTimeout`、`resolveHandleToDIDViaDNS`（DNS TXT 方式単体、ユニットテスト5件） |
| PR-2 | Phase 3 | `resolveHandle`（DNS→HTTPS 優先順位・フォールバック制御、ログ出力、ユニットテスト3件） |
| PR-3 | Phase 4 / Phase 5 | `StubDNSTXTLookup` テストヘルパー、`NewClient` の切り替え、既存テスト6箇所への注入、`TestNewClient_DNSTXTSuccess_StillGoesThroughDownstreamPipeline`（AC-09 動的検証）、全テスト回帰確認 |

## 4. テスト戦略

- **ユニットテスト**: Phase 2/3 で追加する `TestResolveHandleToDIDViaDNS_*`（5件）・`TestResolveHandle_*`（3件）が、ネットワーク I/O なしで DNS TXT 方式・優先順位/フォールバック制御のすべての分岐を検証する（[02_architecture.md](02_architecture.md) 7.1 節）。
- **統合テスト**: 既存の `TestNewClient_*`（`did_test.go`/`client_test.go`）・`TestRunnerRun_*`（`runner_integration_test.go`/`idempotency_integration_test.go`）・`TestRun_*`（`cmd/main_test.go`/`cmd/secret_leak_integration_test.go`）が、`StubDNSTXTLookup` 注入後も回帰なく成功することを Phase 5 で確認する（同 7.2 節）。新規の統合テストは追加しない — 既存カバレッジで十分であり、重複したテストを避ける（DRY）。
- **セキュリティテスト**: AC-09 は静的検証（`rg` によるコード構造チェック）と動的検証（Phase 5 の `TestNewClient_DNSTXTSuccess_StillGoesThroughDownstreamPipeline`）の両方で確認する（本書 7 節）。fail-closed 挙動（複数 `did=` レコード、AC-07）は Phase 2 のユニットテストで動的に検証済み（同 7.3 節）。
- **後方互換性テスト**: `TestResolveHandle_DNSFails_FallsBackToHTTPS`（Phase 3）が、DNS TXT 方式導入後も既存の HTTPS well-known 方式の成功パス・エラーパスが変わらないことを直接検証する。

## 5. リスク管理

| リスク | 内容 | 緩和策 |
|---|---|---|
| テストの実 DNS 依存化 | `StubDNSTXTLookup` の注入漏れがあると、該当テストが CI 環境で実際の DNS へ問い合わせてしまい、ハング・不安定化を招く | 1.3 節で対象 28 件のテスト関数を全数洗い出し済み。Phase 5 の完了条件で `go test -tags test ./...` を実行し、DNS 起因のタイムアウト・エラーが発生しないことを確認する |
| フォールバックによるレイテンシ増加 | DNS TXT 方式単体にはリトライがなく、一過性の DNS 障害時は HTTPS フォールバック分の追加リトライ待ち（最大約31秒）が発生しうる（[02_architecture.md](02_architecture.md) 5.3 節） | 設計時点で許容されたトレードオフであり、本タスクでの追加対応は不要（将来必要になった場合は9章「将来の拡張性」を参照） |
| `net.Resolver` の cgo/pure Go 実装差異 | ビルド・実行環境によって `net.Resolver` の内部実装が変わりうる（[02_architecture.md](02_architecture.md) 5.4 節） | 本タスクのスコープ外の既知リスクとして受け入れる。実装中に新たな問題が判明した場合はアーキテクチャ設計書の更新を検討する |

## 6. 実装チェックリスト

- [ ] PR-1 マージ済み（対象ステップ: Phase 1 / Phase 2。`ErrDNSHandleResolutionFailed`、`txtLookuper`/`lookupTXT`/`dnsTXTLookupTimeout`/`resolveHandleToDIDViaDNS` 追加、ユニットテスト5件追加）
- [ ] PR-2 マージ済み（対象ステップ: Phase 3。`resolveHandle` 追加、ユニットテスト3件追加）
- [ ] PR-3 マージ済み（対象ステップ: Phase 4 / Phase 5。`StubDNSTXTLookup` 追加、`NewClient` 切り替え、既存テスト6箇所への注入、`TestNewClient_DNSTXTSuccess_StillGoesThroughDownstreamPipeline` 追加（AC-09 動的検証）、全テスト回帰確認）
- [ ] `make fmt && make test && make lint` が最終的に成功する
- [ ] `make deadcode` で新規追加コードに未使用箇所がないことを確認する

## 7. 受け入れ基準の検証

**AC-01: `did=did:plc:xxxx` 形式の TXT レコードが1件存在する場合、その DID を解決結果として取得できる**
- 種別: `test`
- テスト: `internal/atproto/did_test.go::TestResolveHandleToDIDViaDNS_SingleDIDRecord_Success`
- 実装: `internal/atproto/did.go`（`resolveHandleToDIDViaDNS`、Phase 2）

**AC-02: TXT レコードが存在しない場合、DNS TXT 方式の解決失敗として扱われる**
- 種別: `test`
- テスト: `internal/atproto/did_test.go::TestResolveHandleToDIDViaDNS_NoRecords_ReturnsDNSHandleResolutionFailed`
- 実装: `internal/atproto/did.go`（`resolveHandleToDIDViaDNS`、Phase 2）

**AC-03: `did=` プレフィックスを持たない TXT レコードは無視される**
- 種別: `test`
- テスト: `internal/atproto/did_test.go::TestResolveHandleToDIDViaDNS_RecordsWithoutDIDPrefix_Ignored`
- 実装: `internal/atproto/did.go`（`resolveHandleToDIDViaDNS`、Phase 2）

**AC-04: DNS TXT 方式で一意な DID が解決できた場合、HTTPS well-known 方式への問い合わせは行わない**
- 種別: `test`
- テスト: `internal/atproto/did_test.go::TestResolveHandle_DNSSucceeds_DoesNotCallHTTPS`（`atprototestutil.MockHTTPDoer.CallCount()` が 0 であることを確認）
- 実装: `internal/atproto/did.go`（`resolveHandle`、Phase 3）

**AC-05: DNS TXT 方式が解決失敗した場合、HTTPS well-known 方式にフォールバックし、既存の [0002_atproto_client](../0002_atproto_client/01_requirements.md) AC-01〜AC-03 の挙動がそのまま維持される**
- 種別: `test`
- テスト: `internal/atproto/did_test.go::TestResolveHandle_DNSFails_FallsBackToHTTPS`（新規、Phase 3）に加え、Phase 5 完了後の既存回帰テスト `internal/atproto/did_test.go::TestNewClient_ResolvesHandleToDIDAndPDSEndpoint`・`TestNewClient_DIDResolutionFailure`・`TestNewClient_RejectsUntrustedHost_NoFurtherRequest`、`internal/atproto/client_test.go::TestNewClient_WrapsHTTPDoerWithRetry` が回帰なく成功すること
- 実装: `internal/atproto/did.go`（`resolveHandle`、Phase 3）

**AC-06: 両方式とも解決に失敗した場合、両方式の失敗を汲み取ったエラーが返り処理が中断される**
- 種別: `test`
- テスト: `internal/atproto/did_test.go::TestResolveHandle_BothFail_ReturnsErrDIDResolutionFailed`
- 実装: `internal/atproto/did.go`（`resolveHandle`、Phase 3）

**AC-07: `did=` プレフィックスを持つ TXT レコードが複数存在する場合、DID を確定させずに解決失敗として扱う**
- 種別: `test`
- テスト: `internal/atproto/did_test.go::TestResolveHandleToDIDViaDNS_MultipleDIDRecords_ReturnsDNSHandleResolutionFailed`
- 実装: `internal/atproto/did.go`（`resolveHandleToDIDViaDNS`、Phase 2）

**AC-08: DNS 解決処理で発生したエラーが `errors.Is`/`errors.AsType[T]` で判別可能な型付きエラーとして返る**
- 種別: `test`
- テスト: `internal/atproto/did_test.go::TestResolveHandleToDIDViaDNS_ResolverError_ReturnsTypedError`（フェイクは具体的な `*net.DNSError` を返し、`errors.Is`/`errors.AsType[*net.DNSError]` の両方を確認する）
- 実装: `internal/atproto/did.go`（`resolveHandleToDIDViaDNS`、Phase 2）

**AC-09: DNS TXT 方式の解決処理は、DID から先の処理（DID ドキュメント取得、PDS エンドポイント決定）が既存の `resolveDIDDocument`/`validatePDSEndpoint` の安全性検証をそのまま経由し、迂回経路が生まれない**
- 種別: `static` + `test`
- 検証コマンド（static）: `rg -n "ViaDNS" internal/atproto/client.go` — 期待結果: マッチなし（`NewClient` が DID の取得方式で分岐する新しい経路を持たないことを確認する。`NewClient` は `resolveHandle` のみを呼び、その戻り値 `(string, error)` は `resolveHandleToDID` 単体を呼んでいた旧経路と同じ型であるため、以降の `resolveDIDDocument`/`validatePDSEndpoint` 呼び出しに変更はない）
- テスト（test）: `internal/atproto/did_test.go::TestNewClient_DNSTXTSuccess_StillGoesThroughDownstreamPipeline`（Phase 5 で新規追加）。DNS TXT 方式で得た DID に対して `resolveDIDDocument`/`validatePDSEndpoint` が実際に実行され、HTTPS well-known 方式へは一切リクエストが飛ばないまま `NewClient` が `*Client` を構築できることをふるまいレベルで確認する。static 検証はコード構造上「新しい分岐がない」ことを保証するが、その経路が実際に最後まで動作することまでは証明しないため、この test 項目を併用する。
- 実装: `internal/atproto/client.go`（`NewClient`、Phase 5）

## 8. 成功基準

- **機能的完全性**: AC-01〜AC-09 のすべてが、本書 7 節に記載したテスト・静的検証で満たされている。DNS TXT 方式のみで検証されたハンドル（[01_requirements.md](01_requirements.md) 1.1 節の `issei.org` の事例）を `BSKY_HANDLE` に指定した場合でも、DID 解決が成功し以降の PDS エンドポイント決定・API 呼び出しに進めること（[01_requirements.md](01_requirements.md) 6 章）。
- **品質指標**: `make fmt && make test && make lint` がすべて成功し、`make deadcode` で新規追加コードに未使用箇所がないこと。
- **セキュリティ検証**: AC-09（迂回経路が生まれないこと）が static 検証と `TestNewClient_DNSTXTSuccess_StillGoesThroughDownstreamPipeline` の両方で確認されていること。AC-07 の fail-closed 挙動（複数 `did=` レコード）がユニットテストで確認されていること。
- **ドキュメント整備**: [package_reference.md](../../dev/developer_guide/package_reference.md) の `internal/atproto` の説明は「ハンドル解決」を抽象的に指しており DNS TXT 方式の追加によって文言の矛盾は生じないため、更新は不要（既存の記載のままで正しい）。

## 9. 次のステップ

本計画のレビュー・承認後、Phase 1 から順に実装を開始する。実装完了後は [requirements_process.md](../../dev/developer_guide/requirements_process.md) の Pre-Commit Checklist に従い、全 AC のテストが成功していることを確認してからコミットする。
