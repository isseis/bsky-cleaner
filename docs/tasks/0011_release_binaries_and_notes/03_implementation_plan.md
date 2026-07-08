# GitHub Release へのバイナリ添付とリリースノート自動生成 — 実装計画書

## Document Status

| Item | Value |
|---|---|
| Status | `draft` |
| Created | 2026-07-08 |
| Review date | - |
| Reviewer | - |
| Comments | - |

関連ドキュメント: [要件定義書](01_requirements.md) / [アーキテクチャ設計書](02_architecture.md)

## 1. 実装概要

### 1.1 目的

本計画は [02_architecture.md](./02_architecture.md) に基づき、[01_requirements.md](./01_requirements.md) の AC-01〜AC-10 を満たすための作業を、検証可能な単位に分解する。本タスクは、`.github/workflows/release.yml` への `linux/amd64` バイナリビルド・アーカイブ生成・`SHA256SUMS` 生成・GitHub Release 作成（アセット添付・リリースノート自動生成込み）ジョブの追加と、関連ドキュメント（`docs/design/docker_deployment.md`）の整備を行う。本タスクは Go ソースコード（`cmd/`, `internal/`）に変更を加えない。

### 1.2 実装方針

- **設計の再掲を避ける**: 各コンポーネントの設計詳細・エラー分類・処理フロー・脅威モデルは [02_architecture.md](./02_architecture.md) を参照し、本計画では「どのファイルに何を追加し、どう検証するか」のみを書く。
- **フェーズは [02_architecture.md 8 章](./02_architecture.md#8-実装優先順位) の定義・順序に一致させる**（フェーズ1: バイナリビルドとアーカイブ生成 → フェーズ2: チェックサム生成とジョブ間受け渡し → フェーズ3: GitHub Release 作成ジョブの新設 → フェーズ4: 検証とドキュメント整備）。
- **既存の `release` ジョブの Docker ビルド・push ステップは変更しない**（[02_architecture.md NF-002](./02_architecture.md#13-要件との対応)）。フェーズ1〜2で追加するステップは、既存の "Build image" ステップの直後・"Tag and push latest" ステップの前に挿入する純粋な追加であり、既存ステップの内容・順序を書き換えない。
- **`release` ジョブの `permissions` は変更しない**: 現行の `permissions: { contents: read, packages: write }`（ワークフロー・トップレベル宣言、`release` ジョブが継承）はそのまま残す。`contents: write` は新設する `publish-release` ジョブの**ジョブレベル `permissions` として追加**する。GitHub Actions のジョブレベル `permissions` はワークフロー・トップレベルの宣言を継承せず完全に置き換えるため、`publish-release` ジョブには `packages: write` を含めない（[02_architecture.md 5.2](./02_architecture.md#52-脅威と対策)）。
- **ジョブ間のデータ受け渡しは `actions/upload-artifact`/`download-artifact` を使う**: `release` ジョブが生成したアーカイブと `SHA256SUMS` を、GHCR や GitHub Release とは独立したワークフロー成果物として `publish-release` ジョブに渡す。両方とも `dist/` 配下のファイルを対象にするため、least common ancestor は `dist/` となり、アーティファクト内では `dist/` プレフィックスなしにフラットに格納される（[GitHub 公式ドキュメント](https://github.com/actions/upload-artifact)で確認済み）。`actions/download-artifact@v4` で `path: dist` を指定してダウンロードすれば、`release` ジョブ側と同じ相対パス（`dist/bsky-cleaner-<tag>-linux-amd64.tar.gz`, `dist/SHA256SUMS`）で参照できる。
- **タグ名の受け渡しにはジョブ出力を使う**: `release` ジョブに `outputs: resolved_tag: ${{ steps.resolve-tag.outputs.resolved_tag }}` を追加し、`publish-release` ジョブは `needs.release.outputs.resolved_tag` を参照する。`workflow_dispatch` 契機かどうかの判定には、ジョブをまたいでも直接参照できる `github.event_name` コンテキストをそのまま使う（ジョブ出力に含める必要はない）。
- **`gh` CLI の認証・リポジトリ解決を明示する**: `publish-release` ジョブは `actions/checkout` を行わないため、ローカル git リポジトリのコンテキストがない。`gh release create` 実行ステップに `env: { GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}, GH_REPO: ${{ github.repository }} }` を明示し、認証とリポジトリ解決の両方を環境変数で確定させる（[gh CLI 公式ドキュメントの環境変数一覧](https://cli.github.com/manual/gh_help_environment)で `GH_TOKEN`/`GH_REPO` の役割を確認済み）。
- **`--verify-tag` で二重の fail-closed を確保する**: `publish-release` ジョブはチェックアウトを行わないため、対象タグが本当に存在するかをローカルで確認できない。`gh release create` はデフォルトで「指定タグが存在しない場合、自動的に対象ブランチの最新コミットに新規タグを作成する」（[`gh release create --help`](https://cli.github.com/manual/gh_release_create) で確認済み）。これは 02_architecture.md が前提とする「タグは `release` ジョブのチェックアウト時点で既に存在する」という状況を裏切るバグが混入した場合に、意図しないタグ・Release が作成されてしまう fail-open の経路になる。`--verify-tag` フラグ（タグが既に存在しない場合に中断する）を明示的に付与し、この経路を塞ぐ。02_architecture.md 3.2.3 の疑似コードにはない追加の安全策だが、既存の設計判断（fail-closed 原則）と矛盾しない具体化である。
- **Go ソースは変更しない**: 本タスクは `cmd/`, `internal/` のいずれにも変更を加えない。追加するのは `.github/workflows/release.yml` の YAML と `docs/design/docker_deployment.md` の Markdown のみである。
- **各編集後に `make fmt` → `make test` → `make lint` を実行**し、緑を維持する（NF-001）。Go ソースの変更はないため、これらは主に無変更であることの確認になる。

### 1.3 既存コード調査結果

**`.github/workflows/release.yml`（フェーズ1〜3対象）**

- 既存: `release` ジョブが単一ジョブ構成で存在する。トリガー（`push.tags: ['v*']` / `workflow_dispatch.inputs.tag`, `.inputs.bootstrap_first_release`）、`permissions: { contents: read, packages: write }`、`concurrency`（タグ名キー、`cancel-in-progress: false`）、`timeout-minutes: 15` が設定済み。ステップは "Resolve tag name"（`id: resolve-tag`, output `resolved_tag`）→ "Validate tag format" → "Checkout code" → "Resolve short commit SHA"（`id: short-sha`, output `sha`）→ "Set up Docker Buildx" → "Log in to GHCR" → "Check existing vX.Y.Z tag" → "Warn about skipped existing-tag check" → "Build image"（`docker/build-push-action`, `push: false`, `load: true`, `build-args: VERSION=<resolved_tag> COMMIT=<sha>`）→ "Tag and push latest" → "Tag and push major version tag" → "Tag and push minor version tag" → "Tag and push patch version tag (vX.Y.Z)" の順（`.github/workflows/release.yml:1-132`）。`outputs:` はジョブレベルで未宣言。
- 不足: Go セットアップ、バイナリビルド、アーカイブ生成、チェックサム生成、ワークフロー成果物アップロードの各ステップ（すべて "Build image" と "Tag and push latest" の間に挿入）。`release` ジョブの `outputs: resolved_tag`。`publish-release` ジョブ全体（`needs: release`, 専用 `permissions`, `timeout-minutes`, ダウンロード・`gh release create` ステップ）。
- 変更: 上記の不足分を追加する。既存の13ステップ（"Resolve tag name" 〜 "Tag and push patch version tag (vX.Y.Z)"）の内容・順序は一切変更しない。

**`cmd/main.go`**

- 既存: `version`/`commit` パッケージレベル変数（既定値 `"dev"`/`""`）と `formatVersion()` が 0010 で導入済み（`cmd/main.go:28-48`）。`-ldflags "-X main.version=... -X main.commit=..."` で上書きする仕組みも `Dockerfile` に実装済み。
- 変更: なし。本タスクのバイナリビルドステップは、この既存の変数・埋め込み機構をそのまま再利用する（新しい `-ldflags` の指定方法を追加するだけで、`cmd/main.go` 自体は変更しない）。

**`Makefile`**

- 既存: `BINARY=build/bsky-cleaner`、`make build` は `go build -o $(BINARY) ./cmd`（`-ldflags` なし、ローカル開発用）。`build/` は `.gitignore` 対象。
- 変更: なし。本タスクの CI 用バイナリビルドは `Makefile`/`make build` を使わず、`release.yml` 内で直接 `go build` を呼び出し、出力先を `dist/`（`build/` とは別の CI 専用ディレクトリ）にする（[02_architecture.md 3.2.1](./02_architecture.md#321-バイナリビルドとアーカイブ生成ac-01〜02-release-ジョブ)）。

**`docs/design/docker_deployment.md`（フェーズ4対象）**

- 既存: 「リリース公開手順（開発者向け）」節に、タグ push 後の自動処理を4項目（semver 検証・既存タグ確認・イメージビルド・4タグ push）で記載済み（`docs/design/docker_deployment.md:33-60`）。GitHub Release・バイナリ添付・チェックサム・リリースノートについての記載はない。
- 不足: 自動処理の項目一覧へのバイナリ・チェックサム・Release 作成・リリースノート自動生成の追記、`sha256sum -c SHA256SUMS` によるダウンロード後検証手順、`workflow_dispatch` 実行時は Release が `--draft` 付きで作成される旨の説明。
- 変更: 既存の4項目リストに項目を追加し、新しい小節を追記する（既存記述は変更しない）。

**`README.md`**

- 既存: 「インストールと実行（ビルド済み実行ファイル）」節に、`curl` によるアーカイブ・`SHA256SUMS` のダウンロード手順と `sha256sum -c SHA256SUMS` によるチェックサム検証手順が**既に記載済み**（`README.md:64-79`、`bsky-cleaner-vX.Y.Z-linux-amd64.tar.gz`・`SHA256SUMS` というファイル名も本タスクの設計と一致している）。
- 変更: なし。この節は本タスクが実装する挙動を先取りして書かれたものであり、01_requirements.md の F-005 は「0010 の F-004 で整備した開発者向けリリース手順ドキュメント」（＝ `docs/design/docker_deployment.md`）への追記のみを求めているため、`README.md` は対象外とする。フェーズ4の完了確認で、この既存記述が実装後の実際の挙動と齟齬がないことを確認する。

**`scripts/`**

- 既存: `check-existing-tag.sh`／`check_existing_tag_test.go`（0010 で導入、Docker タグの既存確認用）。
- 変更: なし。本タスクは GitHub Release の重複防止に専用スクリプトを追加しない（[02_architecture.md 1.1](./02_architecture.md#11-設計原則)、`gh release create` の既存動作をそのまま利用するため）。

**テストヘルパ**

- 新規のクロスパッケージヘルパ（`testutil/`）・パッケージ内蔵ヘルパ（`test_helpers.go`）はいずれも不要。本タスクは Go テストコードを追加しない（[test_organization.md](../../dev/developer_guide/test_organization.md) の分類が必要になる新規ヘルパは生じない）。

## 2. 実装ステップ

### PR-1 作成ポイント
- **対象ステップ**: フェーズ1〜3（バイナリビルド・チェックサム・GitHub Release 作成ジョブ）

**推奨タイトル**: feat(0011): add binary build, checksum, and GitHub Release publishing to release workflow

**レビュー観点**:
- 新規ステップが "Build image" と "Tag and push latest" の間に挿入されており、既存の Docker ビルド・push ステップの内容・順序が変更されていないか（NF-002）
- `go build` の `-ldflags` が `steps.resolve-tag.outputs.resolved_tag`／`steps.short-sha.outputs.sha` を、Docker イメージビルドの `build-args` と同じ値として参照しているか（AC-02 の構造的保証）
- `release` ジョブの `permissions` が変更されていないか。`publish-release` ジョブの `permissions` が `contents: write` のみであるか（NF-003）
- `publish-release` ジョブが `needs: release` を持ち、`release` ジョブ失敗時に実行されないか
- `gh release create` に `--generate-notes`・`--verify-tag`・`workflow_dispatch` 時のみの `--draft` が正しく組み込まれているか

PR checkpoint checkboxes (used by step 4/5a to detect PR boundaries):
- [ ] `go run github.com/rhysd/actionlint/cmd/actionlint@latest .github/workflows/release.yml` を実行し、YAML構文・式構文のエラーがないことを確認した（`make lint`/`make test` は Go ソースのみを対象とし `.github/workflows/*.yml` の構文は検証しないため、マージ前にワークフロー自体の構文エラーを検出する唯一の手段）
- [ ] グリーンゲート通過: `make fmt && make test && make lint && make deadcode`
- [ ] PR を作成した
- [ ] PR がマージされた
- [ ] 次のブランチへ切り替えた

### フェーズ1: バイナリビルドとアーカイブ生成（AC-01〜02, `release` ジョブ）

**対象ファイル**: `.github/workflows/release.yml`

- [ ] "Build image" ステップの直後に "Set up Go" ステップを追加する: `actions/setup-go@v6`、`with: { go-version-file: go.mod, cache: true }`（`ci.yml` の既存ステップと同一の指定方法。`ci.yml:26-29` 参照）。
- [ ] 続けて "Build linux/amd64 binary" ステップを追加する（[02_architecture.md 3.2.1](./02_architecture.md#321-バイナリビルドとアーカイブ生成ac-01〜02-release-ジョブ)）:
  ```yaml
  - name: Build linux/amd64 binary
    env:
      TAG: ${{ steps.resolve-tag.outputs.resolved_tag }}
      COMMIT: ${{ steps.short-sha.outputs.sha }}
    run: |
      mkdir -p dist
      GOOS=linux GOARCH=amd64 go build \
        -ldflags "-X main.version=${TAG} -X main.commit=${COMMIT}" \
        -o dist/bsky-cleaner ./cmd
  ```
- [ ] 続けて "Package binary archive" ステップを追加する（AC-01）:
  ```yaml
  - name: Package binary archive
    env:
      TAG: ${{ steps.resolve-tag.outputs.resolved_tag }}
    run: |
      tar czf "dist/bsky-cleaner-${TAG}-linux-amd64.tar.gz" -C dist bsky-cleaner
  ```

**完了基準**: `release.yml` の該当箇所に3ステップが追加されている。既存の "Build image" 以前・"Tag and push latest" 以降のステップに差分がない。

### フェーズ2: チェックサム生成とジョブ間受け渡し（AC-03〜04, `release` ジョブ）

**対象ファイル**: `.github/workflows/release.yml`

- [ ] "Package binary archive" の直後に "Generate checksum file" ステップを追加する（AC-03）:
  ```yaml
  - name: Generate checksum file
    env:
      TAG: ${{ steps.resolve-tag.outputs.resolved_tag }}
    run: |
      cd dist && sha256sum "bsky-cleaner-${TAG}-linux-amd64.tar.gz" > SHA256SUMS
  ```
- [ ] 続けて "Upload release artifacts" ステップを追加する（1.2 節「ジョブ間のデータ受け渡し」参照）:
  ```yaml
  - name: Upload release artifacts
    uses: actions/upload-artifact@v4
    with:
      name: release-assets
      path: |
        dist/*.tar.gz
        dist/SHA256SUMS
      if-no-files-found: error
      retention-days: 1
  ```
  `retention-days: 1` は、この成果物が同一ワークフロー実行内の `publish-release` ジョブでのみ使われ、実行後に保持する必要がないための最小値。
- [ ] `release` ジョブに `outputs: { resolved_tag: ${{ steps.resolve-tag.outputs.resolved_tag }} }` を追加する（`publish-release` ジョブが参照するため）。

**完了基準**: `release.yml` に "Generate checksum file"／"Upload release artifacts" ステップと `release` ジョブの `outputs.resolved_tag` が追加されている。

### フェーズ3: GitHub Release 作成ジョブの新設（AC-05〜09, 新設 `publish-release` ジョブ）

**対象ファイル**: `.github/workflows/release.yml`

- [ ] `release` ジョブの後に新規ジョブ `publish-release` を追加する:
  ```yaml
  publish-release:
    needs: release
    runs-on: ubuntu-latest
    timeout-minutes: 5
    permissions:
      contents: write
    steps:
      - name: Download release artifacts
        uses: actions/download-artifact@v4
        with:
          name: release-assets
          path: dist

      - name: Create GitHub Release
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          GH_REPO: ${{ github.repository }}
          TAG: ${{ needs.release.outputs.resolved_tag }}
        run: |
          DRAFT_FLAG=""
          if [ "${{ github.event_name }}" = "workflow_dispatch" ]; then
            DRAFT_FLAG="--draft"
          fi
          gh release create "$TAG" \
            "dist/bsky-cleaner-${TAG}-linux-amd64.tar.gz" \
            dist/SHA256SUMS \
            --title "$TAG" \
            --generate-notes \
            --verify-tag \
            $DRAFT_FLAG
  ```
  - `--verify-tag`: タグが存在しない場合は中断する（1.2 節「`--verify-tag` で二重の fail-closed を確保する」参照）。
  - `$DRAFT_FLAG` は未設定時に空文字列として展開され、`gh release create` に余分な空引数を渡さない（bash の unquoted 変数展開の挙動）。
  - `gh release create` は指定タグに対する Release が既に存在する場合エラーを返し非0終了する（AC-07。既存動作の利用のみで、追加の判定ロジックは実装しない）。

**完了基準**: `publish-release` ジョブが `release.yml` に追加されている。`needs: release` により `release` ジョブ失敗時は実行されない。`permissions` が `contents: write` のみである。

## 3. 実装順序とマイルストーン

### 3.1 マイルストーン

| マイルストーン | 内容 | 成果物 |
|---|---|---|
| M1 | フェーズ1〜3完了（PR-1） | `release.yml` にバイナリビルド・チェックサム・`publish-release` ジョブが追加され、静的検証（7章）がすべて通過する |
| M2 | フェーズ4完了（PR-2） | 実タグ push による実機検証と `gh release create` の重複防止動作の直接確認が完了し、`docs/design/docker_deployment.md` の追記が完了する |

### 3.2 フェーズ間の依存関係

フェーズ1→フェーズ2→フェーズ3の順に依存する: フェーズ2の "Upload release artifacts" はフェーズ1が生成するアーカイブを対象とし、フェーズ3の `publish-release` ジョブはフェーズ2でアップロードされた成果物と `release` ジョブの `outputs.resolved_tag` を前提とする。フェーズ4はフェーズ1〜3が実装された `release.yml` に対する実機検証であるため、PR-1 のマージ後に着手する。

### PR-2 作成ポイント
- **対象ステップ**: フェーズ4（検証とドキュメント整備）

**推奨タイトル**: docs(0011): verify release workflow and document GitHub Release publishing

**レビュー観点**:
- 実タグ push による本番実行（比較対象タグがない初回リリース相当）で、`gh release create --generate-notes` がエラーにならないことを実機で確認できているか（AC-09 の未検証リスクの解消）
- `gh release create` を同一タグに対して2回連続実行し、2回目が非0終了して Release を上書きしないことを直接確認できているか（AC-07）
- `docs/design/docker_deployment.md` の追記が実際のワークフロー挙動と一致しているか

PR checkpoint checkboxes (used by step 4/5a to detect PR boundaries):
- [ ] グリーンゲート通過: `make fmt && make test && make lint && make deadcode`
- [ ] PR を作成した
- [ ] PR がマージされた
- [ ] 次のブランチへ切り替えた

### フェーズ4: 検証とドキュメント整備（AC-02, AC-05〜10）

**対象ファイル**: `docs/design/docker_deployment.md`

**検証手順の前提（重要）**: `workflow_dispatch` の "Checkout code" ステップは、指定したタグが git tag としてリモートに既に存在することを要求する（`docs/design/docker_deployment.md:60`）。したがって、まだ存在しない新規タグを使った動作確認は、`workflow_dispatch` からではなく、実際に `git tag -a <タグ> && git push origin <タグ>` でタグを push することで行う。この push は `on: push: tags: ['v*']` トリガーを発火させ、**`push` 契機（非ドラフト）の本番実行**が自動的に起動する。これは 0010 の [11_manual_verification_log.md](../0010_docker_image_release/11_manual_verification_log.md) が実際に踏んだ手順と同じである（`workflow_dispatch` は、タグが既に存在する状態でのみ意味を持つ再実行・再検証の手段である）。`--draft` は `workflow_dispatch` 契機の実行にのみ付与されるため、新規タグでの最初の動作確認自体をドラフトとして行うことはできない。

- [ ] 未使用の実 semver タグ（例 `v0.0.2`。0010 の検証で使われた `v0.0.1` は削除済みだが、ログとの混同を避けるため別の番号を使う）を用意し、`git tag -a v0.0.2 -m "test: manual verification for release.yml binary/release publishing" && git push origin v0.0.2` を実行する。push 契機で `release.yml` が自動起動することを確認したうえで、次を確認する。
  - "Build linux/amd64 binary" 〜 "Upload release artifacts" のステップがすべて成功する。
  - `publish-release` ジョブが起動し、非ドラフトの GitHub Release が作成される（`gh release view v0.0.2` で内容を取得できることを確認。これは 0010 が実施した検証と同じ「本物の初回リリース」であり、AC-09（直前のタグが存在しない場合でもエラーにならない）を自然な形で検証できる。02_architecture.md 3.2.3 の未検証リスクをここで解消する）。
  - 添付ファイル（`bsky-cleaner-v0.0.2-linux-amd64.tar.gz`, `SHA256SUMS`）をダウンロードし、`sha256sum -c SHA256SUMS` が成功することを確認する（AC-04）。
  - ダウンロードしたアーカイブ内のバイナリの `--version` 出力が、同じ実行で GHCR に公開された `ghcr.io/isseis/bsky-cleaner:v0.0.2` の `--version` 出力と一致することを確認する（AC-02）。
  - Release 本文に GitHub 標準形式のコミット/PR 一覧が含まれることを確認する（AC-08）。
- [ ] 検証完了後、後片付けを行う: `gh release delete v0.0.2 --yes` で Release を削除し、0010 の [11_manual_verification_log.md](../0010_docker_image_release/11_manual_verification_log.md) 3-1節と同じ手順（GHCR Package 設定画面、または `gh api --method DELETE` によるバージョン削除）で GHCR 側の4タグ（`latest`/`v0`/`v0.0`/`v0.0.2`。他のバージョンに影響しないことを確認してから削除する）を削除し、`git push origin :refs/tags/v0.0.2 && git tag -d v0.0.2` でタグを削除する。
- [ ] AC-07（同名タグの Release が既に存在する場合に fail-closed で非0終了する）は、上記の push 契機の実行を再現せずに、`gh release create` そのものの既存動作を直接 CLI で確認することで検証する。これは、`publish-release` ジョブを実際にもう一度実行して同じ状態を再現しようとした場合の制約による。その場合、`release` ジョブの既存タグ確認（0010 由来、AC-03a）が先に非0終了してしまうため、本タスクが追加する Release 重複防止の経路（`gh release create` 自体のエラー）に到達できない（`bootstrap_first_release` 入力で Docker 側の確認を迂回する手段はあるが、これは「初回リリース専用・以降のリリースでは使用しないこと」と明記された補助フラグであり、検証目的での流用は本来の用途に反するため用いない）。
  - 上記とは別の未使用タグ（例 `v0.0.3`）を用意し、`git tag -a v0.0.3 -m "test: gh release create duplicate-prevention check" && git push origin v0.0.3` で push する。この push でも `release.yml` が起動し実際に GHCR へ公開されるため、検証後に上記と同様の後片付けが必要になる。GHCR 公開を避けたい場合は、`gh release create v0.0.3 --generate-notes --target main` を直接実行して Release オブジェクトのみを作成してもよい。
  - 続けて、同じコマンド（`gh release create v0.0.3 --generate-notes --target main`、または実際にワークフローが実行するのと同じ引数）をもう一度実行し、非0終了かつエラーメッセージに Release が既に存在する旨が含まれることを確認する。これにより `publish-release` ジョブが依拠する `gh release create` の重複防止動作そのものを実機で確認する（AC-07）。
  - 検証完了後、`gh release delete v0.0.3 --yes` で Release を削除し、push した場合は上記と同様に GHCR タグ・git タグも削除する。
- [ ] `docs/design/docker_deployment.md` の「リリース公開手順（開発者向け）」節（`docs/design/docker_deployment.md:33-60`）の自動処理一覧（現在4項目）に、次の項目を追記する。
  - `linux/amd64` バイナリのビルドとアーカイブ（`bsky-cleaner-vX.Y.Z-linux-amd64.tar.gz`）の生成
  - `SHA256SUMS`（チェックサムファイル）の生成
  - アーカイブと `SHA256SUMS` を添付した GitHub Release オブジェクトの作成
  - GitHub 標準の自動生成リリースノート（`--generate-notes`）が Release 本文に反映されること
- [ ] `docs/design/docker_deployment.md` に、ダウンロード後のチェックサム検証手順（`sha256sum -c SHA256SUMS`）についての一文を追記する（README.md の該当手順への参照でよい）。
- [ ] `docs/design/docker_deployment.md` の「動作確認用の手動実行」節に、`workflow_dispatch` 契機の実行では Release が `--draft` 付きで作成されること、確認後は `gh release delete` で削除するか `gh release edit --draft=false` で明示的に公開する必要があることを追記する。

**完了基準**: 実タグ push による実機検証（AC-02, AC-04, AC-08〜09）と `gh release create` の直接実行による重複防止確認（AC-07）が完了し、検証専用の Release・GHCR タグ・git タグがすべて削除済みである。`docs/design/docker_deployment.md` の追記が完了している。

## 4. テスト戦略

### 4.1 単体テスト

- 該当なし。本タスクは Go ソースコードに変更を加えないため、新規のユニットテストは追加しない。

### 4.2 統合テスト

- Go の統合テストは対象外（本タスクは GitHub Actions ワークフローと Markdown ドキュメントが中心）。ワークフローの検証は「7. 受け入れ基準の検証」の静的検証（`rg` によるワークフロー定義の確認）とフェーズ4の手動検証（実タグ push による実機確認、および `gh release create` の直接実行による重複防止確認）で行う。

### 4.3 後方互換テスト

- フェーズ1完了時点で `git diff` を取り、"Resolve tag name" 〜 "Tag and push patch version tag (vX.Y.Z)" の既存13ステップの内容に差分がないことを確認する（NF-002）。
- `cmd/main.go`／`Makefile` は変更しないため、既存の `cmd/main_test.go` の全テストは非影響のまま `make test` で通過する。

### 4.4 テストヘルパ方針

新規のクロスパッケージヘルパ（`testutil/`）・パッケージ内蔵ヘルパ（`test_helpers.go`）はいずれも不要（1.3 節参照）。本タスクは Go テストコードを追加しない。

## 5. リスク管理

| リスク | 影響 | 対策 |
|---|---|---|
| `actions/upload-artifact`/`download-artifact` のディレクトリ構造の扱いが想定と異なり、`publish-release` ジョブが期待するパス（`dist/bsky-cleaner-<tag>-linux-amd64.tar.gz`, `dist/SHA256SUMS`）でファイルを見つけられない | "Create GitHub Release" ステップがファイル未検出で失敗する | フェーズ4の実タグ push による実機検証で実際のパスを確認する。想定と異なる場合はダウンロード側の `path` 指定、または `gh release create` に渡すパスを実際の構造に合わせて修正する |
| `gh release create` の `--generate-notes` が、比較対象タグがない場合（初回リリース相当）に想定外のエラーや空の本文を返す（[02_architecture.md 3.2.3](./02_architecture.md#323-github-release-の作成とアセット添付リリースノート自動生成ac-05〜09-新設-publish-release-ジョブ) の未検証リスク） | AC-09 が満たされない | フェーズ4で、比較対象となる過去の Release が存在しない状態（実タグ push による初回リリース相当）で実機検証する。想定外の挙動が確認された場合は `--notes` で最小限のフォールバック本文を用意する設計変更を検討し、02_architecture.md にフィードバックする |
| フェーズ4の実機検証（実タグ push・`gh release create` の直接実行）が実際に GHCR へイメージを公開し、GitHub Release を作成してしまう | 検証用のバージョン番号・Release・GHCR パッケージバージョンが残ってしまう可能性がある | 未使用の実 semver（例 `v0.0.2`, `v0.0.3`）を使い、検証後は `gh release delete` で Release を削除し、GHCR 側のタグも 0010 の手順（[11_manual_verification_log.md](../0010_docker_image_release/11_manual_verification_log.md) 3-1節、Package 設定画面または `gh api --method DELETE` によるバージョン削除）に従って削除し、git タグも削除する |
| `publish-release` ジョブがチェックアウトを行わないため、`gh` CLI のリポジトリ解決に失敗する | "Create GitHub Release" ステップが対象リポジトリを誤認識、または解決できず失敗する | `GH_REPO: ${{ github.repository }}` を明示することで、ローカル git コンテキストに依存せずリポジトリを確定させる（1.2 節で確認済み） |

## 6. 実装チェックリスト

- [ ] フェーズ1完了（Go セットアップ・バイナリビルド・アーカイブ生成ステップ追加）
- [ ] フェーズ2完了（チェックサム生成・ワークフロー成果物アップロード・`release` ジョブの `outputs` 追加）
- [ ] フェーズ3完了（`publish-release` ジョブ新設）
- [ ] フェーズ4完了（実タグ push による実機検証・`gh release create` の直接実行による重複防止確認・`docs/design/docker_deployment.md` 追記）
- [ ] `make fmt` / `make test` / `make lint` がすべて通過（NF-001。Go ソース無変更のため実質的に無影響であることの確認）
- [ ] `make deadcode` で未使用コードがないことを確認（Go ソース無変更のため実質的に無影響であることの確認）

## 7. 受け入れ基準の検証（Acceptance Criteria Verification）

**AC-01: `bsky-cleaner-${TAG}-linux-amd64.tar.gz` が成果物として生成される**
- Verification (static): `rg -n "tar czf" .github/workflows/release.yml` で、対象文字列に `bsky-cleaner-\${TAG}-linux-amd64.tar.gz` が含まれることを確認する。
- Verification (manual): フェーズ4の実タグ push による実機検証で、実際にダウンロードしたアーカイブのファイル名が `bsky-cleaner-v0.0.2-linux-amd64.tar.gz` の形式であることを確認する。

**AC-02: アーカイブ内バイナリと GHCR 上の Docker イメージが同じ `vX.Y.Z (短縮コミットSHA)` を出力する**
- Verification (static): `rg -n "steps.resolve-tag.outputs.resolved_tag" .github/workflows/release.yml` の一致が2件以上（既存の "Build image" ステップの `build-args: VERSION=...` 行と、新規の "Build linux/amd64 binary" ステップの `env: TAG:` 行の両方）であることを確認する。同様に `rg -n "steps.short-sha.outputs.sha" .github/workflows/release.yml` の一致も2件以上であることを確認する。これにより、Docker イメージビルドとバイナリビルドの両方が同じ2つの step output を値の源として参照していることを構造的に確認する（`rg -n "main.version=|main.commit="` は `Dockerfile` の `-ldflags` 文字列にのみ一致し `release.yml` には一致しないため、この確認には使えない）。
- Verification (manual): フェーズ4の実機検証で、`tar xzf` 展開後の `./bsky-cleaner --version` と `docker run --rm ghcr.io/isseis/bsky-cleaner:v0.0.2 bsky-cleaner --version` の出力文字列が完全一致することを確認する。

**AC-03: `SHA256SUMS` が生成される**
- Verification (static): `rg -n "sha256sum" .github/workflows/release.yml` で "Generate checksum file" ステップの存在を確認する。
- Verification (manual): フェーズ4の実機検証でダウンロードした `SHA256SUMS` の中身が、アーカイブファイル名とそのチェックサム1行のみであることを確認する。

**AC-04: `sha256sum -c SHA256SUMS` が検証に成功する**
- Verification (manual): フェーズ4の実機検証で、アーカイブと `SHA256SUMS` を同じディレクトリに置いて `sha256sum -c SHA256SUMS` を実行し、`OK` が出力されることを確認する。

**AC-05: `gh release view <tag>` で Release オブジェクトが取得できる**
- Verification (static): `rg -n "gh release create" .github/workflows/release.yml` で `publish-release` ジョブ内にステップが存在することを確認する。
- Verification (manual): フェーズ4の実機検証で `gh release view v0.0.2` を実行し、Release オブジェクトの内容（タイトル、本文）が取得できることを確認する。

**AC-06: アーカイブと `SHA256SUMS` が添付ファイルとして含まれる**
- Verification (static): `rg -n -A6 "gh release create" .github/workflows/release.yml` の範囲に `bsky-cleaner-\${TAG}-linux-amd64.tar.gz` と `dist/SHA256SUMS` の両方が含まれることを確認する。
- Verification (manual): フェーズ4の実機検証で `gh release view v0.0.2 --json assets` を実行し、`assets` に2ファイルが含まれることを確認する。

**AC-07: 既存タグの Release がある場合、上書きせず非0終了する**
- Verification (static): `rg -n -A8 "gh release create" .github/workflows/release.yml` の範囲に、上書きを許可するフラグ（`--clobber` 等、`gh release create` には存在しないが将来の変更による混入を防ぐための否定確認）が含まれないことを確認する。
- Verification (manual): フェーズ4で、`release.yml` の "Create GitHub Release" ステップが実行するのと同じ `gh release create` コマンドを、未使用タグ（`v0.0.3`）に対して直接2回連続実行し、1回目は成功、2回目は非0終了かつ「Release が既に存在する」旨のエラーになることを確認する。`workflow_dispatch` による再実行では `release` ジョブの既存タグ確認（0010 由来、AC-03a）が先に非0終了してしまい、本タスクが追加する Release 重複防止の経路には到達できないため、ワークフロー経由の再現ではなく `gh release create` そのものを直接確認する（フェーズ4節「AC-07」参照）。

**AC-08: Release 本文に前回タグからのコミット/PR 一覧が含まれる**
- Verification (static): `rg -n -- "--generate-notes" .github/workflows/release.yml` で1件以上一致することを確認する。
- Verification (manual): フェーズ4の実機検証で、2つ目以降の検証用タグに対する Release 本文に、直前のタグ以降のコミット一覧（またはそれに相当する GitHub 標準フォーマットの内容）が含まれることを確認する。

**AC-09: 直前のタグが存在しない場合でもエラーにならない**
- Verification (static): `rg -n -- "--notes-start-tag" .github/workflows/release.yml` が0件であることを確認する（比較対象タグをハードコードしていないため、初回リリースでも `--generate-notes` の既定動作に委ねられることの確認）。
- Verification (manual): フェーズ4の実機検証で、比較対象となる過去の Release が存在しない状態（検証用タグでの最初の実行）でも "Create GitHub Release" ステップがエラーにならず、Release 本文が生成されることを確認する（[02_architecture.md 3.2.3](./02_architecture.md#323-github-release-の作成とアセット添付リリースノート自動生成ac-05〜09-新設-publish-release-ジョブ) の未検証リスクの解消を兼ねる）。

**AC-10: `docs/design/docker_deployment.md` に GitHub Release・バイナリ添付についての記述が追記されている**
- Verification (static): `rg -n "SHA256SUMS|GitHub Release|--generate-notes" docs/design/docker_deployment.md` がそれぞれ1件以上一致することを確認する。

**NF-001: `make fmt`・`make test`・`make lint` が成功する**
- Verification (build): 各フェーズ完了時に `make fmt && make test && make lint` を実行し、終了コード0であることを確認する。

**NF-002: 本タスクの追加により 0010 の既存 AC の挙動が変わらない**
- Verification (static): `git diff <フェーズ1着手前のコミット> -- .github/workflows/release.yml` で、"Resolve tag name" 〜 "Tag and push patch version tag (vX.Y.Z)" の既存13ステップのブロックに差分がなく、追加行のみが差分として現れることを確認する。

**NF-003: バイナリビルド・添付・Release 作成は追加のシークレット登録を必要とせず、`release` ジョブの `permissions` は変更しない**
- 補足: `01_requirements.md` の NF-003 原文は「ワークフローの `permissions` を `contents: read` から `contents: write` に変更する」と、単一ジョブ全体への変更を文言上想定している。[02_architecture.md 1.1](./02_architecture.md#11-設計原則)・5.2 節は、レビューにより `contents: write` の付与範囲をジョブ分割で最小化する設計に改め、`release` ジョブの `permissions` は変更せず `publish-release` ジョブにのみ付与する方式を採用した。以下の検証は、NF-003 の文言ではなく、この承認済みアーキテクチャが満たす意図（追加シークレット不要、必要最小限の権限付与）を検証するものである。
- Verification (static): `rg -n "permissions:" -A3 .github/workflows/release.yml` で、`release` ジョブ（またはワークフロー・トップレベル）が `contents: read`／`packages: write` のまま、`publish-release` ジョブが `contents: write` のみであることを確認する。`rg -n "secrets\." .github/workflows/release.yml` の一致がすべて `secrets.GITHUB_TOKEN` であることを確認する。

## 8. 成功基準

- **機能的完全性**: AC-01〜AC-10 がすべて緑（7章参照）。
- **品質指標**: `make fmt` / `make test` / `make lint` / `make deadcode` がすべて成功。既存の `cmd/main_test.go` の全テストが非影響のまま通過。
- **セキュリティ検証**: [02_architecture.md 5.2](./02_architecture.md#52-脅威と対策) の脅威対策（`publish-release` ジョブの `contents: write` 単独スコープ、`--verify-tag` による fail-closed、`--draft` によるテスト時の非公開化）がワークフローファイル上で確認できる。
- **ドキュメント整備**: `docs/design/docker_deployment.md` の追記が完了している。

## 9. 次のステップ

- 実装完了後、[docs/dev/developer_guide/package_reference.md](../../dev/developer_guide/package_reference.md) は `internal/`／`cmd/` 配下に変更がないため更新不要。
- フォローアップ候補（本タスクのスコープ外、[02_architecture.md 9章](./02_architecture.md#9-将来の拡張性) 参照）: macOS/Windows バイナリの提供、PR ラベルに基づくリリースノートのカテゴリ分け、イメージ署名・SBOM・バイナリの暗号学的署名、Release 作成のみの独立した再試行、リリースノート本文サイズ・生成時間の上限対応。

## 10. クロス検索チェックリスト

- [ ] `publish-release`／`release-assets`／`Build linux/amd64 binary`／`Package binary archive`／`Generate checksum file`／`Create GitHub Release`／`Download release artifacts` は本タスクで新規追加するステップ名・ジョブ名・アーティファクト名であり、`.github/workflows/release.yml`・`.github/workflows/ci.yml` の既存ステップ名と衝突しないことを実装後に `rg -n "Build linux/amd64 binary|Package binary archive|Generate checksum file|Create GitHub Release|Download release artifacts" .github/workflows/*.yml` で確認する（各1箇所のみ一致すること）。
- [ ] `README.md` の既存の「インストールと実行（ビルド済み実行ファイル）」節（1.3 節で確認済みの先行記述）が、実装後の実際のアーカイブ名・`SHA256SUMS` の形式と一致し続けていることを、フェーズ4完了時に目視で再確認する（`rg -n "bsky-cleaner-vX.Y.Z-linux-amd64.tar.gz|SHA256SUMS" README.md` で該当箇所を再表示して確認する）。
