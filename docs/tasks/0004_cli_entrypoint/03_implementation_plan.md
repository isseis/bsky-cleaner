# CLI エントリポイント — 実装計画書

## Document Status

| Item | Value |
|---|---|
| Status | `approved` |
| Created | 2026-07-04 |
| Review date | 2026-07-04 |
| Reviewer | isseis |
| Comments | - |

関連ドキュメント: [要件定義書](01_requirements.md) / [アーキテクチャ設計書](02_architecture.md)

## 1. 実装概要

### 1.1 目的

`internal/report`・`internal/runner` の2パッケージを新設し、`cmd/main.go` からそれらと既存の `internal/config`・`internal/atproto`・`internal/cleanup` を呼び出すことで、設定読み込み → ログイン → 投稿一覧取得 → 削除対象判定 → （dry-run または実削除）→ 結果表示、という一連の処理を実行できる CLI バイナリに仕上げる。

### 1.2 実装方針

[アーキテクチャ設計書](02_architecture.md) 8節の実装優先順位（フェーズ1: `internal/report` → フェーズ2: `internal/runner` → フェーズ3: `cmd/main.go`）にそのまま従う。本計画書ではこれに加えて、品質確認とドキュメント更新をフェーズ4として独立させる（[0003_cleanup_engine](../0003_cleanup_engine/03_implementation_plan.md) と同じ構成）。

設計書 7.1 節は `cmd/main.go` の終了コード決定ロジックをAC-10由来（終了コード1）・AC-11由来（終了コード3）の両方について単体テストで検証することを求めている。`parseFlags` をテスト可能な関数として切り出した設計判断（設計書 3.2.1・付録決定履歴）と同じ理由づけを、この終了コード決定ロジックを含む `cmd/main.go` の残り部分（設定読み込み〜終了コード決定までの一連の呼び出し）にも適用する。具体的には、`main()` 本体は `parseFlags` の呼び出しと `os.Exit` の呼び出しのみに専念させ、実際の組み立て処理は `run(configPath string, apply bool, now time.Time, httpDoer atproto.HTTPDoer, stdout, stderr io.Writer) int` という関数に切り出す。`httpDoer`/`stdout`/`stderr` を引数として受け取れる形にするのは、`os.Exit` を発生させずにテストから終了コードを直接検証するためである。加えて、`atproto.NewClient` に渡す `HTTPDoer` の具体実装を `internal/atproto/testutil.MockHTTPDoer`（既存）に差し替えることで、AC-10 の「クライアント初期化・ログイン・一覧取得の失敗」パスまで含めてネットワーク I/O なしに検証できるようにするためでもある。`main()` は `run` の呼び出し時に `http.DefaultClient`・`os.Stdout`・`os.Stderr` を渡すのみであり、これは設計書 3.3 節が `cmd/main.go` の責務として挙げる「`HTTPDoer` の具体実装の構築」をそのまま担う形になっており、設計書の記述と矛盾しない。

### 1.3 既存コード調査結果

- `cmd/main.go` は `fmt.Println("Hello, bsky-cleaner!")` のみのプレースホルダで、既存のフラグパース・呼び出しロジックは存在しない（確認済み: `cat cmd/main.go`）。既存テスト（`cmd/main_test.go` 等）も存在しない。したがって本タスクは `cmd/main.go` の置き換えであり、既存挙動の後方互換テストは不要。
- `internal/runner`・`internal/report` パッケージはまだ存在しない（`find internal -maxdepth 1 -type d` で確認済み。既存は `internal/atproto`・`internal/cleanup`・`internal/config` の3つのみ）。本タスクはこの2パッケージを新規作成する。
- `internal/atproto.Client` は `Login(ctx, config.SecretString) error`・`ListPosts(ctx) ([]Post, error)`・`DeleteRecord(ctx, rkey string) error` の3メソッドを既に実装済み（`internal/atproto/session.go:81`・`internal/atproto/posts.go:122`・`internal/atproto/delete.go:35`）。設計書 3.1 節の `runner.Client` インターフェースはこの3メソッドのシグネチャとそのまま一致するため、`*atproto.Client` は追加の変更なしに `runner.Client` を満たす。
- `internal/cleanup.SelectDeletionTargets(posts []atproto.Post, retentionDays int, now time.Time) []atproto.Post` は実装・テストとも完了済み（`internal/cleanup/cleanup.go:20`、0003_cleanup_engine で AC-01〜AC-08 が検証済み）。本タスクは判定ロジックを再実装せず、この関数をそのまま呼び出す。
- `internal/config.LoadAppConfig(path string) (*AppConfig, error)`（`internal/config/app_config.go:13`）は `Config`（`RetentionDays` 等）と `Credentials`（`Handle`・`AppPassword`）を1回の呼び出しで取得できる、既に完成した唯一のエントリポイントである。本タスクはこれをそのまま呼び出し、`Load`/`LoadCredentials` を個別に呼び出さない。
- `config.SecretString`（`internal/config/secret.go:14`）はエクスポートされたコンストラクタを持たず、非公開フィールド `value` を直接組み立てられるのは `package config` 内のみである。`internal/runner`・`cmd` のテストで `config.SecretString` の値が必要な場合は、既存の `config.LoadCredentials()`（`t.Setenv` で環境変数を設定した上で呼び出す、`internal/config/credentials_test.go` と同じパターン）を再利用して取得し、新規のコンストラクタや `testutil` ヘルパーは追加しない。
- `internal/atproto/testutil.MockHTTPDoer`（`internal/atproto/testutil/mocks.go`）と `internal/atproto/testutil` のレスポンスフィクスチャ関数群（`fixtures.go` の `CreateSessionResponseJSON`・`ListRecordsResponseJSON`・`GetRecordResponseJSON`・`DeleteRecordResponseJSON` 等）は既存かつ公開 API のみに依存しており、`internal/runner`・`cmd` の統合テストからそのまま再利用できる。新規モックは不要。
- `internal/atproto` の DID 解決（`resolveHandleToDID`・`resolveDIDDocument`）は非公開関数 `lookupIPAddr`（パッケージ変数）をテスト用に差し替える `stubSymbolicHostLookup`（`internal/atproto/did_test.go:29`、`//go:build test` 配下ではなく通常の `_test.go` だが `package atproto` 内でのみ参照可能）に依存しており、`internal/atproto` パッケージの外からは差し替えられない。同様に `newTestClient`（`internal/atproto/test_helpers.go:13`、`//go:build test`）も `package atproto` 内限定の非公開ヘルパーである。設計書 7.2 節が要求する「`MockHTTPDoer` を用いて実 `*atproto.Client` を `runner.Client` として注入する結合テスト」を置く場所について、当初は `internal/runner` は本番コードとして `internal/atproto` に依存するのみで逆方向の依存がないため、`internal/atproto` パッケージ内（`package atproto`、`internal/runner` をインポートする新規テストファイル）に置いても import 循環にならないと想定していた。しかし実装時に、`package atproto` の内部テストファイルが `internal/runner`（`internal/atproto` に依存する）をインポートすると、`go test` のパッケージ間循環検出（"import cycle not allowed in test"）に実際に抵触することが判明した（`go vet`/`go test` で再現確認済み）。これは Go の内部テストファイル（`package atproto` のままの `_test.go`）が「テスト対象パッケージ自身の拡張版」として扱われるためで、外部テストパッケージ（`package atproto_test`）であれば同じ組み合わせでも問題にならない。そのため、この結合テストは `internal/atproto/runner_integration_test.go` に `package atproto_test` として置く（詳細は設計書 2.1 節・本計画書フェーズ2の該当タスクを参照）。
- 上記の調査の過程で、`atproto.NewClient` は DID 解決フェーズでのみ呼び出し元の `HTTPDoer` を使い、PDS エンドポイント検証成功後は `Client.httpDoer` を実際に TCP 接続する `newRestrictedDoer`（SSRF/DNS リバインディング対策）に差し替えることも判明した。このため `atproto.NewClient` 経由で構築した `*Client` は、モック `HTTPDoer` を渡していても `Login`/`ListPosts`/`DeleteRecord` の段階で実ネットワーク接続を試みてしまい（`newRestrictedDoer` はループバック・プライベートアドレスへの接続を拒否するため `httptest.Server` でも代替できない）、上記の結合テストに加え、後述する `cmd/main_test.go` の `TestRun_LoginFailure_ReturnsExitCode1` 以降のテストも同じ理由でオフライン実行できないことが分かった。この問題を本番の安全性を弱めずに解消するため、`internal/atproto` に次のテスト専用の差し替え口を追加した（設計書 2.1 節「実装時に判明した `internal/atproto` への追加」参照）。
  - `internal/atproto/client.go`: `httpDoer` の最終組み立てをパッケージ変数 `newPDSDoer`（デフォルトは従来通り `newRestrictedDoer` を返す）に切り出した。
  - `internal/atproto/test_helpers.go`（既存、`//go:build test`）: エクスポートされた `StubPassthroughPDSDoer(t *testing.T)` を追加し、テスト実行中のみ `newPDSDoer` を「元の `httpDoer` をそのまま返す」実装に差し替える。DID 解決・PDS エンドポイント検証（`resolveHandleToDID`/`resolveDIDDocument`/`validatePDSEndpoint`）はこの差し替えの影響を受けない。`newTestClient` と異なりエクスポートされているため、`cmd`（`package main`）や `internal/atproto/runner_integration_test.go`（`package atproto_test`）など `internal/atproto` の外からも呼び出せる。
