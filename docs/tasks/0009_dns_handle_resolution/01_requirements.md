# DNS TXT によるハンドル解決 — 要件定義書

## Document Status

| Item | Value |
|---|---|
| Status | `draft` |
| Created | 2026-07-07 |
| Review date | - |
| Reviewer | - |
| Comments | - |

## 1. 背景と目的

### 1.1 背景

[0002_atproto_client](../0002_atproto_client/01_requirements.md) で実装したハンドル解決（`internal/atproto/did.go` の `resolveHandleToDID`）は、HTTPS well-known 方式（`GET https://{handle}/.well-known/atproto-did`）のみをサポートしている。AT Protocol の仕様上、ハンドルから DID への解決方式には HTTPS well-known 方式に加えて DNS TXT レコード方式（`_atproto.<handle>` TXT レコードに `did=<DID>` を設定する方式）があり、アカウントによってはこの DNS 方式のみで検証を行っているケース（HTTP サーバーを持たない、あるいは `/.well-known/atproto-did` を配信していないドメインをハンドルに使うケース）がある。

実際に、`BSKY_HANDLE=issei.org` で本ツールを実行したところ、`https://issei.org/.well-known/atproto-did` への接続がタイムアウトし続け（`WARN retrying HTTP request` を繰り返す）解決に失敗する事象が確認された。このハンドルが DNS TXT 方式のみで検証されている場合、現状の実装では原理的に解決不可能であり、DNS TXT 方式のサポートが必要である。

DID 解決ロジックは [0002_atproto_client](../0002_atproto_client/01_requirements.md) の AC-01〜AC-03 および [セキュリティ設計](../../design/security.md) にある通り、誤った実装が SSRF や意図しないホストへの認証情報送信につながりうるため、DNS 解決を追加する際も同水準の安全性を維持する必要がある。

### 1.2 目的（ゴール）

- DNS TXT レコード方式でのみハンドル検証を行っているアカウントも、本ツールでハンドルから DID を解決できる
- 既存の HTTPS well-known 方式のハンドル解決の挙動（成功パス・エラーパス双方）を壊さない
- 2つの解決方式を試行する際の優先順位・フォールバック条件が明確に定義されている
- DNS 応答内容の異常（複数レコード、不正な形式等）に対して fail-closed で処理される

## 2. スコープ

### In Scope

- **F-001**: `_atproto.<handle>` TXT レコードによる DID 解決
- **F-002**: DNS TXT 方式と HTTPS well-known 方式の優先順位・フォールバック制御
- **F-003**: DNS 解決結果の妥当性検証とエラーハンドリング

### Out of Scope

- DID ドキュメントの取得方法（`did:plc` / `did:web` の解決先切り替え）— 既存の `resolveDIDDocument` / `didDocumentURL` の対象であり、本タスクはハンドル→DID の解決のみを扱う（[0002_atproto_client](../0002_atproto_client/01_requirements.md) で実装済み、変更なし）
- DNS over HTTPS (DoH) やカスタム DNS リゾルバの指定など、名前解決基盤自体の変更 — 本タスクは標準ライブラリの DNS リゾルバ（`net.Resolver`）を用いた TXT レコード取得のみを対象とし、名前解決のトランスポート自体はスコープ外とする
- HTTP リクエストに対する既存のリトライ・指数バックオフ機構（[0005_retry_timeout](../0005_retry_timeout/01_requirements.md)）の変更 — DNS 解決の再試行方針は本タスクの中で別途定義する（4章 NF-004 参照）が、既存の HTTP 用リトライ実装自体は変更しない

## 3. 機能要件と受け入れ基準

### F-001: DNS TXT レコードによる DID 解決

ハンドル `<handle>` に対して `_atproto.<handle>` の TXT レコードを問い合わせ、`did=<DID>` という形式のレコードから DID を抽出する。

**Acceptance Criteria**:
- **AC-01**: 対象ハンドルの `_atproto.<handle>` に `did=did:plc:xxxx` 形式の TXT レコードが1件存在する場合、その DID を解決結果として取得できる
- **AC-02**: `_atproto.<handle>` に TXT レコードが存在しない場合、DNS TXT 方式の解決失敗として扱われる（エラーで処理全体を止めず、F-002 のフォールバックに委ねられる）
- **AC-03**: `_atproto.<handle>` の TXT レコードのうち `did=` プレフィックスを持たないものは無視され、`did=` プレフィックスを持つレコードのみが解決候補として扱われる

