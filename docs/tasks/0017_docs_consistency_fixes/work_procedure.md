# ドキュメント整合性修正 作業手順書

## 文書ステータス

| 項目 | 値 |
|---|---|
| 種別 | 軽量作業手順書（フルスペックの要件/設計/計画文書は作成しない） |
| Status | `approved` |
| Created | 2026-07-12 |
| Reviewer | isesis |
| Comments | Step 0〜9・DoD すべて完了(2026-07-12)。Step 6/7 の任意項目はユーザー承認の上で実施。§6 未解決事項なし。 |

## 1. 背景と目的

既存ドキュメント（`README.md`、`docs/overview.md`、`docs/design/*`、
`docs/dev/developer_guide/*`。英日両方が存在する場合は日本語版を正とし、
`/mktrans` で英語版へ同期する）を
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

- [x] 作業ブランチにいることを確認（`issei/0017-docs-consistency-fixes-01`）。
- [x] `make test` / `make lint` が現状 green であることを確認（ベースライン）。

### Step 1 (F-8): `mermaid_reference.md` の環境変数名修正 ※英語のみ・独立

- **確認**:
  - [x] `grep -rn "BSKY_SLACK_WEBHOOK_URL" internal/ cmd/` で実際の環境変数名を確認する。
    → `BSKY_SLACK_WEBHOOK_URL_SUCCESS` / `BSKY_SLACK_WEBHOOK_URL_FAILURE` /
    `BSKY_SLACK_WEBHOOK_URL_TEST`（notify-preview）を確認。
  - [x] `grep -rn "GSCR_" .` で他に混入が無いか確認する。
    → `mermaid_reference.md:27` 以外に混入なし。
- **修正**:
  - [x] `mermaid_reference.md` の `GSCR_SLACK_WEBHOOK_URL` を実在する接頭辞
    （`BSKY_SLACK_WEBHOOK_URL_SUCCESS` 等）に置換、または汎用例文へ変更。
- **検証**:
  - [x] Mermaid ブロックの体裁が崩れていないこと（ラベルのクォート規約）を目視確認。

### Step 2 (F-3): Slack ペイロード同梱情報の列挙を実装に一致させる

- **確認（実装が一次情報）**:
  - [x] `internal/notify/payload.go` の `buildPayload` を読み、実際に payload へ
    含めるフィールド（Host / Account / Targets / Deleted / Duration /
    失敗詳細 / error kind 等）を列挙する。
    → Host・Account は常時付与、Targets/Deleted/Duration は `Result != nil` の
    ときのみ、失敗時は Error または Failed posts（rkey + errorKind）を追加。
  - [x] `report.Result` の構造体定義（`internal/report`）で各フィールドの出所を確認。
    → `Result{Mode, Targets, Deleted, Failed}`（`internal/report/report.go:33-38`）。
  - [x] 本文（post body）が含まれないことをコードで再確認（`atproto.Post` に本文
    フィールドが無いこと）。
    → `internal/atproto/posts.go:84-89` の `Post{RKey, Type, CreatedAt, Pinned}`
    に本文フィールドなしを確認。
- **修正**:
  - [x] `slack_notification_security.ja.md` の「ペイロードに含める情報の制限」節を
    実装の実際のフィールド集合に更新（Host/Account/Duration を追記）。
    「本文を含めない」という不変条件は維持。
- **検証**:
  - [x] 可能なら `make notify-preview` で実際の出力フィールドと文書の記述が
    一致することを目視確認。
    → 全5シナリオ（success-empty/success-apply/partial-failure/run-error/
    truncation）の出力フィールドが更新後の記述と一致することを確認。

### Step 3 (F-1): `print-schedule` の位置づけ統一