- `docs/dev/developer_guide/package_reference.md` はまだ `cmd/main.go` を「プレースホルダのみ」と記載しており（1行目）、`internal/runner`・`internal/report` も未記載である。フェーズ4でこれらを更新する。
- `cmd/main_test.go` は `package main` であり、`internal/atproto/did_test.go` の `stubSymbolicHostLookup`（非公開のパッケージ変数 `lookupIPAddr` を差し替える）にも `test_helpers.go` の `newTestClient`（`atproto.NewClient` を経由せず内部フィールドを直接組み立てる）にも一切アクセスできない。したがって `cmd/main_test.go` から `run`（`atproto.NewClient` を呼ぶ）を実行するテストは、`atproto.NewClient` が内部で行う DNS 解決（`internal/atproto/did.go` の `checkRequestHostSafety`/`validatePDSEndpoint` が呼ぶ `net.DefaultResolver.LookupIPAddr`）を実際に発生させてしまい、ネットワーク未接続の CI 環境では失敗する。この問題は、次の挙動を利用して回避する。`netip.ParseAddr` はホスト文字列を先に判定し、渡された文字列が IP リテラルであれば実際の名前解決を一切行わずに即座にそのアドレスを返す（Go 標準ライブラリ `net/lookup.go` の `lookupIPAddr` の実装。`internal/atproto/did_test.go` の `publicIPLiteral`/`stubSymbolicHostLookup` のコメントも同じ前提に依拠している）。具体的には、`cmd/main_test.go` のテストで用いる `BSKY_HANDLE`（`config.LoadCredentials()` 経由で `Credentials.Handle` に渡る値）を `did_test.go` と同じ RFC 5737 の公開 IP リテラル（例: `203.0.113.5`）にし、`MockHTTPDoer` に次の2レスポンスを固定で返させる。`validatePDSEndpoint` は HTTP リクエストを送らず DNS 解決のみを行うため、モックすべき HTTP レスポンスはこの2つで足りる（ただし2つ目を省略すると、DID ドキュメントに PDS サービスエントリが含まれないため `validatePDSEndpoint` が「PDSサービスが見つからない」エラーで失敗し、意図と異なる理由でテストが失敗する）。
  1. `GET https://203.0.113.5/.well-known/atproto-did` → ボディ `did:web:203.0.113.5`（`did:plc:...` にしない。`did:plc:` は固定のシンボリックホスト `plc.directory` の DID ドキュメント URL になり DNS 解決が必要になるため）。
  2. `GET https://203.0.113.5/.well-known/did.json` → `serviceEndpoint` が `https://203.0.113.5` である `AtprotoPersonalDataServer` サービスエントリを含む DID ドキュメント JSON（`atprototestutil` に相当するビルダーがないため、この JSON はテストコード内にリテラルで組み立てる）。
  上記 `serviceEndpoint`（`https://203.0.113.5`）に対する `validatePDSEndpoint` のホスト解決は同じ IP リテラルであるため DNS 解決のみで完結し、追加の HTTP レスポンスをモックする必要はない。

上記の DNS 解決の回避に加え、`TestRun_ClientInitFailure_ReturnsExitCode1`（DID解決・PDS検証失敗で止まる）以外の `TestRun_*` テスト（`TestRun_LoginFailure_ReturnsExitCode1` 以降、`Login`/`ListPosts`/`DeleteRecord` に到達するもの）は、上述の「実装時に判明した `internal/atproto` への追加」で導入した `atproto.StubPassthroughPDSDoer(t)` を各テストの冒頭で呼び出す必要がある。これを呼ばない場合、`run` 内部で構築される `*atproto.Client` は `NewClient` が差し替える実ネットワーク接続の `httpDoer`（`newRestrictedDoer`）を使ってしまい、`MockHTTPDoer` に設定したレスポンスが一切使われずに実接続を試みて失敗する。

この4点（IP リテラルベースの `did:web` 構成、`hermeticHandler` による1・2の自動応答、`StubPassthroughPDSDoer`）がすべて揃って初めて、DID 解決からログイン・一覧取得・削除に至るまで一切の実ネットワーク I/O なしに `cmd/main_test.go` から `run` を駆動できる。フェーズ3の各テストタスクはこの構成を前提とし、`hermeticHandler` ヘルパー（後述）が1・2を常に自動応答し、テストごとに `createSession`/`listRecords`/`getRecord`/`deleteRecord` のレスポンスのみを追加設定できるようにする。

## 2. 実装ステップ

アーキテクチャ設計書 8節の実装優先順位に沿って記載する。

### フェーズ1: `internal/report` パッケージの実装（NF-005 の実装基盤、AC-04・AC-06・AC-09 の表示ロジック）

