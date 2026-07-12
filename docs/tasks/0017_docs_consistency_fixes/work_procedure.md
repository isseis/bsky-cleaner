# ドキュメント整合性修正 作業手順書

## 文書ステータス

| 項目 | 値 |
|---|---|
| 種別 | 軽量作業手順書（フルスペックの要件/設計/計画文書は作成しない） |
| Status | `approved` |
| Created | 2026-07-12 |
| Reviewer | isesis |
| Comments | - |

## 1. 背景と目的

既存ドキュメント（`README.md`、`docs/overview.md`、`docs/design/*`、
`docs/dev/developer_guide/*`。英日両方が存在する場合は英語版を正とする）を
横断的にレビューした結果、ドキュメント間の**矛盾・記述漏れ・陳腐化**が
複数見つかった。本作業はこれらを解消するための軽量ドキュメント修正であり、
コードの挙動変更は伴わない。

通常の開発プロセス（`01_requirements.md` → `02_architecture.md` →
`03_implementation_plan.md`）は適用せず、本手順書1枚で作業を管理する。

### 重要な作業原則

- **必ず一次情報で裏取りする**: 各修正は「ドキュメント同士の突き合わせ」だけで
  完結させず、対象の記述が**実装コード（`internal/` / `cmd/`）** または
  **公式情報（AT Protocol / Slack / Docker 等の公式ドキュメント）** と
  一致することを確認してから確定する。確認できない場合は修正せず、
  「§6 未解決事項」に記録する。
- **推測で書き換えない**: 実装・公式情報で確認できた事実だけを反映する。
- **バイリンガル同期規約を守る**: `README.md` / `docs/overview.md` /
  `docs/design/*` は英日ペア。日本語版を先に編集・コミットし、その後
  `/mktrans <日本語ファイル>` で英語版へ差分翻訳する。英日を同一コミットで
  手編集しない（`development_workflow.md` §4）。
  `docs/dev/developer_guide/*` は英語のみ（ペアなし）なので直接編集する。

## 2. 対象ファイル一覧

| # | ファイル | ペア | 直接編集可否 |
|---|---|---|---|
| a | `docs/dev/developer_guide/mermaid_reference.md` | 英語のみ | 直接編集可 |
| b | `docs/design/slack_notification_security.ja.md`（→ `.md` は `/mktrans`） | 英日ペア | 日本語先行 |
| c | `docs/design/docker_deployment.ja.md`（→ `.md`） | 英日ペア | 日本語先行 |
| d | `docs/overview.ja.md`（→ `.md`） | 英日ペア | 日本語先行 |
| e | `README.ja.md`（→ `README.md`） | 英日ペア | 日本語先行 |
| f | `docs/design/configuration.ja.md`（→ `.md`）※任意 | 英日ペア | 日本語先行 |

## 3. 発見事項サマリ

| ID | 種別 | 概要 | 主対象 | 優先度 |
|---|---|---|---|---|
| F-1 | 矛盾 | `print-schedule` が「hidden subcommand」と記されつつ README の Usage で公開コマンドとして案内されている | docker_deployment / README | 中 |
| F-2 | 矛盾 | overview の節名「Development and Distribution Strategy」を docker_deployment が「Development and **Deployment** Strategy」と参照 | docker_deployment / overview | 低 |
| F-3 | 陳腐化 | Slack ペイロード同梱情報の列挙（result type / counts / rkey / error type）が 0012/0013 で追加された Host・Account・Duration を反映していない | slack_notification_security | 中 |
| F-4 | 記述漏れ | README の TOML 設定表・設定例に `hostname` フィールドが無い（configuration.md には有る） | README | 高 |
| F-5 | 記述漏れ | 通知メッセージの「内容」の仕様が `docs/design/` に無く、task 文書と package_reference のみに存在 | overview（設計層） | 中 |
| F-6 | 記述漏れ | CLI 契約（Exit Code 0/1/2/3、`--apply`/`--version`、partial-failure 意味）が README のみで設計層に無い | overview / 設計層 | 低 |
| F-7 | 重複/ドリフト | リトライ数値（5回/1s/30s/約31s、Slack の約12s）が overview・configuration・slack_notification_security に重複 | 複数 | 低 |
| F-8 | 混入 | `mermaid_reference.md` の例に他プロジェクト由来の `GSCR_SLACK_WEBHOOK_URL` が残存（本プロジェクトは `BSKY_` 接頭辞） | mermaid_reference | 低 |
| F-9 | 陳腐化 | overview / design の `Status: Draft` と `2026-07-02` 日付が、0012/0013/0016 反映後も更新されていない | overview / design | 低 |
| F-10 | 方針決定 | `Status: Draft` のまま放置されている以下5文書を、本作業で `Final` に確定する（ユーザー指示、2026-07-12）: `docs/overview`, `docs/design/configuration`, `docs/design/docker_deployment`, `docs/design/security`, `docs/design/slack_notification_security`（各 `.md`/`.ja.md`） | overview / design 全体 | 高 |