- **確認**:
  - [x] `cmd/` 実装で `print-schedule` サブコマンドの登録方法を確認し、
    実際に「隠しコマンド」なのか「公開コマンド」なのかを判定する
    （ヘルプ出力の有無・フラグ定義を確認）。
    → `cmd/main.go:461` のコメントで明示的に "hidden, used by entrypoint.sh
    for cron integration"、かつ `printUsage`（`--help` 出力）にも一切
    現れないことを確認。実装の意図は「隠しコマンド」。
  - [x] `docs/tasks/0007_docker_distribution` の該当記述を参照。
    → `docker_deployment.ja.md` は既に「隠しサブコマンド」と記述しており
    実装と一致。矛盾していたのは README 側だった。
- **修正（実装の実態に合わせて一方へ寄せる）**:
  - [x] 公開コマンドなら `docker_deployment.ja.md` の "hidden" 表現を削除/修正。
    → 該当せず（実装は hidden 側が正しい）。
  - [x] 内部用途なら README Usage 側で内部/上級者向けである旨を明記、または削除。
    → `README.ja.md` の Usage ブロックから `print-schedule` を分離し、
    「隠しサブコマンドで通常運用では直接実行不要」である旨の注記を追加。
- **検証**:
  - [x] `docker_deployment` と `README` の記述が同じ位置づけを述べていること。
    → 両文書とも「隠し/内部用途、Docker cron 連携用」で統一。

### Step 4 (F-2): 節名参照の不一致修正

- **確認**:
  - [x] `docs/overview.md` の該当節の正式名称を確認
    （現状 "Development and Distribution Strategy"）。
    → 日本語版（`overview.ja.md` / `docker_deployment.ja.md`）は両方とも
    「開発・配布方針」で既に一致していた。矛盾は `/mktrans` 出力の英語版
    のみに存在（docker_deployment.md 側が "Deployment" と誤訳）。
- **修正**:
  - [ ] ~~`docker_deployment.ja.md`（および必要なら overview 側）で節名参照を一致させる。~~
    → 日本語版は変更不要（既に一致）。
  - [x] `docs/design/docker_deployment.md`（英語版）の "Development and
    Deployment Strategy" を overview.md の正式見出し "Development and
    Distribution Strategy" に修正（日本語側の変更を伴わない片側修正のため
    `/mktrans` 経由ではなく直接編集）。
- **検証**:
  - [x] 参照名と実際の見出しが一致すること。
    → `docker_deployment.md:11` と `overview.md:58` の文言が一致することを確認。

### Step 5 (F-4): README に `hostname` TOML フィールドを追記 ※優先度高

- **確認（実装と configuration.md が一次情報）**:
  - [x] `internal/config` の TOML 読み込み・`ResolveHostname` を読み、`hostname`
    の型・必須/任意・デフォルト（`os.Hostname()` フォールバック、空白のみは未設定扱い）
    を確認する。
    → `internal/config/hostname.go`（`ResolveHostname`）と `config.go:33-37,50`
    で確認: string、任意、`TrimSpace` 後空なら `os.Hostname()` にフォールバック。
  - [x] `configuration.md` の該当行（`hostname` 行）と実装が一致していることも確認。
    → `configuration.md:28` の記述が実装と一致。
- **修正**:
  - [x] `README.ja.md` の「TOML 設定ファイル」表に `hostname` 行を追加。
  - [x] README 内の TOML 例（2 箇所）に `hostname` を追記するか、任意項目である旨を注記。
    → 2箇所とも `hostname = "worker-1"` を追記（1箇所目はコメントで役割も説明）。
- **検証**:
  - [x] README の記述が `configuration.md` と実装の双方に矛盾しないこと。
    → 3者の記述内容(任意・`os.Hostname()`フォールバック・空白のみ未設定扱い)が一致することを確認。

### Step 6 (F-5 / F-6): 設計層への通知内容・CLI 契約の反映（任意・要判断 → ユーザー承認により実施）

