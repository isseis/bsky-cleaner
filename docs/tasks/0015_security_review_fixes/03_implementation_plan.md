# セキュリティレビュー指摘事項の修正 — 実装計画書

## Document Status

| Item | Value |
|---|---|
| Status | `approved` |
| Created | 2026-07-11 |
| Review date | 2026-07-11 |
| Reviewer | isseis |
| Comments | - |

関連ドキュメント: [要件定義書](01_requirements.md) / [アーキテクチャ設計書](02_architecture.md)

## 1. 実装概要

### 1.1 目的

[01_requirements.md](01_requirements.md) が定義する5件の欠陥（F-001〜F-005）を、[02_architecture.md](02_architecture.md) の設計に従って最小差分で修正する。新機能は追加せず、各修正は独立して実装・レビュー可能である（[02_architecture.md](02_architecture.md) 8章の優先順位に従う）。

### 1.2 実装方針

- 各 Phase は1件の欠陥（F-001〜F-005）に対応し、対応する受け入れ基準（AC）をすべて満たすまでを1単位とする。
- 実装順序は [02_architecture.md](02_architecture.md) 8章の優先順位（F-001 → F-004 → F-002 → F-003 → F-005）に従う。誤削除防止に直結する修正を最優先し、次に fail-closed 強化、SSRF 対策拡張、通知信頼性、バックオフ正確性の順とする。
- 既存テストのシグネチャ更新（シグネチャ変更への追従）と、新規 AC 検証テストの追加は別のチェックボックスに分ける。

### 1.3 既存コード調査結果

対象パッケージごとに、現状・欠落・変更内容を示す。詳細な設計根拠は [02_architecture.md](02_architecture.md) の該当節を参照。

**`internal/atproto`**
- `delete.go`: `DeleteRecord(ctx, rkey string)` が `Collection: collectionFeedPost` を固定送信している（F-001 の欠陥そのもの）。`Post` を受け取る形へのシグネチャ変更が必要。
- `posts.go`: `PostType`（`PostTypeOriginal`/`Reply`/`Quote`/`Repost`）と `collectionFeedPost`/`collectionRepost` 定数は既に定義済み。`collectionForPostType` は未実装で新規追加が必要。
- `session.go`: `Login` が `createSession` レスポンスの `DID` を検証せず `c.session` に格納している（F-004 の欠陥そのもの）。`c.did` は `NewClient` が設定済みで、`Login` 内から参照可能。
- `errors.go`: `ErrSessionDIDMismatch`・`ErrUnknownPostType` は未定義で新規追加が必要。既存センチネルエラー群と同じ `errors.New` 定義パターンを踏襲する。
- `http.go`: `rejectRedirect`（`CheckRedirect` ポリシー）は `restrictedDoer` 用に実装済みで、そのまま再利用できる。DID 解決フェーズ向けの `*http.Client` を返すコンストラクタ（`NewRedirectRejectingHTTPClient`）は未実装で新規追加が必要。
- `client.go`・`did.go`: 変更なし（[02_architecture.md](02_architecture.md) 3.6 の一覧のとおり、リダイレクト拒否は注入される `httpDoer` 側で担保するため）。
- `test_helpers.go`（`//go:build test`）: `newTestClient`・`StubPassthroughPDSDoer`・`StubDNSTXTLookup` は既存のまま再利用する。追加・変更は不要。

**`internal/runner`**
- `runner.go`: `Client` インターフェースの `DeleteRecord(ctx, rkey string) error` と `Run` 内の呼び出し `client.DeleteRecord(ctx, post.RKey)` を `(ctx, post)` 形へ変更する。
- `test_helpers_test.go`: `fakeClient.DeleteRecord(_ context.Context, rkey string) error` のシグネチャを `(ctx, post atproto.Post)` へ変更し、内部で `post.RKey` を使って既存の `DeleteRecordCalls`/`DeleteRecordErrs` を維持する。
- `runner_test.go`: `fakeClient` が `post.RKey` を記録・参照する形にする限り、既存アサーション（`[]string{"first","second"}` 等）は変更不要（[02_architecture.md](02_architecture.md) 3.7 のとおり）。

**`internal/notify`**
- `notify.go`: `perAttemptTimeoutDoer.Do` の `defer cancel()` を、レスポンスボディの `Close` 時にキャンセルする形へ変更する。`send`・`redactWebhookURL`・`SendError` は変更不要。
- `test_helpers_test.go`（B2、ビルドタグなし）: 新規のテストダブル（後述 Phase 4）を追加する。

**`internal/retry`**
- `doer.go`: `parseRetryAfter` の秒数解釈を `time.ParseDuration(value + "s")` から `strconv.Atoi` ベースへ変更し、オーバーフロー安全性のための秒数クランプを追加する。`classify`・`backoffDelay`・`drainAndClose` は変更不要。

**`cmd`**
- `main.go`: `main()` 内の `run(configPath, apply, now, http.DefaultClient, os.Stdout, os.Stderr)` の `http.DefaultClient` を `atproto.NewRedirectRejectingHTTPClient()` に差し替える。`"net/http"` インポートは他に使用箇所がないため削除が必要。`run` 関数のドキュメントコメント（330-334行目）が `http.DefaultClient` を明示的に参照しているため、文言を更新する。

