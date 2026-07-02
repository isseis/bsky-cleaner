# CLI エントリポイント — 要件定義書

## Document Status

| Item | Value |
|---|---|
| Status | `draft` |
| Created | 2026-07-02 |
| Review date | - |
| Reviewer | - |
| Comments | - |

## 1. 背景と目的

### 1.1 背景

[0001_config](../0001_config/01_requirements.md)（設定管理）、[0002_atproto_client](../0002_atproto_client/01_requirements.md)（AT Protocol クライアント）、[0003_cleanup_engine](../0003_cleanup_engine/01_requirements.md)（クリーンアップエンジン）を結線し、実際に動作する CLI バイナリ（`cmd/main.go`）として仕上げる。[プロジェクト概要](../../overview.md) の「安全性についての方針」にある通り、削除は不可逆な操作であるため、デフォルトは dry-run とし、明示的なフラグでのみ実削除を行う。

本タスクの完了により、overview.md の「完了の定義」のうち「単一アカウントに対し、dry-run でのポスト一覧の表示ができる」「`--apply` フラグ指定時に、条件に合致した投稿が実際に削除され、ピン留め投稿は除外される」の 2 項目を満たす。

### 1.2 目的（ゴール）

- 設定ファイルを読み込み、AT Protocol クライアントで投稿一覧を取得し、クリーンアップエンジンで削除対象を判定し、結果を出力する一連の処理を CLI として実行できる
- デフォルト（`--apply` なし）では実際の削除を行わず、削除対象一覧の表示のみを行う
- `--apply` 指定時のみ、実際に削除を実行する

## 2. スコープ

### In Scope

- **F-001**: CLI フラグ・引数のパース（設定ファイルパス、`--apply`）
- **F-002**: dry-run モードでの削除対象一覧表示
- **F-003**: `--apply` モードでの実削除実行
- **F-004**: 各コンポーネント（config/client/engine）の結線とエラー時の終了コード制御

### Out of Scope

- リトライ・実行タイムアウト（[0005_retry_timeout](../0005_retry_timeout/01_requirements.md) で扱う。この段階ではリトライなしの単純な結線を対象とする）
- Slack 通知（[0006_slack_notification](../0006_slack_notification/01_requirements.md) で扱う。結果表示は標準出力のみとする）
- `print-schedule` サブコマンド（[0007_docker_distribution](../0007_docker_distribution/01_requirements.md) で扱う。Docker/cron 連携固有の機能のため）

## 3. 機能要件と受け入れ基準

各機能要件には、独立して検証可能な受け入れ基準（Acceptance Criteria）を明示すること。
AC の採番ルールは [requirements_process.md](../../dev/developer_guide/requirements_process.md) を参照。

### F-001: CLI フラグ・引数のパース

**Acceptance Criteria**:
- **AC-01**: 設定ファイルパスを指定するフラグ（例: `--config`）を受け付け、指定されたパスの TOML を読み込む
- **AC-02**: `--apply` フラグを指定しない場合、常に dry-run モードで動作する
- **AC-03**: 不正なフラグ・引数が指定された場合、使用方法を示すメッセージとともに非 0 の終了コードで終了する

### F-002: dry-run モードでの削除対象一覧表示

**Acceptance Criteria**:
- **AC-04**: dry-run モードで実行した場合、削除対象と判定された投稿の一覧が標準出力に表示される
- **AC-05**: dry-run モードで実行した場合、AT Protocol の削除 API（`deleteRecord`）が一切呼び出されない
- **AC-06**: 削除対象が 0 件の場合でも、エラーにならず「削除対象なし」に相当する出力を行う

### F-003: `--apply` モードでの実削除実行

**Acceptance Criteria**:
- **AC-07**: `--apply` を指定した場合、削除対象と判定された投稿すべてに対して `deleteRecord` が呼び出される
- **AC-08**: `--apply` を指定した場合でも、ピン留め投稿・保持期間内の投稿は削除対象に含まれない（クリーンアップエンジンの判定結果を尊重する）
- **AC-09**: `--apply` モードでの実行結果（削除件数、失敗があればその件数）が標準出力に表示される

### F-004: コンポーネントの結線とエラー時の終了コード制御

**Acceptance Criteria**:
- **AC-10**: 設定読み込み・ログイン・一覧取得のいずれかが失敗した場合、削除処理に進まず非 0 の終了コードで終了する
- **AC-11**: 一部の投稿の削除が失敗した場合でも、残りの投稿の削除処理は継続され、最終的に失敗があったことが分かる形で非 0 の終了コードを返す

## 4. 非機能要件

必要に応じて `NF-XXX` の識別子を付け、実装計画書からの参照を可能にする。

- **NF-001**: `make fmt`・`make test`・`make lint` が成功する。
- **NF-002**: `cmd/main.go` はコンポーネントの結線に専念し、ビジネスロジック（削除対象判定等）を持ち込まない。
- **NF-003**: dry-run がデフォルトであることは、フラグ未指定時の統合テストで明示的に検証されること（「デフォルトが安全側である」ことに対するテストの欠落を防ぐ）。
- **NF-004**: Go 1.26.2 以上でビルドできること。

## 5. スコープ外の根拠

- リトライ・タイムアウトは、cron による定期実行・多重起動対策と関係が深く、CLI の基本的な結線とは独立して追加可能なため後続タスクとする。
- Slack 通知は実行結果の「通知」という別の関心事であり、まず標準出力での動作確認を優先する。
- `print-schedule` は Docker 配布のための cron 連携専用サブコマンドであり、通常の CLI 実行フローとは別の入口のため分離する。

## 6. 成功基準（要約）

- AC-01〜AC-11 が test/static で緑。
- `bsky-cleaner --config <path>` で dry-run 一覧表示、`bsky-cleaner --config <path> --apply` で実削除ができる状態。
