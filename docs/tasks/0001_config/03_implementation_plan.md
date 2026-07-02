# 設定管理 — 実装計画書

## Document Status

| Item | Value |
|---|---|
| Status | `approved` |
| Created | 2026-07-02 |
| Review date | 2026-07-20 |
| Reviewer | isseis |
| Comments | - |

## 1. 実装概要

### 1.1 目的

[要件定義書](01_requirements.md) の AC-01〜AC-13 を満たす `internal/config` パッケージを新設し、TOML 設定ファイルと環境変数から検証済みの設定値（`AppConfig`）を読み込めるようにする。設計の詳細（データ構造・エラーハンドリング・セキュリティ考慮事項）は [02_architecture.md](02_architecture.md) を参照し、本書では実装手順と検証方法のみを扱う。

### 1.2 実装方針

- [02_architecture.md](02_architecture.md) 8節「実装優先順位」の Phase 1〜4 の順序をそのまま踏襲する。
- 各 Phase の完了条件として `make fmt && make test && make lint` が green であることを必須とする（NF-001）。Phase 1 では `//go:build test` タグ付きの `test_helpers.go` を新設するのに合わせて、`make test` 自体の定義も変更する（2章 Phase 1 参照）。そのため、Phase 1 完了以降に本書で「`make test`」と書く場合は、変更後の定義を指す。
- Go ソースのコメント・識別子・文字列リテラルはすべて英語で記述する（CLAUDE.md の Source-language rule）。

### 1.3 既存コード調査結果

現時点で `internal/` 配下にはパッケージが1つも存在せず（`cmd/main.go` はプレースホルダーのみで設定読み込みロジックを持たない）、[Package Reference](../../dev/developer_guide/package_reference.md) も「パッケージ未実装」の状態である。したがって本タスクは全面的に新規実装であり、既存コードの変更・再利用は発生しない。

一方で以下の外部依存・標準ライブラリ API は、実装前に事実確認を行った（数値・シグネチャは実装時に変わりうるため実装時に再確認すること）:

- `github.com/pelletier/go-toml/v2`（pkg.go.dev のドキュメントおよび README で確認、2026-07-02 時点）:
  - `DecodeFile` に相当する関数は存在しない。ファイルからの読み込みは呼び出し側で `os.ReadFile(path)`（または `os.Open` + `io.Reader`）を行った上で `toml.Unmarshal(data, v any) error` に渡す、もしくは `toml.NewDecoder(io.Reader).Decode(v any) error` を使う必要がある。本タスクでは後述のとおり未知キー検出に `DisallowUnknownFields()` を用いるため、`os.ReadFile` + `toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&raw)` を採用する（[02_architecture.md](02_architecture.md) 2.3 節のシーケンス図は元々「バイト列を読んでからデコードする」形を想定しており、変更は不要）。
  - 未知キー（デコードされなかったキー）の検出は `toml.NewDecoder(r).DisallowUnknownFields()` による厳格モードで行う。該当時は `*toml.StrictMissingError` が返る。この型は `Unwrap() []error`（v2.3.0 以降、`errors.Join` 互換）を実装しているため、`errors.As`/`errors.Is` で判定可能。
  - `time.Duration` 型に対する独自解釈（整数→ナノ秒等）の組み込みサポートは持たない。[02_architecture.md](02_architecture.md) 3.1 節の設計どおり、TOML 側は `execution_timeout_seconds`（プレーンな整数）として受け取り、Go 側で明示的に秒→`time.Duration` へ変換する実装を行う。`BurntSushi/toml` の場合と異なり、これは「独自解釈を避けるための回避策」ではなく、単純に「対応する組み込み機能が無いので自前で変換する」という素直な実装になる。
  - Go 1.18 以上が必要（本プロジェクトは Go 1.26.2 のため問題なし）。README には「Go の直近2メジャーバージョンをサポートする」旨の記載がある。
- `errors.AsType[T]`（`go doc errors` で確認、Go 1.26.2 同梱の標準ライブラリ）: `errors.AsType[*fs.PathError](err) (*fs.PathError, bool)` の形で存在する。
- `log/slog.LogValuer`（`go doc log/slog LogValuer` で確認）: `LogValue() Value` を1メソッドに持つインターフェースとして存在する。
- `testing.T.Setenv(key, value string)`（`go doc testing.T.Setenv` で確認）: プロセス環境変数を設定し、テスト終了時に自動復元する。並列テスト（`t.Parallel()`）とは併用不可。
- `os.ReadFile(name string) ([]byte, error)`（`go doc os.ReadFile` で確認、標準ライブラリ）: 内部で `os.Open` を呼び出し、オープンに失敗した場合はそのエラー（`*fs.PathError`）をそのまま返す。したがって `errors.AsType[*fs.PathError](err)` の成否で「ファイルオープン失敗」と「（オープン後の）構文エラー等」を判別できる（2章 Phase 1 の `Load()` 実装で利用する）。
- `.golangci.yml`（本リポジトリの実ファイルで確認）: `gosec` は `_test.go` のみ除外対象で、通常の `.go` ファイルには適用される。`Load(path string)` が `path` をそのまま `os.ReadFile`（または `os.Open`）に渡す実装は `gosec` の G304（"Potential file inclusion via variable"）に該当しうる。該当した場合の対応は2章 Phase 1 のタスクに含める。
- `Makefile`・`.github/workflows/ci.yml`（本リポジトリの実ファイルで確認）: `make test` および CI の `Test` ステップはいずれも `go test ./...` であり、`-tags test` を付与していない。[test_organization.md](../../dev/developer_guide/test_organization.md) が要求する `//go:build test` タグ付きファイル（`test_helpers.go` 等）は、このビルドタグを有効にしない限りコンパイル対象に含まれない（`go help build` の `-tags` 説明で確認）。本タスクが `internal/config/test_helpers.go` を新設する最初のタスクであるため、`Makefile`・CI 側で `-tags test` を有効化する変更が必要であり、2章 Phase 1 のタスクに含める。

