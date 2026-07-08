# GitHub Release へのバイナリ添付とリリースノート自動生成 — 要件定義書

## Document Status

| Item | Value |
|---|---|
| Status | `draft` |
| Created | 2026-07-08 |
| Review date | - |
| Reviewer | - |
| Comments | - |

## 1. 背景と目的

### 1.1 背景

[0010_docker_image_release](../0010_docker_image_release/01_requirements.md) により、`v*` 形式の
git tag を push すると GitHub Actions が Docker イメージをビルドし GHCR に公開できるようになった。
しかし現状、`v*` タグを push しても GitHub 上の Releases ページ
（`https://github.com/isseis/bsky-cleaner/releases/tag/vX.Y.Z`）には GitHub が
tag に対して自動生成するソースコードアーカイブ（zip/tar.gz）が表示されるのみで、
実体を持つ GitHub Release（`gh release` で操作できるオブジェクト）は作成されていない。
そのため、ビルド済み CLI バイナリの配布先が GHCR の Docker イメージに限られており、
Docker を使わずホスト上で直接 `bsky-cleaner` バイナリを実行したい利用者への配布手段がない。

また、0010 の要件定義書は「バージョンごとの自動 changelog 生成」を明示的にスコープ外としていた
（[0010/01_requirements.md](../0010_docker_image_release/01_requirements.md) 2章）。
本タスクは、GitHub Release オブジェクトの作成・ビルド済みバイナリの添付・
リリースノートの自動生成をまとめて扱う後続タスクとして切り出す。

### 1.2 目的（ゴール）

- `v*` タグの push を契機に、実体を持つ GitHub Release が自動作成される
- その Release に、ビルド済み Linux/amd64 バイナリ（バージョン情報埋め込み済み）と
  そのチェックサムファイルが添付される
- Release にはタグ間のコミット/PR から自動生成されたリリースノートが記載される
  （手動での changelog 執筆は不要）

## 2. スコープ

### In Scope

- **F-001**: `release.yml` に、ビルド済み CLI バイナリ（`linux/amd64`）を成果物として
  ビルドするステップを追加する
- **F-002**: バイナリの SHA256 チェックサムファイル（`SHA256SUMS`）を生成する
- **F-003**: `v*` タグ push を契機に GitHub Release オブジェクトを作成し、
  F-001 のバイナリと F-002 のチェックサムファイルを添付する
- **F-004**: GitHub Release 作成時に、GitHub 標準の自動生成リリースノート機能
  （`generate_release_notes` / `gh release create --generate-notes`）を用いて
  本文を自動生成する
- **F-005**: 開発者向けドキュメント（0010 の F-004 で整備したリリース手順）に、
  GitHub Release とバイナリ添付についての記述を追記する

### Out of Scope

- **macOS/Windows バイナリの提供**: 現状 Docker（linux/amd64）がこのツールの主要な配布形態であり、
  ホスト直接実行のニーズは開発者自身のローカル検証程度に限られる。将来的にニーズが顕在化した場合に
  別タスクで対応する
- **PR ラベルに基づくリリースノートのカテゴリ分け（`.github/release.yml` によるセクション分割）**:
  GitHub 標準の自動生成（コミット/PR の単純な一覧）で当面の目的は十分に達成でき、
  PR ラベル運用の徹底という追加の運用コストに見合わない。将来必要になれば追加を検討する
- **イメージ署名・SBOM 生成**: [0010](../0010_docker_image_release/01_requirements.md) が
  既にスコープ外とした decision を踏襲する
- **バイナリの `go install` 経由配布や Homebrew tap 等のパッケージマネージャ対応**:
  GitHub Release からの直接ダウンロードで当面の目的は達成でき、追加のパッケージング基盤は
  現時点で要求されていない

## 3. 機能要件と受け入れ基準

### F-001: ビルド済みバイナリの生成