**影響を受けるが変更不要な既存テスト**（[02_architecture.md](02_architecture.md) 3.7 のとおり、シグネチャ変更への追従のみ、または無変更）
- `internal/atproto/delete_test.go`・`errors_test.go`・`client_test.go`: `DeleteRecord` 呼び出し箇所をすべて `Post{...}` 引数へ更新する（詳細は Phase 1）。
- `internal/atproto/session_test.go`・`errors_test.go`: `loginTestClient` のシグネチャに `did` 引数を追加する（詳細は Phase 2 — [02_architecture.md](02_architecture.md) には明記されていないが、`Login` の DID 検証追加により既存テストの `did: ""` では 2xx 応答のケースが新たに不一致エラーになってしまうため、本計画で追加が必要と判明した差分である）。
- `cmd/main_test.go`・`cmd/secret_leak_integration_test.go`・`internal/atproto/idempotency_integration_test.go`・`internal/atproto/runner_integration_test.go`・`internal/notify/notify_test.go`（F-003 で変更する行を除く）: いずれもモック `HTTPDoer` 経由か `run()`/`Send()` にモックを注入する形であり、`http.DefaultClient` 差し替え（F-002）や `DeleteRecord` シグネチャ変更（F-001、`runner.Client` 経由のため直接の呼び出しがない）の影響を受けない。変更不要。

## 2. 実装順序とマイルストーン

[02_architecture.md](02_architecture.md) 8章のとおり、5件の修正は相互に独立しており並行実装・レビュー可能だが、本計画では破壊的操作の正当性に直結する順に直列に並べる。

| マイルストーン | 内容 | 対応 Phase | 完了条件 |
|---|---|---|---|
| M1 | F-001（リポスト削除時のコレクション選択） | Phase 1 | AC-01〜AC-04 のテストがすべて緑 |
| M2 | F-004（Login におけるセッション DID の検証） | Phase 2 | AC-12〜AC-14 のテストがすべて緑 |
| M3 | F-002（DID 解決フェーズにおけるリダイレクト拒否） | Phase 3 | AC-05〜AC-07 のテストがすべて緑 |
| M4 | F-003（Slack 通知リトライのコンテキストキャンセルタイミング是正） | Phase 4 | AC-08〜AC-11 のテストがすべて緑 |
| M5 | F-005（Retry-After ヘッダのパース厳格化） | Phase 5 | AC-15〜AC-17 のテストがすべて緑 |
| M6 | 全体統合 | — | `make fmt && make test && make lint` が成功（NF-001） |

各マイルストーンはそれぞれ独立した PR として作成可能である（各 Phase 内の「PR 作成ポイント」参照）。

## 3. Phase 1: F-001 — リポスト削除時のコレクション選択

### 3.1 作業内容

**対応 AC**: AC-01〜AC-04。**設計**: [02_architecture.md](02_architecture.md) 3.1節。

**対象ファイル**:
- `internal/atproto/errors.go`（変更）
- `internal/atproto/posts.go`（変更）
- `internal/atproto/delete.go`（変更）
- `internal/atproto/posts_test.go`（変更）
- `internal/atproto/delete_test.go`（変更）
- `internal/atproto/errors_test.go`（変更）
- `internal/atproto/client_test.go`（変更）
- `internal/atproto/runner_integration_test.go`（変更）
- `internal/runner/runner.go`（変更）
- `internal/runner/test_helpers_test.go`（変更）

**実装項目**:

- [x] `internal/atproto/errors.go` に `ErrUnknownPostType = errors.New("unknown post type")` を、既存のセンチネルエラー群（`ErrDIDResolutionFailed` 等）と同じ `var (...)` ブロック内に追加する。
- [x] `internal/atproto/posts.go` に `collectionForPostType(t PostType) (string, error)` を追加する。`PostTypeOriginal`・`PostTypeReply`・`PostTypeQuote` は `collectionFeedPost` を、`PostTypeRepost` は `collectionRepost` を返す。それ以外の値は `("", fmt.Errorf("collection for post type: %w", ErrUnknownPostType))` を返す（[02_architecture.md](02_architecture.md) 3.1 の fail-closed 設計）。
- [x] `internal/atproto/delete.go` の `DeleteRecord` のシグネチャを `func (c *Client) DeleteRecord(ctx context.Context, post Post) error` に変更する。関数内で `collectionForPostType(post.Type)` を呼び、エラーが返れば `fmt.Errorf("delete record: %w", err)` でラップして返す（HTTP リクエストは送信しない）。成功した場合は `deleteRecordRequest.Collection` にその結果を設定し、`RKey` は `post.RKey` を使う。
- [x] `DeleteRecord` の doc コメント（現状20-34行目）を更新する。「the app.bsky.feed.post record identified by rkey」という記述を、コレクションが `post.Type` から選択される旨に改める。
- [x] `internal/runner/runner.go` の `Client` インターフェースの `DeleteRecord(ctx context.Context, rkey string) error` を `DeleteRecord(ctx context.Context, post atproto.Post) error` に変更する。
- [x] `internal/runner/runner.go` の `Run` 内の呼び出し `client.DeleteRecord(ctx, post.RKey)`（49行目）を `client.DeleteRecord(ctx, post)` に変更する。
- [x] `internal/runner/test_helpers_test.go` の `fakeClient.DeleteRecord(_ context.Context, rkey string) error` を `func (c *fakeClient) DeleteRecord(_ context.Context, post atproto.Post) error { c.DeleteRecordCalls = append(c.DeleteRecordCalls, post.RKey); return c.DeleteRecordErrs[post.RKey] }` に変更する（`DeleteRecordCalls`/`DeleteRecordErrs` のフィールド定義・型は変更しない）。
- [x] `internal/atproto/delete_test.go` の既存呼び出し5箇所（`client.DeleteRecord(context.Background(), "abc123")` など、38・56・66・84・96行目）を、対応する `Post{RKey: "<元のrkey文字列>", Type: PostTypeOriginal}` 引数に置き換える。
- [x] `internal/atproto/errors_test.go` の既存呼び出し2箇所（70・84行目、いずれも `client.DeleteRecord(context.Background(), "abc123")`）を `client.DeleteRecord(context.Background(), Post{RKey: "abc123", Type: PostTypeOriginal})` に置き換える。
- [x] `internal/atproto/client_test.go` の既存呼び出し1箇所（58行目、`client.DeleteRecord(ctx, "abc123")`）を `client.DeleteRecord(ctx, Post{RKey: "abc123", Type: PostTypeOriginal})` に置き換える。