## 2. 実装ステップ

### Phase 1 — TOML 読み込みの基盤（AC-01, AC-02, AC-03, AC-04）

対象 AC: AC-01（正常マッピング）, AC-02（構文エラー検知）, AC-03（ファイル不在検知）, AC-04（必須項目欠落検知）。

**ファイル**: `go.mod`, `go.sum`, `Makefile`, `.github/workflows/ci.yml`, `internal/config/errors.go`, `internal/config/config.go`, `internal/config/test_helpers.go`, `internal/config/config_test.go`, `internal/config/errors_test.go`

- [x] `go get github.com/pelletier/go-toml/v2@latest` を実行し、`go.mod`/`go.sum` にバージョン固定で依存を追加する（ネットワークアクセスを伴うため、実行前にユーザーの承認を得ること）。
- [x] `Makefile` の `test` ターゲットを `go test ./...` から `go test -tags test ./...` に変更する。`.github/workflows/ci.yml` の `test` ジョブ「Test」ステップも同様に `go test ./...` から `go test -tags test ./...` に変更する（1.3 節のとおり、`//go:build test` タグ付きファイルを実際にテストビルドへ含めるために必須の変更。本タスクが `test_helpers.go` を導入する最初のタスクであるため、ここでリポジトリ全体の設定を変更する）。
- [x] `internal/config/errors.go` を新設し、センチネルエラー `ErrFileNotFound`, `ErrParseFailed`, `ErrMissingField`, `ErrInvalidValue`, `ErrMissingEnv`（[02_architecture.md](02_architecture.md) 4節のとおり）を定義する。Phase 3 で追加する範囲検証・環境変数検証でも同じ5つを再利用するため、ここで全種類をまとめて定義する（Phase 3 側で新規にセンチネルエラーを追加する必要はない）。このうち `ErrInvalidValue`・`ErrMissingEnv` は Phase 1 のコードからは参照されない（初回使用は Phase 2・Phase 3）。`make lint`（`unused`）が green であることを Phase 1 完了条件で確認する際、この2つが未使用として指摘されないことも合わせて確認する（golangci-lint の `unused` はエクスポートされた識別子を既定では対象としないため通常は問題にならない想定だが、本タスクは `internal/config` パッケージを新設する最初のケースであるため実測で確認する）。
- [x] `internal/config/errors.go` に `FieldError` 型（`Field string`, `Value string`, `Err error`）を定義し、`Error() string`（`Field` と `Err` の内容を含むメッセージを組み立てる）と `Unwrap() error`（`Err` を返す）を実装する。
- [x] `internal/config/config.go` に公開構造体 `Config`（`RetentionDays int`, `Schedule string`, `ExecutionTimeout time.Duration`）を定義する。
- [x] `internal/config/config.go` に非公開構造体 `rawConfig`（`RetentionDays *int`, `Schedule *string`, `ExecutionTimeoutSeconds *int`、それぞれ `toml:"retention_days"` 等のタグを付与）を定義する。ポインタ型により「未設定（`nil`）」と「明示的なゼロ値」を区別する（[02_architecture.md](02_architecture.md) 3.1 節）。
- [x] `internal/config/config.go` に `Load(path string) (*Config, error)` を実装する:
  - `os.ReadFile(path)` でファイル読み込みを行う（`path` は呼び出し元がコマンドライン引数等から解決した信頼できる値であり、外部入力の直接埋め込みではない旨をコメントで明記する。1.3 節のとおり `gosec` の G304 で検知された場合は、この呼び出し1行のみを対象にした `//nolint:gosec` とその理由を説明する短いコメントを追加する）。
  - `os.ReadFile` の戻り値のエラーが `nil` でない場合、`errors.AsType[*fs.PathError](err)` で判定する。真の場合はファイルオープン失敗と判断し、`fmt.Errorf("...: %w: %w", ErrFileNotFound, err)` で元のエラーを保持したままラップする（1.3 節で確認したとおり `os.ReadFile` は内部の `os.Open` 失敗時のエラーをそのまま返すため）。
  - 読み込んだバイト列を `toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&raw)` でデコードする。エラーが `nil` でない場合、未知キー検出（`*toml.StrictMissingError`）・TOML 構文エラーのいずれであっても `fmt.Errorf("...: %w: %w", ErrParseFailed, err)` で同一にラップする（[02_architecture.md](02_architecture.md) 4節）。両ケースとも最終的なラップ結果（`ErrParseFailed` + 元エラー）が同一であるため、`errors.AsType[*toml.StrictMissingError]` によるケース分岐は実装しない（YAGNI。今後どちらかのケースでメッセージを分ける必要が生じた場合に追加する）。元エラー（`err`）自体は `%w` で保持されるため、呼び出し元が `err.Error()` の文字列を見れば `*toml.StrictMissingError` かどうかは判別可能。
  - `raw.RetentionDays`・`raw.Schedule`・`raw.ExecutionTimeoutSeconds` のいずれかが `nil` の場合、該当フィールド名を `Field` に、`Value` を空文字列に設定した `*FieldError`（`Err: ErrMissingField`）を返す（AC-04 の必須項目欠落検知。欠落時は報告すべき「実際の値」が存在しないため `Value` は常に空文字列とする。この時点では範囲検証は行わず、Phase 3 で `validateConfig()` に統合する）。
  - すべて存在する場合は `rawConfig` から `Config`（`ExecutionTimeoutSeconds` は `time.Duration(seconds) * time.Second` に変換）を組み立てて返す。