- [x] **対象ファイル**: `internal/report/report.go`（新規作成）
  - **作業内容**:
    - `package report` を宣言する。
    - 設計書 3.1 節の型定義をそのまま実装する: `Mode`（`ModeDryRun`/`ModeApply` の2値）、`DeleteFailure{ Post atproto.Post; Err error }`、`Result{ Mode Mode; Targets, Deleted []atproto.Post; Failed []DeleteFailure }`。
    - `FormatText(r Result) string` を実装する。`r.Mode == ModeDryRun` の場合は `r.Targets` の一覧（rkey を含む）を表示し、`len(r.Targets) == 0` のときは「削除対象なし」に相当する文言を出力する（AC-06）。`r.Mode == ModeApply` の場合は `len(r.Deleted)` と `len(r.Failed)` の件数に加え、`r.Failed` の各要素について rkey と `Err.Error()` を1件ずつ列挙する（AC-09、設計書 3.2.3）。
  - **完了基準**: `go build ./...` が成功する。フェーズ1のテストがすべて通過する。

- [x] **対象ファイル**: `internal/report/report_test.go`（新規作成）
  - **作業内容**: 設計書 7.1 節の4パターンをテストする。
    - [x] `TestFormatText_DryRun_WithTargets`: `Mode: ModeDryRun`、`Targets` に2件以上の投稿を設定し、出力に各投稿の rkey が含まれることを検証する（AC-04）。
    - [x] `TestFormatText_DryRun_NoTargets`: `Mode: ModeDryRun`、`Targets: nil` の場合、エラーにならず「削除対象なし」に相当する文言が出力に含まれることを検証する（AC-06）。
    - [x] `TestFormatText_Apply_AllSucceeded`: `Mode: ModeApply`、`Deleted` に2件、`Failed` が空の場合、出力に削除件数 `2` が含まれ、失敗件数が `0`（または失敗なしを示す文言）であることを検証する（AC-09 の一部）。
    - [x] `TestFormatText_Apply_WithFailures`: `Mode: ModeApply`、`Deleted` に1件、`Failed` に rkey・`error` の異なる2件を設定し、出力に失敗件数 `2` に加え、各失敗の rkey と対応するエラーメッセージの両方が個別に含まれることを検証する（AC-09、設計書 3.2.3 が要求する「件数だけでなく個々の失敗内容」）。
  - **完了基準**: `make test` で `internal/report` パッケージの全テストが成功する。各テストは `assert`/`require`（`github.com/stretchr/testify`）でアサーションを記述する（CLAUDE.md テスト方針）。

### フェーズ2: `internal/runner` パッケージの実装（F-002・F-003、AC-05・AC-07・AC-08・AC-09（データ生成側）・AC-10・AC-11）

- [x] **対象ファイル**: `internal/runner/runner.go`（新規作成）
  - **作業内容**:
    - `package runner` を宣言し、設計書 3.1 節の `Client` インターフェースをそのまま定義する: `Login(ctx context.Context, appPassword config.SecretString) error`・`ListPosts(ctx context.Context) ([]atproto.Post, error)`・`DeleteRecord(ctx context.Context, rkey string) error`。
    - `Run(ctx context.Context, client Client, appPassword config.SecretString, retentionDays int, apply bool, now time.Time) (*report.Result, error)` を実装する。処理順序は設計書 2.2 節のシーケンス図の通り: `client.Login` → `client.ListPosts` → `cleanup.SelectDeletionTargets` → (`apply` が `false` ならここで終了、`true` なら `Targets` の各要素に対して `client.DeleteRecord` をループ呼び出し)。
    - `Login`/`ListPosts` のエラーはラップせずそのまま `Run` の戻り値として返す（設計書 4節、AC-10）。
    - `apply == false` の分岐では `client.DeleteRecord` を一切呼び出さない（AC-05）。この分岐は設計書 5.1 節の通り、`runner.Run` 内の単一の `if apply` 分岐でのみ制御する。
    - `apply == true` の分岐では、`Targets` の各要素について `DeleteRecord` を呼び出し、エラーが返れば `Result.Failed` に `DeleteFailure{Post: post, Err: err}` を追加して次の要素の処理を継続し、成功すれば `Result.Deleted` に追加する（AC-07・AC-11）。ループ内で `DeleteRecord` の呼び出し結果が確定するたびに、成否を問わず `slog.Default()` 経由で rkey と成否を含む1行のログを出力する（設計書 6節）。
    - `Run` 自体はネットワーク I/O を行わず、渡された `Client` 経由でのみ `atproto` を呼び出す（設計書 3.3 節）。
  - **完了基準**: `go build ./...` が成功する。以下のテストがすべて通過する。

- [x] **対象ファイル**: `internal/runner/test_helpers.go`（新規作成、`//go:build test`）
  - **作業内容**: 設計書 7.1 節が指定する「テスト専用の軽量モック」を実装する。`Client` インターフェースを満たす `fakeClient` 構造体を定義し、次を設定可能にする: `LoginErr error`（`Login` が返すエラー）、`ListPostsResult []atproto.Post`・`ListPostsErr error`、`DeleteRecordErrs map[string]error`（rkey ごとに返すエラー。マップに存在しない rkey は成功として扱う）。呼び出された `DeleteRecord` の rkey を呼び出し順に記録するフィールド（例: `DeleteRecordCalls []string`）も持たせ、テストが「どの rkey に対して呼ばれたか」「呼ばれた回数」の両方をアサートできるようにする。
  - **完了基準**: `internal/runner` 配下のテストからのみ参照され、`//go:build test` タグにより本番ビルドに含まれないことを `go build ./...`（タグなし）でも確認する。

