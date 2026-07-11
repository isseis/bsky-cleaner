# セキュリティレビュー指摘事項の修正 — 要件定義書

## Document Status

| Item | Value |
|---|---|
| Status | `approved` |
| Created | 2026-07-11 |
| Review date | 2026-07-11 |
| Reviewer | isseis |
| Comments | - |

## 1. 背景と目的

### 1.1 背景

本タスクに先立ち、リポジトリ全体（`cmd/`・`internal/` 配下の全 Go ソース、`Dockerfile`、`entrypoint.sh`）を対象にセキュリティレビューとエッジケース調査を実施した。全体としては [0002_atproto_client](../0002_atproto_client/01_requirements.md) 以降で積み上げてきた SSRF 対策・シークレットマスキング・fail-closed 設計・入力サニタイズ・ページネーション上限といった安全策は妥当に機能しているが、その調査の中で以下の欠陥が見つかった。

1. **リポスト削除が誤ったコレクションに対して実行される（重大）**: `internal/atproto/delete.go` の `DeleteRecord` は常に `Collection: "app.bsky.feed.post"` を送信するが、削除対象にはリポスト（`app.bsky.feed.repost` コレクション由来の `PostTypeRepost`）も含まれる（`internal/runner/runner.go` は rkey しか渡していない）。この結果、リポストは実際には一度も削除されないまま「削除成功」として報告され続け、かつリポストの rkey が偶然 `app.bsky.feed.post` 側の別レコードの rkey と一致した場合には、削除対象に選定されていない投稿が意図せず削除される可能性がある。これは本プロジェクトが最重視する「誤削除防止」の安全性保証（[Project Overview](../../overview.md)）に反する。
2. **DID 解決時の HTTP リダイレクトが SSRF チェックを迂回する（中）**: `internal/atproto/client.go` が DID 解決（ハンドルの well-known 取得・DID ドキュメント取得）に使う `hostSafetyCheckedDoer`（`internal/atproto/did.go`）は初期 URL のホストしか検証せず、内部で委譲する `http.DefaultClient` はリダイレクト追従を制限していない。ハンドルサーバまたは `did:web` ドキュメント配信元が悪意ある/侵害された 3xx 応答を返すと、内部アドレスやメタデータサービスへの blind SSRF が成立しうる。認証情報送信後の PDS 通信（`restrictedDoer`）にはリダイレクト拒否（`rejectRedirect`）と IP ピン留めがあるのに対し、この解決フェーズには同等の防御がない非対称な状態になっている。
3. **Slack 通知のリトライがレスポンスボディ読み取り前にコンテキストをキャンセルする（中）**: `internal/notify/notify.go` の `perAttemptTimeoutDoer.Do` は `defer cancel()` により `Do` の戻り値を返した時点で当該リクエストのコンテキストをキャンセルするが、レスポンスボディの読み取り（リトライ可否判定後の drain、または最終的な成功時のレスポンス処理）はその後に `internal/retry.Doer` 側で行われる。キャンセル済みコンテキストでのボディ読み取りは `context.Canceled` を返しうるため、429/5xx を受けた際に設計上意図されたリトライ（`internal/notify` のリトライポリシー、最大 2 回）が機能せず、Slack 通知が失われる可能性がある。
4. **`createSession` 応答の DID が検証されていない（軽微〜中、多層防御）**: `internal/atproto/session.go` の `Login` は、`NewClient` が事前に解決・検証済みの `c.did` と、PDS から返された `createSession` レスポンスの `DID` フィールドとを突き合わせていない。異常な PDS が想定と異なる DID を返した場合でも、そのまま `DeleteRecord` の `repo` フィールドに使われてしまう。
5. **429 応答の `Retry-After` ヘッダ解釈に誤りがある（軽微）**: `internal/retry/doer.go` の `parseRetryAfter` は `time.ParseDuration(value + "s")` を用いて秒数形式をパースしているが、`Retry-After: 5m` のような値を `"5ms"`（5 ミリ秒）と誤解釈してしまう。HTTP の `Retry-After` ヘッダは秒数の整数値か HTTP-date のいずれかしか許容されない（RFC 9110 10.2.3）ため、想定外の値に対してもバックオフを短くしすぎない安全側の挙動が必要である。

これらはいずれも既存タスク（[0002_atproto_client](../0002_atproto_client/01_requirements.md)、[0003_cleanup_engine](../0003_cleanup_engine/01_requirements.md)、[0005_retry_timeout](../0005_retry_timeout/01_requirements.md)、[0006_slack_notification](../0006_slack_notification/01_requirements.md)、[0009_dns_handle_resolution](../0009_dns_handle_resolution/01_requirements.md)）で確立された既存の受け入れ基準や安全性保証を壊す形の欠陥であり、新機能の追加ではなく既存実装の修正（バグフィックス）として扱う。

### 1.2 目的（ゴール）