- [x] `internal/config/test_helpers.go` を新設し、`//go:build test` タグを付与する。`package config` とし、`writeTempTOML(t *testing.T, content string) string`（`t.TempDir()` と `os.WriteFile` を用いて一時 TOML ファイルを書き出しパスを返すヘルパー。4.2 節参照）を実装する。
- [x] `internal/config/config_test.go` に表駆動テストを実装する（詳細は 4章参照）。
- [x] `internal/config/errors_test.go` に `TestFieldError_ErrorAndUnwrap` を実装する（詳細は 4章参照）。
- [x] `make fmt && make test && make lint` が green であることを確認する。

### PR-1 作成ポイント: TOML config loading foundation

**対象ステップ**: Phase 1

**推奨タイトル**: `feat(0001): add TOML config loading foundation`

**レビュー観点**: TOML デコードエラー（構文エラー / 未知キー）の分類が `ErrParseFailed` に正しく集約されているか / `rawConfig` のポインタ判定による必須項目欠落検知（AC-04）の網羅性 / `os.ReadFile` の `gosec` G304 対応（`//nolint:gosec` の適用範囲とコメント） / `test_helpers.go` 導入に伴う `Makefile`・CI の `-tags test` 変更がリポジトリ全体の `make test` に与える影響（`internal/config/test_helpers.go` が `-tags test` を要求する最初のファイルであるため、CI/Makefile 変更とパッケージ新設を1つの PR に含めることは不可避である点に留意する。CI/Makefile 変更単独では検証対象コードがなく独立した PR として意味を持たないため分割していない） / Phase 1 時点で未使用の `ErrInvalidValue`・`ErrMissingEnv` が `make lint` で指摘されていないか

- [x] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [x] PR を作成した（https://github.com/isseis/bsky-cleaner/pull/11）
- [x] PR がマージされた
- [x] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

### Phase 2 — 秘匿情報の取り扱い（AC-05, AC-06, AC-07, AC-13）

対象 AC: AC-05（環境変数からの読み込み）, AC-06（必須環境変数欠落検知）, AC-07（秘匿情報の非表示化）, AC-13（Slack Webhook URL の任意項目としての読み込み）。

**ファイル**: `internal/config/secret.go`, `internal/config/credentials.go`, `internal/config/secret_test.go`, `internal/config/credentials_test.go`

- [x] `internal/config/secret.go` に `SecretString` 型（非公開フィールド `value string`）を定義する。
- [x] `SecretString` に `Reveal() string`（`value` をそのまま返す）を実装する。
- [x] `SecretString` に `String() string` と `GoString() string` を実装し、いずれも固定文字列（例: `"[REDACTED]"`）を返す（AC-07）。
- [x] `SecretString` に `LogValue() slog.Value` を実装し、`slog.StringValue("[REDACTED]")` のような固定値を返す（AC-07, NF-003）。
- [x] `internal/config/credentials.go` に `Credentials` 構造体（`Handle string`, `AppPassword SecretString`, `SlackSuccessWebhookURL SecretString`, `SlackFailureWebhookURL SecretString`）を定義する。
- [x] `internal/config/credentials.go` に `LoadCredentials() (*Credentials, error)` を実装する:
  - `os.LookupEnv("BSKY_HANDLE")` と `os.LookupEnv("BSKY_APP_PASSWORD")` を呼び出し、いずれかが未設定の場合は該当する環境変数名を `Field` に設定した `*FieldError`（`Err: ErrMissingEnv`, `Value` は空文字列のまま）を返す（AC-06。[02_architecture.md](02_architecture.md) 4節「設計方針」のとおり `Credentials` 由来のエラーは `Value` を埋めない）。
  - `os.Getenv("BSKY_SLACK_WEBHOOK_URL_SUCCESS")` と `os.Getenv("BSKY_SLACK_WEBHOOK_URL_FAILURE")` を呼び出す（任意項目のため「未設定」と「明示的な空文字列」を区別する必要がなく、`os.LookupEnv` の`ok`判定は不要。`os.Getenv` は未設定時に空文字列を返すため、そのまま `SecretString` のゼロ値相当として扱える）。値が存在しない場合はエラーにせず、対応するフィールドをゼロ値の `SecretString` のままにする(AC-13)。この時点では URL 形式の検証は行わず、Phase 3 で `validateCredentials()` に統合する。
  - 取得した値から `Credentials` を組み立てて返す（AC-05）。
