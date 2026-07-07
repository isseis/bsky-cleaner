# Docker イメージのビルド済み配布 — 要件定義書

## Document Status

| Item | Value |
|---|---|
| Status | `approved` |
| Created | 2026-07-07 |
| Review date | 2026-07-08 |
| Reviewer | isseis |
| Comments | - |

## 1. 背景と目的

### 1.1 背景

[0007_docker_distribution](../0007_docker_distribution/01_requirements.md) により、内蔵 cron 付きの Docker イメージを `docker build` でビルドできる状態までは整った。しかし現状の `docker-compose.yml` は `build: .` を指定しており、利用者は `docker compose up` のたびにローカルで Go ソースからイメージをビルドする必要がある。これは [プロジェクト概要](../../overview.md) の配布方針にある「ユーザーは `docker-compose.yml` で環境変数・TOML 設定ファイルを用意して起動するだけでよい」という導入容易性のゴールを完全には満たしていない。

本タスクは、開発者側が Docker イメージをビルドしてコンテナレジストリに公開し、利用者側は `docker compose pull && docker compose up -d` のようにビルド済みイメージを取得するだけでコンテナを起動できるようにする、リリース配布の仕組みを整備する。

### 1.2 目的（ゴール）

- git tag の push を契機に GitHub Actions が Docker イメージをビルドし、GHCR (`ghcr.io`) に公開できる
- 公開されたイメージにはビルド時のバージョン情報が埋め込まれ、`--version` で確認できる
- `docker-compose.yml` がローカルビルド（`build: .`）ではなく公開済みイメージ（`image:`）を参照し、利用者はイメージを自分でビルドせずに起動できる
- 開発者向けのリリース公開手順、利用者向けの取得手順がそれぞれドキュメント化されている

## 2. スコープ

### In Scope

- **F-001**: git tag push を契機に GHCR へ Docker イメージを公開する GitHub Actions ワークフロー
- **F-002**: ビルド時バージョン埋め込みと `--version` フラグ
- **F-003**: `docker-compose.yml` のイメージ参照方式への変更
- **F-004**: リリース公開・取得手順のドキュメント整備
- **F-005**: `ci.yml` への Docker イメージビルド確認ジョブの追加

### Out of Scope

- **マルチアーキテクチャ対応（arm64 等）**: [0007](../0007_docker_distribution/01_requirements.md) の決定（初期リリースは amd64 のみ）を本タスクでも踏襲する。arm64 対応は将来的な検討事項とする
- **Docker Hub 等 GHCR 以外のレジストリへの公開**: 個人プロジェクトの配布に必要十分な GHCR のみを対象とし、複数レジストリ運用によるシークレット管理・CI 設定の複雑化を避ける
- **イメージ署名・SBOM 生成（cosign 等）**: 現時点では要求されていない付加的なサプライチェーン対策であり、YAGNI に照らして本タスクのスコープ外とする。ベースイメージの digest 固定（0007 で対応済み）を当面の対策とする
- **バージョンごとの自動 changelog 生成**: リリースノート作成の自動化は本タスクでは扱わない
- **リリース公開ワークフロー（GHCR への push）自体の `ci.yml` への統合**: GHCR への公開は F-001 のとおり別ワークフローファイルとして tag push 契機で維持する。`ci.yml` に追加するのは F-005 の「ビルドが成功するかどうかの確認」のみで、push は行わない

## 3. 機能要件と受け入れ基準

### F-001: GHCR へのイメージ公開ワークフロー

`v*` 形式の git tag が push されたことを契機に、GitHub Actions が Dockerfile からイメージをビルドし、GHCR (`ghcr.io/isseis/bsky-cleaner`) に公開する。認証は追加のシークレット登録を必要とせず、ワークフロー組み込みの `GITHUB_TOKEN` のみを用いる。動作確認用に、任意のタグ名を指定して同じジョブを手動実行できる `workflow_dispatch` も補助トリガーとして用意する（tag push が引き続き主契機であることは変えない）。

