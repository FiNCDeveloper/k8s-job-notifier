# ポストモーテム: Job.Status.Conditions の並び順変化によるサイレント障害

## Executive Summary

- **Purpose**: Production の job-notifier が CronJob 失敗を検知しても Slack 通知しなくなっていた障害の原因と対処を記録する
- **Approach**:
  - 原因は `slack/slack.go` が通知条件判定に `Job.Status.Conditions[0]` のみを参照しており、Kubernetes 1.25+ で先頭に `FailureTarget` / `SuccessCriteriaMet` 等の中間 condition が挿入されるようになったため一致しなくなったこと
  - 対処として、全 conditions を走査して `Status: "True"` のものだけを通知条件と照合するロジックに変更
  - 再発検知策として、1日1回 + 起動時の Slack heartbeat 通知を追加（ADR: `2026-04-03_ADR_JOB_NOTIFIER_HEALTH_MONITORING.md` Option B）
- **Scope**: `slack/slack.go`, `main.go`, `handler/handler.go` の変更、`slack/slack_test.go` 新規追加。finc_infra 側の変更なし
- **Risks/Notes**: `client-go v0.21.2` と EKS v1.34 サーバ間のバージョンスキューは残存（今回は動作に支障なしと確認したがスコープ外）

## 背景

Production クラスタは 2026-07-27 に EKS v1.34.9 へアップグレードされた（全ノード入れ替え、job-notifier Pod も再起動）。2026-08-05、ユーザーから「job-notifier が動いていない気がする、cronjob の失敗通知をしてほしい」と報告があり調査した。

## 調査で判明した事実

1. job-notifier Pod は `Running` のまま、Pod 起動（2026-07-27T04:58:52Z）以降**ログを1行も出力していない**（エラーログなし）
2. 一方でクラスタ上には 2026-07-01 以降、複数の namespace で失敗した Job が多数存在した（例: `wellness-survey-production/daily-notify-finish-rate-notify-*`、`tegata-production/user-ios-plan-update-all-expired-receipt-status-*` 等）。これらの CronJob には `notify-slack.finc.com/enabled: "true"` アノテーションが付与されており、本来通知されるべきだった
3. 失敗 Job の `status.conditions` を実クラスタで確認すると:
   ```json
   [
     {"type": "FailureTarget", "status": "True", ...},
     {"type": "Failed", "status": "True", ...}
   ]
   ```
   成功 Job も同様に先頭に中間 condition が入る:
   ```json
   [
     {"type": "SuccessCriteriaMet", "status": "True", ...},
     {"type": "Complete", "status": "True", ...}
   ]
   ```
4. `slack/slack.go` の `Handle()` は通知条件判定に `job.Status.Conditions[0].Type` のみを使用しており、デフォルトの通知条件は `["Failed"]`。先頭が `FailureTarget` になったことで、実際に `Failed` condition が存在していても一致せず、通知が一切送られなくなっていた
5. RBAC（`jobs`/`cronjobs`/`pods` の list/watch）、`notify-slack.finc.com/*` アノテーションの Job への伝播、`SLACK_TOKEN` の ExternalSecret 経由の取得は全て正常であることを確認した。watcher 自体はエラーなく稼働しており、informer の sync 失敗ではない

## 根本原因

Kubernetes 1.25 以降、`Job.Status.Conditions` に `FailureTarget`（Failed の前段階）や `SuccessCriteriaMet`（Complete の前段階）といった中間状態の condition が追加されるようになった。job-notifier は `Conditions[0]` 固定参照で実装されていたため、この仕様変更により通知条件マッチングが完全に機能しなくなった。

エラーログを一切出力しない「サイレント障害」であるため、2026-04 の障害（[PR #2](https://github.com/FiNCDeveloper/k8s-job-notifier/pull/2)、`batch/v1beta1` API 削除起因）を受けて作成した ADR の Option C（DataDog ログアラート）では検知できないパターンだった。

## 対処

1. `matchCondition()` を新設し、`Conditions[0]` 固定参照をやめて、全 conditions のうち `Status: "True"` のものだけを通知条件（大文字小文字無視・前後空白除去）と照合するロジックに変更（`slack/slack.go`）
2. `buildMessage()` / `buildAttachment()` / `slackColor()` は、マッチした condition を明示的に受け取るように変更し、通知メッセージの `Message`/`Status` 欄とカラーが正しい condition を反映するようにした
3. ADR ([`2026-04-03_ADR_JOB_NOTIFIER_HEALTH_MONITORING.md`](./2026-04-03_ADR_JOB_NOTIFIER_HEALTH_MONITORING.md)) の Option B（Slack heartbeat）を採用し実装。1日1回 + 起動時に Slack へ生存通知を送ることで、今回のような「エラーログを出さないサイレント障害」を将来検知できるようにした
4. `matchCondition()` に対するユニットテストを追加（1.25+ の condition 並び順、旧挙動の後方互換、`Status: False` の除外等をカバー）

## 学び・再発防止策

- **Kubernetes API のマイナーバージョンアップに伴うオブジェクト仕様の変化は、client-go のバージョンを上げなくても発生しうる**。今回は `client-go v0.21.2` のまま、サーバ側（EKS v1.34）が返す `status.conditions` の中身が変わったことが原因であり、コンパイル時には検知できない
- **配列の先頭要素固定参照は壊れやすい**。ステータス判定は「該当する状態が Status=True で存在するか」で判定すべきで、順序に依存すべきではない
- **エラーログが出ないサイレント障害は、ログベースの監視だけではカバーできない**。heartbeat のような能動的な生存通知の仕組みが必要
- **今後の推奨事項（今回のスコープ外）**: `client-go`/`k8s.io/api` を EKS のバージョンに追従して定期的に更新する運用を検討する。今回は v0.21.2 のままでも動作したが、将来的な非互換 API の削除（今回の `batch/v1beta1` のような）に備え、追従コストを下げておきたい

## 影響範囲

- Production: 2026-07-27〜2026-08-05 の間、`notify-slack.finc.com/enabled: "true"` を持つ CronJob の失敗が Slack に通知されていなかった
- Staging: 同一イメージを使用しているため、同時期に同様の障害が発生していたと推測される（本 PR のデプロイ手順で復旧を確認する）

## 改訂履歴

| 日付 | 内容 |
|---|---|
| 2026-08-05 | 初版作成 |
