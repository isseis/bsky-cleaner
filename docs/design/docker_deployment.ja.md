# Docker 配布の詳細設計

- 作成日: 2026-07-02
- ステータス: Draft
- 関連ドキュメント: [プロジェクト概要](../overview.ja.md)

## 位置付け

本ドキュメントは、[プロジェクト概要](../overview.ja.md) の「開発・配布方針」で述べた Docker 配布方針の実装レベルの詳細を記載する。方針レベルの決定事項（なぜこの構成にしたか）は overview 側を参照し、ここでは「どう実装するか」のみを扱う。

## 秘匿情報の管理（`.env`）

`docker-compose.yml` には秘匿情報を直接書かず、Compose の変数展開機能を通じて `.env` の値を参照する形にする。

> **`.env` と `env_file` の役割の違い**: Compose における `.env` ファイルは `docker-compose.yml` 内の変数展開（例: `${BSKY_APP_PASSWORD}`）専用であり、コンテナの環境変数には自動では入らない。コンテナへは `environment:` キーで `BSKY_APP_PASSWORD: ${BSKY_APP_PASSWORD}` のように明示的に渡す。`env_file:` ディレクティブはファイル内の変数をコンテナに丸ごと渡す別機能であり、本設計では使用しない。

- `.env` に実際の app パスワード等を記載する
- `.env` の扱いは、運用に応じて以下のいずれかを選択できるようにする
  - **`git-crypt` で `.env` のみを暗号化対象にする**: `.gitattributes` に `.env filter=git-crypt diff=git-crypt` を設定する。`docker-compose.yml` や TOML 設定など秘匿情報を含まないファイルは平文のまま差分管理・レビューでき、秘匿情報だけをリポジトリ内で安全に共有できる。複数人・複数環境で `.env` 自体をバージョン管理・共有したい場合に向く
  - **`.env` を `.gitignore` に追加してバージョン管理から外す**: 最もシンプルで事故を起こしにくいが、`.env` 自体の共有は git 管理外の手段（パスワードマネージャ等）に委ねる必要がある
- `dot.env.example`（値は空/ダミー）はいずれの方式でも通常通りコミットし、初期セットアップの手引きとする

## スケジュール設定と内蔵 cron の連携

実行スケジュールも他の設定と同様に TOML に一元化する（`docker-compose.yml` の環境変数には出さない）。TOML はコンテナ起動時にしか読めない設定ファイルであり、内蔵 cron（supercronic）が読み取る crontab 定義ファイルとは別物である。そのため、以下の手順で TOML の設定値を crontab に橋渡しする。

1. バイナリに隠しサブコマンド `print-schedule` を用意し、TOML から schedule フィールド（例: `schedule = "0 3 * * *"`）だけを取り出して標準出力する。TOML のパース処理をこのサブコマンドに集約し、二重実装を避ける。出力前に「改行を含まない1行の cron 式」であることを検証し、不正な値の場合は非0で終了する（改行を含む値をそのまま crontab に書き込むと、crontab の追加行として任意コマンドを注入できるため）
2. イメージ同梱のエントリポイントスクリプトが、コンテナ起動時にこのサブコマンドを呼び出し、結果を使って `supercronic` 用の crontab ファイルを動的生成する
3. `exec supercronic <生成した crontab>` で内蔵 cron を起動し、以降はそのスケジュールに従って本体（`bsky-cleaner --apply --config ...`）を定期実行する

この方式により、設定は TOML に一元化されたまま、ユーザーは Docker イメージを起動するだけで良く、ホスト側の cron 設定は不要になる。

## リリース

リリースは CI（`.github/workflows/release.yml`）が自動化しており、開発者が行う操作は以下の通り。

### 通常のリリース

```sh
git tag vX.Y.Z
git push --tags
```

タグの push をトリガーに CI が以下を自動実行する。

- semver 形式の検証、既存タグの重複チェック
- Docker イメージのビルドと GHCR への push（`latest`・`vX`・`vX.Y`・`vX.Y.Z` の4タグ）
- バイナリアーカイブ（`bsky-cleaner-vX.Y.Z-linux-amd64.tar.gz`）と SHA256 チェックサムの生成
- GitHub Release の作成とアーカイブの添付

### 動作確認用の手動実行

`workflow_dispatch` で任意のタグを指定して同じワークフローを実行できる。指定したタグが実在しない場合はチェックアウトに失敗する。`workflow_dispatch` 契機では GitHub Release が **ドラフト** 状態で作成されるため、確認後は `gh release delete <tag>` で削除するか、`gh release edit <tag> --draft=false` で公開する。

### ダウンロード後のチェックサム検証

```sh
# ダウンロードした bsky-cleaner-vX.Y.Z-linux-amd64.tar.gz と SHA256SUMS を同じディレクトリに配置し、以下を実行
sha256sum -c SHA256SUMS
# → bsky-cleaner-vX.Y.Z-linux-amd64.tar.gz: OK と出力されれば改ざんされていない
```

詳細は `README.md` の「インストールと実行（ビルド済み実行ファイル）」節も参照。

## GHCR パッケージ可視性の切り替え手順（初回のみ）

GHCR に初めてイメージを公開した直後は、パッケージの可視性が **private** になっている。`GITHUB_TOKEN` の権限では可視性を変更できないため、以下の手順で一度だけ手動で public に切り替える必要がある。

1. GitHub リポジトリのページを開く
2. 右側のサイドバーにある「Packages」セクションから `bsky-cleaner` をクリックする
3. パッケージのトップページで「Package settings」をクリックする
4. 「Danger Zone」セクションまでスクロールする
5. 「Change visibility」をクリックし、確認ダイアログで「public」を選択する

この操作はパッケージ初回作成時に一度だけ必要であり、2回目以降のリリースでは不要である。