- [x] `internal/config/secret_test.go` に `SecretString` の非表示化を検証するテストを実装する（詳細は 4章参照）。
- [x] `internal/config/credentials_test.go` に `t.Setenv()` を用いた表駆動テストを実装する（詳細は 4章参照）。
- [x] `make fmt && make test && make lint` が green であることを確認する。

### PR-2 作成ポイント: credential and secret handling

**対象ステップ**: Phase 2

**推奨タイトル**: `feat(0001): add secret-masked credentials loading`

**レビュー観点**: `SecretString` のすべての出力経路（`String`/`GoString`/`LogValue`）で元の値が漏洩しないか（AC-07, NF-003） / 必須環境変数欠落時の `FieldError.Value` が空文字列のままであること（秘匿値を誤って含めていないか） / Slack Webhook URL 未設定時に非エラーでゼロ値の `SecretString` になる挙動が AC-13 と整合しているか

- [x] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [x] PR を作成した（https://github.com/isseis/bsky-cleaner/pull/13）
- [x] PR がマージされた
- [x] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

### Phase 3 — 検証（AC-08, AC-09, AC-10, AC-11）

対象 AC: AC-08（`retention_days` の 0 以下・未設定の拒否）, AC-09（`retention_days` の正常系）, AC-10（実行タイムアウトの 0 以下・未設定の拒否）, AC-11（Slack Webhook URL の形式検証）。AC-04・AC-06・AC-13 についても、Phase 1・Phase 2 で導入した検証ロジックを、ここで `validateConfig()`・`validateCredentials()` として改めて整理する。

**ファイル**: `internal/config/validate.go`, `internal/config/config.go`（`Load()` の呼び出し先を差し替え）, `internal/config/credentials.go`（`LoadCredentials()` の呼び出し先を差し替え）, `internal/config/config_test.go`, `internal/config/credentials_test.go`

- [x] `internal/config/validate.go` を新設し、`validateConfig(raw rawConfig) (Config, error)` を実装する。Phase 1 で `Load()` に直接書いた欠落検出ロジック（`nil` チェックと `*FieldError` 生成）をこの関数に移し、`Load()` からは `validateConfig()` を呼び出す形に変更する。
- [x] `validateConfig()` に `retention_days` の範囲検証を追加する: 値が `0` 以下の場合、`Field: "retention_days"`, `Value: strconv.Itoa(*raw.RetentionDays)`, `Err: ErrInvalidValue` の `*FieldError` を返す（AC-08）。正の整数の場合のみ `Config.RetentionDays` に値を設定する（AC-09）。
- [x] `validateConfig()` に実行タイムアウトの範囲検証を追加する: `execution_timeout_seconds` が `0` 以下の場合、`Field: "execution_timeout_seconds"`, `Value` に元の整数値の文字列表現, `Err: ErrInvalidValue` の `*FieldError` を返す（AC-10）。加えて、`time.Duration`（`int64` ナノ秒）へ変換した際にオーバーフローしないよう、秒→ナノ秒変換前の生値の段階で上限（24時間 = `86400` 秒、[02_architecture.md](02_architecture.md) 3.1 節「`ExecutionTimeout` の表現形式とオーバーフロー対策」で言及されている値）を超える場合も同様に `ErrInvalidValue` の `*FieldError` を返す。上限値はパッケージ内の名前付き定数として定義する（例: `maxExecutionTimeoutSeconds = 24 * 60 * 60`）。
- [x] `internal/config/validate.go` に `validateCredentials(handle, appPassword string, slackSuccessURL, slackFailureURL string) (Credentials, error)` を実装する。Phase 2 で `LoadCredentials()` に直接書いた必須環境変数の欠落検出ロジックをこの関数に移し、`LoadCredentials()` からは `validateCredentials()` を呼び出す形に変更する。実装時の変更点: 必須環境変数の欠落検出は `os.LookupEnv` の `ok` 判定ではなく、`validateCredentials()` に渡す文字列が空文字列かどうかで判定する形にした（`validateCredentials()` のシグネチャが文字列のみを受け取るため）。`LoadCredentials()` 側は `BSKY_HANDLE`・`BSKY_APP_PASSWORD` も含めた4変数すべてを `os.Getenv` で読み込むように変更した。必須環境変数が「未設定」と「明示的な空文字列」を区別する要件は AC-06 にないため、両者を同じ `ErrMissingEnv` として扱うこの変更は要件を満たす。
- [x] `validateCredentials()` に Slack Webhook URL の形式検証を追加する: 値が空文字列でない場合のみ `net/url.Parse` で構文解析し、パースエラーまたは `u.Scheme != "https"` の場合に `Field` を対応する環境変数名（`"BSKY_SLACK_WEBHOOK_URL_SUCCESS"` または `"BSKY_SLACK_WEBHOOK_URL_FAILURE"`）とした `*FieldError`（`Err: ErrInvalidValue`, `Value` は空文字列のまま）を返す（AC-11）。空文字列（未設定）の場合はエラーにせずスキップする（AC-13 の継続確認）。
- [x] `internal/config/config_test.go` に境界値テストを追加する（詳細は 4章参照）。
- [x] `internal/config/credentials_test.go` に Slack Webhook URL 形式検証のテストを追加する（詳細は 4章参照）。
- [x] `make fmt && make test && make lint` が green であることを確認する。

