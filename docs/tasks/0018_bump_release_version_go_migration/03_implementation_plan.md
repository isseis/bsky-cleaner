# bump-release-version.sh の Go 移行 — 実装計画書

## Document Status

| Item | Value |
|---|---|
| Status | `approved` |
| Created | 2026-07-13 |
| Review date | 2026-07-13 |
| Reviewer | isseis |
| Comments | - |

関連ドキュメント: [要件定義書](01_requirements.md)、[アーキテクチャ設計書](02_architecture.md)

## 1. 実装概要

### 1.1 目的

現行 Bash 実装 `scripts/bump-release-version.sh`（`docker-compose.yml` / `README.md` / `README.ja.md` の
バージョン文字列一括更新）を、[アーキテクチャ設計書](02_architecture.md)に沿って `scripts/bump_release_version/` 配下の
Go 実装（`package main`）へ移行する。CLI 契約と安全特性（引数検証・symlink 安全性・パーミッション保持・
フェイルクローズ）を過不足なく引き継ぎ、検証を全書き込みに先行させる2段階化（AC-08）のみ挙動を改善する。

### 1.2 実装原則

- 設計判断は[アーキテクチャ設計書](02_architecture.md)に従い、本書では再掲せず参照する。
- Go ソースのコメント・識別子・文字列リテラルは英語（本書の説明文は日本語）。
- 新規の外部依存は追加しない。標準ライブラリ（`regexp`/`os`/`errors`/`io`/`fmt`）と既存の `testify` のみを使う（NF-004）。
- 各フェーズ完了時に `make fmt`・`make test`・`make lint` を通す（NF-001）。

### 1.3 既存コード調査結果

- **`scripts/` は既に `package main`**: 実装当初、`scripts/` 直下には `bump_release_version_test.go`・
  `check_existing_tag_test.go`（いずれも `package main`）と2つのシェルスクリプトのみが存在し、
  **非テストの `.go` ファイルはまだ存在しなかった**（`main()` が未定義）。当初案では新規の非テストコードを
  この `package main` に合流させる予定だったが、PR-1 のレビューを経て `go run ./scripts/bump_release_version`
  のように起動コマンドにツール名が現れる構成へ変更したため、`scripts/bump_release_version/` サブディレクトリの
  独立した `package main`（`scripts/bump_release_version/main.go`）として実装した。`scripts/check-existing-tag.sh`
  とその既存テスト（`scripts/check_existing_tag_test.go`）は本タスクの対象外であり、`scripts/` 直下に残る
  （引き続き別パッケージとして共存する）。
  - **シンボル衝突の確認**: 独立パッケージとして分離したため、`check_existing_tag_test.go` の
    `scriptPath`/`runScript` との衝突は当初から起こり得ない。
- **ビルド・テスト・Lint への影響**:
  - `make build` は `go build -o build/bsky-cleaner ./cmd` のみ。`scripts/bump_release_version/` 配下に
    `main()` を追加してもリリースバイナリには影響しない。ツールの起動は
    `go run ./scripts/bump_release_version vX.Y.Z` または `make bump-version ARGS=vX.Y.Z`。
  - `make test`（`go test -tags test ./...`）は `scripts/` と `scripts/bump_release_version/` の両方を含む。
    テストは同一パッケージ内から `run`/`update`/`validateVersion` を直接呼び出す。
  - `make lint`（`golangci-lint run`）の `gosec` は **`_test.go` のみ除外**（`.golangci.yml` の exclusions）。
    非テストコード `scripts/bump_release_version/main.go` には `gosec` が適用される。ファイル I/O（`os.ReadFile`/`os.WriteFile`/
    `os.CreateTemp`/`os.Rename`/`os.Chmod`）に対しては、既存の `internal/config/config.go:59`
    （`os.ReadFile(path) //nolint:gosec // path is a trusted, caller-resolved value, not external input`）に
    倣い、**`//nolint:gosec` に理由コメントを添えて**抑制する（対象パスは固定の作業ディレクトリ + 既知の
    ファイル名から構成され、外部入力ではない）。
  - `unused` linter（`.golangci.yml` で有効）は未使用の非公開シンボル・定数・フィールドを検出する。
    `errorKind` の各定数（`errorKindFileNotFound`/`errorKindPatternNotFound`/`errorKindIO`）と
    `updateError.Err` は、実装内で必ず代入・参照されるようにする（未使用なら Lint 失敗）。
  - `make deadcode` は必須チェック（`make test && make lint`。以下 green gate）には含まれないが、参考として実行する。新規
    `package main` の関数は自身の `main()` から到達可能。
