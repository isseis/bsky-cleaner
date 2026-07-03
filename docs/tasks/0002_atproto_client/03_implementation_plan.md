# AT Protocol クライアント — 実装計画書

## Document Status

| Item | Value |
|---|---|
| Status | `approved` |
| Created | 2026-07-02 |
| Review date | 2026-07-03 |
| Reviewer | isseis |
| Comments | - |

関連ドキュメント: [要件定義書](01_requirements.md) / [アーキテクチャ設計書](02_architecture.md)

## 1. 実装概要

### 1.1 目的

[要件定義書](01_requirements.md) の AC-01〜AC-15 を満たす `internal/atproto` パッケージを新設する。ログイン・投稿一覧取得・投稿削除の3操作を、DID 解決を経由した正しい PDS エンドポイントに対してのみ、秘匿情報を漏洩させずに実行できるようにする。設計の詳細は [アーキテクチャ設計書](02_architecture.md) を参照し、本書では実装作業を検証可能な単位に分解する。

### 1.2 実装方針

- [アーキテクチャ設計書](02_architecture.md) 8節の Phase 1〜6 の順序をそのまま踏襲する。各 Phase は前段の Phase が提供する型・関数に依存するため、順序の入れ替えは行わない。
- 各 Phase の完了条件は `make fmt && make test && make lint` の成功、および該当 AC のテスト green とする。
- 新規ファイルはすべて `internal/atproto/` 配下に置き、既存パッケージ（`internal/config`）へは `config.SecretString` を `Login` の引数型としてのみ依存する（[アーキテクチャ設計書](02_architecture.md) 1.1節・2.1節参照）。

### 1.3 既存コード調査結果

`internal/atproto` パッケージは存在せず、本タスクの全ファイルが新設となる。調査の結果、以下の既存資産を再利用する。

- **`internal/config.SecretString`（`internal/config/secret.go`）**: `Login(ctx, appPassword config.SecretString)` の引数型としてそのまま再利用する。新たに構築する手段がないため、`config.Credentials.AppPassword` として既に保持されている値を渡すことのみを想定する（アーキテクチャ 3.1節「`config.SecretString` の再利用は『消費のみ』に限定する」）。セッションアクセス JWT 用の `secretString`（非公開型）は `internal/atproto` 内に新設が必要で、既存の `config.SecretString` の実装（`String()`/`GoString()`/`LogValue()` で固定文字列 `"[REDACTED]"` を返す方式）と同じマスキング方式を踏襲する。
- **`errors.AsType[T]` の使用パターン（`internal/config/config.go:40`, `internal/config/errors.go`）**: 型付きエラー（`*FieldError`）を `Unwrap()` で連鎖させ `errors.Is`/`errors.AsType[T]` の両方で判定可能にする既存パターンを、本タスクの `HTTPError`/`SSRFError`（アーキテクチャ4節）にもそのまま適用する。`go.mod` の Go バージョン（1.26.2 以上、動作確認環境は 1.26.4）で `errors.AsType` が標準ライブラリ関数として利用できることを確認済み。
- **`internal/config/test_helpers.go` の `//go:build test` パターン**: パッケージ内部限定のテストヘルパー（非公開フィールドを扱うファクトリ関数）を `test_helpers.go` に置き `//go:build test` を付与する既存パターンをそのまま踏襲する（4.2節参照）。ただし `unsetEnv`/`writeTempTOML` 自体は環境変数・TOML 読み込み専用のヘルパーであり、`internal/atproto` では再利用しない。
- **`cmd/main.go`**: プレースホルダーのみで AT Protocol 通信ロジックを持たないため、本タスクでは変更しない（[0004_cli_entrypoint](../0004_cli_entrypoint/01_requirements.md) の責務）。
- **`docs/dev/developer_guide/package_reference.md`**: 現状 `internal/config` のみが記載されており、`internal/atproto` の記載がない。Phase 6 完了時にパッケージ責務を追記する（4.3節「ドキュメント整合性」参照）。
- 上記以外に変更が必要な既存コードはない。

## 2. 実装ステップ

### Phase 1 — HTTP 抽象化とエラー型

**対応 AC**: AC-14, AC-15（NF-004 の土台）

**変更対象ファイル**:
- `internal/atproto/http.go`（新設）
- `internal/atproto/errors.go`（新設）
- `internal/atproto/testutil/mocks.go`（新設）