### PR-3 作成ポイント: config and credential validation

**対象ステップ**: Phase 3

**推奨タイトル**: `feat(0001): add config value validation`

**レビュー観点**: `retention_days`・`execution_timeout_seconds` の境界値判定（0 以下、上限 86400 秒超過、オーバーフロー対策の順序）の正確性 / Slack Webhook URL のスキーム検証ロジック（`https` 限定、空文字列はスキップ） / `Load()`・`LoadCredentials()` を `validateConfig()`・`validateCredentials()` 呼び出しへ差し替えるリファクタリングで Phase 1・2 の欠落検知挙動が壊れていないか

- [x] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [ ] PR を作成した
- [ ] PR がマージされた
- [ ] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

### Phase 4 — 統合と文書化（AC-12）

対象 AC: AC-12（統合設定リファレンス文書の作成）。

**ファイル**: `internal/config/app_config.go`, `internal/config/app_config_test.go`, `docs/design/configuration.md`, `docs/dev/developer_guide/package_reference.md`

- [ ] `internal/config/app_config.go` に `AppConfig` 構造体（`Config` と `Credentials` を埋め込み）を定義する。
- [ ] `internal/config/app_config.go` に `LoadAppConfig(path string) (*AppConfig, error)` を実装する: `Load(path)` と `LoadCredentials()` を呼び出し、いずれかがエラーを返した場合はそのエラーをそのまま返す。両方成功した場合は `Config` と `Credentials` を埋め込んだ `*AppConfig` を返す。
- [ ] `internal/config/app_config_test.go` に統合テストを実装する（詳細は 4章参照）。
- [ ] `docs/design/configuration.md` を新設し、以下を記載する（[02_architecture.md](02_architecture.md) 3.3 節の表のとおり）:
  - TOML の各項目（`retention_days`, `schedule`, `execution_timeout_seconds`）について、項目名・型・必須/任意・書式や制約（`retention_days` は正の整数、`execution_timeout_seconds` は 1〜86400 の整数）・記述例。
  - 環境変数の各項目（`BSKY_HANDLE`, `BSKY_APP_PASSWORD`, `BSKY_SLACK_WEBHOOK_URL_SUCCESS`, `BSKY_SLACK_WEBHOOK_URL_FAILURE`）について、変数名・必須/任意・書式や制約（Slack Webhook URL はスキームが `https` であることのみを要求し、パス・クエリパラメータの内容には制約を設けない旨を明記する）・記述例。
  - `schedule` は本パッケージでは存在確認のみを行い、cron 構文としての妥当性検証は [0007_docker_distribution](../0007_docker_distribution/01_requirements.md) の `print-schedule` サブコマンドの責務であることを明記する（[02_architecture.md](02_architecture.md) 3.1 節）。
- [ ] `docs/dev/developer_guide/package_reference.md` を更新し、「パッケージ未実装」の記述を、`internal/config` パッケージ（責務: TOML 設定ファイルと環境変数を読み込み、検証済みの設定値を返す）を含む実際のディレクトリ構成に置き換える（CLAUDE.md「Keep Package Reference in sync with `cmd/` and `internal/` as packages are actually added」に対応）。
- [ ] `make fmt && make test && make lint` が green であることを確認する。

### PR-4 作成ポイント: AppConfig integration and documentation

**対象ステップ**: Phase 4

**推奨タイトル**: `feat(0001): integrate AppConfig and add configuration docs`

**レビュー観点**: `LoadAppConfig()` が `Load()`・`LoadCredentials()` のいずれのエラーもそのまま伝播しているか / `docs/design/configuration.md` の記載（TOML キー名・環境変数名・制約・記述例）が実装と一致しているか（AC-12） / `package_reference.md` 更新後に AC-12 の静的検証コマンド（`rg -c "internal/config"` 等）が実際にヒットするか

- [ ] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [ ] PR を作成した
- [ ] PR がマージされた（最終 PR のため、次のブランチへの切り替えは不要）

## 3. 実装順序とマイルストーン

### 3.1 マイルストーン

| マイルストーン | 内容 | 完了条件 |
|---|---|---|
| M1 | Phase 1 完了 | `Load()` が AC-01〜AC-04 の全ケース（正常系・構文エラー・ファイル不在・必須項目欠落）で期待どおり動作し、`config_test.go` が green |
| M2 | Phase 2 完了 | `LoadCredentials()` が AC-05〜AC-07・AC-13 の全ケースで期待どおり動作し、`SecretString` の非表示化が確認できる |
| M3 | Phase 3 完了 | `validateConfig()`・`validateCredentials()` により AC-08〜AC-11 の境界値・形式検証がすべて green |
| M4 (最終) | Phase 4 完了 | `LoadAppConfig()` が `Load`・`LoadCredentials` を正しく合成し、`docs/design/configuration.md` が実装と一致し、AC-01〜AC-13 がすべて green |

