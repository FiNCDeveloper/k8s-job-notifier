# ADR: job-notifier 自身の健全性監視

- **Status**: Proposed
- **Date**: 2026-04-03
- **Context**: [2026-04-03_BATCH_V1BETA1_MIGRATION_POSTMORTEM.md](./2026-04-03_BATCH_V1BETA1_MIGRATION_POSTMORTEM.md)

## 背景

job-notifier は Kubernetes クラスタ全体の Job/CronJob 失敗を検知して Slack に通知するコントローラである。2026-04-03 に、CronJob watcher が `batch/v1beta1`（K8s 1.25 で削除済み）を使用していたため、推定半年以上にわたりサイレント障害が発生していたことが発覚した。

この障害が長期間検知されなかった理由:

1. Pod は `Running` のまま（プロセスは生きている）
2. readinessProbe / livenessProbe が未設定（policy で免除されている）
3. エラーログが出続けていたが、ログ監視アラートがない
4. 監視対象（CronJob）の失敗通知が来ないこと自体に気づく仕組みがない

**本 ADR では、job-notifier 自身の異常を検知する方法を決定する。**

## 決定すべきこと

job-notifier が正常に機能していない状態をどのように検知・通知するか。

## 選択肢

### Option A: `/healthz` エンドポイント + livenessProbe

**概要**: informer の `HasSynced()` を使い、Job / CronJob 両方の informer が初期同期を完了しているかをチェックする HTTP エンドポイントを追加。livenessProbe として設定する。

**実装イメージ**:
```go
// controller 側で informer の sync 状態を公開
func (c *MainController) IsHealthy() bool {
    return c.jobInformerSynced() && c.cronjobInformerSynced()
}

// main.go で HTTP サーバーを起動
http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
    if controller.IsHealthy() {
        w.WriteHeader(200)
    } else {
        w.WriteHeader(503)
    }
})
```

```yaml
# deployment.yaml
livenessProbe:
  httpGet:
    path: /healthz
    port: 8080
  initialDelaySeconds: 30
  periodSeconds: 30
```

**メリット**:
- informer が sync できない場合に Pod が自動再起動される
- CrashLoopBackOff 状態になることで、kubectl やモニタリングで異常に気づきやすい
- Kubernetes ネイティブな仕組み

**デメリット**:
- 今回のケースのように API 自体が存在しない場合、再起動しても回復しない → **CrashLoopBackOff の無限ループ**になる
- CrashLoopBackOff を検知するアラートが別途必要（結局外部監視が必要）
- コード変更 + マニフェスト変更 + policy の probe 免除除去が必要

**検知可能な障害**:
- informer の初期同期失敗（API 廃止、RBAC 不足、ネットワーク障害）
- プロセスのハング・デッドロック

**検知不可能な障害**:
- Slack トークンの期限切れ（通知送信は成功しないが informer は正常）
- Slack API の障害
- 個別のイベントハンドリングのバグ

### Option B: 定期 heartbeat 通知

**概要**: job-notifier が定期的（例: 1日1回）に「alive」メッセージを Slack に送信する。通知が来なくなったら異常。

**実装イメージ**:
```go
// main.go または controller
go func() {
    ticker := time.NewTicker(24 * time.Hour)
    for range ticker.C {
        slackClient.PostMessage("#job-notify", "job-notifier heartbeat: alive")
    }
}()
```

**メリット**:
- Slack 通知パス全体（informer → Slack API）の疎通確認になる
- Slack トークン期限切れも検知可能
- 実装がシンプル

**デメリット**:
- 検知が最大24時間遅れる（頻度を上げると Slack がノイジーになる）
- 「heartbeat が来ない」ことに人間が気づく必要がある（受動的な監視）
- heartbeat チャンネルを誰かが定期的に見ている前提

**検知可能な障害**:
- プロセス停止、Pod 異常
- Slack トークン期限切れ
- Slack API 障害

**検知不可能な障害**:
- informer は動いているが特定のイベントを見落としているケース
- heartbeat goroutine は生きているがメインの watch goroutine が死んでいるケース

### Option C: エラーログの DataDog アラート

**概要**: DataDog Log Monitor で `Failed to watch` や `slack error` などのエラーパターンを検知してアラートを発火する。

**設定イメージ**:
```
# DataDog Log Monitor
Query: source:kubernetes namespace:job-notifier-production "Failed to watch"
Threshold: count > 5 in 10 minutes
Notification: #job-notify or PagerDuty
```

**メリット**:
- **コード変更不要**（インフラ側の設定のみ）
- 今回の障害パターンをそのまま検知可能
- 即座にアラートが飛ぶ（遅延なし）
- 他のエラーパターン（Slack API エラー等）も同時に監視可能

**デメリット**:
- DataDog にログが送信されている前提（job-notifier namespace で Fluentd/Datadog Agent が動いている必要あり）
- ログパターンに依存するため、エラーメッセージが変わると検知漏れする
- エラーが出ないタイプの障害（静かに壊れるケース）は検知できない

**検知可能な障害**:
- informer エラー（API 廃止、RBAC 不足）
- Slack API エラー
- 任意のログ出力されるエラー

**検知不可能な障害**:
- エラーログを出さないサイレント障害
- ログが DataDog に届いていない場合

### Option D: Prometheus メトリクス

**概要**: informer のエラーカウント、通知成功/失敗カウントを Prometheus メトリクスとして公開し、Grafana/Alertmanager で監視する。

**実装イメージ**:
```go
var (
    notifySuccess = prometheus.NewCounterVec(...)
    notifyFailure = prometheus.NewCounterVec(...)
    informerErrors = prometheus.NewCounter(...)
)
```

**メリット**:
- 最も正確で粒度の細かい監視が可能
- 通知の成功率、エラー率をダッシュボードで可視化

**デメリット**:
- 実装コストが高い（Prometheus client ライブラリ追加、メトリクス設計、Grafana ダッシュボード作成）
- job-notifier の規模（single replica, 低トラフィック）に対してオーバーエンジニアリング
- Prometheus / ServiceMonitor の設定が必要

## 各選択肢の比較

| 観点 | A: healthz | B: heartbeat | C: ログアラート | D: メトリクス |
|---|---|---|---|---|
| 実装コスト | 中（コード+マニフェスト） | 低（コードのみ） | **なし** | 高 |
| 検知速度 | 30秒〜数分 | 最大24時間 | **即座** | 即座 |
| 今回の障害を検知できるか | Yes（CrashLoopBackOff） | Yes（通知停止） | **Yes（ログパターン）** | Yes |
| Slack パスの疎通確認 | No | **Yes** | No | No |
| 外部依存 | なし | Slack | DataDog | Prometheus |
| 人間の介入が必要か | No（自動再起動） | Yes（目視確認） | No（アラート自動） | No（アラート自動） |
| 運用負荷 | 低 | 中（見落としリスク） | **低** | 中 |

## 推奨

**議論のためのたたき台として、以下の組み合わせを提案する:**

### 短期（すぐやれること）: Option C

- コード変更不要
- 今回のような障害を即座に検知可能
- DataDog のログが取れていることの確認が前提

### 中期（コード変更を伴う）: Option A or B

- A（healthz）: API 消滅のようなケースで CrashLoopBackOff にすることで「壊れていることを見えるようにする」
- B（heartbeat）: Slack トークン期限切れも含めた通知パス全体の疎通確認

A と B はトレードオフが異なるため、チームで議論して決定する。

## 決定

(未決定 — チーム議論後に記入)