**作業内容**:
- [x] `internal/atproto/http.go` に `HTTPDoer` インターフェース（`Do(req *http.Request) (*http.Response, error)`）を定義する（アーキテクチャ 3.1節）。
- [x] `internal/atproto/http.go` に XRPC リクエスト構築・応答デコードの共通処理（メソッド名・エンドポイント URL 組み立て、JSON デコード、非 2xx 応答を `HTTPError` へラップする処理）を実装する。生の `net/http` エラーや `*url.Error` を呼び出し元にそのまま透過させない（アーキテクチャ4節「秘匿情報を含まない設計」）。
- [x] `internal/atproto/http.go` に、検証済みアドレス集合のみに接続を許可する `DialContext` ラッパーと、3xx 応答を常に拒否する `CheckRedirect` ポリシーを提供する関数（例: 検証済みアドレス集合とホスト名を受け取り `*http.Client` を組み立てる関数）を実装する（アーキテクチャ5.2節の具体的方式）。この関数はホスト名の再解決を行わず、渡されたアドレスへ IP リテラルとして直接 `Dial` し、TLS の `ServerName` は元のホスト名に設定する。`net.IP` の `IsPrivate`/`IsLoopback`/`IsLinkLocalUnicast`/`IsLinkLocalMulticast`/`IsUnspecified` はいずれも Go 標準ライブラリ `net` パッケージに存在することを確認済み（`go doc net.IP` で確認、アーキテクチャ 5.2節1）。実際にこの関数を検証済みアドレス集合とともに呼び出し `Client` の `HTTPDoer` として組み込む配線は Phase 2（`did.go`）で行う。
- [x] `internal/atproto/errors.go` にセンチネルエラー群（`ErrDIDResolutionFailed`, `ErrUntrustedPDSEndpoint`, `ErrAuthenticationFailed`, `ErrHTTPStatus`, `ErrTransportFailure`, `ErrPaginationStalled`）を定義する（アーキテクチャ4節のコード例通り）。
- [x] `internal/atproto/errors.go` に `SSRFStage` 型（`SSRFStageInitialValidation`, `SSRFStageDialRevalidation`）を定義する。
- [x] `internal/atproto/errors.go` に `HTTPError`（`Method`, `StatusCode`, `Err` フィールド、`Error()`/`Unwrap()`）を実装する。`*http.Request`/`*http.Response`/生のレスポンスボディを一切保持しない。
- [x] `internal/atproto/errors.go` に `SSRFError`（`Endpoint`, `Stage`, `Err` フィールド、`Error()`/`Unwrap()`）を実装する。
- [x] `internal/atproto/testutil/mocks.go`（`package atprototestutil`）に `HTTPDoer` のモック実装を作成する。呼び出しごとにリクエスト（メソッド・URL・ボディ）を記録し、後続テストが「これ以上リクエストが発行されていないこと」をアサートできるよう呼び出し回数・記録済みリクエストを公開する。レスポンスは呼び出し元が `func(*http.Request) (*http.Response, error)` 形式のハンドラを差し替えられる設計とし、DID 解決・ログイン・投稿一覧・削除の各テストが URL/メソッドごとに異なる応答を返せるようにする。
- [x] `make lint` を実行し、`gosec` が動的な URL を使う HTTP リクエスト構築（`http.go` の XRPC 共通処理、5.2節の `DialContext`/`CheckRedirect` ラッパー）を指摘するか確認する。`.golangci.yml` は `gosec` を `_test\.go` ファイルのみ除外しており、`testutil/mocks.go`（`//go:build test` だが `_test.go` ではない）を含む非テストファイルには適用され続けるため、`internal/atproto/testutil/mocks.go` も対象に含めて確認する。指摘された場合は、指摘箇所に限定した `//nolint:gosec // <理由>` を追加する（ファイル単位・パッケージ単位の抑制は行わない）。理由コメントには、宛先ホストが `validatePDSEndpoint`（アーキテクチャ 5.2節）で事前検証済みであることを明記し、抑制が本番コードの一般的な機能ではなく本パッケージの検証済み接続に限定されることが分かる文言にする。`did.go` の DID ドキュメント取得は Phase 2 で新設されるため、その箇所の同様の確認は Phase 2 の完了条件で改めて行う（本書 2節 Phase 2 参照）。

**完了条件**: `go build ./internal/atproto/...` が通り、`go test -tags test -run '^$' ./internal/atproto/...` がコンパイルエラーなく完了する（テスト本体は Phase 2 以降で追加するため、この時点では空でよい）。`make fmt` を実行する。

### Phase 2 — DID 解決

**対応 AC**: AC-01, AC-02, AC-03（NF-003）

**変更対象ファイル**:
- `internal/atproto/client.go`（新設）
- `internal/atproto/did.go`（新設）
- `internal/atproto/did_test.go`（新設）
- `internal/atproto/http_test.go`（新設）

**作業内容**:
- [x] `internal/atproto/client.go` に `Client` 構造体（`httpDoer HTTPDoer`, `pdsBaseURL *url.URL`, `handle string`, `did string`）を定義する（アーキテクチャ 3.1節）。`session *Session` フィールドは `Session` 型が定義される Phase 3 で追加する（Phase 2 の時点では `Client` 構造体に `session` フィールドを含めない）。
- [x] `internal/atproto/did.go` に `resolveHandleToDID(ctx context.Context, httpDoer HTTPDoer, handle string) (string, error)` を実装する。
- [x] `internal/atproto/did.go` に `resolveDIDDocument(ctx context.Context, httpDoer HTTPDoer, did string) (serviceEndpoint string, err error)` を実装する。
- [x] `internal/atproto/did.go` に `validatePDSEndpoint(ctx context.Context, serviceEndpoint string) (verifiedAddrs []net.IP, host string, err error)` を実装する。スキームが `https` 以外なら `SSRFError{Stage: SSRFStageInitialValidation}` を返す（AC-02）。名前解決には `net.DefaultResolver.LookupIPAddr(ctx, host)`（`[]net.IPAddr` を返す）を使い、`ctx` のキャンセル・タイムアウトが名前解決にも及ぶようにする（アーキテクチャ 5.2節1 は `net.LookupIP` と表記しているが、`net.LookupIP` は `context.Context` を受け取らず本関数の `ctx` 引数を使わない死んだ引数になってしまうため、同一の「ホスト名を一度だけ解決し全アドレスを検証する」設計を保ったまま、`ctx` 対応版である `LookupIPAddr` に読み替える）。各 `net.IPAddr` の `.IP` フィールド（`net.IP` 型）に対し、返された全アドレスについてプライベート/ループバック/リンクローカル/未指定アドレスを判定し、1つでも該当すれば `SSRFError{Stage: SSRFStageInitialValidation}` を返す（AC-03、アーキテクチャ 5.2節1）。
- [x] `internal/atproto/client.go` に `NewClient(ctx context.Context, handle string, httpDoer HTTPDoer) (*Client, error)` を実装する。`resolveHandleToDID` → `resolveDIDDocument` → `validatePDSEndpoint` の順に呼び出し、いずれかが失敗した場合は `*Client` を返さない（AC-03: app パスワードを送信できる `*Client` を渡さないことで型レベルで保証する。アーキテクチャ 3.1節）。検証成功後、Phase 1 で実装した検証済みアドレス限定の `HTTPDoer` ラッパーで元の `httpDoer` を包み、以降 `Client` が保持する `httpDoer` として使う。
- [x] `internal/atproto/did_test.go` に `TestValidatePDSEndpoint_Success`（AC-01: 正常な DID 解決で PDS エンドポイントを抽出できること）を実装する。
- [x] `internal/atproto/did_test.go` に `TestNewClient_ResolvesHandleToDIDAndPDSEndpoint`（AC-01: `NewClient` が `handle`/`did`/`pdsBaseURL` を正しく保持すること）を実装する。
- [x] `internal/atproto/did_test.go` に `TestValidatePDSEndpoint_RejectsNonHTTPSScheme`（AC-02: スキームが `https` 以外の場合に `SSRFError{Stage: SSRFStageInitialValidation}` を返すこと）を実装する。
- [x] `internal/atproto/did_test.go` に `TestValidatePDSEndpoint_RejectsUntrustedHost`（AC-03）を表駆動テストとして実装する。`validatePDSEndpoint` が判定に使う5つの `net.IP` 判定メソッド（`IsPrivate`/`IsLoopback`/`IsLinkLocalUnicast`/`IsLinkLocalMulticast`/`IsUnspecified`、アーキテクチャ 5.2節1）それぞれに対応する行を最低1つ含める表とし、具体的には: プライベートアドレス（RFC 1918、例 `10.0.0.1`）、ループバック（IPv4 `127.0.0.1` と IPv6 `::1` の両方）、リンクローカルユニキャスト（例 `169.254.1.1`）、リンクローカルマルチキャスト（例 `224.0.0.251`）、未指定アドレス（`0.0.0.0`）、IPv4-mapped IPv6（`::ffff:127.0.0.1`）、IPv6 ユニークローカルアドレス（`IsPrivate` の対象となる `fc00::/7`、例 `fd00::1`）、および1つのホスト名が「公開 IP と非公開 IP の両方」を返す複数アドレス応答を含める（アーキテクチャ 7.1節）。
- [x] `internal/atproto/did_test.go` に `TestNewClient_DIDResolutionFailure`（AC-03: DID 解決自体が失敗した場合に `*Client` を返さないこと）を実装する。
- [x] `internal/atproto/did_test.go` に `TestNewClient_RejectsUntrustedHost_NoFurtherRequest`（AC-03: 拒否後、モックへの追加リクエストが発行されていないことを `testutil` の呼び出し回数記録でアサートする）を実装する。
- [x] `internal/atproto/http_test.go` に `TestRestrictedDialContext_RejectsUnverifiedAddress`（アーキテクチャ 5.2節2: 検証済みアドレス集合外への接続が拒否されること）を実装する。この検証は、検証済みアドレス集合とラッパーへ渡すアドレスを比較するロジック単体のテストとして実装し、実際の TCP 接続を発生させない純粋なロジックテストとする（`net.Listener` 等の実リソースは使わない）。
- [x] `internal/atproto/http_test.go` に `TestRestrictedDialContext_ConnectFailureIsTransportError`（アーキテクチャ 5.2節: `DialContext` 自体の接続失敗が `SSRFError` ではなく `ErrTransportFailure` になること）を実装する。このテストは実際の接続失敗を発生させる必要があるため、ローカルの `net.Listener`（`net.Listen("tcp", "127.0.0.1:0")` で確保後、接続前に `Close()` して未使用ポートを作る、または即座に `Close()` して「接続拒否」を発生させる）を使う。`net.Listener` を確保した場合は取得直後に `t.Cleanup(func() { _ = listener.Close() })` を登録する（`net.Listener.Close` は標準ライブラリの実 API）。
- [x] `internal/atproto/http_test.go` に `TestCheckRedirect_AlwaysRejects`（アーキテクチャ 5.2節: 3xx 応答を受信した場合に `SSRFError{Stage: SSRFStageDialRevalidation}` を返し、リダイレクト先へ接続しないこと）を実装する。
- [x] `make lint` を実行し、`gosec` が `did.go` の DID ドキュメント取得（動的な URL を使う HTTP リクエスト構築）を指摘するか確認する（Phase 1 の同様の確認の続き、本書 2節 Phase 1 参照）。指摘された場合は、指摘箇所に限定した `//nolint:gosec // <理由>` を追加し、理由コメントに宛先ホストが `validatePDSEndpoint` で事前検証済みであることを明記する。