**Acceptance Criteria**:
- **AC-01**: `v` から始まるタグ（例: `v1.2.3`）の push、または `workflow_dispatch`（対象タグ名を入力として受け取る）をトリガーとしてワークフローが起動する。`v` 以外の tag や、ブランチへの push では起動しない
- **AC-02**: 対象タグが semver 形式（`vX.Y.Z`）に一致しない場合、ワークフローはイメージを公開せず非 0 で終了する（fail-closed。不正なタグから誤ったバージョンのイメージが公開されることを防ぐ）。`workflow_dispatch` 経由で不正な形式のタグ名を指定した場合も同様に拒否する
- **AC-03**: ビルドしたイメージに `vX.Y.Z`・`vX.Y`・`vX`・`latest` の4種のタグを付与し、GHCR に push する
- **AC-03a**: `vX.Y.Z` タグは公開後イミュータブルとして扱う。ワークフローは浮動タグの push に先立って GHCR 上の既存タグの有無を確認し、同名の `vX.Y.Z` タグが既に存在する場合は上書きせず非 0 で終了する（fail-closed。同一バージョン番号で内容が異なるイメージが黙って公開されることを防ぐ）。存在確認の具体的な実現方法（`docker manifest inspect`・`gh api` 等）はアーキテクチャ設計時に選定する。再リリースが必要な場合は、運用者が GHCR 側で該当タグを手動削除してから再実行する。`vX.Y`・`vX`・`latest` は「最新パッチへの追従」を目的とした浮動タグであり、この保護の対象外とする（毎回上書きされるのが意図した挙動）
- **AC-03b**: ワークフローは、上書き保護対象の `vX.Y.Z` タグを最後に push する順序で4タグを公開する。これにより、途中で失敗した場合（例: 浮動タグの push 失敗）でも `vX.Y.Z` はまだ公開されておらず、タグ削除等の手動対応なしにワークフローを再実行できる
- **AC-04**: 公開されるイメージは `linux/amd64` のみを対象とする（0007 の決定を踏襲）
- **AC-05**: GHCR パッケージの可視性は public とする。ただし可視性設定は `GITHUB_TOKEN` の権限では変更できず、パッケージ初回作成後に運用者が GitHub の Package 設定画面から一度だけ手動で public に切り替える必要がある。この手順は F-004 でドキュメント化する
- **AC-06**: ワークフローは `GITHUB_TOKEN` 以外の追加シークレットを必要としない
- **AC-07**: 既存の `ci.yml`（PR/push 契機の test/lint ジョブ）とは独立した別ワークフローファイルとして実装され、既存 CI の挙動に影響を与えない

### F-002: ビルド時バージョン埋め込みと `--version` フラグ

CLI バイナリに `--version`（または `-v`）フラグを追加し、ビルド時に埋め込まれたバージョン情報を標準出力に表示して正常終了する。Docker イメージのビルド時には、公開契機となった git tag の値と、ビルド元コミットの短縮 SHA をこのバージョン情報として埋め込む。バージョン情報に commit SHA を含めるのは、tag が万一 force-push 等で移動した場合でも、実際にビルドされたコミットを障害調査時に一意に追跡できるようにするため。

**Acceptance Criteria**:
- **AC-08**: `bsky-cleaner --version` を実行すると、ビルド時に埋め込まれたバージョン情報が `vX.Y.Z (短縮コミットSHA)`（例: `v1.2.3 (a1b2c3d)`）の形式で標準出力に1行で出力され、終了コード 0 で終了する
- **AC-09**: バージョン文字列を埋め込まずにビルドした場合（通常の `make build` 等のローカル開発ビルド）、`--version` はプレースホルダ値（例: `dev`）を出力する。既存の `make build`／`go build` の呼び出し方に変更は不要
- **AC-10**: F-001 のワークフローが GHCR に公開するイメージは、契機となった git tag の値とビルド元コミットの短縮 SHA をバージョン情報として埋め込んだバイナリを含む
- **AC-11**: `--version` はネットワークアクセス・設定ファイル読み込み・認証情報の参照を一切行わない（バージョン確認のためだけに `.env`/TOML/Bluesky 認証が必要にならない）
- **AC-11a**: `--config` を指定せずに `bsky-cleaner --version` を実行しても、「必須フラグ不足」のような usage エラーにはならず、バージョン情報を出力して終了コード 0 で終了する（既存の `parseFlags` が `--config` を必須とする経路とは独立して扱われる）

### F-003: `docker-compose.yml` のイメージ参照方式への変更

`docker-compose.yml` の `build: .` を、GHCR に公開されたイメージへの `image:` 参照に置き換える。本ツールは不可逆な削除操作を行うため、`pull` のたびに未レビューのバージョンへ黙って切り替わることを避け、デフォルトは具体的なバージョンタグを指定する方針とする（`latest` は「最新を試す」用途としてコメントで案内するに留める）。

**Acceptance Criteria**:
- **AC-12**: `docker-compose.yml` に `build:` キーが存在せず、`image: ghcr.io/isseis/bsky-cleaner:vX.Y.Z` のような特定バージョンタグへの参照に置き換わっている（`latest` をデフォルト値にしない）
- **AC-13**: `docker-compose.yml` 内、または隣接するドキュメントに、(a) バージョンを上げる際は利用者がこの行を明示的に新しいタグへ書き換える必要があること、(b) より厳密に固定したい場合は digest（`@sha256:...`）参照も使えること、(c) `latest` は最新パッチを試す目的でのみ利用を推奨する旨、のコメント／説明がある
- **AC-14**: ローカルに `bsky-cleaner` のソースコードが存在しない環境でも、`docker-compose.yml`・`.env`・TOML 設定ファイルのみを用意した状態で `docker compose pull && docker compose up -d` によりコンテナが起動する

