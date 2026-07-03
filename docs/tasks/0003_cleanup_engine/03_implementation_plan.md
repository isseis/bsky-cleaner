# クリーンアップエンジン — 実装計画書

## Document Status

| Item | Value |
|---|---|
| Status | `approved` |
| Created | 2026-07-04 |
| Review date | 2026-07-04 |
| Reviewer | isseis |
| Comments | - |

関連ドキュメント: [要件定義書](01_requirements.md) / [アーキテクチャ設計書](02_architecture.md)

## 1. 実装概要

### 1.1 目的

`internal/cleanup` パッケージを新設し、投稿一覧（`[]atproto.Post`）・保持日数（`retentionDays int`）・判定基準時刻（`now time.Time`）を入力として削除対象の投稿一覧を返す純粋関数 `SelectDeletionTargets` を実装する。

### 1.2 実装方針

[アーキテクチャ設計書](02_architecture.md) 8節の実装優先順位に従う。判定条件（経過日数・投稿種別・ピン留め）間に依存関係がないため、フェーズ分けは「実装」→「テスト」→「品質確認」→「ドキュメント更新」の順とし、判定ロジック自体を複数フェーズに分割しない。

### 1.3 既存コード調査結果

- `internal/cleanup` パッケージは存在しない（リポジトリルートで `find internal -type f -name '*.go'` を実行して確認済み）。本タスクはこのディレクトリと配下のファイルを新規作成するのみで、既存ファイルへの変更は発生しない。
- 入出力型 `atproto.Post` / `atproto.PostType` は `internal/atproto/posts.go` に既に定義済み（`internal/atproto/posts.go:34-56`）。`PostTypeOriginal`/`PostTypeReply`/`PostTypeQuote`/`PostTypeRepost` の4定数がこの順で `iota` 定義されている。本タスクはこれらの型をそのまま再利用し、新しい型を追加しない（設計書 3.1 節）。
- `internal/config/validate.go:28-34` で `retentionDays <= 0` は既に拒否されており、本パッケージが受け取る時点で正の整数であることが保証されている（設計書 1.1 節・4節）。本パッケージ側で再検証は行わない。
- テストヘルパーの観点では、`atproto.Post`/`atproto.PostType` はいずれもエクスポートされた型・定数であり、`internal/cleanup/cleanup_test.go` から直接 `atproto.Post{...}` を組み立てられる。想定外の `PostType` 値も `atproto.PostType(99)` のようにテストコードから直接構成できるため、`internal/atproto/testutil` 配下のフィクスチャ（XRPC レスポンス JSON 用であり本タスクの入力形式とは無関係）や新規モックは不要である。よって `testutil/` にも `test_helpers.go` にも新規ヘルパーファイルは不要と判断する（7節参照）。
- `docs/dev/developer_guide/package_reference.md` はまだ `internal/cleanup` を記載していない。設計書 8節の手順4に従い、実装完了後にこのファイルへパッケージ責務を追記する必要がある（フェーズ3）。

## 2. 実装ステップ

アーキテクチャ設計書 8節の実装優先順位に沿って記載する。

### フェーズ1: `SelectDeletionTargets` の実装（F-001〜F-003、AC-01〜AC-08 全体の実装基盤）

- [ ] **対象ファイル**: `internal/cleanup/cleanup.go`（新規作成）
  - **作業内容**:
    - `package cleanup` を宣言し、`internal/atproto` パッケージをインポートする。
    - 設計書 3.1 節のシグネチャをそのまま実装する: `func SelectDeletionTargets(posts []atproto.Post, retentionDays int, now time.Time) []atproto.Post`（`error` を返さない）。
    - 閾値時刻の算出方法・3条件（種別・ピン留め・経過日数）の判定順序・比較演算子の選び方は、設計書 1.1 節・2.2 節・3.2 節の記述にそのまま従う（ここでは再掲しない）。
    - 戻り値は `nil` 入力に対しても空スライスを返す（設計書 4節、パニックしないことを保証する）。
  - **完了基準**: `go build ./...` が成功する。フェーズ2のテストがすべて通過する。

### フェーズ2: 単体テストの作成（AC-01〜AC-08）