**完了条件**: `make test`（`go test -tags test ./internal/atproto/...` を含む）、`make lint` が成功する。

### PR-1 作成ポイント: HTTP abstraction, error types, DID resolution

**対象ステップ**: Phase 1 / Phase 2

**推奨タイトル**: `feat(0002): add HTTP abstraction, error types, and SSRF-safe DID resolution`

**レビュー観点**: `validatePDSEndpoint` の境界値（プライベート/ループバック/リンクローカル/IPv4-mapped/複数アドレス応答）の網羅性 / `DialContext`/`CheckRedirect` の SSRF ガードが検証済みアドレス集合外への接続を確実に拒否すること / `HTTPError`/`SSRFError` が生のリクエスト/レスポンスを保持しないこと — このPRは本タスクの中で最もレビュー負荷が高い（SSRF 防御の中核と土台部分の骨組みが同居する）ため、コミットを SSRF 関連（`DialContext`/`CheckRedirect`/`validatePDSEndpoint`/境界値テスト）とそれ以外（インターフェース定義・エラー型・モック）に分けて積み、SSRF 関連のコミットを独立してレビューできるようにする

- [x] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [x] PR を作成した（https://github.com/isseis/bsky-cleaner/pull/20）
- [x] PR がマージされた
- [x] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

### Phase 3 — ログイン

**対応 AC**: AC-04, AC-05, AC-06

**変更対象ファイル**:
- `internal/atproto/session.go`（新設）
- `internal/atproto/session_test.go`（新設）
- `internal/atproto/test_helpers.go`（新設）
- `internal/atproto/client.go`（変更: `session` フィールド追加）