- **確認**:
  - [x] 通知内容は Step 2 と同じく `internal/notify` 実装で確定。
    → Step 2 の確認結果を再利用（Host/Account/Targets/Deleted/Duration/失敗詳細）。
  - [x] CLI 契約は `cmd/` の Exit Code 実装（0/1/2/3 の分岐）と
    `docs/tasks/0004_cli_entrypoint` を参照して確定。
    → `cmd/main.go:57-60`（`exitOK=0`/`exitUsageError=2`/`exitSetupOrRunFail=1`/
    `exitPartialFailure=3`）と `0004_cli_entrypoint/01_requirements.md` の
    AC-03/AC-10/AC-11 を確認し、README の終了コード表と一致することを確認。
- **修正**:
  - [x] `overview.ja.md` に通知メッセージ内容の要約小節を追加（task 文書非依存化）。
    → 「実行結果の通知」節下に「### 通知メッセージの内容」を新設。
  - [x] Exit Code / partial-failure の意味を設計層（overview または新規小節）に反映。
    → 新設した「## CLI 契約」節に終了コードの意味づけ（設計判断の理由）を追加。
    具体的なフラグ・コマンド例は重複させず README への参照に留めた。
- **検証**:
  - [x] README の CLI 記述と設計層の記述が一致すること。
    → 終了コード 0/1/2/3 の意味、部分的失敗時も削除処理を継続する点が
    README・overview.ja.md・実装の三者で一致することを確認。

### Step 7 (F-7): リトライ数値の重複整理（任意・要判断 → ユーザー承認により実施）

- **確認**:
  - [x] リトライ既定値の一次情報を特定する
    （`internal/retry` / `internal/atproto` / `internal/notify` の実装値）。
    → `internal/atproto/client.go:25-29`: `MaxRetries=5, BaseDelay=1s,
    MaxDelay=30s`（1+2+4+8+16=31秒、ドキュメント記載と一致）。
    `internal/notify/notify.go:60-64`: `MaxRetries=2, BaseDelay=1s,
    MaxDelay=4s`（slack_notification_security.ja.md の記載と一致）。
    `internal/notify/notify.go:54`: `defaultRequestTimeout=3s`
    （試行ごとのタイムアウト。configuration.md の「3秒×最大3試行+1秒+2秒
    ≒12秒」の内訳と整合）。数値そのものに誤りは無かった。
- **修正**:
  - [x] 数値の正本を1箇所（`configuration.md` を推奨）に集約し、他は相互参照へ置換。
    → `configuration.ja.md` は既に 0005/0006 のアーキテクチャ設計書を一次情報
    として引用する形で正本の役割を果たしていたため変更不要。
    `overview.ja.md`「リトライ方針」節の重複していた具体数値
    （5回/1秒/30秒/約31秒）を削除し、`configuration.ja.md` への参照のみに変更。
    `slack_notification_security.ja.md` はこれらの数値の実装詳細（Slack 通知
    リトライの主要な記述場所）であり重複ではないため変更なし。
- **検証**:
  - [x] overview / configuration / slack_notification_security の数値が実装と一致。
    → 上記確認で3文書とも実装値と一致することを確認済み。overview は数値を
    削除し参照のみとしたため、今後の実装変更時に更新すべき箇所が
    `configuration.md`/`slack_notification_security.md` の2箇所に限定された。

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
  - [x] Step 1〜7 で洗い出した該当文書の記述漏れ・矛盾・陳腐化がすべて解消済み、
    または §6 に「対応不要」の理由とともに記録済みであることを確認する。
    → F-1〜F-9 すべて解消済み（Step 1〜7 参照）。
  - [x] 各文書が言及する実装（`internal/config`、`internal/notify`、
    `internal/retry`、Docker/cron 関連ファイル等）を再度確認し、現在の実装
    挙動と文書の記述が一致していることを確認する（Step 2/3/5/7 の確認結果を
    再利用可）。
    → Step 2/3/5/6/7 の確認結果を再利用。
  - [x] `docs/tasks/` 配下で当該設計文書に関連する未完了・未承認タスクが
    無いことを確認する（`grep -rl "Status.*draft" docs/tasks/*/0[1-3]_*.md` 等）。
    関連タスクが `draft` のままなら、その設計文書を `Final` にしてよいか
    Reviewer に確認する。
    → `docs/tasks/0000_template/*` 以外に draft の実タスク文書なし
    （0001〜0016 はすべて approved）。ブロッカーなしと判断。