- **作業ディレクトリの解決**: 現行 Bash はスクリプト位置基準で repo root を解決するが、Go 版は
  `run` が**カレントディレクトリ基準**で3つの対象ファイルパスを構成する（`go run ./scripts/bump_release_version vX.Y.Z` を
  repo root から実行する前提。ドキュメントにも明記）。テストは `t.Chdir(seededTempDir)` で対象ディレクトリを
  切り替える。ファイル単位の挙動テストは `update(version, []target)` に一時ディレクトリ上のパスを直接
  渡して検証する。
- **移行対象の既存テスト**: `scripts/bump_release_version/main_test.go`（実装当初は `scripts/bump_release_version_test.go`）の6テスト
  （`TestBumpReleaseVersion_UpdatesAllFiles` / `_RejectsInvalidSemver` / `_RejectsNewlineInjectedVersion` /
  `_RequiresExactlyOneArgument` / `_DoesNotTouchUnrelatedOccurrencesOfTheOldVersion` / `_FailsIfPatternMissing`）
  は、`exec.Command("bash", ...)` 経由から Go 実装のインプロセス呼び出しへ書き換える。あわせて
  AC-02a（末尾コメント保持）・AC-02b（コメントアウト digest 行保持）・AC-03（symlink 非追跡）・
  AC-04（mode 保持）の新規テストを追加する。
- **ドキュメント参照**: `scripts/bump-release-version.sh` を参照する箇所は
  `docs/design/docker_deployment.md`（2箇所）・`docs/design/docker_deployment.ja.md`（2箇所）のみ。
  CI（`.github/workflows/release.yml`）は `check-existing-tag.sh` のみを参照し、bump スクリプトは
  参照しない（本タスクで CI 変更は不要）。`docs/dev/developer_guide/package_reference.md` に `scripts/` の
  記述はまだ無いため、新規に追記する。

## 2. 実装ステップ

各フェーズはアーキテクチャ設計書 §8「実装優先順位」に対応する。

### フェーズ 1: コアロジック

**対象ファイル**: `scripts/bump_release_version/main.go`（新規）

- [x] **ステップ 1-1**: `validateVersion(arg string) error` を実装（semver 全体アンカー検証、AC-01。設計 §3.2.1）。
  不一致時はセンチネル `errInvalidVersion` を返す。
- [x] **ステップ 1-2**: エラー型を定義（設計 §3.1）: センチネル `errInvalidVersion`、`errorKind`（`errorKindFileNotFound`/
  `errorKindPatternNotFound`/`errorKindIO`）、`updateError{Path, Kind, Err}`。各 `errorKind` 定数と `Err` フィールドが
  実装内で使われるようにする（`unused` 対策）。
- [x] **ステップ 1-3**: `target{Path, Pattern}` 型と、3対象ファイルの記述子を構成する内部関数を実装（設計 §3.2.2）。
  一致・置換パターンは設計 §3.2.2 の表に従い、一致確認と置換で同一の `*regexp.Regexp` を用いる（AC-02）。
  置換は `ReplaceAll` の**波括弧付きグループ参照 `${1}`/`${2}`** を用い、接頭辞・行末残余を保持
  （設計 §3.2.2「実装上の注意」。素の `$1` の直後にバージョン（例: `v1`）が続くと `${1v1}` と解釈されて
  壊れるため必須）。`target` に `ReplacementTemplate`（`fmt.Sprintf` 用の `%s` テンプレート）フィールドを
  追加し、ファイルごとに異なるグループ構成（`${1}%s` / `${1}%s${2}`）に対応させた（設計の高レベル
  スケッチにない追加フィールドだが、設計 §3.1 が「実装コードは記述しない」と明示しており矛盾しない）。
- [x] **ステップ 1-4**: アトミック書き込み + mode 保持の内部関数を実装（AC-03/AC-04。設計 §3.2.3）: 元 mode を
  `os.Stat(...).Mode().Perm()`（型ビットを除いたパーミッションビット）で退避 → 同一ディレクトリに
  ランダム名一時ファイルを作成 → 置換後内容を書き込み → `os.Rename` → `os.Chmod` で退避した元 mode を
  復元。ファイル I/O 各所に `//nolint:gosec // <理由>` を付す（§1.3 参照）。