`release.yml` に、`GOOS=linux GOARCH=amd64` でバイナリをビルドするステップを追加する。
既存の Docker イメージビルドと同様に、契機となった git tag の値とビルド元コミットの短縮 SHA を
`-ldflags` でバージョン情報として埋め込む（[0010 F-002](../0010_docker_image_release/01_requirements.md)
で追加済みの `main.version`/`main.commit` 変数を再利用する）。

**Acceptance Criteria**:
- **AC-01**: ワークフロー実行後、`bsky-cleaner-${TAG}-linux-amd64.tar.gz`
  （バイナリ本体を含む tar.gz アーカイブ）が成果物として生成される
- **AC-02**: 生成されたアーカイブ内のバイナリに対して `--version` を実行すると、
  同じ実行で GHCR に公開される Docker イメージ内のバイナリと同じ
  `vX.Y.Z (短縮コミットSHA)` が出力される

### F-002: チェックサムファイルの生成

**Acceptance Criteria**:
- **AC-03**: F-001 のアーカイブに対する SHA256 チェックサムを含む `SHA256SUMS`
  ファイルが生成される
- **AC-04**: `sha256sum -c SHA256SUMS` をアーカイブと同じディレクトリで実行すると
  検証に成功する

### F-003: GitHub Release の作成とバイナリ添付

**Acceptance Criteria**:
- **AC-05**: `v*` タグ push（または `workflow_dispatch`）でワークフローが成功すると、
  対象タグに対する GitHub Release オブジェクトが存在し、`gh release view <tag>` で
  内容を取得できる
- **AC-06**: 作成された Release に F-001 のアーカイブと F-002 の `SHA256SUMS` が
  添付ファイルとして含まれる
- **AC-07**: 既に同名タグの Release が存在する場合、ワークフローは上書きせず非 0 で終了する
  （fail-closed。[0010 AC-03a](../0010_docker_image_release/01_requirements.md) の
  イメージタグ不変性ポリシーと整合させる）

### F-004: リリースノートの自動生成

**Acceptance Criteria**:
- **AC-08**: 作成された Release の本文に、前回の `vX.Y.Z` タグからのコミット/PR一覧を
  元にした GitHub 標準フォーマットのリリースノートが含まれる
- **AC-09**: 直前のタグが存在しない場合（初回リリース）でも、ワークフローはエラーにならず
  Release を作成する（GitHub 標準の自動生成は比較対象がない場合でも動作する）

### F-005: ドキュメント整備

**Acceptance Criteria**:
- **AC-10**: [0010 F-004](../0010_docker_image_release/01_requirements.md) で整備した
  開発者向けリリース手順ドキュメントに、GitHub Release とバイナリ添付に関する記述が
  追記されている

## 4. 非機能要件

- **NF-001**: `make fmt`・`make test`・`make lint` が成功する。
- **NF-002**: 本タスクの追加により、[0010](../0010_docker_image_release/01_requirements.md) の
  既存の受け入れ基準（Docker イメージの GHCR 公開・タグ保護等）の挙動を変更しない
- **NF-003**: バイナリビルド・添付・Release 作成は既存の `release.yml` 内で完結し、
  追加のシークレット登録を必要としない（`GITHUB_TOKEN` の既存権限で完結する。
  Release 作成には `contents: write` 権限が必要なため、ワークフローの `permissions` を
  `contents: read` から `contents: write` に変更する）

## 5. スコープ外の根拠

- macOS/Windows バイナリ、PR ラベルベースのリリースノートカテゴリ分け、パッケージマネージャ対応は
  いずれも「現時点で明確な需要がない付加機能」であり、YAGNI に照らして本タスクでは追加しない
- イメージ署名・SBOM 生成は [0010](../0010_docker_image_release/01_requirements.md) の
  既存のスコープ外判断をそのまま踏襲する

## 6. 成功基準（要約）

- AC-01〜AC-10 が test/static/手動検証で緑。
- `v*` タグを push するだけで、GHCR への Docker イメージ公開に加えて、
  ビルド済みバイナリとチェックサムを添付し自動生成されたリリースノートを持つ
  GitHub Release が作成される状態になっている。