### 3.2 テスト内容

**テストファイルの配置**: テストは各対象ファイルと同じパッケージ内の `*_test.go` に追加する。テストヘルパーは既存ファイル内に置く私有（Classification B2）ヘルパーのみを追加し、新規ファイル・cross-package ヘルパー（`testutil/`）は不要である。

- [x] `internal/atproto/posts_test.go::TestCollectionForPostType_KnownTypes_ReturnsExpectedCollection` — `PostTypeOriginal`/`PostTypeReply`/`PostTypeQuote` → `app.bsky.feed.post`、`PostTypeRepost` → `app.bsky.feed.repost` をテーブル駆動で検証する。
- [x] `internal/atproto/posts_test.go::TestCollectionForPostType_UnknownType_ReturnsErrUnknownPostType` — `PostType(99)` のような未知値を渡すと `errors.Is(err, ErrUnknownPostType)` になり、返る collection 文字列が空であることを検証する。
- [x] `internal/atproto/delete_test.go::TestClient_DeleteRecord_Repost_UsesRepostCollection` — `Post{RKey: "abc123", Type: PostTypeRepost}` を渡し、送信された `deleteRecordRequest.Collection` が `app.bsky.feed.repost` であることを検証する（AC-01）。
- [x] `internal/atproto/delete_test.go::TestClient_DeleteRecord_NonRepostTypes_UsesFeedPostCollection` — `PostTypeOriginal`/`PostTypeReply`/`PostTypeQuote` をテーブル駆動で渡し、いずれも `Collection` が `app.bsky.feed.post` であることを検証する（AC-02）。
- [x] `internal/atproto/delete_test.go::TestClient_DeleteRecord_Repost_SendsOnlyRepostCollection_NotFeedPost` — `Post{RKey: "shared-rkey", Type: PostTypeRepost}` を渡し、`mock.CallCount()` が1、かつその唯一のリクエストの `deleteRecordRequest.Collection` が `app.bsky.feed.repost` であることを検証する（AC-03）。`com.atproto.repo.deleteRecord` は `(repo, collection, rkey)` の3つ組でしか対象レコードを特定できない AT Protocol の lexicon 仕様上、`collection=app.bsky.feed.repost` を指定した削除リクエストは `app.bsky.feed.post` 側に実在する同名 rkey のレコードに一切作用し得ない。したがって「送信された唯一のリクエストの `collection` が `app.bsky.feed.repost` である」ことの検証は、`MockHTTPDoer` にコレクション別の実データ状態を持たせずとも、AC-03 が求める「コレクションを跨いだ誤削除が発生しないこと」の証明として十分である。この論拠をテスト本体のコメントとして明記する（`app.bsky.feed.post` 側に同一 rkey のレコードが実在するという前提部分は、テストの前提コメントとして記述するに留め、実際の HTTP 応答スクリプトとしては用意しない）。
- [x] `internal/atproto/delete_test.go::TestClient_DeleteRecord_UnknownPostType_NoRequestSent` — 未知の `PostType` を渡すと `errors.Is(err, ErrUnknownPostType)` になり、`mock.CallCount()` が0（HTTPリクエストが一切送信されない = fail-closed）であることを検証する。
- [x] `internal/atproto/runner_integration_test.go::TestRunnerRun_RepostDeleteSuccessAndFailure_MapsToDeletedAndFailed` — `runner.Run` に2件のリポスト投稿を目標として与え、モックが一方に2xx・他方に5xxを返すよう設定した上で、`result.Deleted`/`result.Failed` への振り分けが HTTP 応答の成否と一致することを検証する（AC-04）。モックのハンドラでリクエストボディの JSON をデコードして `Collection` フィールドが `app.bsky.feed.repost` であることも併せて確認し、コレクション選択とHTTP成否判定が両方正しいことを1テストで確認する。

### 3.3 PR-1 作成ポイント

- **PR タイトル**: `fix(0015): send repost deletions to app.bsky.feed.repost collection`
- **レビュー観点**: `collectionForPostType` の網羅性（既知4種別＋fail-closed）、`DeleteRecord`/`runner.Client` のシグネチャ変更が呼び出し元すべてに反映されているか、AC-01〜AC-04 のテストが実際にコレクション取り違えを検出できる設計になっているか。