**成功基準**: `go build ./scripts/bump_release_version` が成功し、`make lint` が `main.go` について警告を出さない。

### フェーズ 2: 2段階制御とエントリポイント

**対象ファイル**: `scripts/bump_release_version/main.go`（続き）

- [x] **ステップ 2-1**: `update(version string, targets []target) error` を実装（AC-02/AC-07/AC-08。設計 §3.2.4）。
  フェーズ1（全件の存在・パターン一致確認 + 置換後内容と退避 mode をインメモリ構築、1件でも失敗なら
  どのファイルにも書き込まず終了）→ フェーズ2（全件通過時のみ書き込み、I/O エラーで後続を止める）。
  複数検証失敗は `errors.Join` でまとめてよい（設計 §3.2.4）。
- [x] **ステップ 2-2**: `run(args []string, stdout, stderr io.Writer) int` を実装（AC-05/AC-06/AC-09。設計 §3.1・§3.2.5）:
  引数個数チェック（ちょうど1個、他は Usage を stderr へ出し非ゼロ）→ `validateVersion` →
  カレントディレクトリ基準で `[]target` を構成 → `update` 呼び出し → 成功時に更新通知
  （`Updated <path>`）と後続リリース手順を stdout へ出力。エラーメッセージは設計 §4 の文言に揃える。
- [x] **ステップ 2-3**: `main()` を `os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))` の薄いラッパとして実装（設計 §3.1）。

**成功基準**: `go run ./scripts/bump_release_version` が引数不足で Usage を出して非ゼロ終了する。`make test`（この時点では既存
テストは書き換え前だが、コンパイルが通ること）。

### フェーズ 3: テスト移行と後始末

**対象ファイル**: `scripts/bump_release_version/main_test.go`（書き換え）、`scripts/bump-release-version.sh`（削除）、
`docs/design/docker_deployment.md`・`docs/design/docker_deployment.ja.md`（変更）、
`docs/dev/developer_guide/package_reference.md`（変更）

- [x] **ステップ 3-1**: `scripts/bump_release_version_test.go`（当初案の配置）を書き換え: `bumpScriptPath`・`runBumpScript` を削除し、
  `setupRepo` を「一時ディレクトリに3ファイルをシードする」用途へ縮小（`.sh` のコピーは削除）。
  `check_existing_tag_test.go` のシンボルには触れない。詳細なテスト内容は §4 を参照。
  （実装では `setupRepo` を `seedRepo` に、`runBumpScript` を `runInDir`（`run` を直接呼び出す）にリネーム。）
- [x] **ステップ 3-2**: AC ごとのテストを実装/移行（§4 のテスト一覧に対応）。

### PR-1 作成ポイント: Go implementation and in-process tests

**対象ステップ**: 1-1 / 1-2 / 1-3 / 1-4 / 2-1 / 2-2 / 2-3 / 3-1 / 3-2

**推奨タイトル**: `feat(0018): implement bump-release-version in Go with in-process tests`

**レビュー観点**: 2段階検証→書き込みのフェイルクローズ（AC-07/AC-08）と `${1}`/`${2}` グループ参照による置換の正しさ / 全 AC を網羅するインプロセステスト（`run`/`update` 直接呼び出し・`t.Chdir`）/ `//nolint:gosec` 抑制の妥当性（理由コメント・対象パスの範囲）

> 本 PR で実装コードと同一パッケージのインプロセステストを同時に投入する。`run` の `io.Writer` 引数は、
> 呼び出し元が `main`（`os.Stdout`/`os.Stderr` 固定）だけだと `unparam` が「常に同一値」と警告するため、
> `bytes.Buffer` を注入するテストを同じ PR に入れて2つ目の呼び出し元を与える。厳密に同時投入が必要なのは
> 「`run` + それを呼ぶ非 `main` のテスト」だけで、実装全体を1 PR にまとめること自体が必須なわけではない
> （`unused` 警告は最終状態では `main` から全シンボルへ到達するため発生しない）。本タスクは対象が単一の
> 約200行ファイルで、さらに分割する費用対効果が低いと判断し、実装一式とテストを1 PR にまとめる。旧 `.sh` は
> ステップ 3-1 のテスト書き換え後は参照されなくなるため、削除は PR-2 に委ねる（本 PR 時点では孤立して残るが
> 害はない）。