**作業内容**:
- [x] `internal/atproto/session.go` に非公開型 `secretString`（`value string` フィールド、`String()`/`GoString()`/`LogValue()` で固定文字列 `"[REDACTED]"` を返す）を実装する（`internal/config/secret.go` の `SecretString` と同じマスキング方式、アーキテクチャ 3.1節）。
- [x] `internal/atproto/session.go` に `Session` 構造体（`DID string`, `AccessJWT secretString`）を実装する。
- [x] `internal/atproto/client.go` の `Client` 構造体に `session *Session` フィールドを追加する。
- [x] `internal/atproto/session.go` に `Client.Login(ctx context.Context, appPassword config.SecretString) error` を実装する。`identifier` には `NewClient` で検証済みの `c.handle` を使う（`Login` の引数として別の handle を受け取らない、アーキテクチャ 3.1節）。`appPassword.Reveal()` はリクエストボディ構築の直前でのみ呼び出す。
- [x] `com.atproto.server.createSession` への POST リクエストが 401 を返した場合、`HTTPError` を返し `c.session` を更新しない（AC-05）。
- [x] 認証成功時、`c.session = &Session{DID: ..., AccessJWT: newSecretString(accessJwt)}` を設定する（AC-04）。
- [x] `internal/atproto/test_helpers.go`（`//go:build test`）に `newTestClient(httpDoer HTTPDoer, pdsBaseURL *url.URL, handle, did string, session *Session) *Client` を実装する。`Client` の非公開フィールド（`httpDoer`/`pdsBaseURL`/`handle`/`did`/`session`）すべてに直接値を設定し、DID 解決・ログインを経由せずに `Login`/`ListPosts`/`DeleteRecord` を単体テストできるようにする（`pdsBaseURL` を渡さないと `ListPosts`/`DeleteRecord` が XRPC リクエスト URL を組み立てられないため必須。`handle` は `Login` の request-body assertion（`Login` は常に `c.handle` を identifier として送るため）に必要。Phase 4・Phase 5 で再利用、test_organization.md の Classification B「非公開フィールドを扱うファクトリ関数」に該当）。
- [x] `internal/atproto/session_test.go` に `TestClient_Login_Success`（AC-04: 正しい認証情報でセッション情報が取得できること）を実装する。
- [x] `internal/atproto/session_test.go` に `TestClient_Login_InvalidCredentials_NoFurtherCalls`（AC-05: 誤った認証情報でエラーが返り、後続の API 呼び出し用のセッションが設定されないこと）を実装する。
- [x] `internal/atproto/session_test.go` に `TestClient_Login_ErrorDoesNotLeakSecrets`（AC-06: エラーオブジェクトの `Error()` 文字列表現に送信した app パスワード・レスポンスに含まれるセッション JWT のいずれも含まれないこと）を実装する。

**完了条件**: `make test`、`make lint` が成功する。

### PR-2 作成ポイント: Login

**対象ステップ**: Phase 3

**推奨タイトル**: `feat(0002): add Login`

**レビュー観点**: `appPassword.Reveal()` の呼び出し箇所がリクエストボディ構築の直前に限定されていること / エラー文字列に秘匿情報が含まれないこと / `newTestClient` が非公開フィールドを直接設定するテスト専用ファクトリとして適切に隔離されていること

- [x] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [x] PR を作成した（https://github.com/isseis/bsky-cleaner/pull/21）
- [x] PR がマージされた
- [x] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

### Phase 4 — 投稿一覧取得

**対応 AC**: AC-07, AC-08, AC-09, AC-10

**変更対象ファイル**:
- `internal/atproto/posts.go`（新設）
- `internal/atproto/posts_test.go`（新設）

**作業内容**:
- [x] `internal/atproto/posts.go` に `PostType` 列挙型（`PostTypeOriginal`, `PostTypeReply`, `PostTypeQuote`, `PostTypeRepost`）を実装する。
- [x] `internal/atproto/posts.go` に `Post` 構造体（`RKey string`, `Type PostType`, `CreatedAt time.Time`, `Pinned bool`）を実装する。
- [x] `internal/atproto/posts.go` に `Client.ListPosts(ctx context.Context) ([]Post, error)` を実装する。アーキテクチャ 3.2節の分類ロジック（`reply` フィールドの有無で `PostTypeReply`、`embed.$type` が `app.bsky.embed.record`/`app.bsky.embed.recordWithMedia` で `PostTypeQuote`、両方に該当しうる場合は `PostTypeReply` を優先、いずれでもなければ `PostTypeOriginal`）に従う。実装時に、`listRecords`/`getRecord` の呼び出しが `c.session`（`Login` で取得したアクセス JWT）を必要とするか（`Authorization` ヘッダーを要求するか）を確認した結果、AT Protocol の lexicon 上 `com.atproto.repo.listRecords`/`getRecord` は認証不要の公開読み取りエンドポイントであり（`createSession`/`deleteRecord` と異なり auth 要件を持たない）、`ListPosts` は `Authorization` ヘッダーを送信せず `c.session` のガードも設けない設計とした。判断根拠は `internal/atproto/posts.go` の `ListPosts` 直上のコメントに記録し、`TestClient_ListPosts_WithoutSession_ReturnsError` は追加していない。
- [x] `com.atproto.repo.listRecords`（`collection=app.bsky.feed.post`）のページネーションループを実装し、`cursor` が空になるまで全件取得する（AC-08）。
- [x] `com.atproto.repo.listRecords`（`collection=app.bsky.feed.repost`）のページネーションループを実装し、全件を `PostTypeRepost` として追加する（AC-08）。
- [x] 上記2つのページネーションループそれぞれに、直前に使ったカーソルと新たに返されたカーソルが同一の場合にループを打ち切り `ErrPaginationStalled` を返す終端保証を実装する（アーキテクチャ 6.2節「ページネーションの終端保証」）。2箇所のループに同一の停止ロジックを適用する。
- [x] `com.atproto.repo.getRecord`（`collection=app.bsky.actor.profile`, `rkey=self`）を呼び出し `pinnedPost` を取得する処理を実装する。レコードが存在しない場合はエラーにせず「ピン留めなし」として継続する（アーキテクチャ 3.2節3）。
- [x] `pinnedPost` の rkey と一致する `Post.Pinned` を `true` に設定する処理を実装する（AC-09）。
- [x] 投稿0件の場合、空の `[]Post` をエラーなしで返すことを確認する（AC-10。`listRecords` が空配列を返す前提での自然な帰結であり、追加の分岐は不要）。
- [x] `internal/atproto/posts_test.go` に `TestClient_ListPosts_ClassifiesPostTypes`（AC-07: 通常投稿・リプライ・引用ポストそれぞれのレコード形状に対する `PostType` 判定、`reply` と `embed.$type` 両方に該当するケースで `PostTypeReply` が優先されることを含む）を表駆動テストとして実装する。`newTestClient`（Phase 3）を使う。
- [x] `internal/atproto/posts_test.go` に `TestClient_ListPosts_IncludesReposts`（AC-07: リポストが別コレクションから取得され `PostTypeRepost` になること）を実装する。
- [x] `internal/atproto/posts_test.go` に `TestClient_ListPosts_Pagination`（AC-08: 複数ページにまたがる `cursor` の追従。`app.bsky.feed.post`・`app.bsky.feed.repost` 双方のページネーションを検証する）を実装する。
- [x] `internal/atproto/posts_test.go` に `TestClient_ListPosts_PaginationStalled`（アーキテクチャ 6.2節: カーソルが進行しない応答に対し `ErrPaginationStalled` で打ち切られること）を実装する。
- [x] `internal/atproto/posts_test.go` に `TestClient_ListPosts_PinnedDetection`（AC-09: `pinnedPost` と一致する投稿が `Pinned: true` になること、`profile` レコードが存在しない場合にエラーにならず「ピン留めなし」として継続すること）を実装する。
- [x] `internal/atproto/posts_test.go` に `TestClient_ListPosts_EmptyResult`（AC-10: 投稿0件のアカウントに対しエラーにならず空の `[]Post` を返すこと）を実装する。

