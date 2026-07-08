# `release.yml` 手動検証手順書

## 目的

[03_implementation_plan.md](03_implementation_plan.md) のフェーズ2ドライラン検証項目（`workflow_dispatch` による正常系確認、浮動タグ push 失敗時の fail-closed / 再実行確認）を、実際に入力するコマンドとともに手順化したもの。対応する検証観点は [02_architecture.md 7.1節](02_architecture.md#71-テスト対象) の「手動検証（`workflow_dispatch`）」を参照。

## 設計上の注意点

`push: tags: ['v*']` と `workflow_dispatch` は独立したトリガーだが、`workflow_dispatch` の `actions/checkout` ステップは入力タグ名をリモートの参照として解決するため、**タグを GitHub に push した時点で自動的に push トリガーの実行も走る**（`concurrency` でタグ名ごとに直列化されるだけで、実行自体は防げない）。そのため本手順では、タグ push による自動実行を検証の主経路とし、`workflow_dispatch` は補助的に使う。

## 事前準備

```bash
cd <REPO_DIR>  # e.g., /Users/issei/git/bsky-cleaner
git status                     # 作業ツリーがクリーンであることを確認
git fetch origin --tags        # v0.0.1 がまだリポジトリに存在しないことを再確認
git tag -l 'v0.0.1'            # 何も出力されなければOK
gh auth status                 # gh CLI がログイン済みであることを確認
git checkout main && git pull origin main  # main ブランチに切り替えて最新化
```

## フェーズ1: 異常系（浮動タグ push 失敗 → 再実行で復旧）を先に検証する

正常系（4タグ公開）は、この異常系検証を修正して再実行した**最終的な成功実行**でまとめて確認する（AC-03b, NF-004 の実挙動検証）。

### 1-1. 検証用ブランチで、意図的に浮動タグ push を失敗させる

```bash
git checkout -b test/release-dry-run
```

`.github/workflows/release.yml` の「Tag and push major version tag」ステップを一時的に壊す（存在しない/権限のないリポジトリパスへの push にする）。当該ステップ内の docker tag/push 行を以下のように変更する。

```yaml
      - name: Tag and push major version tag
        env:
          TAG: ${{ steps.resolve-tag.outputs.resolved_tag }}
        run: |
          MAJOR=$(echo "$TAG" | sed 's/^v\([0-9]*\)\..*$/v\1/')
          docker tag bsky-cleaner:release "ghcr.io/nonexistent-owner/bsky-cleaner:${MAJOR}"
          docker push "ghcr.io/nonexistent-owner/bsky-cleaner:${MAJOR}"
```

コミット・push する。

```bash
git add .github/workflows/release.yml
git commit -m "test: temporarily break major-tag push for release.yml dry run"
git push -u origin test/release-dry-run
```

### 1-2. 検証タグ `v0.0.1` を、壊れたコミットに向けて作成・push する

```bash
git tag v0.0.1
git push origin v0.0.1
```

このpushで自動的に `release.yml` の push トリガーが走る（`test/release-dry-run` ブランチのコミットの内容で実行される）。

### 1-3. 失敗を確認する

```bash
gh run list --workflow=release.yml --limit 5
gh run watch <RUN_ID>          # 上のコマンドで確認した run ID を指定
```

期待される結果:
- `Validate tag format` / `Check existing vX.Y.Z tag` は成功
- `Tag and push latest` は成功
- `Tag and push major version tag` が失敗し、ワークフローが非0終了
- `Tag and push minor version tag` / `Tag and push patch version tag (vX.Y.Z)` は**実行されない**（＝`v0`/`v0.0`/`v0.0.1` はまだ GHCR に push されていない）

GHCR側の確認（`v0.0.1` がまだ無いこと）:

```bash
tags=$(gh api /users/isseis/packages/container/bsky-cleaner/versions --paginate --jq '.[].metadata.container.tags[]') || { echo "ERROR: gh api failed (check auth/permissions)"; exit 1; }
echo "$tags" | grep -qx 'v0.0.1' || echo "v0.0.1 not published yet (expected)"
```

### 1-4. 壊した箇所を修正し、同じタグ名で再実行する（NF-004の検証）

```bash
git checkout test/release-dry-run
git revert --no-edit HEAD      # 1-1のコミットを打ち消す
git push origin test/release-dry-run
```

タグをタグ削除なしに、修正後のコミットへ付け替えて再push する（force-push が必要 — 一時検証タグに対する操作である旨、実行前に把握しておくこと）。

```bash
git tag -f v0.0.1
git push origin v0.0.1 --force
```

この再pushで自動的に `release.yml` が再度走る。これが「タグの手動削除なしに再実行して正常終了する」ことの確認になる。

```bash
gh run list --workflow=release.yml --limit 5
gh run watch <新しいRUN_ID>
```

期待される結果: 4ステップすべて成功し、ワークフロー全体が正常終了する。

## フェーズ2: 正常系の最終確認（4タグがGHCRに公開されていること）

```bash
gh api /users/isseis/packages/container/bsky-cleaner/versions --paginate --jq '.[] | {id: .id, tags: .metadata.container.tags}'
```

`latest` / `v0` / `v0.0` / `v0.0.1` の4タグがいずれかのversionに紐づいていることを確認する。

イメージの中身（バージョン埋め込み）も確認する場合:

```bash
docker pull ghcr.io/isseis/bsky-cleaner:v0.0.1
docker run --rm ghcr.io/isseis/bsky-cleaner:v0.0.1 --version
```

（任意）`workflow_dispatch` トリガー自体も一度動作確認したい場合、既に `v0.0.1` が公開済みなので今度は「既存タグ検出で拒否される」(AC-03a) ことの確認になる。

```bash
gh workflow run release.yml --ref main -f tag=v0.0.1
gh run list --workflow=release.yml --limit 3
```

期待される結果: `Check existing vX.Y.Z tag` ステップで非0終了し、公開処理は走らない。

## フェーズ3: 後片付け

### 3-1. 検証用GHCRタグの削除（GitHub UI から）

1. https://github.com/isseis?tab=packages の `bsky-cleaner` パッケージを開く
2. 「Package settings」→ 該当バージョン（`v0.0.1`/`v0`/`v0.0`/`latest` に紐づく version）を選択して削除

CLIで確認しながら行いたい場合の参考コマンド（削除自体はUIで行う前提のため、ここではID確認のみ）:

```bash
gh api /users/isseis/packages/container/bsky-cleaner/versions --paginate --jq '.[] | select(.metadata.container.tags | index("v0.0.1")) | {id, tags: .metadata.container.tags}'
```

⚠️ `latest` タグは他の正規リリースにも付いている場合があるため、削除対象を誤らないよう、必ずタグ一覧を確認してから実行すること。

⚠️ 再実行により `latest` タグが別の digest に付け替わった場合、元の digest は untagged version として残る可能性がある。`gh api /users/isseis/packages/container/bsky-cleaner/versions --paginate --jq '.[] | select(.metadata.container.tags == []) | .id'` で untagged version を確認し、存在すれば UI から同様に削除すること。

### 3-2. 検証用の git tag / ブランチの削除

```bash
git tag -d v0.0.1
git push origin :refs/tags/v0.0.1   # リモートのタグを削除
git branch -D test/release-dry-run
git push origin --delete test/release-dry-run
git checkout main
```

## 実行上の注意

本手順には次の操作が含まれる。

- `docker push` を伴う実際のGHCR公開（一時的とはいえ）
- `git push --force`（タグの付け替え）
- リモートブランチ・タグの削除

各ステップの結果を見ながら進めること。