## 4. 作業ステップ

各ステップは「**確認 → 修正 → 検証**」の順で実施する。確認手順で一次情報と
食い違った場合は、手順書側（本ファイル）の記述を修正してから作業する。

### Step 0: 準備

- [ ] 作業ブランチにいることを確認（`issei/0017-docs-consistency-fixes-01`）。
- [ ] `make test` / `make lint` が現状 green であることを確認（ベースライン）。

### Step 1 (F-8): `mermaid_reference.md` の環境変数名修正 ※英語のみ・独立

- **確認**:
  - [ ] `grep -rn "BSKY_SLACK_WEBHOOK_URL" internal/ cmd/` で実際の環境変数名を確認する。
  - [ ] `grep -rn "GSCR_" .` で他に混入が無いか確認する。
- **修正**:
  - [ ] `mermaid_reference.md` の `GSCR_SLACK_WEBHOOK_URL` を実在する接頭辞
    （`BSKY_SLACK_WEBHOOK_URL_SUCCESS` 等）に置換、または汎用例文へ変更。
- **検証**:
  - [ ] Mermaid ブロックの体裁が崩れていないこと（ラベルのクォート規約）を目視確認。

### Step 2 (F-3): Slack ペイロード同梱情報の列挙を実装に一致させる

- **確認（実装が一次情報）**:
  - [ ] `internal/notify/payload.go` の `buildPayload` を読み、実際に payload へ
    含めるフィールド（Host / Account / Targets / Deleted / Duration /
    失敗詳細 / error kind 等）を列挙する。
  - [ ] `report.Result` の構造体定義（`internal/report`）で各フィールドの出所を確認。
  - [ ] 本文（post body）が含まれないことをコードで再確認（`atproto.Post` に本文
    フィールドが無いこと）。
- **修正**:
  - [ ] `slack_notification_security.ja.md` の「ペイロードに含める情報の制限」節を
    実装の実際のフィールド集合に更新（Host/Account/Duration を追記）。
    「本文を含めない」という不変条件は維持。
- **検証**:
  - [ ] 可能なら `make notify-preview` で実際の出力フィールドと文書の記述が
    一致することを目視確認。

### Step 3 (F-1): `print-schedule` の位置づけ統一

- **確認**:
  - [ ] `cmd/` 実装で `print-schedule` サブコマンドの登録方法を確認し、
    実際に「隠しコマンド」なのか「公開コマンド」なのかを判定する
    （ヘルプ出力の有無・フラグ定義を確認）。
  - [ ] `docs/tasks/0007_docker_distribution` の該当記述を参照。
- **修正（実装の実態に合わせて一方へ寄せる）**:
  - [ ] 公開コマンドなら `docker_deployment.ja.md` の "hidden" 表現を削除/修正。
  - [ ] 内部用途なら README Usage 側で内部/上級者向けである旨を明記、または削除。
- **検証**:
  - [ ] `docker_deployment` と `README` の記述が同じ位置づけを述べていること。

### Step 4 (F-2): 節名参照の不一致修正

- **確認**:
  - [ ] `docs/overview.md` の該当節の正式名称を確認
    （現状 "Development and Distribution Strategy"）。
- **修正**:
  - [ ] `docker_deployment.ja.md`（および必要なら overview 側）で節名参照を一致させる。
    どちらの語（Distribution / Deployment）を正とするか決めて統一する。
- **検証**:
  - [ ] 参照名と実際の見出しが一致すること。

### Step 5 (F-4): README に `hostname` TOML フィールドを追記 ※優先度高

- **確認（実装と configuration.md が一次情報）**:
  - [ ] `internal/config` の TOML 読み込み・`ResolveHostname` を読み、`hostname`
    の型・必須/任意・デフォルト（`os.Hostname()` フォールバック、空白のみは未設定扱い）
    を確認する。
  - [ ] `configuration.md` の該当行（`hostname` 行）と実装が一致していることも確認。
- **修正**:
  - [ ] `README.ja.md` の「TOML 設定ファイル」表に `hostname` 行を追加。
  - [ ] README 内の TOML 例（2 箇所）に `hostname` を追記するか、任意項目である旨を注記。
- **検証**:
  - [ ] README の記述が `configuration.md` と実装の双方に矛盾しないこと。

### Step 6 (F-5 / F-6): 設計層への通知内容・CLI 契約の反映（任意・要判断）

- **確認**:
  - [ ] 通知内容は Step 2 と同じく `internal/notify` 実装で確定。
  - [ ] CLI 契約は `cmd/` の Exit Code 実装（0/1/2/3 の分岐）と
    `docs/tasks/0004_cli_entrypoint` を参照して確定。