- [ ] **対象ファイル**: `internal/cleanup/cleanup_test.go`（新規作成）
  - **作業内容**: 設計書 7.1 節の表に基づき、以下のテストケースを作成する。`now` はすべてのテストで固定値（例: `time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)`）を明示的に引数として渡し、`time.Now()` は使用しない（NF-002a）。
    - [ ] `TestSelectDeletionTargets_OlderThanThreshold_Included`: `CreatedAt` が閾値時刻より古い投稿1件が戻り値に含まれることを検証する（AC-01）。
    - [ ] `TestSelectDeletionTargets_WithinThreshold_Excluded`: `CreatedAt` が閾値時刻以内（`now` に近い、閾値より新しい）投稿1件が戻り値に含まれないことを検証する（AC-02）。
    - [ ] `TestSelectDeletionTargets_ExactlyAtThreshold_Excluded`: `CreatedAt` が閾値時刻とちょうど等しい投稿1件が戻り値に含まれないことを検証する境界値テスト（AC-03）。
    - [ ] `TestSelectDeletionTargets_NonUTCOffset_NormalizedBeforeComparison`: `CreatedAt` に UTC 以外のオフセット（例: `time.FixedZone("JST", 9*60*60)` で構成した、UTC 換算では閾値より古くなる時刻）を持つ投稿が正しく削除対象と判定されることを検証する（AC-04）。合わせて、同じオフセットで UTC 換算後に閾値より新しくなる時刻の投稿が削除対象外と判定されるケースも1件加え、オフセット変換が「常に削除対象」側に偏っていないことを確認する。
    - [ ] `TestSelectDeletionTargets_AllKnownPostTypes_Included`: `atproto.PostTypeOriginal`/`atproto.PostTypeReply`/`atproto.PostTypeQuote`/`atproto.PostTypeRepost` それぞれについて、経過日数条件を満たす投稿1件ずつが戻り値に含まれることを検証する（AC-05）。4種別をテーブル駆動テスト（`[]struct{ name string; postType atproto.PostType }`）で1関数にまとめてよい。
    - [ ] `TestSelectDeletionTargets_UnknownPostType_Excluded`: `atproto.PostType(99)` のように既知4定数のいずれにも一致しない値を持つ投稿が、経過日数条件を満たしていても戻り値に含まれないことを検証する（AC-06）。
    - [ ] `TestSelectDeletionTargets_Pinned_Excluded`: `Pinned: true` かつ経過日数条件を満たす投稿1件が戻り値に含まれないことを検証する（AC-07）。
    - [ ] `TestSelectDeletionTargets_Unpinned_Included`: `Pinned: false` かつ経過日数条件を満たす投稿1件が戻り値に含まれることを検証する（AC-08。ピン留め解除後の状態を、解除後の `Post` 値をそのまま入力として与えることで表現する。ピン留め状態の時系列変化そのものは本パッケージの関心事ではなく、単に `Pinned` フィールドの現在値に基づく判定であることを確認する）。
    - [ ] `TestSelectDeletionTargets_NilInput_ReturnsEmpty`: `posts` に `nil` を渡した場合にパニックせず空スライスを返すことを確認する回帰テスト（設計書 4節の前提を保証する。AC には対応しないが、`nil` 安全性は実装が満たすべき前提のため追加する）。
  - **完了基準**: `make test` で `internal/cleanup` パッケージの全テストが成功する。各テストは `assert`/`require`（`github.com/stretchr/testify`）を用いてアサーションを記述する（CLAUDE.md テスト方針）。

### PR-1 作成ポイント: cleanup engine implementation

**対象ステップ**: フェーズ1 / フェーズ2

**推奨タイトル**: `feat(0003-cleanup-engine): implement SelectDeletionTargets`

**レビュー観点**: 3条件の判定順序と比較演算子（`Before` のみを使い等号を含まないこと） / `switch` 文が4定数を明示列挙し `default` 側で除外していること / 判定ロジックとそのテストは1つの完結した変更単位であり分割しないこと

- [ ] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [ ] PR を作成した
- [ ] PR がマージされた
- [ ] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

### フェーズ3: 品質確認とドキュメント更新