- **修正**:
  - [x] 上記 5 文書（日本語版を先に編集）の `ステータス` を `Draft` → `Final` に、
    `最終更新日`（`Last updated`）を作業実施日（2026-07-12）に更新する。
    （`configuration.ja.md`/`docker_deployment.ja.md`/`security.ja.md`/
    `slack_notification_security.ja.md` は「最終更新日」欄が無かったため
    新設した。）
  - [x] 英語版は `/mktrans` による差分翻訳で同期する（Step 9 で実施、個別に
    手編集しない）。
- **検証**:
  - [x] 変更後、全5文書の英日ペアで `Status`/`ステータス` が `Final` に統一
    されていること、日付が一致していることを目視確認する。
    → 日本語版5文書すべて `ステータス: Final`、`最終更新日: 2026-07-12` を
    確認。英語版5文書も `/mktrans`（Step 9）経由で同期し、`Status: Final`、
    `Last updated: 2026-07-12` を確認。

参考: この `Final` は `docs/tasks/XXXX_feature/` の `01_requirements.md` 等で
使われる `draft`/`approved` ワークフロー（`requirements_process.md`）とは
別の語彙・別の文書種別（設計リファレンス文書）である。混同しないこと。

### Step 9: バイリンガル同期と最終確認

- [x] 日本語版（`*.ja.md`）の編集を**コミット**する。
  → `04ce63f`（README.ja.md / overview.ja.md / design/*.ja.md 6ファイル）。
- [x] 各日本語ファイルに対し `/mktrans <path-to.ja.md>` を実行し英語版へ差分翻訳、
      翻訳結果を別コミットする（英日を同一コミットで手編集しない）。
      → 6ファイル分を個別コミット（`8d1a4ca` README.md、`166a3a4` overview.md、
      `9689198` configuration.md、`bfa2ec5` docker_deployment.md、
      `caba800` security.md、`7af7082` slack_notification_security.md）。
      各差分翻訳は critical-review サブエージェントでレビュー済み
      （Critical/Major issue なし）。新規用語集追加は不要だった。
- [x] `mermaid_reference.md` 等の英語のみ文書は直接編集済みであることを確認。
      → `bdf8a66` で mermaid_reference.md（Step 1）と docker_deployment.md
      の英語側単独修正（Step 4）をコミット済み。
- [x] `make test` / `make lint`（ドキュメントのみだが CI 相当を通す）を実行。
      → 両方 green（`make test` 全パッケージ ok、`make lint` 0 issues）。
- [x] 変更した全ファイルのリンク切れ・相互参照を最終目視確認。
      → Python スクリプトで全変更ファイルの相対リンクを走査し、実在しない
      パスが無いことを確認（ディレクトリ参照1件を検出したが破損ではない）。
      アンカーリンク（README.md#usage、configuration.md#notes-on-setting-...、
      slack_notification_security.md#restriction-of-information-...）は
      レビューサブエージェントが実見出しとの一致を個別確認済み。

## 5. 完了条件（Definition of Done）

- [x] F-1〜F-9 のうち実施対象とした項目が、すべて実装または公式情報での確認を
      経て修正されている。
- [x] F-10: 対象5文書（英日ペア計10ファイル）の `Status` が `Final` に更新され、
      更新前に内容の正確性が一次情報で確認されている。
- [x] 実施しないと判断した項目は §6 に理由付きで記録されている（該当なし）。
- [x] 英日ペア文書の日本語・英語版が `/mktrans` 経由で同期されている。
- [x] ドキュメント間の相互参照（節名・ファイルパス）が整合している。

## 6. 未解決事項 / 実施しない項目

（作業中に、一次情報で確認できなかった事項や、Reviewer 判断で見送った項目を
理由とともにここへ追記する。）

- （なし）