- **修正（実施可否は Reviewer 判断）**:
  - [ ] `overview.ja.md` に通知メッセージ内容の要約小節を追加（task 文書非依存化）。
  - [ ] Exit Code / partial-failure の意味を設計層（overview または新規小節）に反映。
- **検証**:
  - [ ] README の CLI 記述と設計層の記述が一致すること。

### Step 7 (F-7): リトライ数値の重複整理（任意・要判断）

- **確認**:
  - [ ] リトライ既定値の一次情報を特定する
    （`internal/retry` / `internal/atproto` / `internal/notify` の実装値）。
- **修正**:
  - [ ] 数値の正本を1箇所（`configuration.md` を推奨）に集約し、他は相互参照へ置換。
    ※重複を残す場合でも、全箇所が実装値と一致していることを必ず確認する。
- **検証**:
  - [ ] overview / configuration / slack_notification_security の数値が実装と一致。

### Step 8 (F-9 / F-10): Status を `Draft` → `Final` に確定し、日付を更新する

対象は以下 5 文書（英日ペア = 計 10 ファイル）。
`Status: Draft` を `Final` に変更するのはユーザーの明示指示（2026-07-12）に
基づく方針決定であり、LLM が独自判断で行うものではない。ただし
**「内容が正しいので Final にできる」という判断自体は一次情報の裏取りなしに
行わない** — 本ステップは Step 1〜7 の内容修正が完了した後、かつ以下の確認を
経てから実施する。

- `docs/overview.md` / `docs/overview.ja.md`
- `docs/design/configuration.md` / `.ja.md`
- `docs/design/docker_deployment.md` / `.ja.md`
- `docs/design/security.md` / `.ja.md`
- `docs/design/slack_notification_security.md` / `.ja.md`

- **確認（Final 化前の一次情報チェック）**:
  - [ ] Step 1〜7 で洗い出した該当文書の記述漏れ・矛盾・陳腐化がすべて解消済み、
    または §6 に「対応不要」の理由とともに記録済みであることを確認する。
  - [ ] 各文書が言及する実装（`internal/config`、`internal/notify`、
    `internal/retry`、Docker/cron 関連ファイル等）を再度確認し、現在の実装
    挙動と文書の記述が一致していることを確認する（Step 2/3/5/7 の確認結果を
    再利用可）。
  - [ ] `docs/tasks/` 配下で当該設計文書に関連する未完了・未承認タスクが
    無いことを確認する（`grep -rl "Status.*draft" docs/tasks/*/0[1-3]_*.md` 等）。
    関連タスクが `draft` のままなら、その設計文書を `Final` にしてよいか
    Reviewer に確認する。
- **修正**:
  - [ ] 上記 5 文書（日本語版を先に編集）の `ステータス` を `Draft` → `Final` に、
    `最終更新日`（`Last updated`）を作業実施日に更新する。
  - [ ] 英語版は `/mktrans` による差分翻訳で同期する（Step 9 で実施、個別に
    手編集しない）。
- **検証**:
  - [ ] 変更後、全5文書の英日ペアで `Status`/`ステータス` が `Final` に統一
    されていること、日付が一致していることを目視確認する。

参考: この `Final` は `docs/tasks/XXXX_feature/` の `01_requirements.md` 等で
使われる `draft`/`approved` ワークフロー（`requirements_process.md`）とは
別の語彙・別の文書種別（設計リファレンス文書）である。混同しないこと。

### Step 9: バイリンガル同期と最終確認

- [ ] 日本語版（`*.ja.md`）の編集を**コミット**する。
- [ ] 各日本語ファイルに対し `/mktrans <path-to.ja.md>` を実行し英語版へ差分翻訳、
      翻訳結果を別コミットする（英日を同一コミットで手編集しない）。
- [ ] `mermaid_reference.md` 等の英語のみ文書は直接編集済みであることを確認。
- [ ] `make test` / `make lint`（ドキュメントのみだが CI 相当を通す）を実行。
- [ ] 変更した全ファイルのリンク切れ・相互参照を最終目視確認。

## 5. 完了条件（Definition of Done）

- [ ] F-1〜F-9 のうち実施対象とした項目が、すべて実装または公式情報での確認を
      経て修正されている。
- [ ] F-10: 対象5文書（英日ペア計10ファイル）の `Status` が `Final` に更新され、
      更新前に内容の正確性が一次情報で確認されている。
- [ ] 実施しないと判断した項目は §6 に理由付きで記録されている。
- [ ] 英日ペア文書の日本語・英語版が `/mktrans` 経由で同期されている。
- [ ] ドキュメント間の相互参照（節名・ファイルパス）が整合している。

## 6. 未解決事項 / 実施しない項目

（作業中に、一次情報で確認できなかった事項や、Reviewer 判断で見送った項目を
理由とともにここへ追記する。）

- （なし）