- [ ] **対象コマンド**: `make fmt` / `make test` / `make lint`
  - **作業内容**: 3コマンドを順に実行し、いずれもエラーなく完了することを確認する（NF-001）。
  - **完了基準**: 3コマンドすべてが正常終了する。
- [ ] **対象コマンド**: `make deadcode`
  - **作業内容**: 未使用コードが検出されないことを確認する。
  - **完了基準**: `internal/cleanup` に起因する未使用コードの指摘がない。
- [ ] **対象ファイル**: `docs/dev/developer_guide/package_reference.md`
  - **作業内容**: 以下の2箇所を追記する。この2箇所は既存ファイル内で異なる命名規則を使っているため、それぞれ規則に合わせる（1つ目は `config/`・`atproto/` のようにディレクトリ名のみをそのまま記載する規則、2つ目は `internal/config`・`internal/atproto` のようにフルパスで記載する規則）。
    1. 「Directory Structure」の `internal/` 一覧（`config/`・`atproto/` が並ぶ箇所）に以下の行を追加する。
       ```
       - `cleanup/`: filters an account's post inventory down to deletion targets based on retention days, post type, and pinned status (see docs/tasks/0003_cleanup_engine)
       ```
    2. 「Package Responsibilities」に、既存の `internal/atproto`・`internal/config` の記述形式に合わせて以下の見出しと説明を追加する。
       ```
       **Cleanup Engine**

       - `internal/cleanup`: a pure function (`SelectDeletionTargets`) that filters an account's post inventory (`[]atproto.Post`) down to deletion targets, based on retention-day age (UTC), known post type, and pinned status. No network/file I/O, no dependency on the AT Protocol client itself (see docs/tasks/0003_cleanup_engine/01_requirements.md).
       ```
  - **完了基準**: 以下の2つの `rg` コマンドがそれぞれ1件ずつヒットする。
    - `rg -n "cleanup/" docs/dev/developer_guide/package_reference.md` （Directory Structure への追記を確認）
    - `rg -n "internal/cleanup" docs/dev/developer_guide/package_reference.md` （Package Responsibilities への追記を確認。この2つ目のコマンドは1つ目のディレクトリ構成の行にはヒットしない。その行は `cleanup/` であって `internal/cleanup` という文字列を含まないためである）

### PR-2 作成ポイント: package reference documentation update

**対象ステップ**: フェーズ3

**推奨タイトル**: `docs(0003-cleanup-engine): add internal/cleanup to package reference`

**レビュー観点**: 追記内容が既存の `internal/atproto`・`internal/config` の記述粒度・文体と一致していること

- [ ] グリーンゲート（`_context.md` の "Green gate" 参照）がパスしていることを確認した
- [ ] PR を作成した
- [ ] PR がマージされた
- [ ] 次のブランチへ切り替えた（次ステップは新しいブランチで作業する）

## 3. 実装順序とマイルストーン

### 3.1 マイルストーン

| マイルストーン | 内容 | 成果物 |
|---|---|---|
| M1 | `SelectDeletionTargets` の実装とAC-01〜AC-08の単体テストが完了し `make test` が緑になる | `internal/cleanup/cleanup.go`、`internal/cleanup/cleanup_test.go` |
| M2 | `make lint`・`make deadcode` の完走と `package_reference.md` の更新が完了する | 更新済み `docs/dev/developer_guide/package_reference.md` |

### 3.2 PR 構成

| PR | 対応フェーズ | 概要 |
|---|---|---|
| PR-1 | フェーズ1 / フェーズ2 | `internal/cleanup` パッケージの新設（実装＋単体テスト） |
| PR-2 | フェーズ3 | 品質確認完了後の `package_reference.md` 更新 |

## 4. テスト戦略

### 4.1 単体テスト

設計書 7.1 節の通り、`internal/cleanup/cleanup_test.go` に AC-01〜AC-08 それぞれを検証するテストケースを作成する（フェーズ2のチェックリスト参照）。すべてのテストは `now` を固定値として明示的に渡し、`time.Now()` に依存しない決定的なテストとする（NF-002a）。境界値（AC-03）・タイムゾーン正規化（AC-04）・想定外種別（AC-06）は明示的な境界値テストとして分離し、代表値のみのテーブル駆動に埋没させない。

