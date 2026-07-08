# Docker イメージのビルド済み配布 — 実装計画書

## Document Status

| Item | Value |
|---|---|
| Status | `approved` |
| Created | 2026-07-08 |
| Review date | 2026-07-08 |
| Reviewer | isseis |
| Comments | - |

関連ドキュメント: [要件定義書](01_requirements.md) / [アーキテクチャ設計書](02_architecture.md)

## 1. 実装概要

### 1.1 目的

本計画は [02_architecture.md](./02_architecture.md) に基づき、[01_requirements.md](./01_requirements.md) の AC-01〜AC-22（AC-03a・AC-03b・AC-11a を含む）を満たすための作業を、検証可能な単位に分解する。本タスクは、GHCR へのビルド済み Docker イメージ公開、ビルド時バージョン埋め込みと `--version` フラグ、`docker-compose.yml` のイメージ参照化、関連ドキュメント整備、CI での Docker ビルド確認ジョブ追加を行う。

### 1.2 実装方針

- **設計の再掲を避ける**: 各コンポーネントの設計詳細・エラー分類・処理フロー・脅威モデルは [02_architecture.md](./02_architecture.md) を参照し、本計画では「どのファイルに何を追加し、どう検証するか」のみを書く。
- **フェーズは [02_architecture.md 8 章](./02_architecture.md#8-実装優先順位) の定義・順序に一致させる**（フェーズ1: バージョン埋め込みと `--version` → フェーズ2: GHCR 公開ワークフロー → フェーズ3: 配布経路とドキュメント → フェーズ4: CI ビルド確認）。
- **ロールアウト順序の制約を厳守する**（[02_architecture.md 3.2.5](./02_architecture.md#325-docker-composeyml-の-image-参照化ac-12〜14)）: `docker-compose.yml` の `image:` 参照化コミットは、実タグでの初回リリースおよび GHCR パッケージ可視性の public 切り替え（フェーズ3 ステップ8）が完了するまで `main` にマージしない。
- **write 権限を持つ GitHub Actions のバージョン固定**（[02_architecture.md 3.2.1](./02_architecture.md#321-releaseyml-のトリガーとタグ検証ac-01〜02-ac-06〜07)）: `docker/login-action` と `docker/build-push-action` はコミット SHA で固定参照する。事前に GitHub API で最新リリースの実コミット SHA を確認済み（2026-07-08 時点）。
  - `docker/login-action@af1e73f918a031802d376d3c8bbc3fe56130a9b0`（`v4.4.0` の実コミット）
  - `docker/build-push-action@53b7df96c91f9c12dcc8a07bcb9ccacbed38856a`（`v7.3.0` の実コミット）
  - 読み取り専用の `dorny/paths-filter`（フェーズ4、`ci.yml` 側）と `docker/setup-buildx-action`（`@v4`、フェーズ2で `docker buildx build` を実行可能にするために使用）は、いずれもタグ参照（`@v4`）のままでよい（[02_architecture.md 1.1](./02_architecture.md#11-設計原則)）。`ci.yml` の `docker-build-check` ジョブ（フェーズ4）が使う `docker/build-push-action` は `packages: write` を持たないジョブで実行されるため、このジョブでの参照はタグ参照のままでよい。
  - **3.2.1 と 3.2.2 の対応関係の確認**: [02_architecture.md 3.2.2](./02_architecture.md#322-既存タグ確認とタグ-push-の順序制御ac-03-ac-03a-ac-03b-nf-004) はビルド手段を `docker buildx build --platform linux/amd64 --load` という CLI 表記で記述しているが、3.2.1 は同じジョブ内で `docker/build-push-action` のコミット SHA 固定を明記している。両者は矛盾ではない。`docker/build-push-action`（`push: false`, `load: true`, `platforms: linux/amd64`）が、内部的に上記 buildx 呼び出しと同じ効果を持つビルド手段として選定されたと解釈する。本計画ではこの解釈に従い、`docker/build-push-action` を実装に用いる。
- **タグ解決後の短縮 SHA は `github.sha` コンテキストではなく `git rev-parse --short HEAD` を用いる**: `workflow_dispatch` 経由の実行では `github.sha` はワークフロー実行のトリガーになった ref（既定ブランチ等）を指し、解決済みタグのコミットとは一致しない場合があるためである。チェックアウト後に算出する（[02_architecture.md 3.2.1](./02_architecture.md#321-releaseyml-のトリガーとタグ検証ac-01〜02-ac-06〜07) の「チェックアウト対象」の記述に対応）。
- **`--version` 用フラグ変数名の衝突回避**: ビルド時に埋め込むパッケージレベル変数 `version`（string）と、`parseFlags` 内で `fs.BoolVar` に登録するローカルの bool 変数は名前が衝突するため、後者は `showVersion` と命名する（[02_architecture.md 3.2.4](./02_architecture.md#324-version-v-フラグcmdmaingoac-08〜09-ac-11〜11a) は変数名までは指定していないため、本計画で確定する）。
- **`-h`/`--help` の二重検出パターンを `--version`/`-v` にも完全に踏襲する**: 既存の `help` は (1) `fs.Parse` 前の早期走査ループでの直接一致、(2) `fs.Parse` 後の `if help { ... }` フォールバック、の2経路で検出される（`cmd/main.go:264-268` と `cmd/main.go:279-282`）。(2) は `--help=true` のような早期走査の単純一致では拾えないフラグ形式を救済する。`--version`/`-v` も同じ2経路を実装しないと、`--version=true` が早期走査をすり抜けて `fs.Parse` 後の通常経路（`--config` 必須チェック）に落ちてしまい AC-11a に反する。本計画のフェーズ1では両経路を明記する。
- **既存資産の再利用**: フェーズ1では `-h`/`--help` の早期検出パターン（`cmd/main.go` の `parseFlags` 内、`fs.Parse` 前の直接走査ループ）をそのまま拡張する。新しい分岐構造は導入しない。
- **Go ソースは英語**: 追加する識別子・コメント・文字列リテラルはすべて英語で記述する。`docker-compose.yml` の追加コメントも、既存コメント（英語）との一貫性のため英語で記述する。
- **各編集後に `make fmt` → `make test` → `make lint` を実行**し、緑を維持する（NF-001）。

### 1.3 既存コード調査結果

**`cmd/main.go`（フェーズ1対象）**

- 既存: `parseFlags(args []string, out io.Writer) (configPath string, apply bool, err error)` は `-h`/`--help` を「`fs.Parse` 実行前に `args` を直接走査し、見つかった時点で `fs.Usage()` を呼び `flag.ErrHelp` を返す」という早期検出パターン（`cmd/main.go:264-273`）で扱い、加えて `fs.BoolVar` にも `help`/`h` を登録している（ドキュメント目的、`cmd/main.go:255-256`）。`main()`（`cmd/main.go:429-439`）は `errors.Is(err, flag.ErrHelp)` を判定して `os.Exit(exitOK)` する。
- 不足: `version`/`commit` のビルド時変数、`errVersionRequested` センチネルエラー、`formatVersion()` 関数、`--version`/`-v` の早期検出分岐、`main()` 側での `errVersionRequested` 判定・標準出力書き込み・`exitOK` 終了。
- 変更: `parseFlags` の早期走査ループ（`cmd/main.go:264-273`）に `--version`/`-v` の判定を追加し、該当時 `errVersionRequested` を返す。`fs.BoolVar` にドキュメント目的で `version`/`v`（変数名 `showVersion`）を登録する。パッケージレベルに `version`/`commit` 変数と `formatVersion()` を追加する。`main()`（`cmd/main.go:429-439`）に `errors.Is(err, errVersionRequested)` の判定を、既存の `flag.ErrHelp` 判定と同様の形で追加する。既存シグネチャ・既存フラグの挙動は変更しない。

**`cmd/main_test.go`（フェーズ1対象）**

- 既存: `TestParseFlags_HelpLongFlag_ReturnsErrHelpAndPrintsFlagList` / `TestParseFlags_HelpShortFlag_ReturnsErrHelpAndPrintsFlagList` / `TestParseFlags_HelpFlag_DoesNotRequireConfig`（`cmd/main_test.go:118-142`）が `-h`/`--help` の早期検出パターンを検証済みであり、`--version`/`-v` のテストはこれと同型の構造で追加できる。
- 不足: `formatVersion` のユニットテスト、`--version`/`-v` の早期検出・`--config` 非依存を検証するテスト。
- 変更: `TestFormatVersion_WithCommit_ReturnsVersionAndCommit` / `TestFormatVersion_NonDevVersionEmptyCommit_ReturnsVersionOnly` / `TestFormatVersion_EmptyCommit_ReturnsVersionOnly` / `TestParseFlags_VersionLongFlag_ReturnsErrVersionRequested` / `TestParseFlags_VersionShortFlag_ReturnsErrVersionRequested` / `TestParseFlags_VersionEqualsTrueForm_ReturnsErrVersionRequested` / `TestParseFlags_VersionFlag_DoesNotRequireConfig` を追加する（計7件）。既存の `TestParseFlags_*`／`TestRun_*` は非影響。

**`Dockerfile`（フェーズ1対象）**

- 既存: ビルドステージは `golang@sha256:...` を digest 固定し、`RUN go build -o /out/bsky-cleaner ./cmd` でバイナリをビルドする（`Dockerfile:5-13`）。`ARG` は未使用。
- 不足: `ARG VERSION`/`ARG COMMIT` と、`go build` への `-ldflags` 追加。
- 変更: ビルドステージに `ARG VERSION=dev` / `ARG COMMIT=""` を追加し、`go build` 呼び出しに `-ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT}"` を追加する。マルチステージ構成・digest 固定・`--platform=linux/amd64` 固定・`supercronic` 取得手順は変更しない。

**`.github/workflows/`（フェーズ2・4対象）**

- 既存: `ci.yml` に `test`（`make build` → `go test -tags test ./...`）と `lint`（`golangci-lint-action@v9`）の2ジョブがあり、`on: pull_request/push（branches: [main]、paths-ignore: ['**.md', 'docs/**']）` で起動する。GHCR 関連のワークフロー・action は存在しない。
- 不足: `.github/workflows/release.yml`（新規）、`ci.yml` への `docker-build-check` ジョブ。
- 変更: `release.yml` を新規作成する。`ci.yml` には既存の `test`/`lint` ジョブ定義・`on:` トリガー条件を変更せず、`docker-build-check` ジョブのみを並行追加する。

**`scripts/`（フェーズ2対象、新規ディレクトリ）**

- 既存: リポジトリに `scripts/` ディレクトリは存在しない（`ls scripts/` で未存在を確認済み）。命名の衝突はない。
- 不足: `scripts/check-existing-tag.sh`（AC-03a の fail-closed 判定ロジック）と、そのテスト `scripts/check_existing_tag_test.go`。
- 変更: 2ファイルを新規追加する。`.github/` 配下ではなくリポジトリ直下に置く（Go の `go test ./...` はディレクトリ名が `.` で始まる `.github/` 配下を自動的に除外するため、`make test` の対象に含めるには `.github/` 外に置く必要がある）。

**`docker-compose.yml`（フェーズ3対象、ロールアウト順序の制約あり）**

- 既存: `services.bsky-cleaner.build: .` を指定し、`.env` からの環境変数展開・`./config:/config:ro` のボリュームマウントを持つ（`docker-compose.yml:11-20`）。
- 不足: `image:` 参照とその隣接コメント（バージョン更新方法・digest 参照・`latest` の位置づけ）。
- 変更: `build: .` を `image: ghcr.io/isseis/bsky-cleaner:vX.Y.Z`（実タグ、例 `v1.0.0`）に置き換え、直前にコメントを追加する。`environment:`／`volumes:`／`restart:` は変更しない。**このコミットは、フェーズ3ステップ8（実タグでの初回リリースと可視性切り替え）の完了後にのみ `main` へ反映する。**

**`docs/design/docker_deployment.md`／`README.md`（フェーズ3対象）**

- 既存: `docker_deployment.md` は `.env` 管理方針と `print-schedule`/内蔵 cron の橋渡し手順のみを記載し、リリース公開手順・GHCR 可視性切り替え手順は未記載。`README.md` の Installation 節はソースからの `make build` 手順のみで、Docker 経由の取得・起動手順は未記載。
- 不足: 開発者向けリリース公開手順（`git tag`/`workflow_dispatch`）、GHCR 可視性切り替え手順、利用者向け `docker compose pull && docker compose up -d` 手順、`--version` の使用例。
- 変更: 両ファイルにセクションを追記する（既存記述は変更しない）。

**`internal/` 配下**

- 変更不要。本タスクは `internal/` パッケージに変更を加えない（[02_architecture.md 7.2](./02_architecture.md#72-既存テストへの影響)）。

**テストヘルパ**

- 新規のクロスパッケージヘルパ・パッケージ内蔵ヘルパのいずれも不要。フェーズ1のテストは既存の `bytes.Buffer` を使った `parseFlags` の直接呼び出しパターンをそのまま踏襲する（[test_organization.md](../../dev/developer_guide/test_organization.md) の分類が必要になる新規ヘルパは生じない）。

## 2. 実装ステップ

### PR-1 作成ポイント
- **対象ステップ**: フェーズ1: バージョン埋め込みと `--version`

**推奨タイトル**: feat: add --version flag and build-time version embedding

**レビュー観点**:
- `--version`/`-v` が `-h`/`--help` と同じ早期検出パターン（`fs.Parse` 前の直接走査 + `fs.Parse` 後のフォールバック）を実装しているか
- `formatVersion` が commit 空文字時に括弧なしのバージョンのみを返すか
- `Dockerfile` の `ARG VERSION`/`ARG COMMIT` と `-ldflags` が正しく追加されているか
- `--config` 未指定でも `--version` がエラーにならないか（AC-11a）
- 既存の `TestParseFlags_*`／`TestRun_*` が非影響であること

PR checkpoint checkboxes (used by step 4/5a to detect PR boundaries):
- [x] グリーンゲート通過: `make fmt && make test && make lint && make deadcode`
- [x] PR を作成した
- [x] PR がマージされた
- [x] 次のブランチへ切り替えた

### フェーズ1: バージョン埋め込みと `--version`（AC-08〜09, AC-10 の一部, AC-11〜11a）

**対象ファイル**: `cmd/main.go`, `cmd/main_test.go`, `Dockerfile`

- [x] `cmd/main.go`: パッケージレベルに以下を追加する（[02_architecture.md 3.2.4](./02_architecture.md#324-version-v-フラグcmdmaingoac-08〜09-ac-11〜11a)）。
  ```go
  var version = "dev"
  var commit = ""

  var errVersionRequested = errors.New("version requested")

  func formatVersion() string {
      if commit == "" {
          return version
      }
      return version + " (" + commit + ")"
  }
  ```
- [x] `cmd/main.go`: `parseFlags` の早期走査ループ（`cmd/main.go:264-273`）に `--version`/`-v` の判定を追加し、該当時 `errVersionRequested` を返す（`-h`/`--help` と同じ「`fs.Usage()` を呼ばず即座に返す」形。バージョン出力は `out` ではなく `main()` が標準出力へ書くため）。
- [x] `cmd/main.go`: `parseFlags` 冒頭のローカル変数宣言群（`cmd/main.go:251` の `var help bool` の並び）に `var showVersion bool` を追加し、`fs.BoolVar` 登録群（`cmd/main.go:252-256`）に `fs.BoolVar(&showVersion, "version", false, "print version information and exit")` / `fs.BoolVar(&showVersion, "v", false, "print version information and exit (shorthand for --version)")` を追加する。
- [x] `cmd/main.go`: `fs.Parse(args)` 成功後、既存の `if help { fs.Usage(); return "", false, flag.ErrHelp }`（`cmd/main.go:279-282`）の直後に次のフォールバック分岐を追加する。`help` と同様、早期走査（1つ目の項目）が拾えないフラグ形式（例: `--version=true`）を救済するための2経路目であり、`--config` 必須チェック（`cmd/main.go:289-292`）より前に置く。
  ```go
  if showVersion {
      return "", false, errVersionRequested
  }
  ```
- [x] `cmd/main.go`: `main()`（`cmd/main.go:429-439`）に、既存の `errors.Is(err, flag.ErrHelp)` 判定の直後（またはその前）に次を追加する。既存の他の標準出力/標準エラー出力と同じ `_, _ = fmt.Fprintln(...)` + `//nolint:gosec` パターンに合わせる（`cmd/main.go:307, 319, 325, 331, 340, 403, 408, 412, 423, 437` の既存パターンと同一書式。bare な `fmt.Println` は使わない（errcheck/gosec で `make lint` が失敗するため）。
  ```go
  if errors.Is(err, errVersionRequested) {
      _, _ = fmt.Fprintln(os.Stdout, formatVersion()) //nolint:gosec // stdout is a CLI stream, not an HTTP response body; G705's XSS concern does not apply
      os.Exit(exitOK)
  }
  ```
  この分岐は `configPath, apply, err := parseFlags(...)` の直後、かつ `run(configPath, ...)` 呼び出しより前に位置するため、`config.LoadAppConfig`／`atproto.NewClient` には到達しない（AC-11）。
- [x] `Dockerfile`: ビルドステージ（`Dockerfile:5-13`）を次のように変更する。

  変更前:
  ```dockerfile
  FROM --platform=linux/amd64 golang@sha256:3ad57304ad93bbec8548a0437ad9e06a455660655d9af011d58b993f6f615648 AS build

  WORKDIR /src
  COPY go.mod go.sum ./
  RUN go mod download

  COPY . .
  RUN go build -o /out/bsky-cleaner ./cmd
  ```

  変更後:
  ```dockerfile
  FROM --platform=linux/amd64 golang@sha256:3ad57304ad93bbec8548a0437ad9e06a455660655d9af011d58b993f6f615648 AS build

  ARG VERSION=dev
  ARG COMMIT=""

  WORKDIR /src
  COPY go.mod go.sum ./
  RUN go mod download

  COPY . .
  RUN go build -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT}" -o /out/bsky-cleaner ./cmd
  ```
  `--build-arg` 未指定のローカル `docker build .`／`make build`／`go build ./cmd` の挙動は変わらない（AC-09）。
- [x] `cmd/main_test.go`: `TestFormatVersion_WithCommit_ReturnsVersionAndCommit` を追加する。テスト内で `version`/`commit` を `"v1.2.3"`/`"a1b2c3d"` に設定し（`t.Cleanup` で元の値 `"dev"`/`""` に復元）、`formatVersion()` が `"v1.2.3 (a1b2c3d)"` を返すことを検証する（AC-08）。このテストと以下2件の `TestFormatVersion_*` は同一パッケージレベル変数を書き換えるため、`t.Parallel()` は付与しない。
- [x] `cmd/main_test.go`: `TestFormatVersion_NonDevVersionEmptyCommit_ReturnsVersionOnly` を追加する。`version` を `"v1.2.3"` に、`commit` を `""` に設定し（`t.Cleanup` で復元）、`formatVersion()` が `"v1.2.3"`（commit 部分の括弧なし）を返すことを検証する。`version` が非既定値であっても commit 空文字時に括弧が付与されないことを、既定値どうしのケースと切り分けて確認する（AC-08, AC-09）。
- [x] `cmd/main_test.go`: `TestFormatVersion_EmptyCommit_ReturnsVersionOnly` を追加する。`version`/`commit` を既定値 `"dev"`/`""` のまま呼び出し、`formatVersion()` が `"dev"` を返すことを検証する（AC-09。ローカルビルドの既定値がそのまま出力されることのスモークテスト）。
- [x] `cmd/main_test.go`: `TestParseFlags_VersionLongFlag_ReturnsErrVersionRequested` を追加する。`parseFlags([]string{"--version"}, &bytes.Buffer{})` が `errVersionRequested` を返すことを `require.ErrorIs` で検証する（AC-08 の早期走査経路）。
- [x] `cmd/main_test.go`: `TestParseFlags_VersionShortFlag_ReturnsErrVersionRequested` を追加する。`parseFlags([]string{"-v"}, &bytes.Buffer{})` が `errVersionRequested` を返すことを検証する。
- [x] `cmd/main_test.go`: `TestParseFlags_VersionEqualsTrueForm_ReturnsErrVersionRequested` を追加する。`parseFlags([]string{"--version=true"}, &bytes.Buffer{})` が `errVersionRequested` を返すことを検証する。早期走査ループでは検出できない（トークンが `--version` と完全一致しない）フラグ形式が `fs.Parse` 後の `if showVersion { ... }` フォールバック経路で正しく検出されることを確認する。
- [x] `cmd/main_test.go`: `TestParseFlags_VersionFlag_DoesNotRequireConfig` を追加する。`TestParseFlags_HelpFlag_DoesNotRequireConfig`（`cmd/main_test.go:138-142`）と同型で、`--config` を指定せずに `parseFlags([]string{"--version"}, &bytes.Buffer{})` を呼び、返る `err` が `errVersionRequested` であって「`--config` が必要」エラーでないことを検証する（AC-11a）。

**完了基準**: `make fmt && make test && make lint` が緑。上記7テストがすべてパスする。

### PR-2 作成ポイント
- **対象ステップ**: フェーズ2: GHCR 公開ワークフロー

**推奨タイトル**: feat: add GHCR release workflow with tag validation and existing-tag protection

**レビュー観点**:
- `release.yml` のトリガー設定（`tags: ['v*']` + `workflow_dispatch.inputs.tag`）が AC-01 を満たすか
- semver 検証（`^v[0-9]+\.[0-9]+\.[0-9]+$`）が fail-closed で動作するか（AC-02）
- `docker/login-action`/`docker/build-push-action` がコミット SHA 固定されているか（AC-06）
- `concurrency` 設定がタグ名をキーとし、`cancel-in-progress: false` であるか（AC-03a）
- `scripts/check-existing-tag.sh` の fail-closed 判定ロジックが3ケースすべてを正しく処理するか
- push 順序が `latest` → `vX` → `vX.Y` → `vX.Y.Z` であるか（AC-03b）
- `permissions` が `contents: read, packages: write` のみであるか
- `timeout-minutes: 15` が設定されているか

PR checkpoint checkboxes (used by step 4/5a to detect PR boundaries):
- [x] グリーンゲート通過: `make fmt && make test && make lint && make deadcode`
- [x] PR を作成した
- [x] PR がマージされた
- [x] 次のブランチへ切り替えた

### フェーズ2: GHCR 公開ワークフロー（AC-01〜07, AC-10 の残り, NF-003, NF-004）

**対象ファイル**: `.github/workflows/release.yml`（新規）, `scripts/check-existing-tag.sh`（新規）, `scripts/check_existing_tag_test.go`（新規）

- [x] `on:` トリガーを設定する（[02_architecture.md 3.2.1](./02_architecture.md#321-releaseyml-のトリガーとタグ検証ac-01〜02-ac-06〜07)）: `push.tags: ['v*']` と、タグ名を1つ受け取る `workflow_dispatch.inputs.tag`（`required: true`, `type: string`）（AC-01）。
- [x] 権限を `permissions: { contents: read, packages: write }` のみに限定する（AC-06）。
- [x] ジョブレベルに `timeout-minutes: 15` を設定する（[02_architecture.md 3.2.1](./02_architecture.md#321-releaseyml-のトリガーとタグ検証ac-01〜02-ac-06〜07)、AC-07 に付随する運用要件）。
- [x] タグ名解決ステップを追加する: `push` 契機では `github.ref_name`、`workflow_dispatch` 契機では `inputs.tag` を、以降のステップが共通で参照する1つの outputs 変数（例: `resolved_tag`）にまとめる。
- [x] `concurrency: { group: release-${{ <resolved_tag> }}, cancel-in-progress: false }` を設定する（AC-03a, NF-004）。
- [x] "Validate tag format" ステップを追加する: 解決済みタグが正規表現 `^v[0-9]+\.[0-9]+\.[0-9]+$` に一致しない場合、非0で終了する（AC-02）。
- [x] `actions/checkout@v5` で解決済みタグの ref をチェックアウトする（存在しない `workflow_dispatch` 入力タグはここで失敗し、fail-closed として機能する）。
- [x] "Resolve short commit SHA" ステップを追加する: `git rev-parse --short HEAD` の結果を outputs に保存する（`github.sha` コンテキストは使わない。理由は 1.2 節参照）。
- [x] `docker/setup-buildx-action@v4` で Buildx を有効化する。
- [x] `docker/login-action@af1e73f918a031802d376d3c8bbc3fe56130a9b0`（`v4.4.0`）で GHCR にログインする。`username: ${{ github.actor }}`, `password: ${{ secrets.GITHUB_TOKEN }}`, `registry: ghcr.io`（AC-06）。
- [x] `scripts/check-existing-tag.sh` を新規作成する: AC-03a の fail-closed 判定ロジックを、ワークフロー YAML のインライン shell から切り出した単体の bash スクリプトとして実装する（GitHub Actions のスクリプト化ロジックが複数条件分岐を持つ場合は独立スクリプト化しテストする方針に従う）。
  - 呼び出し規約: 第1引数に直前の `docker manifest inspect` の終了コード、標準入力にその標準エラー出力を受け取る。
  - 判定: 終了コード `0`（マニフェスト取得成功 = 既存）→ 終了コード `1` で終了（fail-closed、AC-03a）。終了コード非0かつ標準入力に GHCR の「不在」を示すエラー文字列（`docker manifest inspect` が出力する `manifest unknown` 相当。実装時に実際の出力文字列を一度取得し、定数として反映する）が含まれる → 終了コード `0`（「不在が確認できた」ため処理継続）。それ以外（一時的なレジストリ障害・レート制限・認証エラー等、不在と確認できない失敗）→ 終了コード `1`（fail-closed、[02_architecture.md 3.2.2](./02_architecture.md#322-既存タグ確認とタグ-push-の順序制御ac-03-ac-03a-ac-03b-nf-004)）。
- [x] `scripts/check_existing_tag_test.go` を新規作成する: `os/exec` で `scripts/check-existing-tag.sh` を起動し、標準入力・第1引数を差し替えて以下3ケースを検証する（AC-03a）。
  - `TestCheckExistingTag_ManifestFound_ExitsFailClosed`: 第1引数 `"0"`、標準入力空 → スクリプトが終了コード `1` で終了することを検証する。
  - `TestCheckExistingTag_ManifestNotFound_ExitsSuccess`: 第1引数 `"1"`、標準入力に確定済みの「不在」エラー文字列 → スクリプトが終了コード `0` で終了することを検証する。
  - `TestCheckExistingTag_InconclusiveFailure_ExitsFailClosed`: 第1引数 `"1"`、標準入力に無関係なエラー文字列（例: レート制限や5xxを模した文字列）→ スクリプトが終了コード `1` で終了することを検証する。
  - このテストファイルは `scripts/` ディレクトリ（`.` で始まらない）に置くため `go test -tags test ./...`（`make test`）の対象に含まれる。新規のクロスパッケージヘルパ・パッケージ内蔵ヘルパは不要（[test_organization.md](../../dev/developer_guide/test_organization.md) が対象とする Go パッケージ間ヘルパではなく、スクリプト自身の直接テストであるため）。
- [x] "Check existing vX.Y.Z tag" ステップを追加する: `docker manifest inspect ghcr.io/isseis/bsky-cleaner:<resolved_tag>` を実行して終了コードと標準エラー出力を捕捉し、`scripts/check-existing-tag.sh` にその終了コードと標準エラー出力を渡す。スクリプトが非0で終了した場合、このステップも非0で終了する。
- [x] "Build image" ステップを追加する: `docker/build-push-action@53b7df96c91f9c12dcc8a07bcb9ccacbed38856a`（`v7.3.0`）を `push: false`, `load: true`, `platforms: linux/amd64`, `build-args: VERSION=<resolved_tag>` / `COMMIT=<short sha>`, `tags: bsky-cleaner:release` で実行する（AC-04, AC-10）。
- [x] "Tag and push latest" ステップを追加する: `docker tag bsky-cleaner:release ghcr.io/isseis/bsky-cleaner:latest && docker push ghcr.io/isseis/bsky-cleaner:latest`。
- [x] "Tag and push major version tag" ステップを追加する: `vX`（例 `v1`）を `docker tag`/`docker push`。
- [x] "Tag and push minor version tag" ステップを追加する: `vX.Y`（例 `v1.2`）を `docker tag`/`docker push`。
- [x] "Tag and push patch version tag (vX.Y.Z)" ステップを追加する: 解決済みタグそのものを `docker tag`/`docker push`（最後に実行、AC-03b, NF-004）。
- [x] `workflow_dispatch` から、まだ GHCR に存在しない実 semver タグ（例 `v0.0.1`。`v0.0.1-plan-check` のような非 semver 値は "Validate tag format" ステップで拒否されるため使えない）を指定して手動実行し、正常系（4タグが GHCR に公開される）を確認する（[02_architecture.md 8章 フェーズ2](./02_architecture.md#8-実装優先順位) の「ドライラン的な動作確認」）。
- [x] 上記ドライラン実行中に、いずれか1つの浮動タグ push ステップ（例 "Tag and push major version tag"）を一時的に失敗させ（例: 存在しないレジストリパスを指す一時的な変更）、`vX.Y.Z` タグが push されないまま非0終了することを確認する。修正後、同じタグ名で `workflow_dispatch` を再実行し、タグの手動削除なしに正常終了して4タグすべてが公開されることを確認する（AC-03b, NF-004 の実挙動検証）。
- [x] ドライラン検証が完了したら、GitHub の Package 設定画面から `v0.0.1` の4タグ（`latest`/`v0`/`v0.0`/`v0.0.1`）を手動削除する。本番のリリース履歴（フェーズ3ステップ8の `v1.0.0` 初回リリース）に検証専用のタグを残さないためである。

上記ドライラン検証の実施手順は [10_manual_verification_runbook.md](10_manual_verification_runbook.md) に、実行ログは [11_manual_verification_log.md](11_manual_verification_log.md) にまとめてある（実施日 2026-07-08）。AC-03a・AC-03b・NF-004・正常系4タグ公開のいずれも確認済み。副次的な発見として、現行イメージには `--version`/`--help` が未実装で `--config` 必須のまま起動が中断される点をログに記録済み（本タスクのスコープ外、フォローアップ候補）。

**完了基準**: `.github/workflows/release.yml`・`scripts/check-existing-tag.sh`・`scripts/check_existing_tag_test.go` が上記すべての要素を含む。`make test` で `scripts/check_existing_tag_test.go` の3ケースが緑。ドライラン実行（正常系・浮動タグ push 失敗からの再実行・GHCR タグ削除）が完了している（[11_manual_verification_log.md](11_manual_verification_log.md) 参照）。実際に GHCR へ公開される点に留意する。

### PR-3 作成ポイント
- **対象ステップ**: フェーズ3: 配布経路とドキュメント, フェーズ4: CI ビルド確認

**推奨タイトル**: feat: add docker-compose image reference, release docs, and CI build check

**レビュー観点**:
- `docker-compose.yml` の `image:` 参照が `latest` 以外の特定バージョンタグを指しているか（AC-12）
- コメントにバージョン更新方法・digest 参照・`latest` の位置づけの3点が記載されているか（AC-13）
- `docs/design/docker_deployment.md` にリリース公開手順と可視性切り替え手順が記載されているか（AC-15, AC-17）
- `README.md` に `docker compose pull && docker compose up -d` 手順と `--version` 使用例が記載されているか（AC-16）
- `ci.yml` の `docker-build-check` ジョブが既存 `test`/`lint` ジョブを変更せず並行追加されているか（AC-20）
- パスフィルタ（`dorny/paths-filter`）が正しく設定されているか（AC-21）
- `cache-from`/`cache-to` に `type=gha` が設定されているか（AC-22）
- `timeout-minutes: 10` が設定されているか（NF-005）
- `docker-compose.yml` の `image:` 変更は実タグリリース後にのみ `main` へマージするロールアウト順序の制約が守られているか

PR checkpoint checkboxes (used by step 4/5a to detect PR boundaries):
- [x] グリーンゲート通過: `make fmt && make test && make lint && make deadcode`
- [x] PR を作成した
- [x] PR がマージされた
- [x] 次のブランチへ切り替えた

### フェーズ3: 配布経路とドキュメント（AC-05 の運用手順, AC-12〜17）

**対象ファイル**: `docker-compose.yml`, `docs/design/docker_deployment.md`, `README.md`

**順序に注意**（[02_architecture.md 3.2.5](./02_architecture.md#325-docker-composeyml-の-image-参照化ac-12〜14) のロールアウト順序の制約）: 以下のステップ6（`docker-compose.yml`）は、ステップ8（実タグでの初回リリースと可視性切り替え）の完了後にのみ `main` へマージする。ステップ7（ドキュメント追記）はステップ8を待つ必要はない。

- [ ] `docker-compose.yml`: `services.bsky-cleaner.build: .`（`docker-compose.yml:11`）を削除し、直前に以下のコメントを追加したうえで `image: ghcr.io/isseis/bsky-cleaner:v1.0.0`（フェーズ3ステップ8で実際に公開する初回バージョンタグに置き換える）に置き換える（AC-12, AC-13）。**未着手（ロールアウト順序制約により、実タグでの初回リリース・可視性切り替え（ステップ8）が完了するまで意図的に見送っている。PR-3 の最初のレビュー時点で誤って先行コミットされていたため、weakreview で検出し `build: .` に戻した）。**

  変更前:
  ```yaml
  services:
    bsky-cleaner:
      build: .
      restart: unless-stopped
  ```

  変更後:
  ```yaml
  services:
    bsky-cleaner:
      # Version: bump this tag when upgrading, then run `docker compose
      # pull` to fetch the new image. Compose does not switch versions on
      # its own.
      # For a stricter pin, use a digest instead of a tag, e.g.
      # image: ghcr.io/isseis/bsky-cleaner@sha256:<digest>
      # `latest` tracks the newest patch release; use it only to try the
      # latest build, not as the default for normal operation.
      image: ghcr.io/isseis/bsky-cleaner:v1.0.0
      restart: unless-stopped
  ```
- [x] `docs/design/docker_deployment.md`: 「開発者向けリリース公開手順」節を追加する。`git tag vX.Y.Z && git push --tags` によるタグ push、および `workflow_dispatch` から対象タグ名を入力して動作確認する手順を記載する（フェーズ2で確定した入力名 `tag` を用いる）（AC-15）。
- [x] `docs/design/docker_deployment.md`: 「GHCR パッケージ可視性の切り替え手順」節を追加する。`GITHUB_TOKEN` の権限では変更できないため、パッケージ初回作成後に GitHub の Package 設定画面から手動で public に切り替える一度きりの手順を記載する（AC-17, AC-05 関連）。
- [x] `README.md`: 「Docker」節（新規）を追加する。`docker-compose.yml`・`.env`（`dot.env.example` からコピー）・TOML 設定ファイルを用意し、`docker-compose.yml` のバージョンタグを確認・更新したうえで `docker compose pull && docker compose up -d` を実行する手順を記載する（AC-16）。
- [x] `README.md`: 既存の「Usage」節（`README.md:60-69` 付近）に `bsky-cleaner --version`（出力例 `v1.2.3 (a1b2c3d)`）の使用例を追加する（AC-16）。
- [ ] 実タグでの初回リリースを実施する: `git tag v1.0.0 && git push --tags` を実行し、フェーズ2の `release.yml` が正常終了して4タグが GHCR に公開されることを確認する。GitHub の Package 設定画面から可視性を public に切り替える（AC-05, AC-17 の実施）。
- [ ] 上記完了後、`docker-compose.yml` の変更（本フェーズ最初のステップ）を `main` にマージする。

**完了基準**: `docker compose pull && docker compose up -d` が、`bsky-cleaner` のソースコードを持たない別ディレクトリ（`docker-compose.yml`・`.env`・TOML 設定ファイルのみ配置）で成功する（AC-14）。

### フェーズ4: CI ビルド確認（AC-18〜22, NF-002, NF-005）

**対象ファイル**: `.github/workflows/ci.yml`

- [x] `docker-build-check` ジョブを既存の `test`/`lint` ジョブと並行に追加する。既存ジョブの定義・`on:` トリガー条件は変更しない（AC-20, NF-002）。
- [x] ジョブの先頭で `dorny/paths-filter@v4` を用いてパス変更を判定するステップを追加する。フィルタ対象は `Dockerfile`・`go.mod`・`go.sum`・`cmd/**`・`internal/**`・`entrypoint.sh` とする（AC-21）。
- [x] ジョブ本体を `if: steps.<filter-id>.outputs.docker == 'true'` で条件付けする（AC-21）。
- [x] `docker/build-push-action@v7`（タグ参照。`packages: write` を持たないジョブのため SHA 固定は不要）を `push: false` で実行し、`docker build .` 相当のビルドを行う。`--build-arg` は指定しない（AC-18, AC-19）。
- [x] `cache-from: type=gha` / `cache-to: type=gha` を設定する（AC-22）。
- [x] ジョブに `timeout-minutes: 10` を設定する（NF-005）。

**完了基準**: `make fmt && make test && make lint` が緑。`Dockerfile` を意図的に壊した状態の PR でこのジョブが red になり、ドキュメントのみの変更 PR ではジョブがスキップされることを手動確認する。

## 3. 実装順序とマイルストーン

### 3.1 マイルストーン

| マイルストーン | 内容 | 成果物 |
|---|---|---|
| M1 | フェーズ1完了 | `--version`/`-v` フラグと `formatVersion` が実装され、ユニットテストが通過する |
| M2 | フェーズ2完了 | `release.yml` が作成され、`workflow_dispatch` による動作確認が完了する |
| M3 | フェーズ3完了 | 実タグでの初回リリース・GHCR 可視性切り替え・`docker-compose.yml` の `image:` 参照化・ドキュメント整備が完了し、`docker compose pull && docker compose up -d` が動作する |
| M4 | フェーズ4完了 | `ci.yml` に `docker-build-check` ジョブが追加され、red/skip の両方を手動確認済み |

### 3.2 フェーズ間の依存関係

フェーズ1→フェーズ2→フェーズ3→フェーズ4の順に依存する: フェーズ2の `release.yml` はフェーズ1の `Dockerfile`／`--version` を前提にビルド時バージョン埋め込みを検証する。フェーズ3の実タグリリースはフェーズ2の `release.yml` が正常動作することを前提とする。フェーズ4はフェーズ1〜3のいずれにも機能的に依存しないが、`Dockerfile` の変更（フェーズ1）を含めた状態でビルド確認を通すため最後に置く。

## 4. テスト戦略

### 4.1 単体テスト

- `formatVersion`（commit あり／なし、かつ `version` が既定値／非既定値の組み合わせ）、`parseFlags` の `--version`/`-v` 検出（早期走査経路・`fs.Parse` 後のフォールバック経路の両方）（AC-08, AC-09, AC-11a）。既存の `TestParseFlags_*`／`TestRun_*` パターンをそのまま踏襲し、新規テストヘルパは追加しない（`cmd/main_test.go`、フェーズ1で作成）。
- `scripts/check-existing-tag.sh` の fail-closed 判定ロジック（マニフェスト既存・不在確認済み・不在未確認の3ケース）（AC-03a）。`scripts/check_existing_tag_test.go`（フェーズ2で作成）が `os/exec` 経由でスクリプトを直接検証する。

### 4.2 統合テスト

- Go の統合テストは対象外（本タスクは CI/CD ワークフローと配布設定が中心）。GitHub Actions ワークフローの検証は「7. 受け入れ基準の検証」のワークフロー静的検証・手動検証で行う。

### 4.3 後方互換テスト

- `cmd/main_test.go` の既存 `TestParseFlags_*`／`TestRun_*` は `parseFlags` の既存シグネチャ・既存フラグの挙動を変更しないため、そのまま実行して非影響を確認する（[02_architecture.md 7.2](./02_architecture.md#72-既存テストへの影響)）。
- `Dockerfile` の `ARG` 追加はデフォルト値を持つため、`--build-arg` を指定しない既存の `docker build .` 呼び出し（0007 のコンテナ検証手順）が同じ挙動を保つことを、フェーズ1完了時に `docker build .`（`--build-arg` なし）→ `docker run --rm <image> bsky-cleaner --version` で `dev` が出力されることを確認して検証する。

### 4.4 テストヘルパ方針

新規のクロスパッケージヘルパ（`testutil/`）・パッケージ内蔵ヘルパ（`test_helpers.go`）はいずれも不要（1.3 節参照）。`scripts/check_existing_tag_test.go` は Go パッケージ間で共有されるヘルパではなく、`scripts/check-existing-tag.sh` 自体を対象とする直接テストであるため、[test_organization.md](../../dev/developer_guide/test_organization.md) の分類（Classification A/B）の対象外である。

## 5. リスク管理

| リスク | 影響 | 対策 |
|---|---|---|
| `docker manifest inspect` が返す「不在」エラー文字列が実装時の想定と異なる | AC-03a の fail-closed 判定条件が誤り、既存タグ保護が機能しない、または常に fail-closed になり正常リリースができない | フェーズ2実装時に実際の GHCR レスポンス（`docker manifest inspect` の標準エラー出力）を一度取得し、判定文字列をそれに合わせて確定する。`workflow_dispatch` による動作確認（フェーズ2完了基準）で新規タグ・既存タグ双方のケースを実際に確認する |
| `workflow_dispatch` によるドライラン実行が実際に GHCR へイメージを公開してしまい、検証専用のバージョン番号が本番のリリース履歴に永続的に残る | 意図しないバージョン番号のイメージが公開され、AC-03a の上書き保護により同名タグの再利用ができなくなる。ドライラン自体が途中失敗した場合、同じ暫定タグでの再実行が AC-03a の保護に阻まれる可能性がある | 未使用の正式 semver（例 `v0.0.1`）を選び、事前に GHCR 側に同名タグが存在しないことを確認してから実行する。ドライラン検証完了後、フェーズ2の実装ステップに明記した通り GHCR 側でこの検証用タグを手動削除する。ドライラン自体が途中失敗した場合は同じタグを使い回さず、新しい未使用タグで再試行する |
| フェーズ3の `docker-compose.yml` 変更を、実タグリリース前に誤って `main` にマージしてしまう | ソースを持たない利用者が `docker compose pull` で「イメージが見つからない」エラーに遭遇する（[02_architecture.md 3.2.5](./02_architecture.md#325-docker-composeyml-の-image-参照化ac-12〜14)） | フェーズ3の実装ステップに明記した順序（`docker-compose.yml` の変更は作成するがマージはステップ8の後）を PR 分割時にも維持する |
| GHCR パッケージ可視性の手動切り替えを忘れる | イメージが private のまま残り、匿名 `docker compose pull` が失敗する | フェーズ3完了基準に「ソースを持たない別ディレクトリでの `docker compose pull` 成功」を含め、可視性切り替え忘れがあれば検出できるようにする |

## 6. 実装チェックリスト

- [x] フェーズ1完了（`--version`/`-v`、`formatVersion`、`Dockerfile` の `ARG`/`-ldflags`）
- [x] フェーズ2完了（`release.yml` 作成・`workflow_dispatch` 動作確認。[11_manual_verification_log.md](11_manual_verification_log.md) 参照）
- [ ] フェーズ3完了（実タグ初回リリース・可視性切り替え・`docker-compose.yml`・ドキュメント）
- [ ] フェーズ4完了（`ci.yml` への `docker-build-check` ジョブ追加・red/skip 確認）
- [ ] `make fmt` / `make test` / `make lint` がすべて通過（NF-001）
- [ ] `make deadcode` で未使用コードがないことを確認

## 7. 受け入れ基準の検証（Acceptance Criteria Verification）

**AC-01: `v` タグ push または `workflow_dispatch` でワークフローが起動する**
- Verification (static): `rg -n "tags:" -A2 .github/workflows/release.yml` で `'v*'` を含むこと、`rg -n "workflow_dispatch:" -A3 .github/workflows/release.yml` で `inputs:`/`tag:` を含むことを確認する。
- Verification (manual): `workflow_dispatch` からタグ名を指定して実行し、起動することを確認する（フェーズ2完了基準）。

**AC-02: semver 形式に一致しないタグは公開せず非0終了する**
- Verification (static): `rg -n "Validate tag format" .github/workflows/release.yml` でステップの存在を確認したうえで、そのステップの実装内容に semver 判定用の正規表現（`v[0-9]` を含むパターン）が使われていることを目視で確認する（正規表現リテラルの厳密な引用符・エスケープは YAML 記法に依存し実装時にしか確定しないため、`rg` は「ステップが存在し semver 判定を含む」ことの確認に留める）。
- Verification (manual): `workflow_dispatch` に `v1` や `abc` のような不正な形式のタグ名を指定して実行し、非0終了・イメージ非公開を確認する（[02_architecture.md 7.1](./02_architecture.md#71-テスト対象) 記載の手動検証）。

**AC-03: `vX.Y.Z`・`vX.Y`・`vX`・`latest` の4タグを GHCR に push する**
- Verification (static): `rg -n "Tag and push" .github/workflows/release.yml` で4ステップの存在を確認する。
- Verification (manual): フェーズ3ステップ8の実タグ push 後、GHCR の Package ページで4タグすべてが存在することを確認する。

**AC-03a: `vX.Y.Z` タグの上書き保護（既存時 fail-closed）**
- Test location: `scripts/check_existing_tag_test.go::TestCheckExistingTag_ManifestFound_ExitsFailClosed` / `TestCheckExistingTag_ManifestNotFound_ExitsSuccess` / `TestCheckExistingTag_InconclusiveFailure_ExitsFailClosed`（fail-closed 判定ロジック本体の直接テスト。フェーズ2の `scripts/check-existing-tag.sh` 抽出タスク参照）
- Verification (static): `rg -n "check-existing-tag.sh" .github/workflows/release.yml` で "Check existing vX.Y.Z tag" ステップがスクリプトを呼び出していることを確認する。
- Verification (manual): 既存の `vX.Y.Z` タグを指定して `workflow_dispatch` を再実行し、非0終了・上書きされないことを確認する（[02_architecture.md 7.1](./02_architecture.md#71-テスト対象)）。

**AC-03b: `vX.Y.Z` を最後に push する順序**
- Verification (static): `rg -n "Tag and push" .github/workflows/release.yml` の一致行が、ファイル中で "Tag and push latest" → "Tag and push major version tag" → "Tag and push minor version tag" → "Tag and push patch version tag (vX.Y.Z)" の順に出現することを行番号の昇順で確認する。
- Verification (manual): フェーズ2のドライラン実行中に浮動タグ push を意図的に失敗させ、`vX.Y.Z` が push されないこと、および同じタグ名での再実行が手動後始末なしに成功することを確認する（フェーズ2実装ステップに明記した検証。NF-004 とも対応）。

**AC-04: `linux/amd64` のみを対象とする**
- Verification (static): `rg -n "platforms:\s*linux/amd64" .github/workflows/release.yml` で、他プラットフォームが含まれないことを確認する。

**AC-05: GHCR パッケージの可視性を public に手動切り替え**
- Verification (static): `rg -n "可視性" docs/design/docker_deployment.md` で手順の記載を確認する（AC-17 と同一コマンド）。
- Verification (manual): フェーズ3ステップ8で実際に GitHub Package 設定画面から public に切り替え、匿名 `docker pull ghcr.io/isseis/bsky-cleaner:latest` が成功することを確認する。

**AC-06: `GITHUB_TOKEN` 以外の追加シークレット不要**
- Verification (static): `rg -n "permissions:" -A3 .github/workflows/release.yml` で `contents: read`/`packages: write` のみであることを確認する。`rg -n "secrets\." .github/workflows/release.yml` の一致がすべて `secrets.GITHUB_TOKEN` であることを確認する（他の `secrets.*` 参照がないこと）。`rg -n "docker/login-action@af1e73f918a031802d376d3c8bbc3fe56130a9b0|docker/build-push-action@53b7df96c91f9c12dcc8a07bcb9ccacbed38856a" .github/workflows/release.yml` で両 action がコミット SHA 固定されていることを確認する。

**AC-07: `ci.yml` とは独立したワークフローファイル**
- Verification (static): `test -f .github/workflows/release.yml && test -f .github/workflows/ci.yml` の両方が真であること。`git diff main -- .github/workflows/ci.yml`（フェーズ2完了時点）が空であること（フェーズ2では `ci.yml` を一切変更しないため）。

**AC-08: `--version` が `vX.Y.Z (短縮コミットSHA)` 形式で標準出力に1行、終了コード0**
- Test location: `cmd/main_test.go::TestFormatVersion_WithCommit_ReturnsVersionAndCommit`（フォーマット）、`cmd/main_test.go::TestParseFlags_VersionLongFlag_ReturnsErrVersionRequested` / `TestParseFlags_VersionShortFlag_ReturnsErrVersionRequested`（早期走査経路での検出）、`cmd/main_test.go::TestParseFlags_VersionEqualsTrueForm_ReturnsErrVersionRequested`（`fs.Parse` 後のフォールバック経路での検出）
- Verification (static): `rg -n -A2 "errors.Is\(err, errVersionRequested\)" cmd/main.go` で、一致行から2行以内に `os.Exit(exitOK)` が含まれることを確認する（`run(` 呼び出しに到達しないことの構造的根拠）。同じ `rg` 結果に `fmt.Fprintln(os.Stdout, formatVersion())` が含まれ、bare な `fmt.Println` でないことも確認する（`make lint` 通過の前提）。
- Implementation: `cmd/main.go`（`formatVersion`, `parseFlags` の早期走査とフォールバック分岐, `main()` の分岐）

**AC-09: バージョン未埋め込みビルドは `dev` を出力**
- Test location: `cmd/main_test.go::TestFormatVersion_EmptyCommit_ReturnsVersionOnly`（既定値どうしのスモークテスト）、`cmd/main_test.go::TestFormatVersion_NonDevVersionEmptyCommit_ReturnsVersionOnly`（`version` が非既定値でも commit 空文字時に括弧が付与されないことの確認）
- Verification (manual): フェーズ1完了基準の `docker build .`（`--build-arg` なし）→ `docker run --rm <image> bsky-cleaner --version` で `dev` が出力されることを確認する。

**AC-10: 公開イメージにタグ値と短縮コミットSHAが埋め込まれる**
- Verification (static): `rg -n 'ARG VERSION|ARG COMMIT|-ldflags' Dockerfile` で `ARG`/`-ldflags` の存在を確認する。`rg -n "build-args:" -A2 .github/workflows/release.yml` で `VERSION=`/`COMMIT=` が渡されていることを確認する。
- Verification (manual): フェーズ3ステップ8の実タグ push 後、`docker run --rm ghcr.io/isseis/bsky-cleaner:v1.0.0 bsky-cleaner --version` が `v1.0.0 (<短縮SHA>)` を出力することを確認する。

**AC-11: `--version` はネットワーク・設定ファイル・認証情報にアクセスしない**
- Verification (static): 同上 AC-08 の `rg` コマンド（`errVersionRequested` 判定が `run(configPath` 呼び出しより前で完結することの確認）。加えて、`rg -n "func parseFlags" -A15 cmd/main.go` の早期走査ループ内に `config\.|atproto\.` への参照がないことを確認する。この静的検証は、既存の `-h`/`--help`（同じく `main()` レベルの `os.Exit` 分岐は本コードベースでユニットテストされていない）と同じ精度の検証手段であり、本タスクで新たに精度を下げるものではない。
- Verification (manual): フェーズ1完了基準の `docker run --rm <image> bsky-cleaner --version` を `--network none`（例: `docker run --rm --network none <image> bsky-cleaner --version`）で実行し、ネットワーク到達性がない状態でも成功することを確認する。

**AC-11a: `--config` 未指定でも `--version` はエラーにならない**
- Test location: `cmd/main_test.go::TestParseFlags_VersionFlag_DoesNotRequireConfig`

**AC-12: `docker-compose.yml` が `build:` を持たず `image:` 参照（`latest` 以外）**
- Verification (static): `rg -n "^\s*build:\s*\.\s*$" docker-compose.yml` が0件、`rg -n "image:\s*ghcr\.io/isseis/bsky-cleaner:v[0-9]" docker-compose.yml` が1件であることを確認する。

**AC-13: バージョン更新・digest参照・latestの位置づけの3点がコメントに記載**
- Verification (static): `rg -n "bump this tag" docker-compose.yml`、`rg -n "@sha256" docker-compose.yml`、`rg -n "latest.*newest|newest.*latest" docker-compose.yml` の3コマンドがそれぞれ1件以上一致することを確認する（実装時の実際の文言に応じてキーワードを調整する）。

**AC-14: ソースなし環境で `docker compose pull && docker compose up -d` が起動する**
- Verification (manual): `bsky-cleaner` のソースを持たない一時ディレクトリに `docker-compose.yml`・`.env`（`dot.env.example` から作成）・`config/config.toml` のみを配置し、`docker compose pull && docker compose up -d` を実行してコンテナが起動状態になることを `docker compose ps` で確認する（フェーズ3完了基準）。

**AC-15: 開発者向けリリース公開手順が記載**
- Verification (static): `rg -n "git tag v" docs/design/docker_deployment.md` と `rg -n "workflow_dispatch" docs/design/docker_deployment.md` がそれぞれ1件以上一致することを確認する。

**AC-16: 利用者向け取得・起動手順が記載**
- Verification (static): `rg -n "docker compose pull && docker compose up -d" README.md` と `rg -n -- "--version" README.md` がそれぞれ1件以上一致することを確認する。

**AC-17: GHCR 可視性切り替え手順が記載**
- Verification (static): `rg -n "可視性" docs/design/docker_deployment.md` が1件以上一致することを確認する（AC-05 と同一コマンド）。

**AC-18: `ci.yml` に `docker build .`（push なし）ジョブが追加**
- Verification (static): `rg -n "docker-build-check" .github/workflows/ci.yml` と `rg -n "push:\s*false" .github/workflows/ci.yml` がそれぞれ1件以上一致することを確認する。

**AC-19: Dockerfile 破損時にジョブが失敗し CI が red になる**
- Verification (manual): `Dockerfile` の `COPY` 対象を存在しないファイル名に一時的に書き換えた PR を作成し、`docker-build-check` ジョブが失敗することを確認したうえで変更を取り消す（[02_architecture.md 7.1](./02_architecture.md#71-テスト対象)）。

**AC-20: 既存 `test`/`lint` ジョブのトリガー・挙動が非影響**
- Verification (static): `git diff <フェーズ4着手前のコミット> -- .github/workflows/ci.yml` で `test:`/`lint:` ジョブ定義ブロックに差分がなく、`docker-build-check` ジョブの追加のみが差分として現れることを確認する。

**AC-21: Docker関連パス変更時のみ実行、無関係な変更ではスキップ**
- Verification (static): `rg -n "dorny/paths-filter" .github/workflows/ci.yml` と、フィルタ定義に `Dockerfile`・`go.mod`・`go.sum`・`cmd/**`・`internal/**`・`entrypoint.sh` が含まれることを確認する。
- Verification (manual): `docs/` 配下のみを変更する PR を作成し、`docker-build-check` ジョブがスキップされることを確認する（[02_architecture.md 7.1](./02_architecture.md#71-テスト対象)）。

**AC-22: レイヤーキャッシュ（GitHub Actions cache）を利用**
- Verification (static): `rg -n "cache-from:\s*type=gha" .github/workflows/ci.yml` と `rg -n "cache-to:\s*type=gha" .github/workflows/ci.yml` がそれぞれ1件以上一致することを確認する。

**NF-001: `make fmt`・`make test`・`make lint` が成功**
- Verification (build): 各フェーズ完了時に `make fmt && make test && make lint` を実行し、終了コード0（`make fmt` は差分なし）であることを確認する（`rg` によるテキスト静的検証ではなく、実際のビルド・テスト・lint 実行であることに留意）。

**NF-002: `docker-build-check` 追加が既存ジョブのトリガー・挙動を変更しない**
- Verification (static): AC-20 と同一コマンド。

**NF-003: GHCR 公開に `GITHUB_TOKEN` 以外の追加シークレット不要**
- Verification (static): AC-06 と同一コマンド。

**NF-004: `vX.Y.Z` push 前に失敗した実行はタグ削除なしで再実行可能**
- Verification (static): AC-03b と同一コマンド（push 順序の構造的保証）。
- Verification (manual): AC-03b と同一の手動検証（フェーズ2のドライラン中の浮動タグ push 失敗・再実行）。

**NF-005: `docker-build-check` はキャッシュヒット時の所要時間への影響が軽微**
- Verification (static): `rg -n "timeout-minutes:\s*10" .github/workflows/ci.yml` を `docker-build-check` ジョブ内で確認する（worst-case 上限の保証）。
- Verification (manual): フェーズ4完了時に、依存関係・`Dockerfile` に変更がないキャッシュヒット状態の PR で `docker-build-check` ジョブの実際の所要時間を GitHub Actions の実行ログで確認し、[02_architecture.md 3.2.7](./02_architecture.md#327-ciyml-への-docker-build-check-ジョブ追加ac-18〜22) の目安（数十秒〜1分程度）と大きく乖離していないことを確認する。

## 8. 成功基準

- **機能的完全性**: AC-01〜AC-22（AC-03a・AC-03b・AC-11a を含む）がすべて緑（7章参照）。
- **品質指標**: `make fmt` / `make test` / `make lint` / `make deadcode` がすべて成功。既存の `TestParseFlags_*`／`TestRun_*` が非影響のまま通過。
- **セキュリティ検証**: [02_architecture.md 5.2](./02_architecture.md#52-脅威と対策) の脅威対策（write 権限 action のコミット SHA 固定、fail-closed な既存タグ確認、`concurrency` によるレース防止、`docker-build-check` への GHCR 資格情報非付与）がワークフローファイル上で確認できる。
- **ドキュメント整備**: `docs/design/docker_deployment.md`・`README.md` の追記が完了し、GHCR 可視性切り替えが実施済み。

## 9. 次のステップ

- 実装完了後、[docs/dev/developer_guide/package_reference.md](../../dev/developer_guide/package_reference.md) は `internal/` 配下に変更がないため更新不要。`cmd/main.go` の説明に `--version`/`-v` の記載を追加するかどうかは、次回 `cmd/` 関連タスクの際に判断する（本タスクでは package_reference.md への変更は必須要件に含まれない）。
- フォローアップ候補（本タスクのスコープ外、[02_architecture.md 9章](./02_architecture.md#9-将来の拡張性) 参照）: マルチアーキテクチャ対応、イメージ署名/SBOM、複数レジストリ公開、リリース失敗の即時アラート。

## 10. クロス検索チェックリスト

- [ ] `docker-compose.yml` の `build: .` 削除に伴い、`README.md`・`docs/overview.md`・`docs/design/docker_deployment.md` に「ソースからビルドする」ことを前提にした記述が残っていないか確認する: `rg -n "docker compose build|docker-compose build" README.md docs/overview.md docs/design/docker_deployment.md`（実装時点で0件であることを確認済み。フェーズ3完了時に再確認する）。
- [ ] `--version`/`-v`・`showVersion`・`errVersionRequested`・`formatVersion` は本タスクで新規追加する識別子であり、既存コードとの衝突がないことを確認済み（`rg -n "\bversion\b|\bcommit\b" cmd/main.go` が本タスクの変更前は0件だったことを1.3節の事前調査で確認済み）。追加実装後にこれらの識別子が意図しない箇所で再定義されていないかを `rg -n "^var version|^var commit|^func formatVersion|errVersionRequested" cmd/*.go` で確認する。
- [ ] `scripts/` ディレクトリは本タスクで新規追加するものであり、既存コードとの衝突がないことを確認済み（1.3節の事前調査で `scripts/` が未存在であることを確認済み）。追加実装後に `go vet ./...`／`make lint` が `scripts/check_existing_tag_test.go` を正しくパッケージとして認識し、`make test` の対象に含まれていることを `go test -tags test ./... -list '.*' 2>&1 | rg -n "check.*existing.*tag" ` 等で確認する。