Phase は [02_architecture.md](02_architecture.md) 8節の順序どおり直列に進める（各 Phase は前の Phase の型・関数に依存するため並行実装はしない）。

### 3.2 PR 構成

1 PR = 1 Phase として粒度を揃える。各 Phase は前の Phase の型・関数にのみ依存し、後続 Phase のスタブを必要としないため、Phase 単位でグリーンゲート（`make fmt && make test && make lint`）を独立して満たせる。

| PR | 対象ステップ | 主な変更内容 |
|---|---|---|
| PR-1 | Phase 1 | TOML 読み込みの基盤（`Load()`、センチネルエラー、`FieldError`、`test_helpers.go` 新設と `-tags test` 化） |
| PR-2 | Phase 2 | 秘匿情報の取り扱い（`SecretString`、`Credentials`、`LoadCredentials()`） |
| PR-3 | Phase 3 | 検証ロジックの分離・拡充（`validateConfig()`、`validateCredentials()`、境界値・形式検証） |
| PR-4 | Phase 4 | `AppConfig`／`LoadAppConfig()` による統合と設定リファレンス文書化 |

## 4. テスト戦略

既存のテストヘルパー・モックは存在しない（1.3 節）。すべて新規作成する。

### 4.1 単体テスト一覧

**`internal/config/config_test.go`**（Phase 1 で骨格を作成し、Phase 3 で境界値ケースを追加）:
- `TestLoad_ValidConfig`: 妥当な TOML を読み込み、`Config` の全フィールドが期待値と一致することを検証する（AC-01）。
- `TestLoad_SyntaxError`: 構文が不正な TOML を渡し、`errors.Is(err, config.ErrParseFailed)` が真になることを検証する（AC-02）。
- `TestLoad_FileNotFound`: 存在しないパスを渡し、`errors.Is(err, config.ErrFileNotFound)` が真になることを検証する（AC-03）。
- `TestLoad_MissingRequiredFields`: `retention_days`・`schedule`・`execution_timeout_seconds` をそれぞれ単独で欠落させた3ケースを表駆動で検証し、`errors.AsType[*config.FieldError](err)` で取得した `Field` が欠落フィールド名と一致すること、`Value` が空文字列であること（2章 Phase 1 のとおり、欠落時は報告すべき値が存在しないため常に空文字列）、`errors.Is(err, config.ErrMissingField)` が真になることを確認する（AC-04）。
- `TestLoad_UnknownKey`: TOML にタイプミスと思われる未知のキーを含め、`errors.Is(err, config.ErrParseFailed)` が真になることを検証する（[02_architecture.md](02_architecture.md) 3.1 節の未知キー検出設計の確認。AC-04 の診断性を補強する補助テスト）。
- `TestLoad_RetentionDaysValidation`: `retention_days` に `0`・負の値・正の値を与える3ケースを表駆動で検証する。`0`・負の値では `errors.Is(err, config.ErrInvalidValue)` が真になり `FieldError.Value` に元の値の文字列表現が入ること（AC-08）、正の値では読み込みが成功し `Config.RetentionDays` に値が設定されること（AC-09）を確認する。
- `TestLoad_ExecutionTimeoutValidation`: `execution_timeout_seconds` に `0`・負の値・上限（`86400`）を超える値（例: `86401`）・妥当な正の値を与える表駆動テストとする。異常系ケースではすべて `errors.Is(err, config.ErrInvalidValue)` が真になることを、正常系ケースでは `Config.ExecutionTimeout` が `time.Duration(seconds) * time.Second` と一致することを確認する（AC-10）。加えて、`int64` の最大値に近い極端に大きい値（`time.Duration` へのナノ秒変換を単独で行うとオーバーフローする値）のケースも含める。上限チェックは秒→ナノ秒変換より先に実行されるため、このケースも `ErrInvalidValue` で拒否されることを確認する（このケースは、2章 Phase 3 の上限チェックが将来の実装変更で変換の後に回された場合の回帰を検知するために残すものであり、86401 のケースとは異なる意図を持つ）。

**`internal/config/errors_test.go`**（Phase 1 で作成）:
- `TestFieldError_ErrorAndUnwrap`: `*FieldError` の `Error()` が `Field` を含む文字列を返すこと、`Unwrap()` が元のセンチネルエラーを返すこと（`errors.Is` で判定可能なこと）を検証する。

**`internal/config/secret_test.go`**（Phase 2 で作成）:
- `TestSecretString_StringRedacted`: `fmt.Sprintf("%v", s)` と `fmt.Sprintf("%s", s)` の出力に元の値が含まれず、固定のマスク文字列になることを検証する（AC-07）。
- `TestSecretString_GoStringRedacted`: `fmt.Sprintf("%#v", s)` の出力に元の値が含まれないことを検証する（AC-07）。
- `TestSecretString_LogValueRedacted`: `slog.Value` を書き込む `io.Writer` に対して `slog.New(...).Info("msg", "secret", s)` を呼び出し、出力に元の値が含まれないことを検証する（AC-07, NF-003）。
- `TestSecretString_Reveal`: `Reveal()` が元の値をそのまま返すことを検証する。