### F-004: リリース公開・取得手順のドキュメント整備

開発者向け（リリースの公開手順）・利用者向け（イメージの取得・起動手順）それぞれの手順をドキュメント化する。

**Acceptance Criteria**:
- **AC-15**: [Docker 配布の詳細設計](../../design/docker_deployment.md) に、開発者がリリースを公開する手順（`git tag vX.Y.Z && git push --tags` 等、および `workflow_dispatch` による動作確認手順）が記載されている
- **AC-16**: 利用者向けドキュメント（README または同等の場所）に、`docker-compose.yml`・`.env`・TOML 設定ファイルを用意し、`docker-compose.yml` のバージョンタグを確認・更新したうえで `docker compose pull && docker compose up -d` を実行するだけで定期実行を開始・更新できる手順が記載されている
- **AC-17**: [Docker 配布の詳細設計](../../design/docker_deployment.md) に、GHCR パッケージの可視性を public に切り替える一度きりの手動手順（GitHub Package 設定画面での操作）が記載されている（AC-05 関連）

### F-005: `ci.yml` への Docker イメージビルド確認ジョブの追加

タグ push でリリースを公開する前に、Dockerfile の壊れ（依存追加漏れ・COPY 対象の欠落等によるビルド失敗）を PR の時点で検知できるようにする。既存の `ci.yml`（PR/push 契機の test/lint ジョブ）に、Docker イメージのビルドのみを行い GHCR には push しない新規ジョブを追加する。ビルド実行そのものに時間がかかるため、(a) ビルド結果に影響しうるパスの変更時のみジョブを実行するパスフィルタと、(b) レイヤーキャッシュの活用により、実行頻度・1回あたりの所要時間の両方を最小限に抑える。

**Acceptance Criteria**:
- **AC-18**: `ci.yml` に、PR/push を契機として `docker build .`（GHCR への push なし）を実行するジョブが追加されている
- **AC-19**: Dockerfile に問題がありビルドが失敗する場合、AC-18 のジョブが失敗し CI 全体が red になる
- **AC-20**: AC-18 のジョブ追加により、既存の test/lint ジョブのトリガー条件・挙動には変更がない（並行して追加されるのみ）
- **AC-21**: AC-18 のジョブは、Docker イメージのビルド結果に影響しうるパス（`Dockerfile`、`go.mod`/`go.sum`、`cmd/`・`internal/` 配下等）に変更がある場合にのみ実行される。ドキュメントのみの変更等、無関係な PR/push では実行されない
- **AC-22**: AC-18 のジョブはビルド高速化のためのレイヤーキャッシュ（例: `docker/build-push-action` の `cache-to`/`cache-from`、GitHub Actions cache）を利用する

## 4. 非機能要件

- **NF-001**: `make fmt`・`make test`・`make lint` が成功する
- **NF-002**: `.github/workflows/ci.yml` に追加する Docker ビルド確認ジョブ（F-005）は、既存の test/lint ジョブのトリガー条件・挙動を変更しない。GHCR への公開（F-001）は引き続き別ワークフローファイルとして tag push 契機で行う
- **NF-003**: GHCR への公開に、リポジトリの `GITHUB_TOKEN` 以外の追加シークレット登録を必要としない
- **NF-004**: `vX.Y.Z` タグの push に成功する前に失敗したワークフロー実行は、タグの手動削除等の後始末なしに再実行できる（AC-03b の冪等性要件）
- **NF-005**: AC-18 のビルド確認ジョブは、キャッシュヒット時に CI 全体の所要時間へ与える影響が軽微な範囲に収まる（具体的な許容時間の数値化はアーキテクチャ設計時に検討する）

## 5. スコープ外の根拠

- マルチアーキテクチャ対応・イメージ署名/SBOM・複数レジストリ公開・changelog 自動生成は、いずれも「導入の容易さ」という本タスクの直接ゴールに必須ではない付加的な対策であり、YAGNI に照らして将来的な検討事項とする。

## 6. 成功基準（要約）

- AC-01〜AC-22（AC-03a・AC-03b・AC-11a を含む）が test/static/manual で緑
- タグ push（または動作確認用の `workflow_dispatch`）だけで GHCR にイメージが公開され、公開済み `vX.Y.Z` タグは上書きされないこと
- 利用者は `docker-compose.yml`・`.env`・TOML を用意し、バージョンタグを確認・更新したうえで `docker compose pull && docker compose up -d` を実行するだけでコンテナを起動・更新できる状態になっていること
- Docker ビルド結果に影響するパスの変更時のみ、PR/push 時の CI で Dockerfile のビルド失敗が検知できる状態になっていること