- リポストが `--apply` 実行時に実際に削除され、かつ他コレクションのレコードを誤って削除する余地がなくなる
- DID 解決の過程で発生するあらゆる HTTP 通信（ハンドル解決・DID ドキュメント取得を含む）が、PDS 本通信と同水準のホスト安全性検証を経由しない限り成立しない
- Slack 通知のリトライが、429/5xx 応答に対して設計どおり機能する
- `Login` が、解決済み DID と異なる DID を返す PDS からの応答を検知し、fail-closed で処理を中断する
- `Retry-After` ヘッダの解釈が RFC 9110 の許容形式（デルタ秒・HTTP-date）のみを対象とし、それ以外の値ではバックオフ短縮につながらない

## 2. スコープ

### In Scope

- **F-001**: リポスト削除時に正しいコレクション（`app.bsky.feed.repost`）を指定する
- **F-002**: DID 解決フェーズの HTTP 通信にリダイレクト拒否とホスト安全性の再検証を適用する
- **F-003**: Slack 通知リトライにおけるコンテキストキャンセルのタイミングを是正する
- **F-004**: `Login` で `createSession` 応答の DID が解決済み DID と一致することを検証する
- **F-005**: `Retry-After` ヘッダのパースを RFC 9110 準拠の形式（デルタ秒・HTTP-date）のみに限定する

### Out of Scope

- `isUnsafeIP`（`internal/atproto/did.go`）が CGNAT（`100.64.0.0/10`）やマルチキャストアドレス等の一部レンジを判定対象に含んでいない件 — 現状の判定（private/loopback/link-local/unspecified）は主要な内部ネットワーク到達経路を塞いでおり、追加レンジの是非は個別の脅威評価を要するため、本タスクでは扱わず別タスクで検討する
- `restrictedDoer` が検証済みアドレスの先頭 1 件のみにピン留めし、複数アドレスへのフェイルオーバーを行わない件 — 可用性のみに関わる問題であり、安全性・正当性の欠陥ではないため対象外
- 長時間実行時のアクセス JWT 失効（`execution_timeout_seconds` が JWT 寿命を超えうる件） — セッション再発行（refresh）の設計は本タスクの調査で見つかった個別バグの修正とは性質が異なる新機能であり、別タスクとする
- `entrypoint.sh` が `BSKY_CONFIG_PATH` を無検証で crontab 行に埋め込む件 — この値は運用者自身が設定するコンテナ起動環境変数であり、外部入力ではないため優先度が低く、本タスクの対象外とする
- 単一の不正な `createdAt` を持つレコードが `ListPosts` 全体を恒久的に失敗させる件（cron 実行が繰り返し失敗しうる） — fail-closed の設計自体は意図通りであり、運用上の緩和策（該当レコードの特定容易化、スキップ設定の要否）は別途運用要件として検討すべきものであり、本タスクの対象外とする

## 3. 機能要件と受け入れ基準

### F-001: リポスト削除時のコレクション指定修正

`internal/runner.Run` が `client.DeleteRecord` を呼ぶ際、削除対象のレコードが属する実際のコレクション（`app.bsky.feed.post` または `app.bsky.feed.repost`）が正しく指定されるようにする。

**Acceptance Criteria**:
- **AC-01**: `PostTypeRepost` の投稿を削除する際、`com.atproto.repo.deleteRecord` リクエストの `collection` フィールドに `app.bsky.feed.repost` が指定される
- **AC-02**: `PostTypeOriginal`・`PostTypeReply`・`PostTypeQuote` の投稿を削除する際は、従来どおり `collection` フィールドに `app.bsky.feed.post` が指定される
- **AC-03**: リポストの rkey が `app.bsky.feed.post` コレクション内の（削除対象に選定されていない）別レコードの rkey と偶然一致していても、そのレコードは削除されない（コレクションを跨いだ誤削除が発生しないことを、両コレクションに同一 rkey を持つレコードが存在するケースのテストで検証する）
- **AC-04**: `--apply` 実行でリポストの削除が実際に成功した場合にのみ `report.Result.Deleted` に計上され、PDS 側で実際に削除が行われなかった場合は `report.Result.Failed` に計上される（存在しない rkey に対する「常に 200 で成功」という `deleteRecord` の lexicon 仕様下で、誤ったコレクション指定によって見かけ上成功するケースがなくなることを確認する）

### F-002: DID 解決フェーズにおけるリダイレクト拒否とホスト安全性の再検証

ハンドルの well-known 取得（`resolveHandleToDID`）および DID ドキュメント取得（`resolveDIDDocument`）が行う HTTP 通信について、PDS 本通信（`restrictedDoer`）と同水準のリダイレクト制御を適用する。

**Acceptance Criteria**:
- **AC-05**: ハンドルの well-known 取得先、または DID ドキュメント取得先が 3xx リダイレクト応答を返した場合、リダイレクト先への接続は行われず、SSRF エラー（`*atproto.SSRFError`）として処理が中断される
- **AC-06**: リダイレクトが発生しない正常系（既存の 2xx 応答によるハンドル解決・DID ドキュメント取得）の既存の挙動・既存テストが変更なく成功する
- **AC-07**: リダイレクト拒否の実装が、[0009_dns_handle_resolution](../0009_dns_handle_resolution/01_requirements.md) で追加された DNS TXT 方式のハンドル解決経路には影響しない（DNS 解決は HTTP 通信を伴わないため、本 AC は「DNS 経路の既存テストに退行がないこと」を指す）

