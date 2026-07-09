# PR #120 vs PR #121 品質比較評価レポート

- **対象**: 同一実装計画書 `docs/tasks/0012_slack_rich_formatting/03_implementation_plan.md`（PR-1: フェーズ1〜6）の 2 実装
- **PR #120**: `issei/notification-style-03` — *feat(0012): add color-coded attachments to slack payload*
- **PR #121**: `issei/notification-style-03a` — *feat(notify): restructure Slack payload with emoji-prefixed summary, color-coded attachments, and structured failure fields*
- **評価日**: 2026-07-10
- **検証**: 両ブランチを worktree に展開し `make test` / `make lint` / `gofmt -l` を実測（両者とも全ゲート通過・0 issues・フォーマット差分なし）

---

## 0. サマリ

本番コードの中核ロジック（`notify.go` の差分、`buildPayload` の分岐と出力、サニタイズ・切り詰めの適用箇所）は**機能的に完全に等価**であり、両者ともテスト・lint・fmt を通過する。したがって優劣は二次的な品質要因に集約される。

- **PR #120 の強み**: 実装計画書の進捗管理が徹底的かつ誠実（`[x]`/`[-]` の使い分け）、`const` のグループ化、`//nolint` コメントが明快、PR リンクによるトレーサビリティ。
- **PR #121 の強み**: 本番コードの記述がやや簡潔（`fmt.Sprintf` 直代入）、`maxPayloadLength`/`truncatedMarker` の陳腐化コメントを更新、既存テストの説明コメントを温存、関数配置の凝集性。
- **総合**: 僅差で **PR #120** が上位。決め手はプロセス遵守（進捗トレーサビリティ）と知的誠実さ。PR #121 は本番コードの細部が僅かに洗練されているが、`test_helpers.go` の `//nolint` コメント破損という明確な瑕疵を持つ。

| | PR #120 | PR #121 |
|---|:---:|:---:|
| **総合スコア（加重）** | **8.9 / 10** | **8.7 / 10** |

---

## 1. 評価軸別スコア

| 評価軸 | 重み | #120 | #121 | 判定根拠 |
|---|:---:|:---:|:---:|---|
| 機能的正しさ・完成度 | 25% | 9.5 | 9.5 | 中核ロジック等価。両者とも全ゲート通過。実質的に同点 |
| 本番コードの明快さ・Go イディオム | 20% | 8.5 | 9.0 | #121 は単一行 `text` を `fmt.Sprintf` で直代入し `strings.Builder` の無駄がない。#120 は `const (...)` グループ化が定石 |
| コード内コメント/ドキュメント正確性 | 15% | 8.0 | 8.5 | #121 は陳腐化コメントを更新（＋）。#120 はテスト説明コメントを削除（−）。ただし #121 は `//nolint` コメントを破損（−） |
| テスト品質 | 15% | 9.0 | 9.0 | テスト本体はほぼ同一。差は説明コメントの扱いと文字列リテラル表現のトレードオフで相殺 |
| 実装計画書の進捗管理・トレーサビリティ | 15% | 9.0 | 6.5 | #120 は 35 個の `[x]` ＋ 2 個の誠実な `[-]`。#121 はステップ単位のチェックを未実施（`[ ]` が 63 個） |
| PR 記述・レビュー容易性 | 10% | 9.0 | 9.0 | 双方とも AC 対応のレビュー観点を明記。言語（英/日）の違いのみ |
| **加重合計** | | **8.9** | **8.7** | |

---

## 2. 詳細比較

### 2.1 本番コード（`internal/notify/payload.go`）

**同等な点**: 型定義 `webhookPayload`/`slackAttachment`/`slackField` は docstring まで含めてほぼ同一。`isFailure`/`colorFor` の抽出、`buildPayload` の 3 分岐、絵文字プレフィックス、常時 1 件の色付き attachment、失敗一覧フィールド、`text`/`Value` への独立切り詰めも等価。`notify.go` の差分は完全一致。

| 観点 | PR #120 | PR #121 | 優位 |
|---|---|---|:---:|
| emoji/color 定数 | `const (...)` 1 ブロックにまとめる（定石） | 4 個の個別 `const`＋各コメント（冗長だが説明的） | #120 |
| `text` 組み立て | `strings.Builder`＋`fmt.Fprintf`→`b.String()`（単一行なのに Builder 経由でやや冗長。旧構造の残滓） | `fmt.Sprintf` を直接代入（簡潔） | #121 |
| 失敗一覧の組み立て | `fmt.Fprintf(&b, "%s: %s", ...)` | `WriteString` 3 連（僅かに高速・読みやすさは同等） | ほぼ同 |
| 関数配置 | `buildPayload` を中盤に維持、`truncate` を直後に | `truncate` を `truncationCutPoint` の隣へ、`buildPayload` を末尾へ（切り詰め処理が凝集、上→下で読める） | #121（主観） |
| `maxPayloadLength`/`truncatedMarker` の陳腐化コメント | **未更新**（"the constructed Slack message text" / "buildPayload's output exceeds" のまま。実際はフィールド単位切り詰めに変化） | 新セマンティクスへ**更新**（"a single Slack text field (text or attachment field Value)" 等） | #121 |
| `failed` の再利用 | `colorFor(isFailure(outcome))` をインライン | `failed := isFailure(outcome)` を先頭で束縛し再利用 | 僅差 #121 |