**完了条件**: `make test`、`make lint` が成功する。

### PR-3 作成ポイント: ListPosts

**対象ステップ**: Phase 4

**推奨タイトル**: `feat(0002): add ListPosts`

**レビュー観点**: `PostType` 判定ロジック（`reply` と `embed.$type` の優先順位）の正しさ / ページネーションの終端保証（`ErrPaginationStalled`）が2つのコレクション双方に適用されていること / ピン留め判定と `profile` レコード不在時のフォールバック挙動

- [x] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [x] PR を作成した（https://github.com/isseis/bsky-cleaner/pull/22）
- [ ] PR がマージされた
- [ ] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

### Phase 5 — 投稿削除

**対応 AC**: AC-11, AC-12, AC-13, AC-14, AC-15（NF-004 の土台、4.2節「単体テストのカバレッジ方針」参照）

**変更対象ファイル**:
- `internal/atproto/delete.go`（新設）
- `internal/atproto/delete_test.go`（新設）
- `internal/atproto/errors_test.go`（新設。全 Phase のエラーパスが出揃った時点で横断的に確認するため、Phase 5 のこの位置で追加する。4.2節参照）

**作業内容**:
- [ ] 着手前に、存在しない `rkey` を指定した `com.atproto.repo.deleteRecord` の実際の挙動（200 で成功応答を返すか、`InvalidRequest` 系のエラー名を伴う 400 応答を返すか）を、AT Protocol lexicon 一次資料（`com.atproto.repo.deleteRecord` の lexicon 定義）で確認する（アーキテクチャ 6.3節「冪等性の前提と検証状況」の未検証の仮定を解消するタスク）。lexicon 定義がエラー応答時の挙動を明示していない場合は、[プロジェクト概要](../../overview.md#完了の定義) の手動 dry-run/apply 確認で実際の PDS 応答を確認し、確認結果を本ステップのチェックボックス完了時のコミットメッセージまたは PR 説明に記録する。
- [ ] 上記調査の結果に応じて、`DeleteRecord` が「正常系」として扱う応答条件（200 のみか、特定のエラー名を伴う 400 も含むか）を確定する。
- [ ] `internal/atproto/delete.go` に `Client.DeleteRecord(ctx context.Context, rkey string) error` を実装する。`com.atproto.repo.deleteRecord` に `{repo: c.session.DID, collection: "app.bsky.feed.post", rkey}` を POST する（AC-11, AC-13）。`repo` パラメータは常に `c.session.DID`（`Login` で取得した自分自身の DID）を使い、呼び出し元から `repo` を受け取るパラメータは公開しない（AC-13）。`c.session` が `nil`（`Login` が呼ばれていない、または失敗した状態）の場合は、リクエストを送信せず `ErrAuthenticationFailed` を返す（AC-05 が要求する「ログイン失敗後は後続の API 呼び出しが行われない」ことを、呼び出し元の実装ミスに依存せずクライアント自身のガードとして保証する）。
- [ ] 上記の調査結果で確定した「正常系」条件（200、または特定エラー名を伴う 400）のいずれの場合も `nil` を返す（AC-12）。
- [ ] それ以外のエラー応答は `HTTPError` として返す。
- [ ] `internal/atproto/delete_test.go` に `TestClient_DeleteRecord_Success`（AC-11: 存在する rkey の削除が成功すること）を実装する。`newTestClient`（Phase 3）を使う。
- [ ] `internal/atproto/delete_test.go` に `TestClient_DeleteRecord_AlreadyDeleted_Idempotent`（AC-12: 既に削除済みの rkey に対してクラッシュせず正常系として扱われること。上記調査で確定した応答条件をモックで再現する）を実装する。
- [ ] `internal/atproto/delete_test.go` に `TestClient_DeleteRecord_UsesOwnDID`（AC-13: リクエストの `repo` パラメータが常に `c.session.DID` であることをモックが記録したリクエストボディでアサートする）を実装する。
- [ ] `internal/atproto/delete_test.go` に `TestClient_DeleteRecord_WithoutSession_ReturnsError`（AC-05: `newTestClient` で `session` に `nil` を渡した `*Client` に対し `DeleteRecord` を呼び出した場合、パニックせず `errors.Is(err, atproto.ErrAuthenticationFailed)` を満たすエラーが返り、モックへのリクエストが1件も発行されていないこと）を実装する。
- [ ] `internal/atproto/errors_test.go` に `TestHTTPError_ErrorsIs`（AC-14: `errors.Is(err, atproto.ErrHTTPStatus)` 等のセンチネル判定が機能すること）を実装する。この時点で Phase 1〜5 の全エラーパスが出揃っているため、横断確認をこの位置で行う（4.2節参照）。
- [ ] `internal/atproto/errors_test.go` に `TestHTTPError_AsType`（AC-14: `errors.AsType[*atproto.HTTPError](err)` でステータスコードを取得できること）を実装する。
- [ ] `internal/atproto/errors_test.go` に `TestErrors_NoSecretLeakage`（AC-15: タイムアウト・5xx・4xx・ログイン失敗の各エラーパスについて、`Error()` の文字列表現に `Authorization` ヘッダーの値・app パスワード・セッション JWT のいずれも含まれないことを表駆動テストで確認する）を実装する。

**完了条件**: `make test`（`errors_test.go` を含む）、`make lint` が成功する。

### PR-4 作成ポイント: DeleteRecord

**対象ステップ**: Phase 5

**推奨タイトル**: `feat(0002): add DeleteRecord`

**レビュー観点**: `repo` パラメータが常に自分の DID であること / 冪等性の前提として確認した実際の PDS 応答条件の妥当性 / `errors_test.go` が全エラーパス（タイムアウト・5xx・4xx・ログイン失敗）で秘匿情報漏洩がないことを網羅していること / `session` が `nil` の状態からのガードがパニックせずエラーを返すこと

- [ ] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [ ] PR を作成した
- [ ] PR がマージされた
- [ ] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

### Phase 6 — テストフィクスチャの lexicon 準拠検証

**対応 AC**: NF-006（AC-01〜AC-15 のいずれの単体テストも本フィクスチャを利用する前提のため、直接対応する AC 番号はない）

**変更対象ファイル**:
- `internal/atproto/testutil/fixtures.go`（新設）
- `internal/atproto/testutil/fixtures_test.go`（新設）
- `docs/dev/developer_guide/package_reference.md`（変更）

**作業内容**:
- [ ] `internal/atproto/testutil/fixtures.go`（`package atprototestutil`）に、`createSession`/`listRecords`（`app.bsky.feed.post`/`app.bsky.feed.repost`）/`getRecord`（`app.bsky.actor.profile`）/`deleteRecord` の各応答 JSON フィクスチャを定義する。Phase 2〜5 のテストがこれらのフィクスチャ（またはその一部を改変したもの）を使用するよう、各テストファイルを本フィクスチャ参照に統一する（Phase 2〜5 の実装時点では各テストファイル内にインラインの JSON を書いても構わないが、Phase 6 でこのファイルへの集約を完了させる）。
- [ ] `internal/atproto/testutil/fixtures.go` に、各 XRPC レスポンスに対応する lexicon 準拠の Go 構造体を定義する。フィールド名は AT Protocol 公式 lexicon（`com.atproto.server.createSession`, `com.atproto.repo.listRecords`, `com.atproto.repo.getRecord`, `com.atproto.repo.deleteRecord` の lexicon 定義）と突き合わせて確認し、各 lexicon が必須（`required`）と定義するフィールドを構造体コメントに列挙する。
- [ ] `internal/atproto/testutil/fixtures_test.go` に、各フィクスチャ JSON を対応する構造体へ `json.Decoder.DisallowUnknownFields()` を用いて厳格デコードし、成功することを確認するテスト（`TestFixtures_CreateSessionResponse_MatchesLexicon`, `TestFixtures_ListRecordsResponse_MatchesLexicon`, `TestFixtures_GetRecordResponse_MatchesLexicon`, `TestFixtures_DeleteRecordResponse_MatchesLexicon`）を実装する。
- [ ] `internal/atproto/testutil/fixtures_test.go` に、`DisallowUnknownFields()` の厳格デコードが実際に機能していることを証明する否定テスト（`TestFixtures_CreateSessionResponse_RejectsUnknownField`）を1件実装する。フィクスチャ JSON に lexicon に存在しないフィールドを追加した文字列を用意し、デコードがエラーになることをアサートする。これは「`json.Unmarshal` に退行してもテストが green のまま」という回帰を検出するためのテストであり、上記の肯定テストだけでは検出できない（`DisallowUnknownFields` は JSON 側の余剰フィールドしか検出せず、構造体側に必須フィールドが欠けているケースは検出しないため、次のタスクで別途カバーする）。
- [ ] `internal/atproto/testutil/fixtures_test.go` に、各フィクスチャ構造体が対応する lexicon の必須フィールドをすべて宣言していることを確認するテスト（`TestFixtures_RequiredFieldsPresent`）を実装する。各構造体のコメントに列挙した必須フィールド名のリストを、`reflect` で構造体のフィールド一覧（JSON タグ名）と突き合わせ、すべて含まれることをアサートする。これにより `DisallowUnknownFields` では検出できない「lexicon 必須フィールドの欠落」を検出する。
- [ ] `docs/dev/developer_guide/package_reference.md` の「Package Responsibilities」に `internal/atproto` の責務（本パッケージ概要、アーキテクチャ 1.1節の単一責任の説明を要約したもの）を追記し、「Directory Structure」のツリーに `internal/atproto/` を追加する。

**完了条件**: `make test`、`make lint`、`make deadcode` が成功する。`internal/atproto` パッケージ全体の AC-01〜AC-15 のテストが green であることを確認する。

### PR-5 作成ポイント: Fixture lexicon compliance

**対象ステップ**: Phase 6

**推奨タイトル**: `test(0002): verify fixtures against AT Protocol lexicon`

**レビュー観点**: フィクスチャのフィールド名が lexicon と一致していること / `DisallowUnknownFields` によって乖離が確実に検出できること / `TestFixtures_RequiredFieldsPresent` が lexicon 必須フィールドの欠落を実際に検出できる網羅性であること

- [ ] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [ ] PR を作成した
- [ ] PR がマージされた（最終 PR のため、次のブランチへの切り替えは不要）

## 3. 実装順序とマイルストーン

### 3.1 マイルストーン

Phase 1〜6 は [アーキテクチャ設計書](02_architecture.md) 8節の優先順位をそのまま踏襲し、この順序で実装する。各 Phase は前段の Phase が定義する型・関数に依存するため、後続 Phase の作業を先に着手しない。

| マイルストーン | 内容 | 完了条件 |
|---|---|---|
| M1 | Phase 1 完了 | `HTTPDoer`/エラー型/モックが揃い、以降の全 Phase の単体テストが依存可能になる |
| M2 | Phase 2 完了 | `NewClient` による DID 解決・SSRF ガードが動作し、AC-01〜AC-03 のテストが green |
| M3 | Phase 3 完了 | `Login` が動作し、AC-04〜AC-06 のテストが green |
| M4 | Phase 4 完了 | `ListPosts` が動作し、AC-07〜AC-10 のテストが green |
| M5 | Phase 5 完了 | `DeleteRecord` が動作し、AC-11〜AC-13 のテストが green。エラーパス横断テスト（`errors_test.go`）も Phase 5 内（本書2節）で実装され、AC-14・AC-15 のテストが green |
| M6 | Phase 6 完了 | フィクスチャの lexicon 準拠検証が完了し、NF-006 を満たす。パッケージ参照ドキュメントが更新される |

### 3.2 PR 構成

1 PR = 1 Phase を基本とし、Phase 5 のみエラーパス横断テスト（旧 M5.5、`errors_test.go`）を含めて1つの PR にまとめる（Phase 1〜4 の全エラーパスが出揃うのが Phase 5 完了時点であるため）。各 PR は前段の PR が導入する型・関数にのみ依存し、後続 PR のスタブを必要としないため、この単位でグリーンゲート（`make fmt && make test && make lint`）を独立して満たせる。

| PR | 対象ステップ | 主な変更内容 |
|---|---|---|
| PR-1 | Phase 1 / Phase 2 | HTTP 抽象化（`HTTPDoer`）、エラー型（`HTTPError`/`SSRFError`）、SSRF ガード付き DID 解決（`NewClient`）、テストダブル（`testutil/mocks.go`）、SSRF ガード単体テスト（`http_test.go`） |
| PR-2 | Phase 3 | `Login`（`Session`/`secretString`） |
| PR-3 | Phase 4 | `ListPosts`（`PostType` 判定、ページネーション、ピン留め判定） |
| PR-4 | Phase 5 | `DeleteRecord`、エラーパス横断テスト（`errors_test.go`） |
| PR-5 | Phase 6 | テストフィクスチャの lexicon 準拠検証、`package_reference.md` 更新 |

## 4. テスト戦略

### 4.1 テストヘルパーの配置

[test_organization.md](../../dev/developer_guide/test_organization.md) の分類に従い、以下の2ファイルを新設する。他に追加が必要なヘルパーファイルはない。

| ファイル | 分類 | 理由 |
|---|---|---|
| `internal/atproto/testutil/mocks.go`（`package atprototestutil`） | Classification A | `HTTPDoer` は公開インターフェースであり、モック実装はパッケージ外の公開 API のみに依存するため。Phase 2〜6 の全テストファイルが依存する。 |
| `internal/atproto/test_helpers.go`（`//go:build test`） | Classification B | `Client` の非公開フィールド（`httpDoer`/`pdsBaseURL`/`handle`/`did`/`session`）へ直接値を設定するファクトリ関数 `newTestClient` が必要なため。Phase 3〜5 のテストが DID 解決・ログインを経由せずに `Login`/`ListPosts`/`DeleteRecord` を単体テストできるようにする。 |

**命名の例外について**: [test_organization.md](../../dev/developer_guide/test_organization.md) の Classification A は「ヘルパー関数（setup・utility・fixtures）→ `testutil/helpers.go`」と定め、`fixtures.go` という個別のファイル名は列挙していない。一方、承認済みの [アーキテクチャ設計書](02_architecture.md) 2.1節はファイル名を `testutil/fixtures.go` と明示している。本タスクでは、承認済みアーキテクチャの決定を優先し `fixtures.go` のファイル名を踏襲する（アーキテクチャの再承認なしに実装計画側でファイル名を変更しない）。これは test_organization.md のカテゴリ分類（Classification A、公開 API のみに依存するテストデータ提供ヘルパー）自体からの逸脱ではなく、そのカテゴリ内でのファイル名のみの逸脱である点に注意する。この命名の扱いに疑問がある場合は、実装着手前に test_organization.md 側の改訂（`fixtures.go` を許容ファイル名として追記する等）を検討する。

### 4.2 単体テストのカバレッジ方針

- 正常系・異常系（認証失敗、HTTP ステータス異常、transport 障害）・境界値（IP アドレス境界、カーソル非進行、0件）をすべて表駆動テストまたは個別テスト関数でカバーする。各テストの詳細は2節の各 Phase のチェックリストに列挙した通り。
- 秘匿情報漏洩の確認（AC-06, AC-15）は、タイムアウト・5xx・4xx・ログイン失敗の各エラーパスを `errors_test.go`（Phase 1 で型を定義するが、テスト自体は全 Phase のエラーパスが出揃う Phase 5 の末尾で実装する。作業内容は本書2節 Phase 5 参照）で横断的に確認する。

### 4.3 クロスサーチチェックリスト

`internal/atproto` は完全新設パッケージであり、既存シンボルの削除・変更は発生しないため、削除済みシンボルへの残存参照チェックは不要。以下のみ確認する。

- [ ] `rg -n "atprototestutil" -g '*.go' internal` — `testutil/mocks.go` と `testutil/fixtures.go` の両方が同一パッケージ名 `atprototestutil` を使っていること（test_organization.md のドメインプレフィックス命名規則、パッケージ名が分かれていないこと）。期待結果: 両ファイルの `package` 宣言行がいずれも `package atprototestutil` であること。
- [ ] `docs/dev/developer_guide/package_reference.md` を目視し、以下の2点を確認する（読みやすさ確認、コマンドの成否では判定できないため目視確認とする）: (1) Phase 6 で追記した `internal/atproto` の説明が既存の `internal/config` の記述と重複した文言になっていないこと、(2) 追記内容が Phase 5 完了時点の実際の公開 API（`NewClient`/`Login`/`ListPosts`/`DeleteRecord`）とアーキテクチャ 1.1節の単一責任の説明を正しく反映しており、実装されなかった機能や誤った責務を記載していないこと。

## 5. リスク管理

| リスク | 影響 | 対策 |
|---|---|---|
| Phase 5 着手前の `deleteRecord` 冪等性調査で lexicon 定義がエラー応答挙動を明示しない場合、確定に時間がかかる | Phase 5 のスケジュール遅延 | lexicon 定義で確認できない場合は手動 dry-run/apply 確認に切り替える代替手順をあらかじめ Phase 5 の作業内容に明記済み（本書 2節 Phase 5）。 |
| `validatePDSEndpoint` の境界値テスト（IPv4-mapped IPv6 等）で `net.IP` の判定メソッドの挙動が想定と異なる可能性 | AC-03 のテストが誤って green/red になる | Phase 2 の `TestValidatePDSEndpoint_RejectsUntrustedHost` で境界値を明示的に列挙し、実装時に `go doc net.IP` で各メソッドの実際の判定範囲を都度確認する。 |
| `DialContext`/`CheckRedirect` ラッパーの実装が複雑になり、Phase 2 のスコープが肥大化する | スケジュール遅延、レビュー負荷増大 | Phase 1 で汎用的なラッパー構築関数を用意し、Phase 2 では `NewClient` からの呼び出し（配線）のみに限定する（本書 2節 Phase 1・Phase 2 の作業内容の分割方針）。 |
| PR-1（Phase 1・2）は本タスクで最大の PR であり、SSRF 防御の中核（`DialContext`/`CheckRedirect`/`validatePDSEndpoint`）と HTTP 抽象化・エラー型の骨組みが同一 PR に同居する。Phase 1 は完了条件がコンパイルのみで実質的なテストを持たないため（本書 2節 Phase 1）、これ以上 PR を細分化すると前段 PR が単体でグリーンゲートを満たせなくなる | レビュー負荷増大、SSRF 関連の変更が他の変更に埋もれるリスク | これ以上の PR 分割は行わず、PR-1 のレビュー観点（本書 2節）でコミット分割によるレビュー順序の指定を行うことで軽減する。 |

## 6. 実装チェックリスト

（各 PR 作成ポイント（2節）のインラインチェックボックスと対応する。ここでは PR 単位の完了状況のみをまとめて確認する）

- [x] PR-1 マージ済み（対象ステップ: Phase 1 / Phase 2）
- [ ] PR-2 マージ済み（対象ステップ: Phase 3）
- [ ] PR-3 マージ済み（対象ステップ: Phase 4）
- [ ] PR-4 マージ済み（対象ステップ: Phase 5）
- [ ] PR-5 マージ済み（対象ステップ: Phase 6）
- [ ] `make fmt && make test && make lint` が最終的に成功する
- [ ] `make deadcode` が成功する

## 7. 受け入れ基準の検証

| AC | 検証方法 | 種別 |
|---|---|---|
| AC-01 | `internal/atproto/did_test.go::TestValidatePDSEndpoint_Success`, `internal/atproto/did_test.go::TestNewClient_ResolvesHandleToDIDAndPDSEndpoint` | test |
| AC-02 | `internal/atproto/did_test.go::TestValidatePDSEndpoint_RejectsNonHTTPSScheme` | test |
| AC-03 | `internal/atproto/did_test.go::TestValidatePDSEndpoint_RejectsUntrustedHost`, `internal/atproto/did_test.go::TestNewClient_DIDResolutionFailure`, `internal/atproto/did_test.go::TestNewClient_RejectsUntrustedHost_NoFurtherRequest` | test |
| AC-04 | `internal/atproto/session_test.go::TestClient_Login_Success` | test |
| AC-05 | `internal/atproto/session_test.go::TestClient_Login_InvalidCredentials_NoFurtherCalls`, `internal/atproto/delete_test.go::TestClient_DeleteRecord_WithoutSession_ReturnsError` | test |
| AC-06 | `internal/atproto/session_test.go::TestClient_Login_ErrorDoesNotLeakSecrets` | test |
| AC-07 | `internal/atproto/posts_test.go::TestClient_ListPosts_ClassifiesPostTypes`, `internal/atproto/posts_test.go::TestClient_ListPosts_IncludesReposts` | test |
| AC-08 | `internal/atproto/posts_test.go::TestClient_ListPosts_Pagination` | test |
| AC-09 | `internal/atproto/posts_test.go::TestClient_ListPosts_PinnedDetection` | test |
| AC-10 | `internal/atproto/posts_test.go::TestClient_ListPosts_EmptyResult` | test |
| AC-11 | `internal/atproto/delete_test.go::TestClient_DeleteRecord_Success` | test |
| AC-12 | `internal/atproto/delete_test.go::TestClient_DeleteRecord_AlreadyDeleted_Idempotent` | test |
| AC-13 | `internal/atproto/delete_test.go::TestClient_DeleteRecord_UsesOwnDID` | test |
| AC-14 | `internal/atproto/errors_test.go::TestHTTPError_ErrorsIs`, `internal/atproto/errors_test.go::TestHTTPError_AsType` | test |
| AC-15 | `internal/atproto/errors_test.go::TestErrors_NoSecretLeakage` | test |
| NF-001 | `make fmt && make test && make lint` の実行、終了コード 0 で成功 | static |
| NF-002 | `rg -n "indigo" go.mod go.sum` — 期待結果: マッチなし（`indigo` 系パッケージへの依存が追加されていないこと） | static |
| NF-003 | `internal/atproto/did_test.go::TestValidatePDSEndpoint_RejectsUntrustedHost`, `internal/atproto/http_test.go::TestRestrictedDialContext_RejectsUnverifiedAddress`, `internal/atproto/http_test.go::TestCheckRedirect_AlwaysRejects` | test |
| NF-004 | `rg -n "type HTTPDoer interface" internal/atproto/http.go` — 期待結果: 1件マッチ（`Do` メソッドを持つインターフェースとして定義されていること）。加えて Phase 2〜5 の全テストが `net/http` の実通信を行わずモックのみで完結すること（`go test -tags test ./internal/atproto/...` がネットワークアクセスなしで成功する） | static + test |
| NF-005 | `go.mod` の `go` ディレクティブが `1.26.2` 以上であることを確認済み（既存の `go.mod` 記載）。追加検証は不要 | static |
| NF-006 | `internal/atproto/testutil/fixtures_test.go::TestFixtures_CreateSessionResponse_MatchesLexicon`, `internal/atproto/testutil/fixtures_test.go::TestFixtures_ListRecordsResponse_MatchesLexicon`, `internal/atproto/testutil/fixtures_test.go::TestFixtures_GetRecordResponse_MatchesLexicon`, `internal/atproto/testutil/fixtures_test.go::TestFixtures_DeleteRecordResponse_MatchesLexicon`, `internal/atproto/testutil/fixtures_test.go::TestFixtures_CreateSessionResponse_RejectsUnknownField`, `internal/atproto/testutil/fixtures_test.go::TestFixtures_RequiredFieldsPresent` | test |

## 8. 成功基準

- 上記 AC-01〜AC-15、NF-001〜NF-006 がすべて 7節の検証方法で green である。
- `internal/atproto` パッケージが [アーキテクチャ設計書](02_architecture.md) 2.1節のファイル一覧通りに実装され、`Client` の公開 API（`NewClient`/`Login`/`ListPosts`/`DeleteRecord`）が [0004_cli_entrypoint](../0004_cli_entrypoint/01_requirements.md) から呼び出し可能な状態になっている。
- `make fmt && make test && make lint && make deadcode` がすべて成功する。
- `docs/dev/developer_guide/package_reference.md` に `internal/atproto` の記載が追加されている。

## 9. 次のステップ

- 本実装計画書のレビュー・承認。
- 承認後、Phase 1 から順に実装に着手する。
- 実装完了後、[0004_cli_entrypoint](../0004_cli_entrypoint/01_requirements.md) が `internal/atproto.Client` を `--dry-run`/`--apply` の切り替えとともに呼び出す配線を行う（本タスクの対象外）。