- [x] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [x] PR を作成した（[#143](https://github.com/isseis/bsky-cleaner/pull/143)）
- [x] PR がマージされた
- [x] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

### 3.4 完了基準

`go build ./...` が成功し、`go test -tags test ./internal/atproto/... ./internal/runner/...` が緑になる。

## 4. Phase 2: F-004 — Login におけるセッション DID の検証

### 4.1 作業内容

**対応 AC**: AC-12〜AC-14。**設計**: [02_architecture.md](02_architecture.md) 3.3節。

**対象ファイル**:
- `internal/atproto/errors.go`（変更）
- `internal/atproto/session.go`（変更）
- `internal/atproto/session_test.go`（変更）
- `internal/atproto/errors_test.go`（変更）

**実装項目**:

- [x] `internal/atproto/errors.go` に `ErrSessionDIDMismatch = errors.New("session DID does not match resolved DID")` を追加する。
- [x] `internal/atproto/session.go` の `Login` に、`doXRPC` 成功後・`c.session` 代入前の位置で `respBody.DID != c.did` の検証を追加する。不一致の場合は `return fmt.Errorf("login: session DID %q does not match resolved DID %q: %w", respBody.DID, c.did, ErrSessionDIDMismatch)` を返し、`c.session` の代入は行わない。
- [x] `internal/atproto/session_test.go` の `loginTestClient(mock *atprototestutil.MockHTTPDoer) *Client` に `did string` 引数を追加し、`newTestClient(mock, &url.URL{...}, testHandle, did, nil)` を呼ぶように変更する。
- [x] 既存テストのシグネチャ追従（全5箇所）:
  - [x] `session_test.go` の `TestClient_Login_Success`（40行目付近）: `loginTestClient(mock, "did:plc:test123")`（レスポンスの DID と一致させる）。
  - [x] `session_test.go` の `TestClient_Login_InvalidCredentials_NoFurtherCalls`（66行目付近）: 401 応答で DID 検証に到達しないため任意の値（例: `"did:plc:test123"`）でよい。
  - [x] `session_test.go` の `TestClient_Login_ErrorDoesNotLeakSecrets`（82行目付近）: 同上。
  - [x] `errors_test.go` の `transport_failure` ケース: `loginTestClient(mock, "did:plc:test123")` に更新（DID 検証に到達しないケースだがシグネチャ変更に追従）。
  - [x] `errors_test.go` の `login_failure` ケース: 同上。

### 4.2 テスト内容

- [x] `internal/atproto/session_test.go::TestClient_Login_SessionDIDMismatch_ReturnsErrorWithoutSettingSession` — `loginTestClient(mock, "did:plc:resolved")` に対し `createSession` レスポンスの DID を `"did:plc:different"` にして `Login` を呼び、エラーが返り `client.session` が `nil` のままであることを検証する（AC-13）。
- [x] `internal/atproto/session_test.go::TestClient_Login_SessionDIDMismatch_ErrorIsSentinelAndOmitsAccessJWT` — 上記と同じ不一致シナリオで、返るエラーが `errors.Is(err, ErrSessionDIDMismatch)` であること、かつレスポンスに含めた `accessJwt` の値がエラーメッセージに含まれないことを検証する（AC-14）。
- [x] AC-12 は 4.1節で更新する `TestClient_Login_Success`（DID を一致させた状態に更新済み）がそのまま検証する。新規テストは追加しない。

### 4.3 PR-2 作成ポイント

- **PR タイトル**: `fix(0015): verify createSession DID matches resolved DID`
- **レビュー観点**: 不一致時に `c.session` が変更されないこと、エラーメッセージに DID 以外の機微情報が含まれないこと、既存 `loginTestClient` 呼び出し元すべての追従漏れがないこと。

- [x] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [x] PR を作成した（https://github.com/isseis/bsky-cleaner/pull/144）
- [x] PR がマージされた
- [x] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

### 4.4 完了基準

`go build ./...` が成功し、`go test -tags test ./internal/atproto/...` が緑になる。

## 5. Phase 3: F-002 — DID 解決フェーズにおけるリダイレクト拒否

### 5.1 作業内容

**対応 AC**: AC-05〜AC-07。**設計**: [02_architecture.md](02_architecture.md) 3.2節。

**対象ファイル**:
- `internal/atproto/http.go`（変更）
- `internal/atproto/http_test.go`（変更）
- `cmd/main.go`（変更）

**実装項目**:

- [x] `internal/atproto/http.go` に `func NewRedirectRejectingHTTPClient() *http.Client` を追加する。`&http.Client{CheckRedirect: rejectRedirect}` を返す（[02_architecture.md](02_architecture.md) 3.2 のとおり、Transport・Timeout は `http.DefaultClient` の既定値のまま変更しない）。doc コメントは [02_architecture.md](02_architecture.md) 3.2 に掲載のものを使用する。
- [x] `cmd/main.go` の `main()` 内、`os.Exit(run(configPath, apply, now, http.DefaultClient, os.Stdout, os.Stderr))`（489行目）の `http.DefaultClient` を `atproto.NewRedirectRejectingHTTPClient()` に置き換える。
- [x] `cmd/main.go` の `import` から `"net/http"`（15行目）を削除する（他に使用箇所がないため、置き換え後は未使用インポートになる）。
- [x] `cmd/main.go` の `run` 関数の doc コメント（330-334行目）を更新する。「rather than reaching for http.DefaultClient/os.Stdout/os.Stderr directly」という記述を、`main` が具体的な `HTTPDoer` の構築方法（`atproto.NewRedirectRejectingHTTPClient()`）を選べることが `run` のテスト容易性の理由である旨に改め、`http.DefaultClient` という具体名は削除する。

**スコープ注記**: `internal/notify/notifypreview/main.go`（`//go:build test` の開発者向けプレビューツール、[test_organization.md](../../dev/developer_guide/test_organization.md) の対象外である独立した `main` パッケージ）も `notify.Send` の呼び出しに `http.DefaultClient` を直接使っている。これは本番のリクエスト経路ではなく、テストビルドタグでゲートされた開発者向けローカルツールであり、[01_requirements.md](01_requirements.md) の In Scope（F-002: DID 解決フェーズおよび `cmd/main.go` が構築する共有クライアント）にも含まれないため、本タスクでは変更しない。

### 5.2 テスト内容

- [x] `internal/atproto/http_test.go::TestNewRedirectRejectingHTTPClient_RejectsRedirect` — `http_test.go` は `package atproto`（内部テスト）である。`httptest.NewServer` で3xxを返すハンドラを用意し、`NewRedirectRejectingHTTPClient()` が返す `*http.Client` で直接 `Do` した結果が `*SSRFError`（`errors.AsType[*SSRFError]` で判別、`Stage == SSRFStageDialRevalidation`）であり、リダイレクト先へは到達しない（サーバ側でリダイレクト先パスへのアクセスがないことをハンドラ内のフラグで確認する）ことを検証する（AC-05）。
- [x] AC-06 は既存の `internal/atproto/did_test.go::TestNewClient_ResolvesHandleToDIDAndPDSEndpoint` が無変更で再実行されることで検証する。
- [x] AC-07 は既存の `internal/atproto/did_test.go::TestResolveHandle_DNSSucceeds_DoesNotCallHTTPS`・`TestNewClient_DNSTXTSuccess_StillGoesThroughDownstreamPipeline` が無変更で再実行されることで検証する。

### 5.3 PR-3 作成ポイント

- **PR タイトル**: `fix(0015): reject HTTP redirects during DID resolution`
- **レビュー観点**: `cmd/main.go` の配線差し替えが Slack 通知側にも意図通り及ぶこと（[02_architecture.md](02_architecture.md) 3.2 の「共有クライアント」節）、未使用インポート削除、AC-05 のテストが実際に3xxを拒否することを確認していること。

- [x] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [x] PR を作成した（[#145](https://github.com/isseis/bsky-cleaner/pull/145)）
- [x] PR がマージされた
- [x] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

### 5.4 完了基準

`go build ./...` が成功し、`go vet ./...` が `"net/http"` の未使用インポートを検出しない。`go test -tags test ./internal/atproto/... ./cmd/...` が緑になる。

## 6. Phase 4: F-003 — Slack 通知リトライのコンテキストキャンセルタイミング是正

### 6.1 作業内容

**対応 AC**: AC-08〜AC-11。**設計**: [02_architecture.md](02_architecture.md) 3.3節。

**対象ファイル**:
- `internal/notify/notify.go`（変更）
- `internal/notify/test_helpers_test.go`（変更）
- `internal/notify/notify_test.go`（変更）

**実装項目**:

- [x] `internal/notify/notify.go` に、レスポンスボディをラップして `Close` 時に per-attempt の `cancel` を呼ぶ非公開型 `cancelOnCloseBody` を追加する。

  ```go
  type cancelOnCloseBody struct {
      io.ReadCloser
      cancel context.CancelFunc
  }

  func (b *cancelOnCloseBody) Close() error {
      err := b.ReadCloser.Close()
      b.cancel()
      return err
  }
  ```

- [x] `perAttemptTimeoutDoer.Do`（現状82-86行目）を次の内容に置き換える: `ctx, cancel := context.WithTimeout(req.Context(), d.timeout)` の後、`defer cancel()` を削除し、`resp, err := d.inner.Do(req.Clone(ctx))` の結果に応じて分岐する。`err != nil` または `resp == nil` または `resp.Body == nil` の場合は即座に `cancel()` を呼んで `resp, err` を返す。それ以外（成功かつボディあり）の場合は `resp.Body = &cancelOnCloseBody{ReadCloser: resp.Body, cancel: cancel}` としてから `resp, nil` を返す。
- [x] `perAttemptTimeoutDoer` 型と `Do` メソッドの doc コメント（現状66-81行目）を、キャンセルのタイミングが「`Do` の戻り時」ではなく「レスポンスボディの `Close` 時」に変わったことを反映する内容に更新する。
- [x] `internal/notify/test_helpers_test.go` に、本 Phase の検証テストが使うテストダブル3種を追加する（B2、ビルドタグなし、私有型のみ）:
  - `ctxSensitiveBody`: `Read` 呼び出し時に自身が保持する `context.Context` の `Err()` が非 nil ならそれをそのまま返し、そうでなければ保持しているデータを1回だけ返してから `io.EOF` を返す `io.ReadCloser`。per-attempt コンテキストがまだ有効な間に読み取られたか、既にキャンセル済みの状態で読み取られたかをテストが区別できるようにする（AC-08・AC-09 用）。
  - `blockingUntilCtxDoneBody`: `Read` 呼び出し時に、自身が保持する `context.Context` が完了する（`<-ctx.Done()`）まで実際にブロックしてから `ctx.Err()` を返す `io.ReadCloser`。ハングしたレスポンスボディの読み取りが per-attempt タイムアウトで実際に打ち切られることを実時間で検証するために使う（AC-11 用。`ctxSensitiveBody` は非ブロッキングで代用できないため別型として追加する）。
  - `scriptedDoer`: `[]func(req *http.Request) (*http.Response, error)` を順番に呼び出す `notify.HTTPDoer` 実装。各要素は `req.Context()` を参照して `ctxSensitiveBody`/`blockingUntilCtxDoneBody` を組み立てられるようにする。

### 6.2 テスト内容

- [x] `internal/notify/notify_test.go::TestSend_429ThenSuccess_DrainsUnderLivePerAttemptContext_RetriesSuccessfully` — `scriptedDoer` で1回目は429（ボディは `ctxSensitiveBody`）、2回目は200を返すよう設定し、`send` が `nil` エラーを返すことを検証する（AC-08）。修正前の実装（`Do` 戻り時キャンセル）では1回目の `drainAndClose` が `context.Canceled` を検知してリトライループごと中断し `SendError` になるため、本テストで確定的に差分が出る（[02_architecture.md](02_architecture.md) 7.3 のとおり）。
- [x] `internal/notify/notify_test.go::TestSend_5xxThenSuccess_DrainsUnderLivePerAttemptContext_RetriesSuccessfully` — AC-08 のテストと同様の構成で1回目のステータスを5xxに変え、AC-09 を検証する。
- [x] `internal/notify/notify_test.go::TestSend_PerAttemptTimeout_BoundsHangingBodyRead` — 1回目のレスポンスを429・ボディを `blockingUntilCtxDoneBody`（per-attempt コンテキストの完了までブロックしてから `ctx.Err()` を返す）として構成し、`send` の全体所要時間が `requestTimeout` を大きく超えない（例: `requestTimeout` の3倍未満）ことをアサートする（AC-11。既存の `TestSend_HTTPTimeout_ReturnsSendError` と同様の実時間ベースのテストであり、同じ Wall-clock cost note を付す）。
- [x] AC-10 は既存の `internal/notify/notify_test.go::TestSend_MaxRetriesExceeded_ReturnsSendError_BoundedAttempts` が無変更で再実行されることで検証する。新規テストは追加しない。

### 6.3 PR-4 作成ポイント

- **PR タイトル**: `fix(0015): cancel notify per-attempt context on body close, not Do return`
- **レビュー観点**: `cancelOnCloseBody` がすべての戻り経路（成功・エラー・ボディ nil）で確実に `cancel` を呼ぶこと（コンテキストリーク防止）、AC-08/AC-09 のテストがループバック環境でのマスキング（アーキテクチャ3.3の引用ブロック参照）を回避した設計になっていること。

- [x] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [x] PR を作成した
- [x] PR がマージされた
- [x] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

### 6.4 完了基準

`go build ./...` が成功し、`go test -tags test ./internal/notify/...` が緑になる。

## 7. Phase 5: F-005 — Retry-After ヘッダのパース厳格化

### 7.1 作業内容

**対応 AC**: AC-15〜AC-17。**設計**: [02_architecture.md](02_architecture.md) 3.5節。

**対象ファイル**:
- `internal/retry/doer.go`（変更）
- `internal/retry/doer_test.go`（変更）

**実装項目**:

- [x] `internal/retry/doer.go` に `maxRetryAfterSeconds` 定数を追加する（値は24時間相当の秒数 `24 * 60 * 60`。[02_architecture.md](02_architecture.md) 3.5 が言う「現実的な `MaxDelay` を十分上回りつつオーバーフローしない定数」の具体値）。
- [x] `parseRetryAfter`（現状213-229行目）の秒数解釈部分を次の内容に置き換える: `time.ParseDuration(value + "s")` の呼び出しを `strconv.Atoi(value)` に変更する。パース成功時、`seconds <= 0` なら従来どおり0を返す。`seconds > maxRetryAfterSeconds` の場合は `seconds = maxRetryAfterSeconds` にクランプしてから `time.Duration(seconds) * time.Second` を返す（乗算前にクランプすることで `int64` オーバーフローを構造的に排除する）。HTTP-date 分岐（`http.ParseTime` 以降）は変更しない。
- [x] `parseRetryAfter` の doc コメント（現状206-212行目）を、`time.ParseDuration` 依存から `strconv.Atoi` ベースの RFC 9110 準拠パースに変わったこと、および巨大な値が `maxRetryAfterSeconds` でクランプされることを反映する内容に更新する。
- [x] `internal/retry/doer.go` の先頭 `import` に `"strconv"` を追加する。

### 7.2 テスト内容

- [x] `internal/retry/doer_test.go::TestDoer_Do_IntegerSecondsRetryAfter_UsesExactDelay` — `Retry-After: "5"`、`MaxDelay` を30秒などキャップに掛からない値に設定し、`clock.SleepCalls[0]` が正確に5秒であることを検証する（AC-15）。
- [x] `internal/retry/doer_test.go::TestDoer_Do_UnitSuffixedRetryAfterIgnored_FallsBackToExponential` — `Retry-After: "5m"` を設定し、`clock.SleepCalls[0]` が `policy.BaseDelay`（指数バックオフの初回値、例: 2秒）と一致すること、`5*time.Millisecond` ではないことを明示的にアサートする（AC-16）。
- [x] `internal/retry/doer_test.go::TestDoer_Do_HugeIntegerRetryAfter_ClampedNotOverflowed` — `Retry-After: "9999999999"`（現行実装なら `time.ParseDuration` がエラーとして拒否し0にフォールバックしていた値）を設定し、`clock.SleepCalls[0]` が `policy.MaxDelay` にキャップされること（`time.Duration` オーバーフローによる異常に短い待機になっていないこと）を検証する（3.5節のオーバーフロー安全性を担保する追加テスト）。
- [x] AC-17 は既存の `TestDoer_Do_FutureRetryAfterHTTPDate_UsesParsedDelay`・`TestDoer_Do_NonPositiveRetryAfterFallsBackToExponential`（`past_http_date` サブテスト）が無変更で再実行されることで検証する。新規テストは追加しない。

### 7.3 PR-5 作成ポイント

- **PR タイトル**: `fix(0015): parse Retry-After as RFC 9110 delta-seconds, not a Go duration`
- **レビュー観点**: `strconv.Atoi` への置き換えが単位付き文字列を正しく無視すること、巨大な整数値がオーバーフローせずクランプされること、既存の HTTP-date 分岐が無変更であること。

- [x] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [x] PR を作成した(https://github.com/isseis/bsky-cleaner/pull/147)
- [ ] PR がマージされた

### 7.4 完了基準

`go build ./...` が成功し、`go test -tags test ./internal/retry/...` が緑になる。

## 8. リスク管理

| リスク | 内容 | 軽減策 |
|---|---|---|
| F-001 のシグネチャ変更漏れ | `DeleteRecord`/`runner.Client` の呼び出し元・テストダブルが多数のファイルに散在しており、更新漏れがあるとコンパイルエラーになる | `go build ./...`/`go test -tags test ./internal/atproto/... ./internal/runner/...` が Phase 1 の成功基準（3.4節）に含まれるため、シグネチャ不一致は即座に検出される。加えて1.3節・各 Phase の作業内容で全呼び出し箇所（`rg -n "DeleteRecord\(" --type go` の結果）を列挙済み |
| F-003 のテストがループバック環境でマスキングされる | [02_architecture.md](02_architecture.md) 3.3 の引用ブロックが指摘するとおり、実サーバ + 小さいレスポンスボディでは EOF がキャンセル前に読み切られてしまい、バグの有無に関わらずテストが緑になる | 6.2節のテストは実サーバではなく `scriptedDoer`/`ctxSensitiveBody`（per-attempt コンテキストの状態を明示的に反映するテストダブル）を用いるため、実ネットワーク環境に依存せず確定的にバグを検出できる |
| F-002 の `cmd/main.go` 配線差し替えが実行時にしか確認できない | `main()` は `os.Exit` を直接呼ぶため単体テストで駆動できず、`http.DefaultClient` → `atproto.NewRedirectRejectingHTTPClient()` の置き換え自体は Go のテストで直接検証できない | 10章の静的検証タスクで `rg` による配線確認を行う。`NewRedirectRejectingHTTPClient` 自体の挙動（AC-05）は `http_test.go` のユニットテストで別途担保する |
| F-004 の `loginTestClient` シグネチャ変更が波及範囲を過小評価している | 呼び出し元は `session_test.go` 3箇所・`errors_test.go` 2箇所の計5箇所（`rg -n "loginTestClient\(" --type go` で確認済み） | Phase 2 の作業内容に全5箇所を明記済み。`go build -tags test ./...` で更新漏れは即座にコンパイルエラーとして検出される |

## 9. 受け入れ基準の検証

| AC | 内容（要約） | 検証方法 | 種別 | 対応 Phase |
|---|---|---|---|---|
| AC-01 | リポスト削除は `app.bsky.feed.repost` を指定する | `internal/atproto/delete_test.go::TestClient_DeleteRecord_Repost_UsesRepostCollection` | test | Phase 1 |
| AC-02 | 非リポストは従来どおり `app.bsky.feed.post` を指定する | `internal/atproto/delete_test.go::TestClient_DeleteRecord_NonRepostTypes_UsesFeedPostCollection` | test | Phase 1 |
| AC-03 | コレクションを跨いだ同一 rkey の誤削除が発生しない | `internal/atproto/delete_test.go::TestClient_DeleteRecord_Repost_SendsOnlyRepostCollection_NotFeedPost` | test | Phase 1 |
| AC-04 | `--apply` での HTTP 成否が `Deleted`/`Failed` に正しく振り分けられる | `internal/atproto/runner_integration_test.go::TestRunnerRun_RepostDeleteSuccessAndFailure_MapsToDeletedAndFailed` | test | Phase 1 |
| AC-05 | DID 解決フェーズの3xx応答はリダイレクト先へ到達せず `*SSRFError` になる | `internal/atproto/http_test.go::TestNewRedirectRejectingHTTPClient_RejectsRedirect` | test | Phase 3 |
| AC-06 | 2xx応答の既存挙動（ハンドル解決・DID文書取得）が変わらない | `internal/atproto/did_test.go::TestNewClient_ResolvesHandleToDIDAndPDSEndpoint`（既存、無変更で再実行） | test | Phase 3 |
| AC-07 | DNS TXT 方式のハンドル解決経路に影響しない | `internal/atproto/did_test.go::TestResolveHandle_DNSSucceeds_DoesNotCallHTTPS`・`TestNewClient_DNSTXTSuccess_StillGoesThroughDownstreamPipeline`（いずれも既存、無変更で再実行） | test | Phase 3 |
| AC-08 | 429応答時にリトライが機能し最終的に成功する | `internal/notify/notify_test.go::TestSend_429ThenSuccess_DrainsUnderLivePerAttemptContext_RetriesSuccessfully` | test | Phase 4 |
| AC-09 | 5xx応答時にも同様にリトライが機能する | `internal/notify/notify_test.go::TestSend_5xxThenSuccess_DrainsUnderLivePerAttemptContext_RetriesSuccessfully` | test | Phase 4 |
| AC-10 | リトライ上限到達時は従来どおり `*notify.SendError` を返す | `internal/notify/notify_test.go::TestSend_MaxRetriesExceeded_ReturnsSendError_BoundedAttempts`（既存、無変更で再実行） | test | Phase 4 |
| AC-11 | 各試行のタイムアウトがボディ読み取りを含めて有効なまま保たれる | `internal/notify/notify_test.go::TestSend_PerAttemptTimeout_BoundsHangingBodyRead` | test | Phase 4 |
| AC-12 | DID 一致時、`Login` は従来どおり成功し `c.session` が設定される | `internal/atproto/session_test.go::TestClient_Login_Success`（Phase 2 で DID を一致させる形に更新） | test | Phase 2 |
| AC-13 | DID 不一致時、`Login` はエラーを返し `c.session` は設定されない | `internal/atproto/session_test.go::TestClient_Login_SessionDIDMismatch_ReturnsErrorWithoutSettingSession` | test | Phase 2 |
| AC-14 | 不一致エラーが型判別可能で、機微情報を含まない | `internal/atproto/session_test.go::TestClient_Login_SessionDIDMismatch_ErrorIsSentinelAndOmitsAccessJWT` | test | Phase 2 |
| AC-15 | `Retry-After: 5` は5秒として解釈される | `internal/retry/doer_test.go::TestDoer_Do_IntegerSecondsRetryAfter_UsesExactDelay` | test | Phase 5 |
| AC-16 | `Retry-After: 5m` は無視され指数バックオフにフォールバックする | `internal/retry/doer_test.go::TestDoer_Do_UnitSuffixedRetryAfterIgnored_FallsBackToExponential` | test | Phase 5 |
| AC-17 | 有効な HTTP-date の既存解釈が変わらない | `internal/retry/doer_test.go::TestDoer_Do_FutureRetryAfterHTTPDate_UsesParsedDelay`・`TestDoer_Do_NonPositiveRetryAfterFallsBackToExponential`（既存、無変更で再実行） | test | Phase 5 |

NF-001（`make fmt`/`make test`/`make lint` の成功）は10章の実装チェックリストで検証する。NF-002（新規外部依存の非追加）は `go.mod`/`go.sum` に差分が生じないことをもって検証する（`git diff --stat go.mod go.sum` が空であること）。NF-003・NF-004 は各 Phase の設計・実装が [02_architecture.md](02_architecture.md) の該当節をそのまま踏襲していることをもって検証する（11章参照）。

## 10. 実装チェックリスト

- [x] Phase 1（F-001）完了
- [x] Phase 2（F-004）完了
- [x] Phase 3（F-002）完了
- [x] Phase 4（F-003）完了
- [x] Phase 5（F-005）完了
- [ ] `rg -n "http.DefaultClient" cmd/main.go` の結果が0件であることを確認する（Phase 3 の置き換え漏れがないことの静的確認）
- [ ] `rg -n "atproto.NewRedirectRejectingHTTPClient\(\)" cmd/main.go` の結果が1件（`run(...)` 呼び出し箇所）であることを確認する
- [ ] `rg -n 'DeleteRecord\(' --type go` の全結果が `Post` 引数を渡す形（または `Post` を受け取るシグネチャ定義自体）になっていることを目視確認する（Phase 1 完了後の残存箇所チェック）
- [ ] `make fmt` を実行し差分がないことを確認する
- [ ] `make test` が成功する
- [ ] `make lint` が成功する
- [ ] `make deadcode` を実行し、新規に追加した公開シンボル（`NewRedirectRejectingHTTPClient`・`collectionForPostType` 等）が意図通り参照されている（デッドコードでない）ことを確認する

## 11. 次のステップ

本計画書のレビュー・承認後、Phase 1（F-001）から順に実装に着手する。実装中に本計画からの逸脱が必要と判明した場合は、[02_architecture.md](02_architecture.md) の該当節を先に更新してから本計画書を追従させる（[_context.md](../../../.claude/commands/_context.md) の Process convention に従う）。