### 2.2 テストヘルパー（`internal/notify/test_helpers.go`）

- **PR #120**: `//nolint:revive` を理由付きで 1 行にまとめ、直前行に正しく配置。明快。
- **PR #121**: `//nolint` の理由文が**分割・逆順**になっている（"...never constructs one itself." が directive の上に浮き、"...notifypreview only" が directive 行末に来る）。lint 抑止は機能するが、コメントとして読むと文が逆順で読みにくい **明確な瑕疵**。

→ この軸は **#120 が明確に優位**。

### 2.3 テスト（`internal/notify/payload_test.go`）

- 追加テスト（`TestIsFailure_FourOutcomePatterns`, `TestBuildPayload_TextTruncates...`）とアサーションはほぼ同一。
- **#121 の利点**: `TestBuildPayload_RunError_EscapesMentionSyntaxInSSRFErrorEndpoint` と `TestBuildPayload_TruncationIsUTF8Safe` の説明コメントを**温存**。`isFailure` の `result_nil_and_err_nil` ケースに runner 契約への言及コメントを追加。
- **#120 の欠点**: 上記 2 テストの説明コメントを**削除**（意図の喪失）。
- **トレードオフ**: #121 は UTF-8 テストの日本語リテラルを `"日..."` エスケープに変換。CLAUDE.md「文字列リテラルは英語」への準拠度は上がるが、可読性は低下（何の文字列か読めない）。#120 は `"日本語のリキー識別子です"` を維持し可読だがルール逸脱。ほぼ相殺。

→ 総合ほぼ同点（説明コメント温存で僅かに #121 有利、リテラル可読性で僅かに #120 有利）。

### 2.4 実装計画書（`03_implementation_plan.md`）— 最大の差

| 観点 | PR #120 | PR #121 |
|---|---|---|
| ステップ単位の進捗 | フェーズ1〜6 の各項目を `[x]` でチェック（35 個） | **未チェック**（`[x]` は PR-1 のグリーンゲート／PR 作成の 2 個のみ、`[ ]` が 63 個） |
| 誠実さ | 一括コミットのため個別検証できなかった 2 つの中間コンパイルチェックを `[-]`＋注記で明示（「最終状態で緑を確認済み」） | 該当注記なし |
| PR トレーサビリティ | `[x] PR を作成した（#120）` とリンク | `[x] PR を作成した`（**リンクなし**） |
| PR 境界の提示 | §3.2「PR 構成」表＋各「作成ポイント」節（一目で PR↔フェーズ対応が分かる） | 「PR 構成」表を削除し「作成ポイント」節に集約（マイルストーンを M1〜M5 のフェーズ対で再構成） |

要件プロセス（`requirements_process.md`）は「チェックボックスによる進捗トラッキング」を明示的に求めており、この観点では **#120 が明確に忠実**。とりわけ #120 が「実施できなかった検証」を隠さず `[-]` で残した点は知的誠実さとして高評価。

### 2.5 PR 記述

- **#120**: 英語。フェーズ／AC への紐付けが詳細。
- **#121**: 日本語。「変更内容」＋「レビュー観点」を AC 対応で構造化し、レビュアー向けに実用的。

→ いずれも高品質。言語の差のみで実質同点。

---

## 3. 結論と推奨

**総合では PR #120 を僅差で推奨**する。本番コードの機能は完全等価であり、両者とも品質ゲートを通過するため、差は以下に集約される:

- **#120 を選ぶ理由**: 実装計画書の進捗トレーサビリティが徹底的かつ誠実（`[-]` による正直な未検証明示）、`//nolint` コメントが明快、PR リンクあり、`const` のグループ化。
- **#121 を選ぶ理由**: 本番コードが僅かに洗練（`fmt.Sprintf` 直代入、陳腐化コメントの更新、関数配置の凝集）、テスト説明コメントの温存。

### 推奨: 「#120 ベース＋#121 の良い部分の取り込み」

いずれをマージするにせよ、以下は反映すべき:

1. **[#121 → 採用]** `maxPayloadLength` / `truncatedMarker` の docstring を新セマンティクス（フィールド単位切り詰め）に更新する（#120 は陳腐化したまま）。
2. **[#121 → 採用]** `TestBuildPayload_RunError_EscapesMentionSyntaxInSSRFErrorEndpoint` と `TestBuildPayload_TruncationIsUTF8Safe` の説明コメントを残す（#120 は削除済み）。
3. **[#121 → 採用]** 単一行 `text` を `fmt.Sprintf` 直代入にして `strings.Builder` の無駄を省く。
4. **[#121 → 修正必須]** `test_helpers.go` の `//nolint` コメントの分割・逆順を #120 のような 1 行の明快な形に直す。
5. **[#121 → 補完]** 実装計画書のステップ単位チェックと PR リンクを #120 並みに補う。
6. **[任意]** UTF-8 テストのリテラルは、`\u` エスケープ（ルール準拠・低可読）か日本語リテラル（高可読・ルール逸脱）かをチームで一本化する。