**`internal/config/credentials_test.go`**（Phase 2 で骨格を作成し、Phase 3 で形式検証ケースを追加）:
- `TestLoadCredentials_Success`: `t.Setenv()` で4つの環境変数すべてを設定し、`Credentials` の各フィールドが期待値と一致することを検証する（AC-05）。
- `TestLoadCredentials_MissingRequiredEnv`: `BSKY_HANDLE`・`BSKY_APP_PASSWORD` をそれぞれ単独で未設定にした2ケースを表駆動で検証し、`errors.Is(err, config.ErrMissingEnv)` が真になり、対応する `*FieldError` の `Field` が環境変数名と一致し、`Value` が空文字列であることを確認する（AC-06）。
- `TestLoadCredentials_SlackWebhookURLOptional`: Slack Webhook URL 用の2環境変数を両方とも未設定にし、エラーにならず該当フィールドがゼロ値の `SecretString` になることを検証する（AC-13）。
- `TestLoadCredentials_SlackWebhookURLInvalidScheme`: Slack Webhook URL に `http://` スキームの値、および構文的に不正な値を与える表駆動テストとし、いずれも `errors.Is(err, config.ErrInvalidValue)` が真になり、対応する `*FieldError` の `Value` が空文字列であることを確認する（AC-11）。

**`internal/config/app_config_test.go`**（Phase 4 で作成）:
- `TestLoadAppConfig_Success`: 妥当な TOML ファイルと `t.Setenv()` による妥当な環境変数を組み合わせ、返却された `*AppConfig` が `Config` と `Credentials` の両方を正しく埋め込んでいることを検証する。
- `TestLoadAppConfig_ConfigLoadError`: TOML ファイルを不正にし、`Load()` 由来のエラーがそのまま `LoadAppConfig()` の戻り値として伝播することを検証する。
- `TestLoadAppConfig_CredentialsLoadError`: 必須環境変数を未設定にし、`LoadCredentials()` 由来のエラーがそのまま `LoadAppConfig()` の戻り値として伝播することを検証する。

### 4.2 テストヘルパー

[test_organization.md](../../dev/developer_guide/test_organization.md) の分類に従う。`writeTempTOML(t *testing.T, content string) string`（一時ディレクトリに TOML ファイルを書き出しパスを返すヘルパー）は `internal/config` パッケージ内の複数テストファイル（`config_test.go`・`app_config_test.go`）から使われるが、他パッケージからは使われないため Classification A（`testutil/`）ではなく Classification B（パッケージ内蔵ヘルパー）とする。作成タスクは2章 Phase 1 に記載済み（`internal/config/test_helpers.go`）。

### 4.3 静的検証

- `docs/design/configuration.md` の記載内容が実装と一致していることを AC-12 の検証として行う（詳細は6章のAC対応表を参照）。
- `docs/dev/developer_guide/package_reference.md` の更新（AC には対応しないが、CLAUDE.md の運用ルールに基づく文書更新であり、ground truth との一致を機械的に確認する）: `rg -c "internal/config" docs/dev/developer_guide/package_reference.md` が1件以上ヒットし、かつ `rg -c "パッケージ未実装" docs/dev/developer_guide/package_reference.md` が0件であることを確認する。

## 5. リスク管理

| リスク | 影響 | 対策 |
|---|---|---|
| `go get` 実行がネットワークアクセスを要し、承認待ちで Phase 1 の着手が遅れる | 進行遅延（小） | Phase 1 着手時点でまず承認を得てから作業を進める。承認が得られるまで他の設計作業（テストケースの洗い出し等）を先行させる |
| `execution_timeout_seconds` の上限値（86400 秒）が要件定義書に明記されておらず、実装判断になる | 実装者間の解釈齟齬（小） | [02_architecture.md](02_architecture.md) 3.1 節が示す「24時間相当」という具体例をそのまま採用し、本書 2章 Phase 3 のタスクで名前付き定数として明記する |
| `go-toml/v2` のバージョンアップにより `DisallowUnknownFields()` 等の挙動が変わる | 低（活発な開発だが破壊的変更はメジャーバージョンでのみ想定） | `go.sum` でバージョン固定するため、意図しないタイミングでの挙動変化は発生しない |

## 6. 受け入れ基準の検証

