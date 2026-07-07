# Docker イメージのビルド済み配布 — 要件定義書

## Document Status

| Item | Value |
|---|---|
| Status | `draft` |
| Created | 2026-07-07 |
| Review date | - |
| Reviewer | - |
| Comments | - |

## 1. 背景と目的

### 1.1 背景

[0007_docker_distribution](../0007_docker_distribution/01_requirements.md) により、内蔵 cron 付きの Docker イメージを `docker build` でビルドできる状態までは整った。しかし現状の `docker-compose.yml` は `build: .` を指定しており、利用者は `docker compose up` のたびにローカルで Go ソースからイメージをビルドする必要がある。これは [プロジェクト概要](../../overview.md) の配布方針にある「ユーザーは `docker-compose.yml` で環境変数・TOML 設定ファイルを用意して起動するだけでよい」という導入容易性のゴールを完全には満たしていない。

本タスクは、開発者側が Docker イメージをビルドしてコンテナレジストリに公開し、利用者側は `docker compose pull && docker compose up -d` のようにビルド済みイメージを取得するだけでコンテナを起動できるようにする、リリース配布の仕組みを整備する。

### 1.2 目的（ゴール）

- git tag の push を契機に GitHub Actions が Docker イメージをビルドし、GHCR (`ghcr.io`) に公開できる
- 公開されたイメージにはビルド時のバージョン情報が埋め込まれ、`--version` で確認できる
- `docker-compose.yml` がローカルビルド（`build: .`）ではなく公開済みイメージ（`image:`）を参照し、利用者はイメージを自分でビルドせずに起動できる
- 利用者向けのリリース公開・取得手順がドキュメント化されている

## 2. スコープ

### In Scope

- **F-001**: git tag push を契機に GHCR へ Docker イメージを公開する GitHub Actions ワークフロー
- **F-002**: ビルド時バージョン埋め込みと `--version` フラグ
- **F-003**: `docker-compose.yml` のイメージ参照方式への変更
- **F-004**: リリース公開・取得手順のドキュメント整備

### Out of Scope

- **マルチアーキテクチャ対応（arm64 等）**: [0007](../0007_docker_distribution/01_requirements.md) の決定（初期リリースは amd64 のみ）を本タスクでも踏襲する。arm64 対応は将来的な検討事項とする
- **Docker Hub 等 GHCR 以外のレジストリへの公開**: 個人プロジェクトの配布に必要十分な GHCR のみを対象とし、複数レジストリ運用によるシークレット管理・CI 設定の複雑化を避ける
- **イメージ署名・SBOM 生成（cosign 等）**: 現時点では要求されていない付加的なサプライチェーン対策であり、YAGNI に照らして本タスクのスコープ外とする。ベースイメージの digest 固定（0007 で対応済み）を当面の対策とする
- **バージョンごとの自動 changelog 生成**: リリースノート作成の自動化は本タスクでは扱わない
- **既存 CI ワークフロー（`ci.yml`）の変更**: リリース公開は別ワークフローファイルとして新設し、PR/push 時に走る既存の test/lint ジョブには手を入れない

## 3. 機能要件と受け入れ基準

### F-001: GHCR へのイメージ公開ワークフロー

`v*` 形式の git tag が push されたことを契機に、GitHub Actions が Dockerfile からイメージをビルドし、GHCR (`ghcr.io/<owner>/bsky-cleaner`) に公開する。認証は追加のシークレット登録を必要とせず、ワークフロー組み込みの `GITHUB_TOKEN` のみを用いる。

**Acceptance Criteria**:
- **AC-01**: `v` から始まるタグ（例: `v1.2.3`）の push をトリガーとしてワークフローが起動する。`v` 以外の tag や、ブランチへの push では起動しない
- **AC-02**: タグが semver 形式（`vX.Y.Z`）に一致しない場合、ワークフローはイメージを公開せず非 0 で終了する（fail-closed。不正なタグから誤ったバージョンのイメージが公開されることを防ぐ）
- **AC-03**: ビルドしたイメージに `vX.Y.Z`・`vX.Y`・`vX`・`latest` の4種のタグを付与し、GHCR に push する
- **AC-04**: 公開されるイメージは `linux/amd64` のみを対象とする（0007 の決定を踏襲）
- **AC-05**: GHCR パッケージの可視性が public に設定されており、認証なしの匿名 `docker pull` で取得できる
- **AC-06**: ワークフローは `GITHUB_TOKEN` 以外の追加シークレットを必要としない
- **AC-07**: 既存の `ci.yml`（PR/push 契機の test/lint ジョブ）とは独立した別ワークフローファイルとして実装され、既存 CI の挙動に影響を与えない