- [x] **対象ファイル**: `internal/runner/runner_test.go`（新規作成）
  - **作業内容**: `fakeClient` を注入し、以下を検証する。
    - [x] `TestRun_DryRun_DoesNotCallDeleteRecord`: `apply=false`、`fakeClient.ListPostsResult` に削除対象条件を満たす投稿を含めた上で `Run` を実行し、`fakeClient.DeleteRecordCalls` が空であることを検証する（AC-05）。
    - [x] `TestRun_Apply_CallsDeleteRecordForAllTargets`: `apply=true`、`SelectDeletionTargets` が複数件を削除対象と判定するような投稿一覧を与え、`fakeClient.DeleteRecordCalls` が判定された対象全件の rkey を含むことを検証する（AC-07）。
    - [x] `TestRun_Apply_RespectsCleanupExclusions_PinnedAndRetention`: ピン留め投稿・保持期間内の投稿・削除対象条件を満たす投稿を混在させた一覧を与え、`fakeClient.DeleteRecordCalls` にピン留め投稿・保持期間内投稿の rkey が含まれず、削除対象条件を満たす投稿の rkey のみが含まれることを検証する（AC-08。`Run` は実際の `cleanup.SelectDeletionTargets` をそのまま呼び出すため、このテストは `runner` が判定結果を追加でフィルタしていないことの確認である）。
    - [x] `TestRun_Apply_PartialFailure_ContinuesAndRecordsFailure`: `fakeClient.DeleteRecordErrs` で複数件中の一部にエラーを設定し、`Run` がエラーを返さず（`err == nil`）、`Result.Deleted`・`Result.Failed` に成功・失敗それぞれの投稿が正しく振り分けられること、かつ全対象に対して `DeleteRecord` が呼ばれた（失敗後も処理が継続した）ことを検証する（AC-11、AC-09 のデータ生成側）。
    - [x] `TestRun_LoginError_ReturnsErrorWithoutListingOrDeleting`: `fakeClient.LoginErr` を設定し、`Run` がそのエラーをそのまま返すこと（`errors.Is`/`errors.AsType[T]` ではなく `assert.Equal`/`assert.ErrorIs` で同一性を確認する。同一の `error` 値をラップせず返す設計のため）、かつ `ListPosts`/`DeleteRecord` が一度も呼ばれていないことを検証する（AC-10 の一部）。
    - [x] `TestRun_ListPostsError_ReturnsErrorWithoutDeleting`: `fakeClient.ListPostsErr` を設定し、`Run` がそのエラーをそのまま返すこと、かつ `DeleteRecord` が一度も呼ばれていないことを検証する（AC-10 の一部）。
    - [x] `TestRun_LogsEachDeleteRecordOutcome`: `apply=true`、成功・失敗それぞれ1件以上を含む対象一覧で `Run` を実行する前に `slog.SetDefault` を `slog.NewTextHandler` でバッファ書き込みするロガーに差し替え（`t.Cleanup` で元に戻す）、実行後のログ出力に各 rkey と成否を示す文字列が含まれることを検証する（設計書 6節。特定の AC には対応しないが、クラッシュ耐性のための逐次ログという設計上の要求を退行させないための回帰テストとして追加する）。
    - [x] `appPassword` が必要なテストでは、`t.Setenv` で `BSKY_HANDLE`/`BSKY_APP_PASSWORD`/`BSKY_SLACK_WEBHOOK_URL_SUCCESS`/`BSKY_SLACK_WEBHOOK_URL_FAILURE` を設定した上で `config.LoadCredentials()` を呼び出し、その戻り値の `AppPassword` フィールドを利用する（1.3 節、新規ヘルパー不要）。
  - **完了基準**: `make test` で `internal/runner` パッケージの全テストが成功する。

- [x] **対象ファイル**: `internal/atproto/runner_integration_test.go`（新規作成、`package atproto_test`）
  - **作業内容**: 設計書 7.2 節が要求する「実 `*atproto.Client` を `runner.Client` として注入する結合テスト」を実装する。`internal/runner` をインポートするため、`package atproto`（内部テスト）ではなく `package atproto_test`（外部テストパッケージ）とする（実装時に判明: `internal/runner` は `internal/atproto` に依存するため、`package atproto` のテストファイルが `internal/runner` をインポートすると `go test` レベルの import cycle になる。詳細は 1.3 節・設計書 2.1 節「実装時に判明した `internal/atproto` への追加」を参照）。`atprototestutil.MockHTTPDoer` に DID 解決（`.well-known/atproto-did`・`.well-known/did.json`、1.3 節と同じ IP リテラルベースの `did:web` 構成を使い実 DNS 解決を避ける）・ログイン・投稿一覧取得・投稿削除の一連のレスポンスをスクリプトし、`atproto.NewClient` で得た `*Client` に対して `atproto.StubPassthroughPDSDoer(t)`（新規追加のテスト専用エクスポート関数、設計書 2.1 節参照）を適用した上で、そのまま `runner.Run` の `client` 引数として渡して実行する。`StubPassthroughPDSDoer` を使わない場合、`NewClient` が内部で `httpDoer` を実ネットワーク接続する `restrictedDoer` に差し替えてしまい、モックが素通りされてしまう。この結合テストの目的はインターフェースの形状不一致の検出（設計書 7.2 節）であり、判定ロジックや異常系の網羅は `internal/runner/runner_test.go`（`fakeClient` 使用）側の責務とするため、正常系1パターン（dry-run または apply いずれか1件成功）のみで十分とする。
  - **完了基準**: `make test` で本テストが成功する。

### PR-1 作成ポイント: report and runner packages

**対象ステップ**: フェーズ1 / フェーズ2

**推奨タイトル**: `feat(0004-cli-entrypoint): implement internal/report and internal/runner`

**レビュー観点**: `apply` 分岐が `runner.Run` 内の1箇所に限定されていること（5.1 節の副作用契約） / dry-run で `DeleteRecord` が呼ばれないことのテストが実際に存在すること / 部分失敗時にループが継続すること / `internal/atproto/runner_integration_test.go` が import 循環を起こさずビルドできること

- [ ] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [ ] PR を作成した
- [ ] PR がマージされた
- [ ] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

### フェーズ3: `cmd/main.go` の実装（F-001・F-004、AC-01〜AC-03・AC-10・AC-11 の組み立て）

- [ ] **対象ファイル**: `cmd/main.go`（変更、プレースホルダを置き換え）
  - **作業内容**:
    - `parseFlags(args []string, stderr io.Writer) (configPath string, apply bool, err error)` を実装する。`flag.NewFlagSet` を `flag.ContinueOnError` で構築し `SetOutput(stderr)` を設定した上で、`--config`/`-c`（同じ変数を指す2つの `StringVar`）と `--apply`（`BoolVar`、デフォルト `false`）を登録し `fs.Parse(args)` する。`fs.Parse` 自体がエラーを返した場合はそのエラーをそのまま返す（`ContinueOnError` モードでは `fs.Parse` が使用方法を `stderr` に書き込み済みのため、追加の出力は不要）。`fs.Parse` が成功しても `fs.NArg() > 0`（未知の位置引数が残っている）場合、または `configPath == ""`（`--config`/`-c` 未指定）の場合は、`fs.Usage()` を呼んでから独自のエラーを返す（AC-01・AC-02・AC-03）。
    - `run(configPath string, apply bool, now time.Time, httpDoer atproto.HTTPDoer, stdout, stderr io.Writer) int` を実装する。処理順序は設計書 3.2.4 節の通り: `config.LoadAppConfig(configPath)` → `atproto.NewClient(ctx, cfg.Handle, httpDoer)` → `runner.Run(ctx, client, cfg.AppPassword, cfg.RetentionDays, apply, now)`。3呼び出しのいずれかがエラーを返した場合、`err.Error()` を `stderr` にそのまま書き込み、終了コード `1` を返す（AC-10、設計書 4節よりマスキングは既存エラー型が担うため追加処理不要）。`runner.Run` が `nil` エラーで戻った場合は `report.FormatText(*result)` を `stdout` に書き込み、`len(result.Failed) > 0` なら終了コード `3`、それ以外は `0` を返す（AC-11、設計書 3.2.4 の終了コード表）。
    - `main()` を、`parseFlags(os.Args[1:], os.Stderr)` の呼び出し（失敗時は `os.Exit(2)`）、`time.Now()` の1回だけの呼び出し、`run(configPath, apply, now, http.DefaultClient, os.Stdout, os.Stderr)` の呼び出し、その戻り値での `os.Exit` のみに専念させる（NF-002、設計書 3.2.4 が要求する「`time.Now()` を1回だけ呼び出しループ中に再取得しない」ことも満たす）。
  - **完了基準**: `go build ./cmd` が成功する。以下のテストがすべて通過する。

