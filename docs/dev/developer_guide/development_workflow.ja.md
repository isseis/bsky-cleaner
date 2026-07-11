# 開発ワークフロー

このページは `docs/dev/developer_guide/` の入り口となる文書です。各文書が何を
扱うか、どの順番で読むべきか、要件定義 → 設計 → 実装計画 → PR のプロセスが
実際にはコマンド単位でどう進むか、各アシスタント向けにどの AI コマンドが存在
するか、そして日英ドキュメントをどう同期させるかをまとめています。まずこの
ページを読んでください。他のガイドはここで参照される個々のトピックの詳細を
扱います。

## 1. ドキュメント一覧と読む順序

| # | ドキュメント | 読むタイミング |
|---|----------|-----------|
| 1 | [requirements_process.md](requirements_process.md) | 新機能やセキュリティ関連の変更に着手する前 — `01_requirements.md` / `02_architecture.md` / `03_implementation_plan.md` の構成、承認ゲート、受け入れ基準のトレーサビリティを定義している。 |
| 2 | [task_identification.md](task_identification.md) | あなたや AI コマンドが、コマンドの対象となる `docs/tasks/XXXX_feature/` ディレクトリを特定する必要があるとき。 |
| 3 | [mermaid_reference.md](mermaid_reference.md) | `02_architecture.md` やその他の設計文書で図を描くとき。 |
| 4 | [test_organization.md](test_organization.md) | 実装中にテストヘルパーファイルを書く、あるいは配置するとき。 |
| 5 | [package_reference.md](package_reference.md) | 新しいコードを追加する前に、現在の `cmd/`/`internal/` パッケージ構成を確認したいとき。 |
| 6 | [build_from_source.md](build_from_source.md) | リリース版や Docker イメージを使わず、ローカルでバイナリをビルドするとき。 |
| 7 | [notify_preview.md](notify_preview.md) | Slack 通知のフォーマットを実装していて、パイプライン全体を実行する前にメッセージをプレビューしたいとき。 |

この文書（development_workflow.md）だけが、開発プロセス全体を通しで説明する
文書です。他の文書はここから必要に応じて参照されます。

## 2. 開発プロセスの流れ

各機能追加やセキュリティ関連の変更は、`docs/tasks/XXXX_feature/` 配下の 3 つの
ゲート文書を経てから実装、PR レビューへと進みます。このフローにおける
`draft → approved` の遷移は必ず**人間**が行います — AI コマンドが文書のステ
ータスを自ら `approved` にすることは許されません（[requirements_process.md](requirements_process.md)
§ 0 を参照）。

1. **`01_requirements.md` を書く**（ステータス: `draft`）。
   専用コマンドはありません — [docs/tasks/0000_template/01_requirements.md](../../tasks/0000_template/01_requirements.md)
   のテンプレートを起点に、新しい `docs/tasks/XXXX_feature/` ディレクトリへ
   コピーし、Claude と会話の中で仕様を議論した上で、その議論をもとに要件定義
   書を作成させます。このステップは本質的に対話的な作業であるため、専用の
   スラッシュコマンドがありません。
   → 人間がレビューし、ステータスを `approved` に設定する。

2. **`/mkarch`** — 承認済みの要件定義書から `02_architecture.md`（ステータス:
   `draft`）を生成する。
   → 人間がレビューし、ステータスを `approved` に設定する。

3. **`/mkplan`** — 承認済みのアーキテクチャ設計書から `03_implementation_plan.md`
   （ステータス: `draft`）を生成する。
   → 人間がレビューし、ステータスを `approved` に設定する。

4. **`/mkplan2`** — `03_implementation_plan.md` に `### PR-N 作成ポイント` の
   マーカーを挿入し、ステップをレビュー可能な PR 単位にグループ化する。1 つ
   の計画につき 1 回実行し、ステップ 3 の文書が承認された後、ステップ 5 の最初
   の `/runplan` 呼び出しより前に行う — 1 つの PR がどこで終わり、次の PR が
   どこから始まるかを決めるコマンドです。