### F-002: ビルド時バージョン埋め込みと `--version` フラグ

CLI バイナリに `--version`（または `-v`）フラグを追加し、ビルド時に埋め込まれたバージョン文字列を標準出力に表示して正常終了する。Docker イメージのビルド時には、公開契機となった git tag の値をこのバージョン文字列として埋め込む。

**Acceptance Criteria**:
- **AC-08**: `bsky-cleaner --version` を実行すると、ビルド時に埋め込まれたバージョン文字列が標準出力に1行で出力され、終了コード 0 で終了する
- **AC-09**: バージョン文字列を埋め込まずにビルドした場合（通常の `make build` 等のローカル開発ビルド）、`--version` はプレースホルダ値（例: `dev`）を出力する。既存の `make build`／`go build` の呼び出し方に変更は不要
- **AC-10**: F-001 のワークフローが GHCR に公開するイメージは、契機となった git tag の値（例: `v1.2.3`）をバージョン文字列として埋め込んだバイナリを含む
- **AC-11**: `--version` はネットワークアクセス・設定ファイル読み込み・認証情報の参照を一切行わない（バージョン確認のためだけに `.env`/TOML/Bluesky 認証が必要にならない）

### F-003: `docker-compose.yml` のイメージ参照方式への変更

`docker-compose.yml` の `build: .` を、GHCR に公開されたイメージへの `image:` 参照に置き換える。

**Acceptance Criteria**:
- **AC-12**: `docker-compose.yml` に `build:` キーが存在せず、`image: ghcr.io/<owner>/bsky-cleaner:latest` 形式の参照に置き換わっている
- **AC-13**: `docker-compose.yml` 内、または隣接するドキュメントに、`latest` の代わりに特定バージョンタグ（例: `v1.2.3`）や digest（`@sha256:...`）を指定して固定する方法についてのコメント／説明がある
- **AC-14**: ローカルに `bsky-cleaner` のソースコードが存在しない環境でも、`docker-compose.yml`・`.env`・TOML 設定ファイルのみを用意した状態で `docker compose pull && docker compose up -d` によりコンテナが起動する

### F-004: リリース公開・取得手順のドキュメント整備

開発者向け（リリースの公開手順）・利用者向け（イメージの取得・起動手順）それぞれの手順をドキュメント化する。

**Acceptance Criteria**:
- **AC-15**: [Docker 配布の詳細設計](../../design/docker_deployment.md) に、開発者がリリースを公開する手順（`git tag vX.Y.Z && git push --tags` 等）が記載されている
- **AC-16**: 利用者向けドキュメント（README または同等の場所）に、`docker-compose.yml`・`.env`・TOML 設定ファイルを用意し `docker compose pull && docker compose up -d` を実行するだけで定期実行を開始できる手順が記載されている

## 4. 非機能要件

- **NF-001**: `make fmt`・`make test`・`make lint` が成功する
- **NF-002**: 新設するワークフローファイルは、既存の `.github/workflows/ci.yml` の内容・トリガー条件を変更しない
- **NF-003**: GHCR への公開に、リポジトリの `GITHUB_TOKEN` 以外の追加シークレット登録を必要としない

## 5. スコープ外の根拠

- マルチアーキテクチャ対応・イメージ署名/SBOM・複数レジストリ公開・changelog 自動生成は、いずれも「導入の容易さ」という本タスクの直接ゴールに必須ではない付加的な対策であり、YAGNI に照らして将来的な検討事項とする。

## 6. 成功基準（要約）

- AC-01〜AC-16 が test/static/manual で緑
- タグ push だけで GHCR にイメージが公開され、利用者は `docker-compose.yml`・`.env`・TOML を用意して `docker compose pull && docker compose up -d` を実行するだけでコンテナを起動できる状態になっていること