- [ ] **対象ファイル**: `cmd/main_test.go`（新規作成、`package main`）
  - **作業内容**: `parseFlags`・`run` を直接呼び出し、以下を検証する。`run` を経由して `atproto.NewClient` の DID 解決まで実行するテスト（`TestRun_ClientInitFailure_ReturnsExitCode1` 以降すべて）は、1.3 節に記載した「`BSKY_HANDLE` を公開 IP リテラル（例: `203.0.113.5`、`internal/atproto/did_test.go` の `publicIPLiteral` と同じ RFC 5737 アドレス）にし、`.well-known/atproto-did` のレスポンスを `did:web:203.0.113.5` にすることで DID ドキュメント URL・PDS エンドポイントの双方のホストを同じ IP リテラルに揃える」という構成を用い、実 DNS 解決が一切発生しないようにする。この一連のレスポンスのうち、1.3 節の1・2（`.well-known/atproto-did` と `.well-known/did.json`、常に固定値を返す）を自動応答する非公開ヘルパー関数（例: `hermeticHandler(t *testing.T, next func(*http.Request) (*http.Response, error)) func(*http.Request) (*http.Response, error)`、`cmd/main_test.go` 内に定義。他パッケージから参照されないため `testutil/`・`test_helpers.go` は不要）を用意する。`next` には各テストが `createSession`/`listRecords`/`getRecord`/`deleteRecord` のリクエストだけをハンドルする関数を渡し、`hermeticHandler` は自身が応答すべき2つのURL（1.3 節の1・2）以外のリクエストを `next` にそのまま委譲する。`createSession`/`listRecords`/`deleteRecord` に到達するテスト（`TestRun_LoginFailure_ReturnsExitCode1` 以降すべて）は、`run` を呼び出す前に `atproto.StubPassthroughPDSDoer(t)`（1.3 節参照）を呼び、`NewClient` が `httpDoer` を実ネットワーク接続する `restrictedDoer` に差し替えないようにする。
    - [ ] `TestParseFlags_ConfigLongFlag_Accepted`: `["--config", "path/to.toml"]` で `configPath == "path/to.toml"` が返ることを検証する（AC-01）。
    - [ ] `TestParseFlags_ConfigShortFlag_Accepted`: `["-c", "path/to.toml"]` でも同様に受理されることを検証する（AC-01、エイリアス）。
    - [ ] `TestParseFlags_ApplyNotSpecified_DefaultsToFalse`: `--config` のみを指定し `apply == false` が返ることを検証する（AC-02、NF-003 の一部）。
    - [ ] `TestParseFlags_ApplySpecified_True`: `--apply` を追加指定した場合に `apply == true` が返ることを検証する（AC-02 の対照ケース）。
    - [ ] `TestParseFlags_MissingConfig_ReturnsError`: `--config`/`-c` のいずれも指定しない場合にエラーが返り、`err != nil` であることを検証する（AC-03）。
    - [ ] `TestParseFlags_UnknownFlag_ReturnsError`: 未定義のフラグ（例: `--unknown`）を指定した場合にエラーが返ることを検証する（AC-03）。
    - [ ] `TestParseFlags_UnexpectedPositionalArgument_ReturnsError`: `["--config", "path/to.toml", "extra-arg"]` のように余分な位置引数を渡した場合にエラーが返ることを検証する（AC-03）。
    - [ ] `TestRun_ConfigLoadFailure_ReturnsExitCode1`: 存在しないパスを `configPath` に渡し、`run` の戻り値が `1` であることを検証する（AC-10。実ファイルシステムに対する `config.LoadAppConfig` の既存の失敗パスをそのまま利用し、モック不要。`atproto.NewClient` に到達する前に失敗するため、上記のハンドル解決用ヘルパーも不要）。
    - [ ] `TestRun_ClientInitFailure_ReturnsExitCode1`: 上記のハンドル解決用ヘルパーを使わず、`.well-known/atproto-did` が非2xxを返す（またはDIDドキュメントの `serviceEndpoint` が存在しない）レスポンスを設定し、`run` の戻り値が `1` であることを検証する（AC-10）。
    - [ ] `TestRun_LoginFailure_ReturnsExitCode1`: ハンドル解決用ヘルパーでDID解決・PDS検証は成功させつつ、`createSession` が非2xxを返すレスポンスを追加設定し、`run` の戻り値が `1` であることを検証する（AC-10）。
    - [ ] `TestRun_DryRunWithTargets_ReturnsExitCode0AndPrintsTargets`: `apply=false`、削除対象を含む投稿一覧が返るようレスポンスを設定し、`run` の戻り値が `0`、`stdout` に対象の rkey が含まれることを検証する（AC-04）。
    - [ ] `TestRun_DryRunNoTargets_ReturnsExitCode0AndPrintsNoTargetsMessage`: 削除対象が0件になるレスポンスを設定し、`run` の戻り値が `0`、`stdout` に「削除対象なし」に相当する文言が含まれることを検証する（AC-06）。
    - [ ] `TestRun_ApplyAllSucceed_ReturnsExitCode0AndPrintsResult`: `apply=true`、`deleteRecord` が全件2xxを返すレスポンスを設定し、`run` の戻り値が `0`、`stdout` に削除件数が含まれることを検証する（AC-07・AC-09）。
    - [ ] `TestRun_ApplyPartialFailure_ReturnsExitCode3AndPrintsFailures`: `apply=true`、`deleteRecord` の一部が非2xxを返すレスポンスを設定し、`run` の戻り値が `3`、`stdout` に失敗件数と失敗した rkey が含まれることを検証する（AC-11・AC-09）。
  - **完了基準**: `make test` で `cmd` パッケージの全テストが成功する（ネットワーク未接続の環境でも成功すること。実 DNS 解決が発生していないことは、テスト実行環境のネットワークを切断した状態で再実行しても結果が変わらないことで確認できる）。AC-10 の3テスト（設定読み込み・クライアント初期化・ログイン）はいずれも同じ終了コード `1` を返すことを検証し、AC-11 のテストとは異なる終了コード（`3`）であることを対比できるようにする。

### PR-2 作成ポイント: main.go wiring

**対象ステップ**: フェーズ3

**推奨タイトル**: `feat(0004-cli-entrypoint): wire up cmd/main.go`

**レビュー観点**: `main()` が組み立てと `os.Exit` の呼び出しのみに専念し判定ロジックを持ち込んでいないこと（NF-002） / AC-10 由来（終了コード1）と AC-11 由来（終了コード3）が明確に区別されテストされていること / `time.Now()` の呼び出しが1箇所のみであること

- [ ] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [ ] PR を作成した
- [ ] PR がマージされた
- [ ] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

### フェーズ4: 品質確認とドキュメント更新

- [ ] **対象コマンド**: `make fmt` / `make test` / `make lint`
  - **作業内容**: 3コマンドを順に実行し、いずれもエラーなく完了することを確認する（NF-001）。
  - **完了基準**: 3コマンドすべてが正常終了する。
- [ ] **対象コマンド**: `make deadcode`
  - **作業内容**: `internal/report`・`internal/runner`・`cmd/main.go` に起因する新規の未使用コードが検出されないことを確認する。
  - **完了基準**: `cmd/main.go` が本タスクで初めて `config`/`atproto`/`cleanup`/`runner`/`report` を実際に呼び出すため、0003 フェーズ3で許容していた「`cmd/` 未結線に起因する一律 `unreachable` 検出」は本タスク完了後に解消されているはずである。解消されていない公開関数が残っている場合は、その関数が本タスクのスコープ外（0005〜0008 で使用予定）であることをコメントではなくこの完了基準の実行結果として明記する。