5. **`/runplan`** — 1 つの `PR-N` グループを実装する: コードとテストを書き、
   グリーンゲート（`fmt`/`test`/`lint`/`deadcode`）を実行し、PR を作成する
   （`gh pr create`）。
   - 1 回の呼び出しは 1 つの `PR-N` グループに限定される。3〜6 個の PR グル
     ープ（`/mkplan2` が目安とするサイズ）を持つ計画であれば、`/runplan` も
     3〜6 回呼び出すことになり、その都度以下のステップ 5〜7 を繰り返す。
   - 既定: Claude（Sonnet）が `/runplan` を実行する。
   - 代替: このステップは Cline で実行することもできる — `Execute
     @.cline/commands/runplan.md`（§ 3 の「呼び出し方法」を参照）— 通常は
     より小さい／安価なモデルで実行される。Cline の `runplan.md` は、別の
     レビュー用サブエージェントに委譲するのではなく会話内で自己レビューを
     行うため、Cline でこのステップを実行した場合は、PR をマージする前に
     必ず Claude の `/weakreview`（下記ステップ 5a）を続けて実行する必要が
     あります。

   5a. **（Cline 経路のみ）Claude で `/weakreview`** — 実行モデルの能力が低い
       場合に見落とされがちな、判断力を要する誤りを狙い撃ちする第二段階の
       レビューであり、`/code-review`/`/simplify` が特に対象としない観点を
       扱う（詳細はコマンドファイルのチェックリスト全体、および § 3 の
       「推奨: Cline の `/runplan` 実行後に Claude の `/weakreview` を実行
       する」を参照）。Claude 主導で `/runplan` を実行した場合でも、実行に
       能力の低いモデルを使ったのであれば任意で利用できます。

6. **PR レビューループ。** 人間（またはレビュアー）が PR にコメントを残す。
   - コメントを直接修正するか、`/fixpr` を実行して PR の未解決レビュースレ
     ッドを解消する（取得 → トリアージ → 修正 → 返信 → 再確認）。手動で対応
     したいコメントには `/fixpr` は不要。
   - PR が承認されるまで繰り返す。

7. **マージして続行。** PR をマージし、新しいブランチに切り替えて、次の
   `PR-N` グループについてステップ 5 に戻る — `03_implementation_plan.md` が
   すべて実装し終わるまで続ける。

## 3. AI コマンド一覧（Claude と Cline）

コマンドは `.claude/commands/`（Claude Code）と `.cline/commands/`（Cline）の
配下に置かれています。どちらも共通のプロジェクト設定をそれぞれの
`_context.md` から読み込みますが、現時点では Cline のコマンドセットの方が
少ないため、両方のツールで利用可能だと決めつける前に下の表を確認してくだ
さい。

| コマンド | Claude | Cline | 目的 |
|---------|:---:|:---:|---------|
| `mkarch` | ✅ | ❌ | 承認済みの `01_requirements.md` から `02_architecture.md` を生成する。 |
| `mkplan` | ✅ | ❌ | 承認済みの `02_architecture.md` から `03_implementation_plan.md` を生成する。 |
| `mkplan2` | ✅ | ❌ | 承認済みの `03_implementation_plan.md` に `PR-N` の境界マーカーを挿入する。 |
| `runplan` | ✅ | ✅ | 1 つの `PR-N` グループを実装する: コード、テスト、グリーンゲート、PR 作成。 |
| `fixpr` | ✅ | ❌ | PR の未解決レビュースレッドを取得・トリアージ・修正し、返信・再確認まで行う。 |
| `mktrans` | ✅ | ✅ | bilingual なドキュメントペア（`.md` ⇄ `.ja.md`）を全文または差分翻訳する。 |
| `japrose` | ✅ | ❌ | 日本語が主言語の文書について、内容を変えずに日本語の文章表現を改善する。 |
| `weakreview` | ✅ | ❌ | 実行モデルの能力が低い場合に典型的な誤りを狙い撃ちする第二段階のレビュー。 |

コマンドを Cline へ移植する際は、Claude 版のコマンドファイルに埋め込まれて
いる移植メモに従い（例: `mkplan2.md` は Go/`cmd`/`internal` 固有のどの部分を
調整すべきか明記している）、共有のプロジェクトコンテキスト値を
`.cline/commands/_context.md` に追加してください。

### 呼び出し方法