### F-002: 解決方式の優先順位とフォールバック

DNS TXT 方式と HTTPS well-known 方式の両方を用いて、AT Protocol の仕様に沿った優先順位でハンドルを解決する。

**Acceptance Criteria**:
- **AC-04**: DNS TXT 方式で一意な DID が解決できた場合、HTTPS well-known 方式への問い合わせは行わずにその DID を採用する
- **AC-05**: DNS TXT 方式が解決失敗（レコードなし、DNS エラー、タイムアウト等）した場合、HTTPS well-known 方式にフォールバックし、既存の [0002_atproto_client](../0002_atproto_client/01_requirements.md) AC-01〜AC-03 の挙動がそのまま維持される
- **AC-06**: 両方式とも解決に失敗した場合、呼び出し元に対して両方式の失敗を汲み取ったエラーが返り、処理が中断される（fail-closed）

### F-003: DNS 解決結果の妥当性検証とエラーハンドリング

不正・曖昧な DNS 応答を安全側に倒して処理する。

**Acceptance Criteria**:
- **AC-07**: `_atproto.<handle>` に `did=` プレフィックスを持つ TXT レコードが複数存在する場合（解決先が曖昧なケース）、DID を確定させずに DNS TXT 方式の解決失敗として扱う
- **AC-08**: DNS 解決処理で発生したエラー（NXDOMAIN、タイムアウト、その他リゾルバエラー）が、呼び出し元が `errors.Is` / `errors.AsType[T]` で判別可能な型付きエラーとして返る
- **AC-09**: DNS TXT 方式の解決処理は、[セキュリティ設計](../../design/security.md) のリスクカテゴリ（意図しないホストへの認証情報送信）に抵触しない。すなわち DNS 解決結果の DID から先の処理（DID ドキュメント取得、PDS エンドポイント決定）は既存の `resolveDIDDocument` / `validatePDSEndpoint` の安全性検証をそのまま経由し、DNS 解決を追加したことによる迂回経路が生まれない

## 4. 非機能要件

- **NF-001**: `make fmt`・`make test`・`make lint` が成功する。
- **NF-002**: DNS TXT レコードの問い合わせは Go 標準ライブラリ（`net.Resolver`）を用いた自作実装とし、新規の外部依存パッケージを追加しない。
- **NF-003**: DNS 問い合わせ部分はインターフェース化し、実際の名前解決を伴わない単体テストが可能であること。
- **NF-004**: DNS 問い合わせがハングした場合に処理全体がブロックされないよう、呼び出しにタイムアウトが設定されていること（具体的な秒数・リトライ有無はアーキテクチャ設計で決定する）。
- **NF-005**: Go 1.26.2 以上でビルドできること。

## 5. スコープ外の根拠

- DID ドキュメント取得方法は既に [0002_atproto_client](../0002_atproto_client/01_requirements.md) で実装済みであり、本タスクが追加するのはハンドル→DID の解決経路の一つであって、DID→PDS エンドポイントの解決には手を入れない。
- DoH やカスタムリゾルバ対応は、現時点で解決すべき課題（DNS TXT 方式そのものが未実装であること）に対して過剰なスコープであり、YAGNI の観点から見送る。必要になった時点で別タスクとする。
- HTTP 用リトライ機構は [0005_retry_timeout](../0005_retry_timeout/01_requirements.md) の責務であり、DNS 解決の再試行はその横流用ではなく本タスクのアーキテクチャ設計で個別に検討する（DNS ルックアップは HTTP リクエストではなく `retry.Doer` の対象外であるため）。

## 6. 成功基準（要約）

- AC-01〜AC-09 が test/static で緑。
- `BSKY_HANDLE` に DNS TXT 方式のみで検証されたハンドルを指定した場合でも、DID 解決が成功し、以降の PDS エンドポイント決定・API 呼び出しに進める状態。