| AC | 内容 | 検証方法 | 種別 |
|---|---|---|---|
| AC-01 | 妥当な TOML の正しいマッピング | `internal/config/config_test.go::TestLoad_ValidConfig` | test |
| AC-02 | 構文不正な TOML の読み込み失敗 | `internal/config/config_test.go::TestLoad_SyntaxError` | test |
| AC-03 | ファイル不在時の読み込み失敗 | `internal/config/config_test.go::TestLoad_FileNotFound` | test |
| AC-04 | 必須項目欠落時の読み込み失敗 | `internal/config/config_test.go::TestLoad_MissingRequiredFields`（+ 補助: `TestLoad_UnknownKey`） | test |
| AC-05 | 環境変数からの秘匿情報読み込み | `internal/config/credentials_test.go::TestLoadCredentials_Success` | test |
| AC-06 | 必須環境変数欠落時の起動失敗 | `internal/config/credentials_test.go::TestLoadCredentials_MissingRequiredEnv` | test |
| AC-07 | 秘匿情報の非表示化 | `internal/config/secret_test.go::TestSecretString_StringRedacted`, `TestSecretString_GoStringRedacted`, `TestSecretString_LogValueRedacted` | test |
| AC-08 | `retention_days` の 0 以下・未設定拒否 | `internal/config/config_test.go::TestLoad_RetentionDaysValidation`（`0`・負値のケース） | test |
| AC-09 | `retention_days` の正常系 | `internal/config/config_test.go::TestLoad_RetentionDaysValidation`（正の整数のケース） | test |
| AC-10 | 実行タイムアウトの 0 以下・未設定拒否 | `internal/config/config_test.go::TestLoad_ExecutionTimeoutValidation` | test |
| AC-11 | Slack Webhook URL の形式検証 | `internal/config/credentials_test.go::TestLoadCredentials_SlackWebhookURLInvalidScheme` | test |
| AC-12 | 統合設定リファレンス文書の存在と実装との一致 | 以下の静的検証手順を実施する:<br>1. `test -f docs/design/configuration.md` でファイルが存在することを確認する（期待結果: 終了コード `0`）。<br>2. TOML フィールド名と環境変数名は表記が異なる（前者は小文字の backtick 付き `toml:"..."` タグ、後者は大文字の二重引用符付き文字列リテラル）ため、抽出コマンドを2つに分ける。まず `rg -o 'toml:"([a-z_]+)"' -r '$1' internal/config/config.go` で TOML フィールド名（`retention_days`, `schedule`, `execution_timeout_seconds`）を抽出する。次に `rg -o '"(BSKY_[A-Z_]+)"' -r '$1' internal/config/credentials.go` で環境変数名（`BSKY_HANDLE`, `BSKY_APP_PASSWORD`, `BSKY_SLACK_WEBHOOK_URL_SUCCESS`, `BSKY_SLACK_WEBHOOK_URL_FAILURE`）を抽出する。抽出した各項目名が `rg -F '<項目名>' docs/design/configuration.md` で1件以上ヒットすることを確認する（期待結果: 全項目がヒット）。<br>3. `docs/design/configuration.md` の型・必須/任意・制約の記載を実装（`internal/config/config.go`・`internal/config/credentials.go`・`internal/config/validate.go`）と目視で突き合わせ、内容が一致していることを確認する（レビュー時に実施） | static |
| AC-13 | Slack Webhook URL の任意項目としての読み込み | `internal/config/credentials_test.go::TestLoadCredentials_SlackWebhookURLOptional` | test |

## 7. 実装チェックリスト

（各 PR 作成ポイント（2章）のインラインチェックボックスと対応する。ここでは PR 単位の完了状況のみをまとめて確認する）

- [x] PR-1 マージ済み（対象ステップ: Phase 1）
- [x] PR-2 マージ済み（対象ステップ: Phase 2）
- [ ] PR-3 マージ済み（対象ステップ: Phase 3）
- [ ] PR-4 マージ済み（対象ステップ: Phase 4）
- [ ] 全 PR マージ後、`make fmt && make test && make lint` が green
- [ ] `make deadcode` を実行し、未使用コードが残っていないことを確認する

## 8. 完了基準

- **機能面**: AC-01〜AC-13 がすべて 6章の検証方法で green である。
- **品質面**: `make test`・`make lint`・`make deadcode` がすべて成功し、`golangci-lint`（`gosec` 含む）の指摘がゼロである。2章 Phase 1 で想定している `Load()` 内の `os.ReadFile`（または `os.Open`）呼び出しに対する `gosec` G304 の指摘（発生した場合のみ、その1行に限定した `//nolint:gosec` で対応）を除き、それ以外の箇所で `//nolint` によるスコープ抑制が必要な箇所は現時点で想定されない（秘匿情報は `SecretString` でラップするのみで、`InsecureSkipVerify` 等の他のセキュリティリンタが検知する構成要素を含まないため）。
- **セキュリティ面**: `secret_test.go` が NF-003 を直接検証し、`FieldError.Value` が `Credentials` 由来のエラーで常に空文字列であることを 6章 AC-06・AC-11 のテストが確認する。
- **文書面**: `docs/design/configuration.md` が実装と一致し、`docs/dev/developer_guide/package_reference.md` が `internal/config` パッケージを反映している。

## 9. 次のステップ

- 本書のレビューを受け、`approved` へのステータス変更を待つ。
- 承認後、2章の Phase 1 から順に実装に着手する（`runplan` 相当のフローに従う）。
- 実装完了後、[0002_atproto_client](../0002_atproto_client/01_requirements.md)・[0006_slack_notification](../0006_slack_notification/01_requirements.md) など、本パッケージの `AppConfig`/`SecretString.Reveal()` を利用する後続タスク側で、`Reveal()` の呼び出し箇所がログ・通知に値をそのまま渡していないことをコードレビューで確認する（[02_architecture.md](02_architecture.md) 5節「残存リスク」参照）。