- [ ] **対象ファイル**: `docs/dev/developer_guide/package_reference.md`
  - **作業内容**: 以下を更新する。
    1. 「Directory Structure」の `cmd/` の説明行を次のように更新する（変更前: `` `main.go`: placeholder only, no config loading logic yet (see docs/tasks/0004_cli_entrypoint) ``、変更後: `` `main.go`: parses CLI flags (`--config`/`-c`, `--apply`) and wires config/atproto/cleanup/runner/report into a runnable CLI (see docs/tasks/0004_cli_entrypoint) ``）。
    2. 同じく「Directory Structure」の `internal/` 一覧に以下の2行を追加する（`cleanup/` の行に続けて）。
       ```
       - `runner/`: wires config/atproto/cleanup together into a single dry-run/apply run, producing a report.Result (see docs/tasks/0004_cli_entrypoint)
       - `report/`: structured run result (Result/Mode/DeleteFailure) and its stdout text rendering (FormatText), independent of how the result was produced (see docs/tasks/0004_cli_entrypoint)
       ```
    3. 「Package Responsibilities」に、既存の見出し形式に合わせて以下を追加する。
       ```
       **Runner**

       - `internal/runner`: performs one wiring pass (`Run`) -- login, list posts, judge deletion targets via `cleanup.SelectDeletionTargets`, and (apply mode only) delete each target, continuing past individual failures. Depends on `atproto`/`cleanup`/`config`/`report` only through the package-local `Client` interface, so tests inject a fake instead of a real network client (see docs/tasks/0004_cli_entrypoint/01_requirements.md).

       **Report**

       - `internal/report`: the structured outcome of a run (`Result`/`Mode`/`DeleteFailure`), independent of how it is rendered, plus `FormatText` for stdout rendering. Reused as-is by a future Slack formatter (see docs/tasks/0006_slack_notification/01_requirements.md).
       ```
  - **完了基準**: 以下の `rg` コマンドがそれぞれ期待通りの結果になる。
    - `rg -n "internal/runner" docs/dev/developer_guide/package_reference.md` — 2件以上ヒットする（Directory Structure の1行 + Package Responsibilities の1行以上）。
    - `rg -n "internal/report" docs/dev/developer_guide/package_reference.md` — 2件以上ヒットする（同様）。
    - `rg -n "placeholder only" docs/dev/developer_guide/package_reference.md` — 0件（更新前の文言が残っていないことを確認）。

### PR-3 作成ポイント: quality gates and documentation

**対象ステップ**: フェーズ4

**推奨タイトル**: `docs(0004-cli-entrypoint): update package reference for runner and report`

**レビュー観点**: 追記内容が既存の `internal/atproto`・`internal/cleanup` の記述粒度・文体と一致していること / `cmd/main.go` の説明が実装後の実態と一致していること

- [ ] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [ ] PR を作成した
- [ ] PR がマージされた

## 3. 実装順序とマイルストーン

### 3.1 マイルストーン

| マイルストーン | 内容 | 成果物 |
|---|---|---|
| M1 | `internal/report`・`internal/runner` の実装と単体テストが完了し `make test` が緑になる | `internal/report/report.go`・`internal/report/report_test.go`・`internal/runner/runner.go`・`internal/runner/test_helpers.go`・`internal/runner/runner_test.go`・`internal/atproto/runner_integration_test.go` |
| M2 | `cmd/main.go` の実装（`parseFlags`・`run`・`main`）が完了し、AC-01〜AC-11 全件が test/static で緑になる | `cmd/main.go`・`cmd/main_test.go` |
| M3 | `make lint`・`make deadcode` の完走と `package_reference.md` の更新が完了する | 更新済み `docs/dev/developer_guide/package_reference.md` |

### 3.2 PR 構成

| PR | 対応フェーズ | 概要 |
|---|---|---|
| PR-1 | フェーズ1 / フェーズ2 | `internal/report`・`internal/runner` パッケージの新設 |
| PR-2 | フェーズ3 | `cmd/main.go` の実装（CLI としての結線） |
| PR-3 | フェーズ4 | 品質確認完了後の `package_reference.md` 更新 |

## 4. テスト戦略

### 4.1 単体テスト

- `internal/report`: `FormatText` を dry-run（対象あり/なし）・apply（全件成功/一部失敗）の4パターンで検証する（フェーズ1、AC-04・AC-06・AC-09）。
- `internal/runner`: `test_helpers.go` の `fakeClient` を注入し、dry-run で `DeleteRecord` が呼ばれないこと（AC-05）、apply で全対象に `DeleteRecord` が呼ばれること（AC-07）、クリーンアップエンジンの除外結果が尊重されること（AC-08）、部分失敗時の継続と振り分け（AC-11）、`Login`/`ListPosts` のエラー伝播（AC-10 の一部）、逐次ログ出力（設計書 6節）を検証する（フェーズ2）。
- `cmd`（`package main`）: `parseFlags` の受理・拒否パターン（AC-01〜AC-03）と、`run` の終了コード決定ロジック（AC-04・AC-06・AC-07・AC-09・AC-10・AC-11）を検証する（フェーズ3）。

### 4.2 統合テスト

- `internal/atproto/runner_integration_test.go`: `atprototestutil.MockHTTPDoer` を用いて実 `*atproto.Client` を `runner.Client` として注入し、インターフェースの形状不一致を検出する最小限の結合テストを1本用意する（設計書 7.2 節、フェーズ2）。
- NF-003（「dry-run がデフォルト」の明示的な検証）は、`cmd/main_test.go::TestParseFlags_ApplyNotSpecified_DefaultsToFalse`（フラグ未指定で `apply=false` が返る）と `internal/runner/runner_test.go::TestRun_DryRun_DoesNotCallDeleteRecord`（`apply=false` で `DeleteRecord` 呼び出し0回）を組み合わせて満たす（設計書 7.2 節）。両テストが揃って初めて「フラグ未指定 → 実削除が一切発生しない」という一連の振る舞いが検証される。
- `cmd/main_test.go` の `TestRun_*` 群は `atprototestutil.MockHTTPDoer` を用いて DID解決〜削除までの一連のレスポンスをスクリプトし、`run` 関数を通じた end-to-end の終了コード決定（AC-10・AC-11）を検証する。

### 4.3 後方互換テスト

該当なし。`cmd/main.go` は既存がプレースホルダのみであり、`internal/report`・`internal/runner` は新設パッケージのため、後方互換の考慮事項はない（1.3 節）。

### 4.4 テストヘルパ方針

[test_organization.md](../../dev/developer_guide/test_organization.md) の分類に従う。