### 4.2 統合テスト

設計書 7.2 節の通り、本パッケージは `internal/atproto` の型のみに依存する純粋関数であり外部結合点を持たないため、統合テストは本タスクのスコープ外とする（[0004_cli_entrypoint](../0004_cli_entrypoint/01_requirements.md) 側で実施）。

### 4.3 後方互換テスト

該当なし。`internal/cleanup` は新設パッケージであり、既存の公開 API・挙動を変更しない。

### 4.4 テストヘルパ方針

[test_organization.md](../../dev/developer_guide/test_organization.md) の分類に従うと、本タスクで必要になるテストデータは `atproto.Post{...}` の直接構成のみであり、複数パッケージにまたがるモックも、`cleanup` パッケージ内部の非公開 API を用いるヘルパーも不要である。したがって `testutil/` サブディレクトリ・`test_helpers.go` のいずれも新規作成しない（1.3 節参照）。

## 5. リスク管理

| リスク | 影響 | 対策 |
|---|---|---|
| 比較演算子の選択ミス（`Before` の代わりに `!After`/`!Before` 等の等号を含む比較を使ってしまい、AC-03 の境界値判定が逆転する） | 境界値の投稿が誤って削除対象に含まれる、または除外されるべきでない投稿が除外される | フェーズ2で境界値テスト（`TestSelectDeletionTargets_ExactlyAtThreshold_Excluded`）を明示的に用意し、レビュー時に比較演算子を目視確認する（PR-1 レビュー観点） |
| `switch` 文の代わりに範囲チェック（`0 <= v && v < 4`）を実装してしまい、AC-06 の「想定外種別の除外」が機能しなくなる | 将来 `PostType` に新しい定数が追加された際、想定外種別が誤って削除対象に含まれる | フェーズ2で `atproto.PostType(99)` を用いた明示的なテストを用意し、実装が `switch` による列挙になっていることをレビューで確認する |
| スケジュールリスク: 本タスクは判定ロジックのみで外部依存がなく規模が小さいため、大きな遅延要因は想定しにくい | 軽微 | バッファは設けず、フェーズ1〜3を1PR＋1PRの計2PRで完結させる |

## 6. 実装チェックリスト

- [ ] PR-1 マージ済み（対象ステップ: フェーズ1 / フェーズ2。`internal/cleanup/cleanup.go`・`internal/cleanup/cleanup_test.go` 作成、AC-01〜AC-08 全テストケース追加）
- [ ] PR-2 マージ済み（対象ステップ: フェーズ3。`package_reference.md` 更新）
- [ ] `make fmt` / `make test` / `make lint` がすべて通過
- [ ] `make deadcode` で未使用コードがないことを確認

## 7. 受け入れ基準の検証（Acceptance Criteria Verification）

| AC | 検証種別 |
|---|---|
| AC-01〜AC-08 | test |

**AC-01: `createdAt` が「現在時刻（UTC） - retention_days 日」より古い投稿は削除対象と判定される**
- Test location: `internal/cleanup/cleanup_test.go::TestSelectDeletionTargets_OlderThanThreshold_Included`
- Implementation: `internal/cleanup/cleanup.go`（`SelectDeletionTargets` の経過日数判定部分）
- Verification method: test（`go test` で当該テストが成功することを確認。閾値時刻より1日以上古い `CreatedAt` を持つ投稿が戻り値のスライスに含まれることをアサートする）

**AC-02: `createdAt` が「現在時刻（UTC） - retention_days 日」以内の投稿は削除対象外と判定される**
- Test location: `internal/cleanup/cleanup_test.go::TestSelectDeletionTargets_WithinThreshold_Excluded`
- Implementation: `internal/cleanup/cleanup.go`
- Verification method: test（閾値時刻より新しい `CreatedAt` を持つ投稿が戻り値のスライスに含まれないことをアサートする）