- [x] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [x] PR を作成した（[#158](https://github.com/isseis/bsky-cleaner/pull/158)）
- [x] PR がマージされた
- [x] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

### フェーズ 3 後半: 旧スクリプト削除とドキュメント整備

- [x] **ステップ 3-3**: `scripts/bump-release-version.sh` を削除。
- [x] **ステップ 3-4**: `docs/design/docker_deployment.md` の2箇所を更新（§4.3 の置換内容）。
- [x] **ステップ 3-5**: `docs/design/docker_deployment.ja.md` の2箇所を更新（§4.3 の置換内容）。
- [x] **ステップ 3-6**: `docs/dev/developer_guide/package_reference.md` に `scripts/` の項を追記（bump ツールの
  配置・責務・起動方法 `go run ./scripts/bump_release_version vX.Y.Z`）。
- [x] **ステップ 3-7**: `make fmt && make test && make lint` を通す。

**成功基準**: 全 AC テストが緑。`.sh` 削除後に bump スクリプトへの参照が残っていない（§8 クロスサーチ）。

### PR-2 作成ポイント: remove legacy script and update docs

**対象ステップ**: 3-3 / 3-4 / 3-5 / 3-6 / 3-7

**推奨タイトル**: `chore(0018): remove bump-release-version.sh and update docs`

**レビュー観点**: `.sh` 削除後に bump スクリプトへの残存参照がゼロ（`rg` 確認、§8）/ `docker_deployment.md`・`.ja.md` の起動方法が `go run ./scripts/bump_release_version` へ一貫更新されているか / `package_reference.md` の `scripts/` 追記内容の正確さ

> 依存関係: 本 PR は PR-1（インプロセステスト化）のマージ後に行う。PR-1 でテストが `.sh` を参照しなくなって
> いるため、本 PR での `.sh` 削除でグリーンゲートは壊れない（`check-existing-tag.sh` は別スクリプトで無関係）。

- [ ] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [ ] PR を作成した
- [ ] PR がマージされた
- [ ] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

## 3. 実装順序とマイルストーン

### 3.1 マイルストーン

| マイルストーン | 内容 | 完了条件 |
|---|---|---|
| M1 | フェーズ1（コアロジック） | `go build ./scripts/bump_release_version` 成功・Lint 警告なし |
| M2 | フェーズ2（2段階制御・エントリポイント） | `go run ./scripts/bump_release_version` が Usage/終了コードを返す |
| M3 | フェーズ3（テスト移行・後始末） | 全 AC テスト緑・`make test && make lint` 成功・参照残存なし |

フェーズ間に順序依存がある（M2 は M1 の型・関数に依存、M3 のテストは M1/M2 の実装に依存）。順序は
アーキテクチャ設計書 §8 と一致する。マイルストーン（M1〜M3）は PR-1 内での実装の積み上げ順を示すもので、
PR 境界（§3.2）とは別軸である。

### 3.2 PR 構成

| PR | 対象ステップ | 主な変更内容 |
|---|---|---|
| PR-1 | 1-1 / 1-2 / 1-3 / 1-4 / 2-1 / 2-2 / 2-3 / 3-1 / 3-2 | `scripts/bump_release_version/main.go`（`package main`）の新規実装（引数検証・エラー型・記述子/パターン・アトミック書き込み+mode 保持・2段階アップデータ・`run`/`main`）と、`main_test.go` を Go 実装のインプロセステストへ書き換え（全 AC を網羅）。実装コードとテストは同時投入（`run` の `io.Writer` 引数を `unparam` に通すため。PR-1 マーカー参照） |
| PR-2 | 3-3 / 3-4 / 3-5 / 3-6 / 3-7 | 旧 `scripts/bump-release-version.sh` の削除、`docs/design/docker_deployment.md`・`.ja.md` の起動方法更新、`docs/dev/developer_guide/package_reference.md` への `scripts/` 追記、最終グリーンゲート確認 |

## 4. テスト戦略

### 4.1 テスト方針

- テストは `scripts/bump_release_version/main_test.go`（`package main`）内に置き、Go 実装をインプロセスで直接
  呼び出す（設計 §7.1）。サブプロセス（`exec.Command`）は使わない。
- 一時ディレクトリ（`t.TempDir`）に実リポジトリと同じ構造の `docker-compose.yml`・`README.md`・
  `README.ja.md` をシードする。`docker-compose.yml` のシードには AC-02b 検証のためコメントアウト
  digest 行（`    # image: ghcr.io/isseis/bsky-cleaner@sha256:<digest>`）と実 `image:` 行の両方を含める。
- CLI 契約（引数個数・Usage・終了コード・案内出力）は `run(args, &stdout, &stderr)` を呼び、返り値の
  終了コードと `bytes.Buffer` の出力を検証する。成功経路は `t.Chdir(seededTempDir)` でカレントディレクトリを
  一時ディレクトリへ切り替えてから `run` を呼ぶ。
- ファイル単位の挙動（一致・置換・mode・部分更新防止）は `update(version, []target)` に一時ディレクトリ上の
  パスを直接渡して検証する。

### 4.2 テスト一覧（`scripts/bump_release_version/main_test.go`）

| テスト関数 | 検証内容 | 対応 AC |
|---|---|---|
| `TestBumpReleaseVersion_UpdatesAllFiles` | 3ファイルすべてが新バージョンに更新される | F-001 |
| `TestBumpReleaseVersion_RejectsInvalidSemver` | 不正な semver を拒否し非ゼロ終了、全ファイル非改変 | AC-01 |
| `TestBumpReleaseVersion_RejectsNewlineInjectedVersion` | 複数行入力（`v9.9.9\n<payload>`）を拒否、全ファイル非改変 | AC-01 |
| `TestBumpReleaseVersion_FailsIfPatternMissing` | パターン不一致・ファイル不在を検出しエラー終了 | AC-02 |
| `TestBumpReleaseVersion_PreservesTrailingComment` | `VERSION=vX.Y.Z  # ...` の末尾コメントが保持される | AC-02a |
| `TestBumpReleaseVersion_DoesNotTouchUnrelatedOccurrencesOfTheOldVersion` | 旧バージョンに言及する無関係なコメント行が非改変 | AC-02 |
| `TestBumpReleaseVersion_PreservesCommentedDigestLine` | コメントアウト digest 行（`@sha256:`）が非改変、実 `image:` 行のみ更新 | AC-02b |
| `TestBumpReleaseVersion_DoesNotFollowPlantedSymlink` | 予測可能な名前（`<file>.tmp`）で事前配置した symlink を追跡せず、リンク先の外部ファイルが非改変であること。また実行中に予測可能な名前の中間ファイルが生成されないこと（一時ファイル名がランダムであること） | AC-03 |
| `TestBumpReleaseVersion_PreservesFileMode` | 0644 でシードしたファイルが更新後も 0644 を保つ | AC-04, NF-003 |
| `TestBumpReleaseVersion_RequiresExactlyOneArgument` | 引数0個・2個以上で Usage を stderr 出力し非ゼロ終了 | AC-05 |
| `TestBumpReleaseVersion_PerformsNoUnexpectedSideEffects` | 成功実行後、シードした3ファイル以外に新規ファイル・ディレクトリが生成されない | AC-06 |
| `TestBumpReleaseVersion_ValidatesAllBeforeWriting` | フェーズ1でいずれか1ファイルの検証（存在・パターン一致）に失敗した時、どのファイルも書き込まれない（部分更新防止） | AC-08 |
| `TestBumpReleaseVersion_FailClosedOnWriteError` | 全ファイルがフェーズ1を通過した後、フェーズ2の書き込みでいずれか1ファイルが失敗（例: 対象をディレクトリに置換、または書き込み不可にする）した時、非ゼロ終了し、まだ書き込んでいない対象は非改変のまま処理が止まる | AC-07 |
| `TestBumpReleaseVersion_PrintsGuidanceOnSuccess` | 成功時に更新通知と後続リリース手順が stdout に出力される | AC-06, AC-09 |

- 境界・異常系: 引数0/1/2個（AC-05）、semver 部分一致・複数行（AC-01）、パターン不在・ファイル不在
  （AC-02/AC-07）を明示的に網羅する。
- 既存テストの挙動契約（全ファイル更新・不正 semver 拒否・複数行拒否・引数個数・無関係箇所の非改変・
  パターン不一致時の部分更新防止）は上表でそのまま引き継ぐ（設計 §7.2）。`_RejectsNewlineInjectedVersion`
  の「`sed` ペイロード非実行」前提は Go 実装では自明に成立するため、観測可能な「複数行拒否・全ファイル
  非改変」を検証する。

### 4.3 ドキュメント更新の内容（ground truth に対する検証つき）

以下の文字列置換を行う。置換後に §7 の AC 検証（NF/参照残存）で確認する。

- `docs/design/docker_deployment.md`
  - 本文: `bump all of them together with` に続く `` `scripts/bump-release-version.sh` `` を
    `` `go run ./scripts/bump_release_version` `` に変更（Go 実装である旨がわかる表現にする）。
  - コード例: `scripts/bump-release-version.sh vX.Y.Z` → `go run ./scripts/bump_release_version vX.Y.Z`
- `docs/design/docker_deployment.ja.md`
  - 本文: `` これらを手で個別に編集するのではなく `scripts/bump-release-version.sh` でまとめて更新すること ``
    の `` `scripts/bump-release-version.sh` `` を `` `go run ./scripts/bump_release_version` `` に変更。
  - コード例: `scripts/bump-release-version.sh vX.Y.Z` → `go run ./scripts/bump_release_version vX.Y.Z`
- `docs/dev/developer_guide/package_reference.md`: `scripts/` の項を追記し、bump ツール
  （`scripts/bump_release_version/main.go`、`package main`、`go run ./scripts/bump_release_version vX.Y.Z` で起動）の配置・責務・
  実行方法を記述する。

### 4.4 テストヘルパー方針

新規の `testutil/` や `test_helpers.go` は不要。テストヘルパー（`setupRepo` 等の非公開関数）は、同一
パッケージ内でのみ使われる private シンボルのため、[テスト構成ガイド](../../dev/developer_guide/test_organization.md)
の Classification B2 に従い、`_test.go` サフィックスの `scripts/bump_release_version/main_test.go` 内に置く（ビルドタグ不要）。

## 5. リスク管理

| リスク | 影響 | 緩和策 |
|---|---|---|
| `gosec` がファイル I/O を G304/G302 で警告 | Lint 失敗 | 既存 `internal/config/config.go:59` の `//nolint:gosec // <理由>` 慣行を、対象パスが信頼できる旨・mode 復元が意図的である旨の理由コメントとともに各所へ適用（§1.3） |
| `${1}`/`${2}` を素の `$1` で書き置換が壊れる | バージョン行が破損 | 設計 §3.2.2「実装上の注意」に従い波括弧付きグループ参照を必須とし、`TestBumpReleaseVersion_UpdatesAllFiles`・`_PreservesTrailingComment` で置換結果を厳密検証 |
| `unused` linter が未使用の `errorKind`/`updateError.Err` を検出 | Lint 失敗 | 各 `errorKind` 定数を対応する失敗経路で代入し、`Err` をエラーメッセージ整形で参照 |
| カレントディレクトリ基準の解決が repo root 以外からの実行で失敗 | 実行時のファイル不在エラー | `go run ./scripts/bump_release_version vX.Y.Z` を repo root から実行する前提をドキュメントに明記し、ファイル不在時は AC-07 のフェイルクローズで安全に停止 |

## 6. 実装チェックリスト

ステップごとのタスクは §2 の各チェックボックスを、PR 構成は §3.2 を参照。以下は PR 単位の完了トラッキング。

- [ ] PR-1 マージ済み（対象ステップ: 1-1 / 1-2 / 1-3 / 1-4 / 2-1 / 2-2 / 2-3 / 3-1 / 3-2）
- [ ] PR-2 マージ済み（対象ステップ: 3-3 / 3-4 / 3-5 / 3-6 / 3-7）
- [ ] §7 の全 AC 行が緑（`test`）または成立（`static`）
- [ ] §8 クロスサーチの全項目が確認済み
- [ ] §9 成功基準をすべて満たす

## 7. 受け入れ基準の検証（Acceptance Criteria Verification）

各行は `test`（実行可能テスト）/`static`（rg・コンパイル）/`manual`（PR・実機確認）で種別を示す。

| AC | 種別 | 検証方法 |
|---|---|---|
| F-001 | test | `scripts/bump_release_version/main_test.go::TestBumpReleaseVersion_UpdatesAllFiles` |
| AC-01 | test | `scripts/bump_release_version/main_test.go::TestBumpReleaseVersion_RejectsInvalidSemver`、`::TestBumpReleaseVersion_RejectsNewlineInjectedVersion` |
| AC-02 | test | `scripts/bump_release_version/main_test.go::TestBumpReleaseVersion_FailsIfPatternMissing`、`::TestBumpReleaseVersion_DoesNotTouchUnrelatedOccurrencesOfTheOldVersion` |
| AC-02a | test | `scripts/bump_release_version/main_test.go::TestBumpReleaseVersion_PreservesTrailingComment` |
| AC-02b | test | `scripts/bump_release_version/main_test.go::TestBumpReleaseVersion_PreservesCommentedDigestLine` |
| AC-03 | test | `scripts/bump_release_version/main_test.go::TestBumpReleaseVersion_DoesNotFollowPlantedSymlink` |
| AC-04 | test | `scripts/bump_release_version/main_test.go::TestBumpReleaseVersion_PreservesFileMode` |
| AC-05 | test | `scripts/bump_release_version/main_test.go::TestBumpReleaseVersion_RequiresExactlyOneArgument` |
| AC-06 | test + static | test: `scripts/bump_release_version/main_test.go::TestBumpReleaseVersion_PerformsNoUnexpectedSideEffects`（3ファイル以外の副作用なし）。static: `rg -n os/exec scripts/bump_release_version/main.go` と `rg -n net/http scripts/bump_release_version/main.go` をそれぞれ実行し、いずれも一致 0 件（サブプロセス・ネットワークの副作用がないこと。正規表現の交替 `|` は表のセル内では `\|` とエスケープが必要で紛らわしいため、2コマンドに分ける） |
| AC-07 | test | `scripts/bump_release_version/main_test.go::TestBumpReleaseVersion_FailClosedOnWriteError`（フェーズ2書き込み失敗時の後続停止） |
| AC-08 | test | `scripts/bump_release_version/main_test.go::TestBumpReleaseVersion_ValidatesAllBeforeWriting`（フェーズ1検証の全書き込み先行） |
| AC-09 | test | `scripts/bump_release_version/main_test.go::TestBumpReleaseVersion_PrintsGuidanceOnSuccess` |
| NF-001 | static | `make test && make lint` が終了コード 0 |
| NF-002 | static | `go build ./scripts/bump_release_version` が終了コード 0（`go.mod` の `go 1.26.2` と整合） |
| NF-003 | test + static | test: `::TestBumpReleaseVersion_PreservesFileMode` を macOS（ローカル）と Linux（CI）双方で実行し緑。static: `rg -n '"os/exec"' scripts/bump_release_version/main.go` → 一致 0 件（OS ごとの `stat` コマンド分岐が無く mode 処理が純 Go であること） |
| NF-004 | static | `git diff -- go.mod go.sum` に追加行なし（新規依存なし）。かつ `scripts/bump_release_version/main.go` の import が標準ライブラリのみ |

## 8. クロスサーチチェックリスト

`make test`/`make lint` では検出できない残存参照・整合性のみを対象とする（AC 検証表と重複する項目は
そちらに集約）。

- [x] `.sh` 削除後の残存参照: `rg -n "bump-release-version\.sh" --glob '!docs/tasks/**'` の結果が 0 件
  （実装コード・`.github/`・`docs/design`・`docs/dev`・README 等）。`docs/tasks/0018_*` 配下の要件・設計・
  計画文書は移行の経緯を記録するため旧名を含んでよく、本チェックの対象外とする。
- [x] 起動方法の表記統一: `rg -n "bump-release-version" docs/design docs/dev` の結果に旧 `.sh` 名が残らず、
  `go run ./scripts/bump_release_version` 形へ移行済みであること（`docker_deployment.md`/`.ja.md`、`package_reference.md`）。

## 9. 成功基準

- F-001・AC-01〜AC-09（AC-02a/02b を含む）に対応するテストがすべて緑（§7）。
- `make fmt && make test && make lint` が成功（NF-001）。
- Go 版が現行 Bash 実装と同一の CLI 契約（引数形式・終了コード・出力メッセージの趣旨）と安全特性
  （引数検証・symlink 安全性・パーミッション保持・フェイルクローズ）を満たす。
- `scripts/bump-release-version.sh` が削除され、その参照が残っていない（§8）。
- `docs/design/docker_deployment.md`／`.ja.md`／`package_reference.md` が Go 実装の起動方法へ更新済み。

## 10. 次のステップ

- 本実装計画書のレビューと `approved` 化（レビュアー）。
- `approved` 後、フェーズ1から実装に着手する。