- `internal/runner/test_helpers.go`: `Client` インターフェースを満たす `fakeClient` は `internal/runner` 自身のテストでのみ使用し、他パッケージから参照されないため、Classification B（`test_helpers.go`、`//go:build test`）に置く（設計書 7.1 節が明示的にこの配置を指定している）。
- `cmd/main_test.go` 内のハンドル解決用ヘルパー（1.3 節・フェーズ3参照）は `cmd/main_test.go` 自身の複数テストからのみ呼ばれる非公開関数であり、他ファイル・他パッケージからは参照されないため、独立した `test_helpers.go` を新設する必要はない（同一テストファイル内のヘルパー関数で足りる）。
- 上記以外の新規テストヘルパー（`testutil/` サブディレクトリ、追加の `test_helpers_<category>.go`）は不要と判断する。`internal/atproto/testutil`（既存）・`config.LoadCredentials()`（既存の公開関数）を再利用することで、`internal/report`・`internal/runner`・`cmd` いずれのテストも新規の共有モックを必要としない（1.3 節）。

## 5. リスク管理

| リスク | 影響 | 対策 |
|---|---|---|
| `parseFlags`/`run` の分離が不十分で `main()` に判定ロジックが残る（NF-002違反） | `cmd/main.go` が肥大化し、テストできないロジックが増える | フェーズ3のレビュー観点（PR-2）で `main()` の行数・内容を確認し、`run`/`parseFlags` 以外の分岐を持ち込んでいないことをレビューで確認する |
| AC-10（設定/初期化/ログイン/一覧取得の失敗、終了コード1）と AC-11（一部削除失敗、終了コード3）の終了コードの実装を取り違える | cron・監視ツールが「何も起きていない失敗」と「一部既に削除された失敗」を区別できなくなる（設計書 3.2.4 の運用上の理由） | フェーズ3で両方の終了コードを明示的にテストし（`TestRun_ConfigLoadFailure_ReturnsExitCode1` 等 vs `TestRun_ApplyPartialFailure_ReturnsExitCode3AndPrintsFailures`）、PR-2 のレビュー観点で両者の対比を確認する |
| `internal/atproto/runner_integration_test.go` が `internal/runner` をインポートすることで、意図せず import 循環やビルドタグの不整合を生む | `make test` が失敗する、または `go build ./...`（本番タグなし）が壊れる | フェーズ2の完了基準で `go test -tags test ./...` と `go build ./...` の両方を確認する。`internal/runner` の本番コードが `internal/atproto` にのみ依存し、逆方向の依存を持たないことをレビューで確認する（PR-1 レビュー観点） |
| スケジュールリスク: 本タスクは3つのフェーズにまたがり結合点が多いため、0003 単体より若干規模が大きい | 軽微〜中程度 | フェーズ1・2（新設パッケージ）とフェーズ3（結線）を別々の PR に分離済みのため、フェーズ間で問題が発覚してもロールバック範囲が限定される |

## 6. 実装チェックリスト

- [ ] PR-1 マージ済み（対象ステップ: フェーズ1 / フェーズ2。`internal/report`・`internal/runner` パッケージ新設、AC-04〜AC-11 関連の単体・結合テスト追加）
- [ ] PR-2 マージ済み（対象ステップ: フェーズ3。`cmd/main.go` 実装、AC-01〜AC-03・AC-10・AC-11 関連テスト追加）
- [ ] PR-3 マージ済み（対象ステップ: フェーズ4。`package_reference.md` 更新）
- [ ] `make fmt` / `make test` / `make lint` がすべて通過
- [ ] `make deadcode` で本タスクに起因する新規の未使用コードがないことを確認

## 7. 受け入れ基準の検証（Acceptance Criteria Verification）

| AC | 検証種別 |
|---|---|
| AC-01〜AC-11 | test |

**AC-01: 設定ファイルパスを指定するフラグ `--config`（エイリアス: `-c`）を受け付け、指定されたパスの TOML を読み込む**
- Test location: `cmd/main_test.go::TestParseFlags_ConfigLongFlag_Accepted`、`cmd/main_test.go::TestParseFlags_ConfigShortFlag_Accepted`
- Implementation: `cmd/main.go`（`parseFlags`）
- Verification method: test（`--config`/`-c` それぞれを指定して `parseFlags` を呼び、返る `configPath` が指定値と一致することをアサートする。TOML の読み込み自体は `internal/config.Load`（既存、0001_config で検証済み）が担うため、本タスクでは `parseFlags` が正しいパスを `config.LoadAppConfig` に渡すことのみを検証する）

**AC-02: `--apply` フラグを指定しない場合、常に dry-run モードで動作する**
- Test location: `cmd/main_test.go::TestParseFlags_ApplyNotSpecified_DefaultsToFalse`、`cmd/main_test.go::TestParseFlags_ApplySpecified_True`
- Implementation: `cmd/main.go`（`parseFlags` の `apply` の `BoolVar` デフォルト値）
- Verification method: test（`--apply` を指定しない場合に `apply == false`、指定した場合に `apply == true` が返ることをアサートする）

**AC-03: 不正なフラグ・引数が指定された場合（`--config`/`-c` が指定されなかった場合を含む）、使用方法を示すメッセージとともに非 0 の終了コードで終了する**
- Test location: `cmd/main_test.go::TestParseFlags_MissingConfig_ReturnsError`、`cmd/main_test.go::TestParseFlags_UnknownFlag_ReturnsError`、`cmd/main_test.go::TestParseFlags_UnexpectedPositionalArgument_ReturnsError`
- Implementation: `cmd/main.go`（`parseFlags`、および `main()` の `os.Exit(2)` 分岐）
- Verification method: test（3パターンいずれも `parseFlags` が非 `nil` の `err` を返すことをアサートする。`main()` が `parseFlags` のエラー時に終了コード `2` で `os.Exit` する部分は、`os.Exit` 自体を直接テストせず、`parseFlags` がエラーを返すことと `main()` の実装レビュー（PR-2 レビュー観点）で確認する）

**AC-04: dry-run モードで実行した場合、削除対象と判定された投稿の一覧が標準出力に表示される**
- Test location: `internal/report/report_test.go::TestFormatText_DryRun_WithTargets`、`cmd/main_test.go::TestRun_DryRunWithTargets_ReturnsExitCode0AndPrintsTargets`
- Implementation: `internal/report/report.go`（`FormatText`）、`cmd/main.go`（`run`）
- Verification method: test（`report_test.go` は `FormatText` の出力に対象の rkey が含まれることを直接アサートする。`main_test.go` は `MockHTTPDoer` で一連のレスポンスをスクリプトし、`run` の `stdout` 引数に書き込まれた内容に対象の rkey が含まれることをアサートする end-to-end の検証である）

**AC-05: dry-run モードで実行した場合、AT Protocol の削除 API（`deleteRecord`）が一切呼び出されない**
- Test location: `internal/runner/runner_test.go::TestRun_DryRun_DoesNotCallDeleteRecord`
- Implementation: `internal/runner/runner.go`（`Run` の `if apply` 分岐）
- Verification method: test（`apply=false` で `Run` を実行し、`fakeClient.DeleteRecordCalls` が空であることをアサートする）

**AC-06: 削除対象が0件の場合でも、エラーにならず「削除対象なし」に相当する出力を行う**
- Test location: `internal/report/report_test.go::TestFormatText_DryRun_NoTargets`、`cmd/main_test.go::TestRun_DryRunNoTargets_ReturnsExitCode0AndPrintsNoTargetsMessage`
- Implementation: `internal/report/report.go`（`FormatText`）
- Verification method: test（`Targets` が空の `Result` を渡し、`FormatText` がパニックせず「削除対象なし」に相当する文言を返すこと、`main_test.go` 側では `run` の戻り値が `0` であることをアサートする）

