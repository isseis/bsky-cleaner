# ラップされた永続エラーのリトライ抑止 — 要件定義書

## Document Status

| Item | Value |
|---|---|
| Status | `approved` |
| Created | 2026-07-11 |
| Review date | 2026-07-12 |
| Reviewer | isseis |
| Comments | - |

## 1. 背景と目的

### 1.1 背景

`internal/retry` の `Doer`（[0005_retry_timeout](../0005_retry_timeout/01_requirements.md)）は、一過性の失敗（トランスポートエラー・HTTP 429・5xx）を指数バックオフでリトライする一方、非公開インターフェース `permanentError`（`Permanent() bool`）を満たすエラーは「リトライしても解決しない永続的な失敗」として即座にリトライを打ち切る。現在このインターフェースを実装するのは `*atproto.SSRFError`（`Permanent()` は常に `true`、`internal/atproto/errors.go`）のみで、SSRF 拒否（意図しないホストへの接続の拒否）は再試行しても同じ検証で同じく拒否されるため、リトライしない設計になっている。

しかし `classify`（`internal/retry/doer.go`）が永続判定に用いているのは、エラーチェーンの**トップレベル**だけを見る素の型アサーション（`doErr.(permanentError)`）である。このため、`*atproto.SSRFError` が別のエラーでラップされていると永続と判定されず、リトライ対象に分類されてしまう。

この差異は [0015_security_review_fixes](../0015_security_review_fixes/01_requirements.md) の F-002（DID 解決フェーズのリダイレクト拒否）の設計過程で顕在化した。`CheckRedirect` に設定した `rejectRedirect` が返す `*SSRFError` は、`http.Client.Do` によって `*url.Error` でラップされて返る。トップレベルは `*url.Error` であり `permanentError` を満たさないため、リダイレクト拒否は「リトライ可能」と分類され、`defaultRetryPolicy`（`MaxRetries: 5`）の回数だけ再試行されてから最終的に `*SSRFError` として浮上する。各試行とも `rejectRedirect` が追従を拒むため内部アドレスへは一度も接続されず、セキュリティ上の結果（SSRF の阻止）は保たれる。しかし外部から観測される挙動は「即座の中断」ではなく「リトライ予算（1+2+4+8+16 ≒ 31 秒）を消費してからの中断」となる。この待ち時間は DID 解決の2箇所（ハンドル解決・DID ドキュメント取得）で発生しうる。

[0015_security_review_fixes](../0015_security_review_fixes/01_requirements.md) はこの挙動を「事実として明示するに留め、`classify` の判定方法の変更は共有パッケージ `internal/retry` の契約変更を伴うためスコープ外」と判断した（同タスク 02_architecture.md §3.2・付録B）。本タスクはその積み残しを、`internal/retry` 単体を対象として個別に是正する。

### 1.2 目的（ゴール）

- エラーチェーンのどこかに永続エラー（`permanentError` を満たすエラー）が含まれる場合、それがラップされていてもリトライを打ち切る。
- これにより、ラップされた `*atproto.SSRFError` が無駄なリトライ予算を消費せず、速やかに呼び出し元へ伝播する。これは `CheckRedirect` によるリダイレクト拒否（`SSRFStageDialRevalidation`）だけでなく、`DialContext` ラッパーが検証済みアドレス集合外への接続を拒否して返す `*atproto.SSRFError`（同じく `*url.Error` でラップされる）にも同様に適用される。
- 永続エラーを含まないエラー（トランスポート失敗、DNS 一時エラー等）の既存のリトライ挙動は一切変えない。

## 2. スコープ

### In Scope

- **F-001**: `internal/retry` の永続エラー判定を、エラーチェーン全体を走査する方式へ変更する。

### Out of Scope

- `permanentError` インターフェース自体の定義変更（メソッド追加、`error` の埋め込み等）— 判定側の走査方法のみを変更し、インターフェースの契約（`Permanent() bool`）は保つ。
- `*atproto.SSRFError` 以外に `permanentError` を実装するエラー型の新設 — 本タスクは既存の唯一の実装に対する判定漏れを塞ぐものであり、新たな永続エラーの導入は行わない。
- リトライ方針（`MaxRetries`・`BaseDelay`・`MaxDelay`・`Retry-After` 解釈）の変更 — [0005_retry_timeout](../0005_retry_timeout/01_requirements.md) および [0015_security_review_fixes](../0015_security_review_fixes/01_requirements.md) F-005 の責務であり、本タスクは永続判定の走査範囲のみを扱う。
- [0015_security_review_fixes](../0015_security_review_fixes/01_requirements.md) 側のドキュメント（`02_architecture.md` の「リトライ枯渇後に浮上」という記述）の追随更新 — 本タスク完了後に別途反映すべき事項であり、本タスクのコード変更の受け入れ基準には含めない（6章参照）。

## 3. 機能要件と受け入れ基準

### F-001: エラーチェーン全体を対象とした永続エラー判定