**AC-03: `createdAt` が閾値時刻とちょうど等しい投稿（境界値）は削除対象外と判定される**
- Test location: `internal/cleanup/cleanup_test.go::TestSelectDeletionTargets_ExactlyAtThreshold_Excluded`
- Implementation: `internal/cleanup/cleanup.go`
- Verification method: test（`CreatedAt` を `now.AddDate(0, 0, -retentionDays)` と厳密に一致する値に設定し、戻り値のスライスに含まれないことをアサートする）

**AC-04: タイムゾーン情報を含む `createdAt`（UTC 以外のオフセット表記）が渡された場合でも、UTC に正しく変換した上で判定される**
- Test location: `internal/cleanup/cleanup_test.go::TestSelectDeletionTargets_NonUTCOffset_NormalizedBeforeComparison`
- Implementation: `internal/cleanup/cleanup.go`（`post.CreatedAt.UTC()` による正規化）
- Verification method: test（`time.FixedZone` で構成した非UTCオフセットの `CreatedAt` に対し、UTC換算後に閾値より古くなるケースは含まれる、新しくなるケースは含まれないことの両方をアサートする）

**AC-05: 通常投稿・リプライ・リポスト・引用ポストのそれぞれについて、経過日数条件を満たせば削除対象と判定される**
- Test location: `internal/cleanup/cleanup_test.go::TestSelectDeletionTargets_AllKnownPostTypes_Included`
- Implementation: `internal/cleanup/cleanup.go`（`switch` による既知4値判定）
- Verification method: test（`atproto.PostTypeOriginal`/`PostTypeReply`/`PostTypeQuote`/`PostTypeRepost` の4種別それぞれについて、経過日数条件を満たす投稿が戻り値に含まれることをテーブル駆動テストでアサートする）

**AC-06: 既知4種別以外の想定外のレコード種別が入力された場合、削除対象としない**
- Test location: `internal/cleanup/cleanup_test.go::TestSelectDeletionTargets_UnknownPostType_Excluded`
- Implementation: `internal/cleanup/cleanup.go`（`switch` の `default` 分岐、または非該当時の除外処理）
- Verification method: test（`atproto.PostType(99)` を持ち経過日数条件を満たす投稿が戻り値に含まれないことをアサートする）

**AC-07: ピン留めされた投稿は、経過日数条件を満たしていても削除対象から除外される**
- Test location: `internal/cleanup/cleanup_test.go::TestSelectDeletionTargets_Pinned_Excluded`
- Implementation: `internal/cleanup/cleanup.go`（`post.Pinned` 判定）
- Verification method: test（`Pinned: true` かつ経過日数条件を満たす投稿が戻り値に含まれないことをアサートする）

**AC-08: ピン留めが解除された投稿は、経過日数条件を満たせば通常通り削除対象と判定される**
- Test location: `internal/cleanup/cleanup_test.go::TestSelectDeletionTargets_Unpinned_Included`
- Implementation: `internal/cleanup/cleanup.go`
- Verification method: test（`Pinned: false` かつ経過日数条件を満たす投稿が戻り値に含まれることをアサートする）

## 8. 成功基準

- **機能的完全性**: AC-01〜AC-08 がすべて7節のテストで検証され成功する。
- **品質指標**: `make fmt` / `make test` / `make lint` / `make deadcode` がすべてエラーなく完了する（NF-001）。
- **セキュリティ検証**: 設計書 5.2 節の通り、本パッケージは外部入出力を持たず新たな攻撃面を追加しない。個別のセキュリティテストは不要（N/A、設計書 7.3 節）。
- **ドキュメント整備**: `docs/dev/developer_guide/package_reference.md` に `internal/cleanup` の責務が追記されている（フェーズ3、`rg -n "internal/cleanup" docs/dev/developer_guide/package_reference.md` で確認）。

## 9. 次のステップ

- [0004_cli_entrypoint](../0004_cli_entrypoint/01_requirements.md) の設計時に、`SelectDeletionTargets` の呼び出し結線（`ListPosts` → `SelectDeletionTargets` → dry-run/apply 分岐）を行う。
- 設計書 9節に記載の通り、除外理由別の内訳（想定外種別／ピン留め／経過日数未達の件数）が Slack 通知やログに必要かどうかを 0004 の設計時に確認し、必要であれば本パッケージのインターフェース拡張を別タスクとして起票する。