**AC-07: `--apply` を指定した場合、削除対象と判定された投稿すべてに対して `deleteRecord` が呼び出される**
- Test location: `internal/runner/runner_test.go::TestRun_Apply_CallsDeleteRecordForAllTargets`、`cmd/main_test.go::TestRun_ApplyAllSucceed_ReturnsExitCode0AndPrintsResult`
- Implementation: `internal/runner/runner.go`（`Run` の apply ループ）
- Verification method: test（`fakeClient.DeleteRecordCalls` が判定対象全件の rkey を含むことをアサートする。`main_test.go` 側は `MockHTTPDoer` が受け取った `deleteRecord` リクエストの件数で end-to-end に確認する）

**AC-08: `--apply` を指定した場合でも、ピン留め投稿・保持期間内の投稿は削除対象に含まれない（クリーンアップエンジンの判定結果を尊重する）**
- Test location: `internal/runner/runner_test.go::TestRun_Apply_RespectsCleanupExclusions_PinnedAndRetention`
- Implementation: `internal/runner/runner.go`（`cleanup.SelectDeletionTargets` の呼び出しをそのまま利用し、追加のフィルタを行わない設計）
- Verification method: test（ピン留め投稿・保持期間内投稿・削除対象投稿を混在させた一覧で `Run` を実行し、`fakeClient.DeleteRecordCalls` に削除対象投稿の rkey のみが含まれることをアサートする。`SelectDeletionTargets` 自体の判定ロジックは 0003_cleanup_engine で検証済みのため、本タスクでは「`runner` がその判定結果をそのまま尊重すること」のみを検証し、判定条件の全パターンを再テストしない）

**AC-09: `--apply` モードでの実行結果（削除件数、失敗があればその件数）が標準出力に表示される**
- Test location: `internal/report/report_test.go::TestFormatText_Apply_AllSucceeded`、`internal/report/report_test.go::TestFormatText_Apply_WithFailures`、`internal/runner/runner_test.go::TestRun_Apply_PartialFailure_ContinuesAndRecordsFailure`、`cmd/main_test.go::TestRun_ApplyAllSucceed_ReturnsExitCode0AndPrintsResult`、`cmd/main_test.go::TestRun_ApplyPartialFailure_ReturnsExitCode3AndPrintsFailures`
- Implementation: `internal/report/report.go`（`FormatText`）、`internal/runner/runner.go`（`Result.Deleted`/`Result.Failed` の構築）
- Verification method: test（`report_test.go` は件数と個々の失敗内容が出力に含まれることを直接アサートし、`runner_test.go` はその入力データ（`Result.Deleted`/`Result.Failed`）が正しく構築されることをアサートする。`main_test.go` は両者を組み合わせた end-to-end の出力内容をアサートする）

**AC-10: 設定読み込み・クライアント初期化（DID解決を含む）・ログイン・一覧取得のいずれかが失敗した場合、削除処理に進まず非 0 の終了コードで終了する**
- Test location: `cmd/main_test.go::TestRun_ConfigLoadFailure_ReturnsExitCode1`、`cmd/main_test.go::TestRun_ClientInitFailure_ReturnsExitCode1`、`cmd/main_test.go::TestRun_LoginFailure_ReturnsExitCode1`、`internal/runner/runner_test.go::TestRun_LoginError_ReturnsErrorWithoutListingOrDeleting`、`internal/runner/runner_test.go::TestRun_ListPostsError_ReturnsErrorWithoutDeleting`
- Implementation: `cmd/main.go`（`run` のエラー分岐、終了コード `1`）、`internal/runner/runner.go`（`Login`/`ListPosts` のエラー伝播）
- Verification method: test（4種類の失敗要因（設定読み込み・クライアント初期化・ログイン・一覧取得）のうち、設定読み込み・クライアント初期化・ログインの3つを `cmd/main_test.go` で `run` の戻り値 `1` として直接検証し、ログイン・一覧取得のエラー伝播については、`internal/runner/runner_test.go` 側で重複を避けつつ検証する。一覧取得失敗の end-to-end 経路は `internal/runner` 側のテストで十分カバーされるため、`cmd/main_test.go` 側に同じシナリオの重複テストは追加しない）

**AC-11: 一部の投稿の削除が失敗した場合でも、残りの投稿の削除処理は継続され、最終的に失敗があったことが分かる形で非 0 の終了コードを返す**
- Test location: `internal/runner/runner_test.go::TestRun_Apply_PartialFailure_ContinuesAndRecordsFailure`、`cmd/main_test.go::TestRun_ApplyPartialFailure_ReturnsExitCode3AndPrintsFailures`
- Implementation: `internal/runner/runner.go`（`Run` の apply ループ内のエラーハンドリング）、`cmd/main.go`（`run` の `len(result.Failed) > 0` 分岐、終了コード `3`）
- Verification method: test（`runner_test.go` は一部失敗時にループが継続し全対象に `DeleteRecord` が呼ばれることをアサートし、`main_test.go` は `run` の戻り値が `3` であること、かつ AC-10 の失敗（終了コード `1`）とは異なる値であることをアサートする）

## 8. 成功基準

- **機能的完全性**: AC-01〜AC-11 がすべて7節のテストで検証され成功する。
- **品質指標**: `make fmt` / `make test` / `make lint` / `make deadcode` がすべてエラーなく完了する（NF-001）。
- **セキュリティ検証**: 設計書 5節の通り、本タスク固有のリスクは「dry-run のつもりが実際に削除してしまう」組み立てミスのみであり、5.1 節の副作用契約（`apply` 分岐を `runner.Run` 内の1箇所に限定）をフェーズ2のテスト（AC-05・AC-07）で検証する。DID解決・SSRF対策・秘密情報マスキングは 0002_atproto_client で、削除対象判定は 0003_cleanup_engine で検証済みのため、本タスクでの追加のセキュリティテストは不要（設計書 7.3 節）。
- **ドキュメント整備**: `docs/dev/developer_guide/package_reference.md` に `internal/runner`・`internal/report` の責務、および `cmd/main.go` の実態が反映されている（フェーズ4、7節の `rg` コマンドで確認）。

## 9. 次のステップ

- [0005_retry_timeout](../0005_retry_timeout/01_requirements.md) は `internal/runner.Client` インターフェースを満たすリトライ付きデコレータを実装し、`cmd/main.go` の `run` に注入する形で本タスクの成果物を再利用する（設計書 9節）。
- [0006_slack_notification](../0006_slack_notification/01_requirements.md) は `internal/report.Result` を入力として Slack 通知用フォーマッタ（例: `FormatSlackBlocks`）を追加する（設計書 9節）。
- [0007_docker_distribution](../0007_docker_distribution/01_requirements.md) の `print-schedule` サブコマンド追加時に、`cmd/main.go` のサブコマンド分岐方針（現状は単一コマンドのみのため、`parseFlags` をそのまま拡張するか `flag.FlagSet` を分ける新しい設計が必要か）を検討する必要がある。