2 つのツールはコマンドの呼び出し方が異なります — コマンドの*内容*は
`.claude/commands/` と `.cline/commands/` の間で共有・移植されることがあり
ますが、呼び出し方自体はツールごとに異なります。

- **Claude Code**: タスク ID を引数にしたスラッシュコマンド。例:
  ```
  /mkarch 0001
  /runplan 0001
  ```
- **Cline**: コマンドファイルを直接 `Execute` し、タスク ID を引数として渡
  す。例:
  ```
  Execute @.cline/commands/runplan.md 0001
  ```
  （Cline にはスラッシュコマンドの登録機構がありません。
  `@.cline/commands/<name>.md` はファイル参照であり、`Execute` はそのファイ
  ルの内容を実行すべき指示として扱うよう Cline に伝えます。パスの後に続く
  部分は、Claude のスラッシュコマンドの引数と同様に `$ARGUMENTS`/`$1` 形式
  の入力として渡されます。）

### 推奨: Cline の `/runplan` 実行後に Claude の `/weakreview` を実行する

Cline の `runplan.md` は、別のレビュー用サブエージェントに委譲するのではな
く（`.cline/commands/_lib/review-self-pattern.md` を通じて）会話内で自己レ
ビューを行い、また通常は Claude の既定モデルより小さい／安価なモデルで実行
されます。そのため、Cline の `runplan` セッションが 1 つの `PR-N` グループ
を完了させたら（PR を作成する前後どちらでもよい）、より強力なモデルが弱い
実行モデルにありがちな判断力を要する誤りを拾えるよう、同じ範囲／PR に対して
Claude の `/weakreview` コマンドを実行してください（§ 2 の `/weakreview` に
関する補足、および `/code-review`/`/simplify` とは別のパスである理由につい
てはコマンドファイル自体の説明を参照）:

```
/weakreview 0001          # または PR 番号 / コミット範囲
```

これは必須のゲートではなく推奨事項です — 実装セッションで能力の低いモデル
を使った場合は、どちらのツールで実行したかに関わらず利用してください。

## 4. 日英ドキュメントの扱い

一部の文書は英語/日本語のペア（`foo.md` + `foo.ja.md`）として存在します —
現時点では `README`、`docs/overview`、`docs/design/*`、そしてこの文書
（`development_workflow.md` / `development_workflow.ja.md`）です。
`docs/dev/developer_guide/` のそれ以外の文書と `docs/tasks/XXXX_feature/`
配下のすべての文書は bilingual なペア**ではありません**: 他の開発者ガイドは
英語のみであり、タスク文書（`01_requirements.md`/`02_architecture.md`/
`03_implementation_plan.md`）は翻訳対応物を持たない日本語主体の作業文書です。

実際に bilingual なペアである文書については、**片方の言語を先に更新し、その
後 `/mktrans` で翻訳する**というルールに従います。

1. 原本となる言語版を直接編集する（どちらの方向が各文書の正本かは `CLAUDE.md`
   の[Translation Guidelines](../../../CLAUDE.md#translation-guidelines-japanese-to-english)
   を参照 — 一般的には日本語版を先に作成・更新し、その後英語へ翻訳する）。
2. 原本言語での変更をコミットする。
3. `/mktrans <path-to-source-file>` を実行する。ファイル拡張子から翻訳方向を
   判定し（`*.ja.md` が原本なら出力は英語、`*.md` が原本なら出力は
   `*.ja.md`）、出力ファイルがまだ存在しなければ全文翻訳、既に存在すれば
   （出力ファイルを最後に更新したコミットに対する `git diff` による）差分の
   みの翻訳を行い、翻訳者ペルソナによるレビューパスを実行した上で、翻訳ファ
   イル（と用語集の更新があればそれ）を別々にコミットする。
4. `/mktrans` を介さずに両方の言語版を同一コミットで手編集しないこと — それ
   は差分翻訳の同期点（出力ファイルに対する `git log`）を壊し、コマンドが
   何が変更されたかを判定できなくなる。

翻訳中に新しく登場した用語は `/mktrans` によって自動的に
`docs/translation_glossary.md` に追加されます。`/japrose` もこの用語集を参
照し、日本語主体の文書（タスク文書やその他の日本語ガイド）の用語の一貫性を
保ちますが、翻訳自体は行いません。