`internal/retry` の `Doer` が、`Do` の結果として返されたエラーについて、そのチェーンを先頭からたどって最初に `permanentError` を満たすエラーを見つけ、その `Permanent()` が `true` を返す場合、当該エラーをリトライせずにそのまま呼び出し元へ返す（`errors.As` / `errors.AsType[permanentError]` の「チェーン先頭側から最初にマッチした1件を返す」意味論に合わせる）。

> **判定の一意性について**: 「最初にマッチした1件」で判定を確定させて問題ないのは、`permanentError` を実装するエラー型が現状 `*atproto.SSRFError`（`Permanent()` は常に `true`）1つだけであり（2章 Out of Scope 参照）、1つのチェーン内に `Permanent()` が異なる複数の永続エラー実装が同居する状況が存在しないためである。将来 `Permanent()` が `false` を返し得る実装や、`Permanent()` の値が異なる実装をチェーンに複数含み得る状況を導入する場合は、この「最初の1件で確定」という判定方針の見直しが必要になる。

**Acceptance Criteria**:
- **AC-01**: トップレベルが直接 `permanentError` を満たす（かつ `Permanent()` が `true` の）エラーは、従来どおりリトライされずに返る（既存挙動の維持）。
- **AC-02**: `permanentError` を満たすエラーが `fmt.Errorf("...: %w", ...)` や `*url.Error` 等で1段以上ラップされていても、リトライされずに返る。リトライ回数は1回（初回試行のみ）であり、バックオフ待機（`Clock.Sleep`）は発生しない。
- **AC-03**: `permanentError` を満たすエラーをチェーンに含まないエラー（トランスポート失敗、DNS 一時エラー等）は、従来どおりリトライ対象として扱われる（永続判定の誤検知がない）。
- **AC-04**: 永続判定は、返されたエラーが `nil` の場合（HTTP レスポンスを受信した成功・非成功の両ケース）の分類には影響しない。2xx・401・非 429 4xx・429・5xx それぞれの既存のリトライ可否分類は変更されない。
- **AC-05**: 変更後も、`Do` はレスポンスとエラーを同時に非 `nil` で返さない（[0005_retry_timeout](../0005_retry_timeout/01_requirements.md) が定める `Doer.Do` の戻り値契約を維持する）。
- **AC-06**: 永続判定の仕組みを記述している `internal/retry/doer.go` の既存コメントが、変更後の実態（チェーン走査）と整合している。具体的には、`permanentError` インターフェース定義のコメントにある「plain type assertion」という記述、および `Doer` 型・`classify` のコメントにある永続エラー判定の説明を、「エラーチェーン全体（のどこか）を対象に判定する」旨へ更新する。`permanentError` を「呼び出し元がこのパッケージの型をインポートせずにオプトアウトできる」構造的型付けの利点を述べたコメントの趣旨は変更後も成立するため、そのまま残す。

## 4. 非機能要件

- **NF-001**: `make fmt`・`make test`・`make lint` が成功する。
- **NF-002**: 本タスクの修正は既存パッケージ内のバグ修正であり、新規の外部依存パッケージを追加しない。
- **NF-003**: `internal/retry` は標準ライブラリのみに依存する（[0005_retry_timeout](../0005_retry_timeout/01_requirements.md) NF）。本タスクも `internal/atproto` 等への依存を導入せず、`permanentError` の構造的型付けによる疎結合を保つ。
- **NF-004**: 修正は永続判定の走査範囲の変更（およびそれに伴う AC-06 のコメント整合）に限定し、無関係なリファクタリングを含めない（最小差分・YAGNI）。判定機構を記述したコメントの更新は「無関係なリファクタリング」ではなく、変更後の実態と食い違うコメントを正す最小差分の一部とみなす。
- **NF-005**: Go 1.26.2 以上でビルドできること。

## 5. スコープ外の根拠

- `permanentError` インターフェースの定義変更を避けるのは、`*atproto.SSRFError` が `internal/retry` をインポートせず構造的型付けだけでこのインターフェースを満たしている疎結合構造（`internal/atproto/errors.go` の該当コメント）を壊さないためである。判定側（`internal/retry`）の走査方法のみを変えれば目的を達成できるため、インターフェース側には手を入れない。
- [0015_security_review_fixes](../0015_security_review_fixes/01_requirements.md) のドキュメント追随を本タスクの受け入れ基準に含めないのは、コード修正（`internal/retry`）とドキュメント整合（`0015` の設計書）は別レビュー単位であり、本タスクの緑判定をドキュメント更新に依存させないためである。追随の要否は6章に記す。

## 6. 成功基準（要約）

- AC-01〜AC-06 が test/static で緑。
- ラップされた `*atproto.SSRFError`（リダイレクト拒否を含む）が、`internal/retry` の `Doer` によって初回試行で永続と判定され、バックオフ待機なしに呼び出し元へ伝播する状態。
- 本タスク完了後、[0015_security_review_fixes](../0015_security_review_fixes/01_requirements.md) の `02_architecture.md` §3.2・§6.1・付録B にある「リダイレクト拒否は `MaxRetries` 回リトライされてから浮上する」旨の記述が実態と食い違うため、別途これらの記述を「初回試行で永続と判定され即座に中断する」に更新する必要がある（本タスクのコード受け入れ基準には含めないが、完了時の申し送り事項とする）。
