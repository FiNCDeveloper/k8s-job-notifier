# 2026-04-03 batch/v1beta1 CronJob API 廃止によるサイレント障害と修正

## 概要

job-notifier の CronJob watcher が `batch/v1beta1` API を使用していたため、Kubernetes 1.25 以降のクラスターで CronJob の watch が完全に失敗していた。Pod 自体は Running のまま毎分エラーを吐き続けるサイレント障害が長期間（推定半年以上）発生していた。

## タイムライン

| 時期 | イベント |
|---|---|
| 2021-12 | job-notifier 最終更新（commit `0870bb4b`、ECR イメージ最終プッシュ） |
| 2021-12〜2026-03 | 約4年間コード・イメージの更新なし |
| 不明（K8s 1.25 アップグレード時） | `batch/v1beta1` API がクラスターから削除され、CronJob watcher が機能停止 |
| 2026-04-03 | 障害発覚、原因特定、修正・デプロイ完了 |

## 障害の内容

### 症状

- Pod は `1/1 Running` で正常に見える
- readinessProbe / livenessProbe が設定されていない（policy で免除）ため、K8s による異常検知もされない
- ログは `Failed to watch *v1beta1.CronJob` エラーが毎分出力されるのみ
- Slack への CronJob 失敗通知が一切送信されない

### 根本原因

```
job-notifier が使用: batch/v1beta1 CronJob API (client-go v0.21.2)
クラスター:         batch/v1beta1 は Kubernetes 1.25 で完全削除
現在の EKS:         v1.33.8
```

`controller/main_controller.go` で CronJob informer が `c.client.BatchV1beta1().RESTClient()` を使用しており、クラスター側に対応する API が存在しないため watch が 100% 失敗していた。

### 影響範囲

- CronJob watcher のエラー自体は **通知欠落には直結しない**
  - `cronjobEvent()` は no-op（将来実装のための準備のみ）
  - 実際の失敗通知は Job watcher（`batch/v1`）が CronJob から生成される子 Job を検知して送信
  - Job watcher は `batch/v1` を使用しており正常動作
- ただし、大量のエラーログによるリソース消費、および CronJob の `MissedSchedule` 検知が機能しない問題があった

## 修正内容

### コード変更（FiNCDeveloper/k8s-job-notifier#2）

| ファイル | 変更 |
|---|---|
| `controller/main_controller.go` | `batchv1beta1` インポート削除、CronJob watcher を `BatchV1()` に変更 |
| `Dockerfile` | Go 1.16 → 1.22 |
| `go.mod` | go directive を 1.22 に更新 |
| `.circleci/config.yml` | `circleci/golang:1.16` → `cimg/go:1.22`、gotestsum インストール追加 |
| `README.md` | サンプル YAML の apiVersion を `batch/v1` に更新 |

### 重要なポイント

- `k8s.io/api v0.21.2` の `batch/v1` パッケージに CronJob 型は**既に含まれていた**
- `client-go v0.21.2` の `BatchV1()` も CronJob をサポート済み
- **Go モジュールのバージョン更新は不要**で、コード内の参照変更のみで修正完了

### デプロイ結果

| 環境 | イメージ | 結果 |
|---|---|---|
| Staging | `sha256:96228b37...` | エラー解消確認 |
| Production | `sha256:96228b37...` | エラー解消確認 |

## 学び・再発防止

### 1. Deprecated API を使用するワークロードの棚卸し

Kubernetes の API は定期的に deprecated → removed となる。特に `beta` API は GA 後2バージョンで削除される。クラスターアップグレード前に deprecated API の使用状況を確認する運用が必要。

```bash
# deprecated API を使用しているリソースを検出するツール例
# https://github.com/doitintl/kube-no-trouble (kubent)
kubent
```

### 2. 長期放置されたコンポーネントの定期レビュー

job-notifier は4年間一度もイメージが更新されなかった。「動いている」ように見えても内部でエラーを吐き続けている場合がある。

対策案:
- ECR イメージの最終プッシュ日を定期的に監査
- `imagePullPolicy: Always` + タグ固定なし（`:latest`）に依存するデプロイは、意図的にイメージを更新しない限り古いまま動き続ける

### 3. 監視用ワークロードこそ監視が必要

job-notifier は「他のジョブを監視する」ためのコンポーネントだが、job-notifier 自身の健全性を監視する仕組みがなかった。

対策案:
- job-notifier にヘルスチェックエンドポイント（`/healthz`）を追加し、readinessProbe を設定する
- CronJob watcher / Job watcher の正常稼働を Prometheus メトリクスとして公開
- 定期的な heartbeat 通知（例: 1日1回「job-notifier is alive」を Slack に送信）

### 4. Pod が Running ≠ 正常稼働

readinessProbe / livenessProbe が設定されていないワークロードでは、プロセスが起動していれば Running になる。アプリケーションレベルの異常は検知できない。

### 5. エラーログの監視

毎分エラーが出続けていたにもかかわらず発覚までに長期間を要した。ログベースのアラート（例: DataDog Log Monitor で `Failed to watch` パターンを検知）があれば早期発見できた。

## 関連リソース

- PR: https://github.com/FiNCDeveloper/k8s-job-notifier/pull/2
- ソースリポジトリ: https://github.com/FiNCDeveloper/k8s-job-notifier
- Kubernetes CronJob GA announcement: https://kubernetes.io/blog/2021/04/08/kubernetes-1-21-release-announcement/
- batch/v1beta1 removal: https://kubernetes.io/docs/reference/using-api/deprecation-guide/#cronjob-v125