### F-003: Slack 通知リトライのコンテキストキャンセルタイミング是正

`internal/notify.perAttemptTimeoutDoer` が生成するコンテキストのキャンセルタイミングを、レスポンスボディの読み取り完了後（またはボディの `Close` 時点）に是正し、429/5xx 応答に対するリトライが設計どおり機能するようにする。

**Acceptance Criteria**:
- **AC-08**: Webhook 送信が 429 応答を返すモックに対し、`internal/notify` のリトライポリシー（最大 2 回）に従って実際に再送が行われ、最終的に成功応答を返した場合は `Send` が `nil` エラーを返す（リトライが機能せず 1 回で `SendError` になっていないことをテストで確認する）
- **AC-09**: Webhook 送信が 5xx 応答を返すモックに対しても AC-08 と同様にリトライが機能する
- **AC-10**: リトライ上限に達した場合は、従来どおり `*notify.SendError` が返る（リトライが機能するようになったことで、リトライ枯渇時のエラー返却という既存の失敗系の挙動が変わらないことを確認する）
- **AC-11**: 各試行（アテンプト）のタイムアウト（`defaultRequestTimeout`）が、ボディ読み取りを含めて従来どおり適用される（コンテキストキャンセルのタイミングを遅らせたことで、ハングしたレスポンスボディの読み取りが無制限に待たされるようにならないことを確認する）

### F-004: `Login` におけるセッション DID の検証

`internal/atproto.Client.Login` が、`createSession` レスポンスの `DID` フィールドと `NewClient` が解決・検証済みの DID（`c.did`）が一致することを検証し、不一致の場合は fail-closed でエラーを返す。

**Acceptance Criteria**:
- **AC-12**: `createSession` レスポンスの `DID` が `c.did` と一致する場合、従来どおり `Login` は成功し `c.session` が設定される
- **AC-13**: `createSession` レスポンスの `DID` が `c.did` と一致しない場合、`Login` はエラーを返し、`c.session` は設定されない（`nil` のまま、あるいは以前の値のまま変更されない）ことで、以降の `DeleteRecord` が実行されない
- **AC-14**: AC-13 のエラーが `errors.Is`/`errors.AsType[T]` で判別可能な型付きエラーとして返り、かつそのエラーメッセージに DID 以外の機微情報（アクセス JWT 等）が含まれない

### F-005: `Retry-After` ヘッダのパース厳格化

`internal/retry.parseRetryAfter` が、RFC 9110 10.2.3 で許容される形式（非負整数の秒数、または HTTP-date）のみを解釈し、それ以外の形式（例: `time.ParseDuration` が受理してしまう `5m` のような単位付き文字列）を無視して既存のバックオフ計算にフォールバックするようにする。

**Acceptance Criteria**:
- **AC-15**: `Retry-After: 5` のような非負整数の秒数値は、従来どおり 5 秒の待機として解釈される
- **AC-16**: `Retry-After: 5m` のような、整数の秒数でも HTTP-date でもない値は無視され（0 を返し）、既存の指数バックオフ計算にフォールバックする（現状の実装のように `"5ms"`=5ミリ秒 と誤解釈されないことをテストで確認する）
- **AC-17**: 有効な HTTP-date 形式の `Retry-After` の既存の解釈（未来の日時は差分秒数、過去の日時は 0 として無視）が変更なく成功する

## 4. 非機能要件

- **NF-001**: `make fmt`・`make test`・`make lint` が成功する。
- **NF-002**: 本タスクの修正はいずれも既存パッケージ内のバグ修正であり、新規の外部依存パッケージを追加しない。
- **NF-003**: F-002 の修正は、[0002_atproto_client](../0002_atproto_client/01_requirements.md) および [セキュリティ設計](../../design/security.md) が定める SSRF 対策の既存の脅威モデル（DID 解決結果を信頼せず、認証情報送信前に必ずホスト安全性を検証する）を維持し、後退させない。
- **NF-004**: 各 AC の修正は独立した最小差分とし、本タスクで見つかった欠陥に無関係なリファクタリングを含めない（YAGNI）。

## 5. スコープ外の根拠

- CGNAT・マルチキャスト等の追加 IP レンジ判定、複数検証済みアドレスへのフェイルオーバー、アクセス JWT の再発行（refresh）、`entrypoint.sh` の crontab 行構築の堅牢化、および不正な `createdAt` 1件による恒久的な実行失敗への運用上の緩和策は、いずれも「本レビューで見つかった具体的な欠陥の修正」という本タスクのスコープに対して、性質が異なる（新機能・運用ポリシーの検討を要する、または深刻度が低く個別の脅威評価が必要）と判断し、それぞれ別タスクとして切り出す。

## 6. 成功基準（要約）

- AC-01〜AC-17 が test/static で緑。
- `--apply` 実行でリポストが正しく削除され、DID 解決の途上で悪意あるリダイレクトが成立せず、Slack 通知のリトライが 429/5xx に対して機能し、`Login` が想定外の DID を返す PDS を検知して処理を中断する状態。
