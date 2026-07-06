# Docker 配布 — 要件定義書

## Document Status

| Item | Value |
|---|---|
| Status | `approved` |
| Created | 2026-07-02 |
| Review date | 2026-07-06 |
| Reviewer | isseis |
| Comments | - |

## 1. 背景と目的

### 1.1 背景

[プロジェクト概要](../../overview.md) の「開発・配布方針」および [Docker 配布の詳細設計](../../design/docker_deployment.md) にある通り、本ツールは Docker イメージとして配布し、軽量 cron（`supercronic` 等）を同梱してコンテナ自体が内蔵スケジューラとして動作する。ユーザーは `docker-compose.yml` と TOML 設定ファイル・`.env` を用意するだけで定期実行を開始できる。

本タスクは、[0004_cli_entrypoint](../0004_cli_entrypoint/01_requirements.md) までで完成した CLI バイナリを前提とし、それを cron 経由で定期実行するための配布形態を整備する。

### 1.2 目的（ゴール）

- CLI バイナリを内蔵 cron 付きの Docker イメージとしてビルドできる
- TOML の `schedule` 設定を、コンテナ内蔵 cron の定義に安全に橋渡しできる
- `docker-compose.yml` と `.env`・TOML を用意するだけで、定期実行が開始できる

## 2. スコープ

### In Scope

- **F-001**: `print-schedule` サブコマンドの実装
- **F-002**: Dockerfile の作成（ベースイメージの digest 固定を含む）
- **F-003**: エントリポイントスクリプトと内蔵 cron の連携
- **F-004**: `docker-compose.yml` および `.env.example` の作成

### Out of Scope

- `.env` の暗号化方式（`git-crypt`）の具体的なセットアップ手順の自動化（[Docker 配布の詳細設計](../../design/docker_deployment.md) に「いずれかを選択できるようにする」とある通り、運用者の選択に委ねる。本タスクでは `.gitignore` によるシンプルな除外をデフォルトとし、`git-crypt` 併用は手順書レベルの補足に留める）
- CLI バイナリ自体の機能追加（[0004_cli_entrypoint](../0004_cli_entrypoint/01_requirements.md) までで完成しているものを前提とする）
- Docker イメージのマルチアーキテクチャ（amd64/arm64）対応（初期リリースでは amd64 のみを対象とし、arm64 対応は将来的な検討事項とする）

## 3. 機能要件と受け入れ基準

### F-001: `print-schedule` サブコマンド

TOML 設定ファイルから `schedule` フィールドのみを取り出し、標準出力に 1 行の cron 式として出力する隠しサブコマンド。`--config`（または `-c`）フラグで TOML ファイルのパスを受け取る。

TOML の読み込みには `config.Load()` を再利用する。`config.Load()` は `schedule` に加えて `retention_days`・`execution_timeout_seconds` も検証するため、これらのフィールドに不備がある場合もエラー終了する。これは意図した動作であり、不完全な設定ファイルで cron が起動してしまうことを防ぐ。

**Acceptance Criteria**:
- **AC-01**: 妥当な TOML ファイルを指定した場合、`schedule` フィールドの値のみが改行なしの 1 行として標準出力に出力される
- **AC-02**: `schedule` の値に改行が含まれる場合、非 0 の終了コードでエラーとなり、標準出力には何も出力されない（crontab へのコマンドインジェクション対策）
- **AC-03**: TOML の読み込み処理は [0001_config](../0001_config/01_requirements.md) のパース処理を再利用し、二重実装しない
- **AC-04**: `schedule` の値が cron 式として構文的に妥当（5 フィールド、各フィールドの値域が cron の仕様に準拠）であることを検証し、不正な場合は非 0 で終了する

### F-002: Dockerfile の作成

**Acceptance Criteria**:
- **AC-05**: マルチステージビルドにより、ビルド用イメージと実行用の軽量イメージが分離されている
- **AC-06**: ベースイメージが、タグではなく digest（例: `alpine@sha256:...`）で参照されている
- **AC-07**: `docker build` によりイメージが正常にビルドできる

### F-003: エントリポイントスクリプトと内蔵 cron の連携

**Acceptance Criteria**:
- **AC-08**: コンテナ起動時、エントリポイントスクリプトが `print-schedule` を呼び出し、その結果を用いて crontab ファイルを動的生成する
- **AC-09**: 生成された crontab に従い、`bsky-cleaner --apply --config ...` が内蔵 cron（`supercronic` 等）経由で定期実行される
- **AC-10**: `print-schedule` がエラー終了した場合（不正な `schedule` 値等）、コンテナは起動を継続せず異常終了する（fail-closed）

### F-004: `docker-compose.yml` および `.env.example`

**Acceptance Criteria**:
- **AC-11**: `docker-compose.yml` に秘匿情報が直接書かれておらず、`environment:` で `.env` の変数を明示的に参照する形になっている
- **AC-12**: `.env.example` が以下の環境変数名を列挙し、値をダミー/空にした状態でコミットされており、初期セットアップの手引きとして機能する: `BSKY_HANDLE`、`BSKY_APP_PASSWORD`、`BSKY_SLACK_WEBHOOK_URL_SUCCESS`、`BSKY_SLACK_WEBHOOK_URL_FAILURE`
- **AC-13**: `docker-compose.yml` が TOML 設定ファイルの volume mount 設定を含む
- **AC-14**: `docker compose up` により、TOML 設定ファイルをボリュームマウントした状態でコンテナが起動し、定期実行が開始される

## 4. 非機能要件

- **NF-001**: `make fmt`・`make test`・`make lint` が成功する。
- **NF-002**: `.env` は `.gitignore` によりデフォルトでバージョン管理から除外される。
- **NF-003**: 内蔵 cron ツールの選定とその理由が本タスクの実装計画書（`03_implementation_plan.md`）に記載されている。

## 5. スコープ外の根拠

- `.env` の暗号化（`git-crypt`）は運用上の選択肢の一つであり、必須の実装対象ではないため、デフォルトの `.gitignore` 方式のみを本タスクのスコープとする。
- CLI バイナリの機能自体は先行タスクで完成している前提とし、本タスクは配布形態の整備に専念する。

## 6. 成功基準（要約）

- AC-01〜AC-14 が test/static で緑。
- `docker-compose.yml` と `.env`・TOML 設定ファイルを用意するだけで、Docker イメージが cron 相当の定期実行を開始できる状態（overview.md の「完了の定義」の最終項目を満たす）。
