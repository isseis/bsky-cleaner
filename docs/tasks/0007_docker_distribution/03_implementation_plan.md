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

本計画は [02_architecture.md](./02_architecture.md) に基づき、[01_requirements.md](./01_requirements.md) の AC-01〜AC-14 を満たすための作業を、検証可能な単位に分解する。本タスクは CLI バイナリを内蔵 cron 付きの Docker イメージとして配布するための基盤を整備する。

### 1.2 実装方針

- **設計の再掲を避ける**: 各コンポーネントの設計詳細・エラー型定義・処理フローは [02_architecture.md](./02_architecture.md) を参照し、本計画では「どのファイルに何を追加し、どう検証するか」のみを書く。
- **既存資産の再利用**: `config.Load()` を `print-schedule` から再利用し、TOML パースの二重実装を避ける（AC-03）。既存の終了コード定数（`exitSetupOrRunFail`・`exitUsageError`）を流用する。
- **Go ソースは英語**: 追加する識別子・コメント・文字列リテラルはすべて英語で記述する。
- **各編集後に `make fmt` → `make test` → `make lint` を実行**し、緑を維持する（NF-001）。
- **cron ツール選定理由の記載**: NF-003 に従い、`supercronic` の選定理由は [02_architecture.md 3.2.3 節](./02_architecture.md#323-エントリポイントスクリプトac-08〜10) を参照する形で本計画書に記載する。

### 1.3 既存コード調査結果

各対象について「既存の状態 / 不足 / 変更内容」を示す。変更不要な領域は省く。

**プロダクションコード（Phase 1 対象）**

- `cmd/main.go`
  - 既存: `parseFlags`（`--config` 必須・`--apply` オプションの 2 フラグ構成）、`run`（`config.LoadAppConfig` 経由の完全な実行パス）、`main`（`os.Args[1:]` を `parseFlags` に渡す）、`sendNotification`。終了コード定数 `exitOK`(0)・`exitSetupOrRunFail`(1)・`exitUsageError`(2)・`exitPartialFailure`(3)。
  - 不足: `print-schedule` サブコマンドの分岐、`parsePrintScheduleFlags`、`runPrintSchedule`、`validateSchedule`、`ScheduleValidationError` 型。
  - 変更: `main()` に `len(os.Args) > 1 && os.Args[1] == "print-schedule"` の分岐を追加。`parsePrintScheduleFlags`・`runPrintSchedule`・`validateSchedule` の 3 関数と `ScheduleValidationError` 型を新規追加。
- `internal/config/config.go`
  - 既存: `Load(path string) (*Config, error)` — TOML 読み込み・`validateConfig` による必須フィールド・値域検証。`Config.Schedule` は `string` 型で保持。
  - 不足: なし。`print-schedule` は `config.Load()` をそのまま再利用する（AC-03）。
  - 変更: なし。
- `internal/config/errors.go`
  - 既存: `ErrFileNotFound`・`ErrParseFailed`・`ErrMissingField`・`ErrInvalidValue`・`FieldError`。
  - 不足: なし。`print-schedule` はこれらのエラーを `config.Load()` 経由で受け取り、標準エラー出力に書き出す。
  - 変更: なし。
- `internal/config/app_config.go`
  - 既存: `LoadAppConfig(path string) (*AppConfig, error)` — `Load` + `LoadCredentials` + `validateSlackAllowedHost`。
  - 不足: なし。`print-schedule` は `LoadAppConfig` ではなく `Load` を直接呼ぶ（認証情報不要のため）。
  - 変更: なし。

**テストコード（Phase 1 対象）**

- `cmd/main_test.go`
  - 既存: `TestParseFlags_*`（6 関数）、`TestRun_*`（12 関数）、ヘルパー群（`validConfigPath`・`setEnvCredentials`・`hermeticHandler`・`postPageResponse`・`emptyPageResponse`・`listRecordsHandler`・`deleteRecordHandler`・`slackWebhookHandler`・`assertOnlySlackRequestURL`）。
  - 不足: `print-schedule` 関連のテストケース。
  - 変更: `TestParsePrintScheduleFlags_*`・`TestRunPrintSchedule_*`・`TestValidateSchedule_*` を追加。`validConfigPath` は `config.Load()` が要求する全フィールド（`retention_days`・`schedule`・`execution_timeout_seconds`・`slack_allowed_host`）を含む TOML を生成するため、`print-schedule` のテストでもそのまま再利用可能。

**Docker 関連ファイル（Phase 2・3 対象）**

- リポジトリルート
  - 既存: なし。`Dockerfile`・`entrypoint.sh`・`docker-compose.yml`・`dot.env.example` はすべて新規作成。
  - `.gitignore`: 既に `.env` が含まれている（NF-002 対応済み）。
  - 変更: 4 ファイルを新規追加。

**その他**

- `Makefile`: 既存の `make build`・`make test`・`make lint`・`make fmt` をそのまま使用。Docker ビルド用のターゲットは追加しない（手動検証で対応）。
- `docs/design/docker_deployment.md`: 本タスクの設計の上位文書。`print-schedule` の概念・`.env` 管理方針・内蔵 cron 連携の背景が記載されている。実装時は [02_architecture.md](./02_architecture.md) を一次参照とし、背景説明が必要な場合に本ドキュメントを参照する。

## 2. 実装ステップ

各フェーズは [02_architecture.md 8 章](./02_architecture.md#8-実装優先順位) のフェーズ定義と順序に一致する。

### Phase 1: コア CLI 機能（AC-01〜AC-04）

**対象ファイル**: `cmd/main.go`, `cmd/main_test.go`

- [x] `cmd/main.go`: `ScheduleValidationError` 型を追加する（[02_architecture.md 3.2.1 節](./02_architecture.md#321-print-schedule-サブコマンドac-01〜04) の定義に従う）。
- [x] `cmd/main.go`: `validateSchedule(s string) error` 関数を実装する。cron 5 フィールドの構文検証（改行の不在確認・5 フィールドのパース・各フィールドの値域チェック）を標準ライブラリのみで行う。外部ライブラリに依存しない。
- [x] `cmd/main.go`: `parsePrintScheduleFlags(args []string) (configPath string, err error)` 関数を実装する。`--config`（または `-c`）フラグのみを受け付け、不足時・不明な位置引数存在時にエラーを返す。`flag.FlagSet` を使用し、`parseFlags` と同じエラー報告パターンに従う。
- [x] `cmd/main.go`: `runPrintSchedule(configPath string, stdout, stderr io.Writer) int` 関数を実装する。`config.Load(configPath)` を呼び出し、成功時に `validateSchedule(cfg.Schedule)` で cron 式を検証し、標準出力に書き出す。エラー時は標準エラー出力にエラーメッセージを書き、`exitSetupOrRunFail`(1) を返す。`runPrintSchedule` はフラグを受け取らないため、フラグエラー（`exitUsageError`(2)）は関与しない — それは `parsePrintScheduleFlags`/`main()` 側で `runPrintSchedule` 呼び出し前に判定・返却する（下記ステップ）。
- [x] `cmd/main.go`: `main()` 関数に `len(os.Args) > 1 && os.Args[1] == "print-schedule"` の分岐を追加する（`len(os.Args) > 1` のチェックが無いと、引数なし起動時に `os.Args[1]` の添字アクセスが index out of range で panic する）。`print-schedule` の場合、`parsePrintScheduleFlags(os.Args[2:])` を呼び出し、エラー時は `exitUsageError`(2) を返す。成功時は `runPrintSchedule(configPath, os.Stdout, os.Stderr)` の経路を通る。それ以外は既存の `parseFlags` → `run` 経路をそのまま通る。
- [x] `cmd/main_test.go`: `TestValidateSchedule_*` テストを追加する。正常な cron 式・改行混入・不正な値域・フィールド数不足・空文字列の各ケースを検証する（AC-02, AC-04）。
- [x] `cmd/main_test.go`: `TestParsePrintScheduleFlags_*` テストを追加する。`--config` 正常・`-c` 正常・`--config` 不足・不明フラグ・余分な位置引数の各ケースを検証する（AC-01 間接的）。
- [x] `cmd/main_test.go`: `TestRunPrintSchedule_*` テストを追加する。TOML 正常（`validConfigPath` 再利用）・TOML ファイル不在・TOML パース失敗・`schedule` フィールド不足・`schedule` 値が cron 式として不正の各ケースを検証する（AC-01, AC-03, AC-04）。`config.Load()` のエラーラップ（`ErrFileNotFound`・`ErrParseFailed`・`ErrMissingField`・`ErrInvalidValue`）が標準エラー出力に書き出されることを確認する。

**完了基準**: `make test`・`make lint` が緑。`print-schedule` の全テストケースがパスする。

### PR-1 作成ポイント: print-schedule subcommand

**対象ステップ**: Phase 1

**推奨タイトル**: `feat(0007-docker-distribution): add print-schedule subcommand for cron integration`

**レビュー観点**: `validateSchedule` が改行文字を正しく拒否し（AC-02）、cron 5 フィールドの値域を検証していること（AC-04） / `parsePrintScheduleFlags` が `--config` のみを受け付け、`parseFlags` と一貫したエラー報告パターンを持つこと / `runPrintSchedule` が `config.Load()` を再利用し、TOML パースの二重実装がないこと（AC-03） / `main()` の分岐が既存の `parseFlags` → `run` 経路に影響を与えないこと / テストが正常系・異常系・境界値をカバーしていること

- [x] グリーンゲート（`make test && make lint`）がパスしていることを確認した
- [x] PR を作成した
- [x] PR がマージされた

### Phase 2: Docker 配布基盤（AC-05〜AC-10）

**対象ファイル**: `Dockerfile`（新規）, `entrypoint.sh`（新規）

- [x] `Dockerfile` を作成する。マルチステージビルド構成（ビルドステージ: `golang:alpine` を digest 固定、実行ステージ: `alpine` を digest 固定）。ビルドステージで `go install github.com/aptible/supercronic@<version>`（リリースタグ固定、チェックサムは Go module checksum database で検証）を実行し、実行ステージにビルド済みバイナリ・`supercronic`・`entrypoint.sh` を同梱し、非特権ユーザー（UID 10001）で実行する（AC-05, AC-06, AC-07）。
- [x] `entrypoint.sh` を作成する。`bsky-cleaner print-schedule --config "$BSKY_CONFIG_PATH"` を呼び出し、終了コードが非 0 なら `exit 1` で異常終了（AC-10, fail-closed）。終了コード 0 なら、標準出力の cron 式に続けて `bsky-cleaner --apply --config "$BSKY_CONFIG_PATH"` を記述した crontab 行を `/tmp/crontab` に書き込み、`exec supercronic /tmp/crontab` で内蔵 cron を起動する（AC-08, AC-09）。crontab 行の形式は `"$SCHEDULE bsky-cleaner --apply --config \"$BSKY_CONFIG_PATH\""` とする（02_architecture.md 3.2.3 節の仕様に従う）。
- [x] `docker build` を実行し、イメージが正常にビルドできることを確認する（AC-07）。手動検証。

**完了基準**: `docker build` が成功し、生成されたイメージが期待通りの構成（バイナリ・`supercronic`・`entrypoint.sh`・非特権ユーザー）を持つこと。

### PR-2 作成ポイント: Dockerfile and entrypoint

**対象ステップ**: Phase 2

**推奨タイトル**: `feat(0007-docker-distribution): add Dockerfile and entrypoint script for containerized cron execution`

**レビュー観点**: マルチステージビルドによりビルド用イメージと実行用イメージが分離されていること（AC-05） / ベースイメージが digest で固定されていること（AC-06） / `entrypoint.sh` が `print-schedule` の終了コードを正しく判定し、fail-closed を実現していること（AC-10） / `exec supercronic` によりシグナルが正しく伝播すること（AC-09） / 非特権ユーザーで実行されること

- [x] グリーンゲート（`make test && make lint`）がパスしていることを確認した
- [x] PR を作成した
- [ ] PR がマージされた

### Phase 3: docker-compose 設定と統合テスト（AC-11〜AC-14）

**対象ファイル**: `docker-compose.yml`（新規）, `dot.env.example`（新規）

- [ ] `docker-compose.yml` を作成する。`services.bsky-cleaner` を定義し、`environment:` で `.env` の変数（`BSKY_HANDLE`・`BSKY_APP_PASSWORD`・`BSKY_SLACK_WEBHOOK_URL_SUCCESS`・`BSKY_SLACK_WEBHOOK_URL_FAILURE`）を注入する（AC-11）。TOML 設定ファイルのディレクトリを `/config` に volume mount する（AC-13）。`BSKY_CONFIG_PATH` を `/config/config.toml` に設定する。
- [ ] `dot.env.example` を作成する。4 つの環境変数名をダミー値で列挙する（AC-12）。
- [ ] `docker compose up` による統合動作確認を手動で実施する（AC-13, AC-14）。TOML 設定ファイルを volume mount した状態でコンテナが起動し、`print-schedule` → `supercronic` → 定期実行の流れが動作することを確認する。
- [ ] `docker-compose.yml` に秘匿情報が直接書かれていないことを静的検証する（AC-11）。`rg -n 'BSKY_APP_PASSWORD|BSKY_HANDLE|BSKY_SLACK_WEBHOOK' docker-compose.yml` で `environment:` 行の `${...}` 参照のみがマッチし、リテラル値が存在しないことを確認する。
- [ ] `dot.env.example` が 4 つの環境変数名を列挙していることを静的検証する（AC-12）。`rg -n 'BSKY_HANDLE|BSKY_APP_PASSWORD|BSKY_SLACK_WEBHOOK_URL_SUCCESS|BSKY_SLACK_WEBHOOK_URL_FAILURE' dot.env.example` で 4 行すべてがマッチすることを確認する。

**完了基準**: `docker compose up` によりコンテナが起動し、`supercronic` が定期実行を開始する。静的検証がパスする。

### PR-3 作成ポイント: docker-compose and env example

**対象ステップ**: Phase 3

**推奨タイトル**: `feat(0007-docker-distribution): add docker-compose.yml and dot.env.example for one-command deployment`

**レビュー観点**: `docker-compose.yml` に秘匿情報が直接書かれていないこと（AC-11） / `dot.env.example` が 4 つの環境変数をダミー値で列挙していること（AC-12） / volume mount 設定が正しいこと（AC-13） / `docker compose up` でコンテナが起動し定期実行が開始されること（AC-14）

- [ ] グリーンゲート（`make test && make lint`）がパスしていることを確認した
- [ ] PR を作成した
- [ ] PR がマージされた

## 3. 実装順序とマイルストーン

| マイルストーン | 内容 | 完了の定義 |
|---|---|---|
| M1 | Phase 1 完了 | `print-schedule` サブコマンドが実装され、単体テストが緑。`make test`・`make lint` が緑 |
| M2 | Phase 2 完了 | Dockerfile・`entrypoint.sh` が作成され、`docker build` が成功 |
| M3 | Phase 3 完了 | `docker-compose.yml`・`dot.env.example` が作成され、`docker compose up` による統合動作確認が完了 |

### 3.1 PR 構成

| PR | 対象ステップ | 主な変更内容 |
|---|---|---|
| PR-1 | Phase 1 | `cmd/main.go` に `print-schedule` サブコマンド（`validateSchedule`・`parsePrintScheduleFlags`・`runPrintSchedule`・`ScheduleValidationError`）を追加し、`cmd/main_test.go` にテストを追加 |
| PR-2 | Phase 2 | `Dockerfile`（マルチステージビルド・digest 固定）・`entrypoint.sh`（`print-schedule` 連携・`exec supercronic`）を新規作成 |
| PR-3 | Phase 3 | `docker-compose.yml`（`.env` 変数注入・volume mount）・`dot.env.example`（環境変数名のダミー値列挙）を新規作成 |

## 4. テスト戦略

### 4.1 単体テスト（Phase 1）

配置は [02_architecture.md 7.1 節](./02_architecture.md#71-テスト対象) のとおり。既存の `validConfigPath`・`setEnvCredentials` 等のヘルパーを再利用する。

| 対象 | 検証内容 | 配置ファイル |
|---|---|---|
| `validateSchedule` | 正常な cron 式（`"0 3 * * *"` 等）が `nil` を返すこと | `cmd/main_test.go` |
| `validateSchedule` | 改行を含む値（`"0 3 * * *\n0 4 * * *"`）が `*ScheduleValidationError` を返すこと（AC-02） | `cmd/main_test.go` |
| `validateSchedule` | フィールド数不足（`"0 3 * *"`）がエラーを返すこと（AC-04） | `cmd/main_test.go` |
| `validateSchedule` | フィールド数超過（`"0 3 * * * extra"`）がエラーを返すこと（AC-04） | `cmd/main_test.go` |
| `validateSchedule` | 各フィールドの値域違反（分 60・時 24・日 0・月 13・曜日 8）がエラーを返すこと（AC-04） | `cmd/main_test.go` |
| `validateSchedule` | ワイルドカード（`*`）・ステップ（`*/15`）・範囲（`1-5`）・リスト（`1,3,5`）を含む標準的な cron 式が受理されること | `cmd/main_test.go` |
| `validateSchedule` | 範囲+ステップ（`1-10/2`）・リスト内に範囲を含む形式（`1-5,10-15`）が受理されること | `cmd/main_test.go` |
| `validateSchedule` | `@daily` 等のマクロ形式（5 フィールド構成でない値）がエラーを返すこと | `cmd/main_test.go` |
| `validateSchedule` | 空文字列がエラーを返すこと | `cmd/main_test.go` |
| `parsePrintScheduleFlags` | `--config path/to.toml` でパスが返ること | `cmd/main_test.go` |
| `parsePrintScheduleFlags` | `-c path/to.toml` でパスが返ること | `cmd/main_test.go` |
| `parsePrintScheduleFlags` | フラグなしでエラーが返ること | `cmd/main_test.go` |
| `parsePrintScheduleFlags` | 不明フラグでエラーが返ること | `cmd/main_test.go` |
| `parsePrintScheduleFlags` | 余分な位置引数でエラーが返ること | `cmd/main_test.go` |
| `runPrintSchedule` | 正常な TOML で `schedule` の値が標準出力に 1 行で出力され、終了コード 0 が返ること（AC-01） | `cmd/main_test.go` |
| `runPrintSchedule` | TOML ファイル不在で終了コード 1 が返り、標準エラー出力にエラーが書かれること | `cmd/main_test.go` |
| `runPrintSchedule` | TOML パース失敗で終了コード 1 が返ること | `cmd/main_test.go` |
| `runPrintSchedule` | `schedule` フィールド不足で終了コード 1 が返ること | `cmd/main_test.go` |
| `runPrintSchedule` | `schedule` 値が cron 式として不正で終了コード 1 が返り、標準出力が空であること（AC-04） | `cmd/main_test.go` |

### 4.2 コンテナテスト（Phase 2・3、手動検証）

Docker デーモンを必要とするテストは手動検証手順として記載する。

| 検証内容 | 手順 | 対応 AC |
|---|---|---|
| Docker イメージのビルド成功 | `docker build -t bsky-cleaner .` を実行し、終了コード 0 を確認 | AC-07 |
| コンテナ起動 → `print-schedule` 成功 → `supercronic` 起動 | `docker run --rm bsky-cleaner` でコンテナが起動し、`supercronic` がフォアグラウンドで動作することを確認（ログ出力で判断） | AC-08, AC-09 |
| 不正な `schedule` 値でのコンテナ起動 → 異常終了 | 不正な `schedule` を含む TOML を volume mount し、コンテナが即座に `exit 1` することを確認 | AC-10 |
| `docker compose up` → コンテナ起動 → 定期実行開始 | `docker compose up` でコンテナが起動し、cron スケジュールに従って `bsky-cleaner --apply` が実行されることを確認 | AC-13, AC-14 |

### 4.3 静的検証（Phase 3）

| 検証内容 | コマンド | 対応 AC |
|---|---|---|
| `docker-compose.yml` に秘匿情報が直接書かれていないこと | `rg -n 'BSKY_APP_PASSWORD|BSKY_HANDLE|BSKY_SLACK_WEBHOOK' docker-compose.yml` が `${...}` 参照のみを返し、リテラル値を返さない | AC-11 |
| `dot.env.example` が 4 つの環境変数名を列挙していること | `rg -n 'BSKY_HANDLE|BSKY_APP_PASSWORD|BSKY_SLACK_WEBHOOK_URL_SUCCESS|BSKY_SLACK_WEBHOOK_URL_FAILURE' dot.env.example` が 4 行すべてにマッチ | AC-12 |
| `.gitignore` に `.env` が含まれていること | `rg -n '^\.env$' .gitignore` が 1 行以上マッチ | NF-002 |

### 4.4 テストヘルパー方針

- 新規のクロスパッケージヘルパー・モックは不要（既存の `atprototestutil`・`validConfigPath`・`setEnvCredentials` で足りる）。
- Phase 1 のテストは `cmd/main_test.go`（`package main`）に追加する。`validConfigPath` は `config.Load()` が要求する全フィールドを含む TOML を生成するため、`print-schedule` のテストでもそのまま再利用可能。
- Phase 2・3 のコンテナテストは手動検証のため、テストファイルは作成しない。

## 5. リスク管理

| リスク | 影響 | 緩和策 |
|---|---|---|
| cron 構文検証の自前実装にバグがある | 不正な cron 式を受理／正常な cron 式を拒否 | テストケースで広範な cron 式パターン（ワイルドカード・ステップ・範囲・リスト・境界値）を網羅する。cron 5 フィールドの値域は単純な整数比較であり、複雑なパースロジックは不要 |
| `config.Load()` の副次的な検証（`retention_days`・`execution_timeout_seconds`）が `print-schedule` の目的と衝突する | 設定ファイルの他のフィールドに不備がある場合、`print-schedule` が非 0 で終了する | これは設計上の意図された動作（fail-closed）であり、リスクではない。テストでこの挙動を確認する |
| Docker イメージの digest が更新されない | セキュリティパッチが適用されない | 定期的な digest 更新を運用プロセスとして確立する（本タスクのスコープ外） |
| `supercronic` のバイナリ取得方法 | チェックサム検証なしで GitHub releases から直接取得すると、配布元改ざん時に検知できない（サプライチェーンリスク） | ビルドステージで `go install github.com/aptible/supercronic@<version>` を実行し、Go の module checksum database（`sum.golang.org`）による検証を経由させる（[02_architecture.md 3.2.2 節](./02_architecture.md#322-dockerfileac-05〜07)参照）。バージョンは具体的なリリースタグで固定し、`ADD` によるチェックサム未検証の直接ダウンロードは採用しない |

## 6. 実装チェックリスト

- [x] PR-1 完了（対象ステップ：Phase 1。`cmd/main.go` に `print-schedule` サブコマンドを追加、`cmd/main_test.go` にテストを追加）
- [x] PR-2 完了（対象ステップ：Phase 2。`Dockerfile`・`entrypoint.sh` を新規作成、`docker build` 成功確認）
- [ ] PR-3 完了（対象ステップ：Phase 3。`docker-compose.yml`・`dot.env.example` を新規作成、`docker compose up` 統合動作確認）
- [x] 全体：`make fmt`・`make test`・`make lint` が緑（NF-001）

## 7. 受け入れ基準の検証

各 AC を `test`（実行可能・誤動作で失敗）／`static`（rg/grep/コンパイル）／`manual`（手動検証）に分類する。

| AC | 種別 | 検証内容 |
|---|---|---|
| AC-01 | test | `cmd/main_test.go::TestRunPrintSchedule_ValidConfig`。正常な TOML で `schedule` の値が改行なしの 1 行として標準出力に出力され、終了コード 0 が返ることを表明 |
| AC-02 | test | `cmd/main_test.go::TestValidateSchedule_NewlineInValue`。改行を含む `schedule` 値で `*ScheduleValidationError` が返り、`runPrintSchedule` 経由で標準出力が空・終了コード非 0 であることを表明 |
| AC-03 | static | `rg -n 'config\.Load\(' cmd/main.go` が `runPrintSchedule` 内の 1 箇所のみマッチし、`runPrintSchedule` 内で TOML パースの二重実装（独自の `toml.Decode` 等）が存在しないことを確認 |
| AC-04 | test | `cmd/main_test.go::TestValidateSchedule_*`（フィールド数不足・超過・各フィールドの値域違反）。不正な cron 式で `*ScheduleValidationError` が返り、`runPrintSchedule` 経由で非 0 終了することを表明 |
| AC-05 | static | `Dockerfile` に `FROM ... AS build` と `FROM ...`（2 つの `FROM`）が存在することを確認。`rg -c '^FROM' Dockerfile` が 2 を返す |
| AC-06 | static | `Dockerfile` の各 `FROM` 行が `@sha256:` を含む digest 参照であることを確認。`rg -n '^FROM.*@sha256:' Dockerfile` が 2 行マッチ |
| AC-07 | static + manual | static: `Dockerfile` に `go build` コマンドが含まれ、ビルド成果物を実行ステージにコピーする構成であることを確認。`rg -n 'go build' Dockerfile` が 1 行以上マッチ。manual: `docker build -t bsky-cleaner .` が終了コード 0 で成功することを確認 |
| AC-08 | static + manual | static: `entrypoint.sh` に `exec supercronic` が含まれていることを確認。`rg -n 'exec supercronic' entrypoint.sh` が 1 行マッチ。manual: `docker run --rm bsky-cleaner` でコンテナが起動し、`supercronic` がフォアグラウンドで動作することをログ出力で確認 |
| AC-09 | static + manual | static: `entrypoint.sh` に `bsky-cleaner --apply` を含む crontab 行を生成する処理が含まれていることを確認。`rg -n 'bsky-cleaner.*--apply' entrypoint.sh` が 1 行マッチ。manual: 上記と同じ手順で、`supercronic` が crontab に従って `bsky-cleaner --apply` を定期実行することを確認 |
| AC-10 | static + manual | static: `entrypoint.sh` が `print-schedule` の終了コードを検査し、非 0 の場合に `exit 1` する分岐を含むことを確認。`rg -n 'exit 1' entrypoint.sh` が 1 行以上マッチ。manual: 不正な `schedule` 値を含む TOML を volume mount したコンテナが即座に `exit 1` することを確認 |
| AC-11 | static | `rg -n 'BSKY_APP_PASSWORD|BSKY_HANDLE|BSKY_SLACK_WEBHOOK' docker-compose.yml` が `${...}` 参照のみを返し、リテラル値を返さないことを確認 |
| AC-12 | static | `rg -n 'BSKY_HANDLE|BSKY_APP_PASSWORD|BSKY_SLACK_WEBHOOK_URL_SUCCESS|BSKY_SLACK_WEBHOOK_URL_FAILURE' dot.env.example` が 4 行すべてにマッチすることを確認 |
| AC-13 | static | `rg -n '/config' docker-compose.yml` がマッチし、TOML 設定ファイルの `/config` へのマウント設定が存在することを確認 |
| AC-14 | static + manual | static: `docker-compose.yml` に `BSKY_CONFIG_PATH` 環境変数が設定されていることを確認。`rg -n 'BSKY_CONFIG_PATH' docker-compose.yml` が 1 行マッチ。manual: `docker compose up` でコンテナが起動し、cron スケジュールに従って定期実行が開始されることを確認 |

## 8. 成功基準

- AC-01〜AC-14 が [7 章](#7-受け入れ基準の検証) の `test`／`static`／`manual` で緑（[01_requirements.md 6 章](./01_requirements.md#6-成功基準要約)）。
- `make fmt`・`make test`・`make lint` が緑（NF-001）。
- `.env` が `.gitignore` によりバージョン管理から除外される（NF-002）。
- 内蔵 cron ツールの選定理由が本計画書から [02_architecture.md 3.2.3 節](./02_architecture.md#323-エントリポイントスクリプトac-08〜10) を参照可能であること（NF-003）。

## 9. 内蔵 cron ツール選定理由（NF-003）

本タスクでは内蔵 cron ツールとして `supercronic` を採用する。選定理由の詳細は [02_architecture.md 3.2.3 節](./02_architecture.md#323-エントリポイントスクリプトac-08〜10) を参照。以下に要点を記載する。

- **crontab 互換**: 標準的な crontab 構文をそのまま受け付けるため、TOML の `schedule` フィールドを変換なしで使用できる。
- **シングルバイナリ**: Go で書かれたシングルバイナリであり、Alpine イメージに追加のランタイム依存を必要としない。
- **フォアグラウンド実行**: コンテナ内で PID 1 としてフォアグラウンド実行する設計になっており、`exec` との相性が良い。
- **ログ出力**: ジョブの標準出力・標準エラー出力を `supercronic` 自身のログに転送するため、`docker logs` で実行結果を確認できる。

代替候補（`busybox crond`・Vixie cron・自前スケジューラ）との比較は [02_architecture.md 3.2.3 節](./02_architecture.md#323-エントリポイントスクリプトac-08〜10) に記載。

## 10. 横断検索チェックリスト

`make lint`・`make test` が検出しない項目のみを挙げる。

- [ ] 新規関数名（`validateSchedule`・`parsePrintScheduleFlags`・`runPrintSchedule`）が既存の識別子と衝突しないこと：`rg -n 'validateSchedule|parsePrintScheduleFlags|runPrintSchedule' cmd/` が新規追加箇所のみを返す。
- [ ] 新規型名（`ScheduleValidationError`）が既存の型と衝突しないこと：`rg -n 'ScheduleValidationError' internal/ cmd/` が `cmd/main.go` の定義と利用箇所のみを返す。
- [ ] `print-schedule` という文字列がドキュメント・コメント・エラーメッセージで一貫して使用されていること：`rg -n 'print-schedule' -g '*.go' cmd/ internal/` でコメント・文字列リテラル内の表記が統一されていることを確認。
- [ ] 終了コードの使い分けが設計と一致すること：`runPrintSchedule` が設定不備で `exitSetupOrRunFail`(1) を、フラグエラーで `exitUsageError`(2) を返すこと。`rg -n 'exitSetupOrRunFail|exitUsageError' cmd/main.go` で確認。

## 11. 次のステップ

- 本計画書のレビューと承認（ステータスを `approved` に更新）
- Phase 1（PR-1）の実装開始
- Phase 2（PR-2）の実装
- Phase 3（PR-3）の実装
- 全 PR マージ後の統合動作確認
