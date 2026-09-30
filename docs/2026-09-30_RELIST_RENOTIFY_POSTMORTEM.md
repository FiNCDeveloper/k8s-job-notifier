# ポストモーテム: informer の relist による過去の失敗 Job の一斉再通知

時刻は特記のない限り UTC で記載する（job-notifier のログと Kubernetes の時刻が UTC のため）。

## Executive Summary

- **Purpose**: 2026-09-29 20:17 UTC（2026-09-30 05:17 JST）に、数週間前に失敗し対応済みの Job の失敗通知が 45 件まとめて再送された障害の原因と対処を記録する
- **Approach**:
  - 原因は、job-notifier が「Job が Failed に**なった**」ではなく「Job が Failed で**ある**」ことで通知していたこと。informer は watch の失敗後に全件を取り直し（relist）、変化のない Job も Update として再配信するため、そのたびに起動後に作られた失敗 Job を全部再通知していた
  - 対処として、通知条件の condition のうち、変更前の Job では True でなかったものが True になったときだけ通知するようにした（状態遷移での判定）
  - あわせて、relist で見つかった削除（`cache.DeletedFinalStateUnknown`）を受けると DeleteFunc の型アサーションで panic してプロセスが落ちる既存の不具合を修正した
  - あわせて、起動時刻での絞り込みを新規検出（Add）のみに限定し、起動前に作られ起動後に失敗した Job が通知されない既存の漏れを解消した
- **Scope**: `slack/slack.go`, `controller/main_controller.go`, `event/event.go`, `main.go` の変更、テスト追加。finc_infra 側の変更なし
- **Risks/Notes**: relist のきっかけとなった apiserver の 503 は EKS 側のインスタンス入れ替わりと同時刻だが、コントロールプレーンのログが無効のため 503 の発生元は特定できていない

## 背景

2026-09-29 20:17:32 UTC（2026-09-30 05:17:32 JST）、`#system_alert_fatal` などに job-notifier から失敗通知が届いた。対象の `advice-engine-production/weekly-report-error-watch-29805135` / `-29806575` は 2026-09-02 / 09-03 に失敗した Job で、当時すでに通知・対応済み（配信は正常で、監視側の判定の問題と結論済み）だった。

## 調査で判明した事実

1. job-notifier のログ（`kubectl logs -n job-notifier-production deploy/job-notifier`）:
   ```
   E0929 20:17:30.140760 ... Failed to watch *v1.Job: the server is currently unable to handle the request (get jobs.batch)
   E0929 20:17:30.140758 ... Failed to watch *v1.CronJob: the server is currently unable to handle the request (get cronjobs.batch)
   2026/09/29 20:17:32 Message successfully sent to channel ...   (45 行)
   ```
2. 再送された 45 件は、クラスタ上の「通知が有効、`Failed` が True、Pod 起動（2026-08-06T08:47:34Z）より後に作成」という条件の Job 数（45）と一致した（本番は `SLACK_DEFAULT_ENABLED=true` で、`enabled: "false"` を付けた CronJob は無いため、通知が有効なのは全 CronJob）。同じ条件で起動前に作られた 536 件は、起動時刻での絞り込み（`CreationTimestamp` と起動時刻の比較）によって送られていない
3. client-go v0.21.2 の `tools/cache/reflector.go` では、watch リクエストが connection refused 以外のエラーで失敗すると `ListAndWatch` をやり直して全件を LIST し、`store.Replace` する（410 Gone や watch 中のエラーイベントも同様）。`tools/cache/controller.go` の処理では、既にキャッシュにある Job は変化の有無に関係なく `OnUpdate(old, new)` として配信される。watch が時間切れで正常終了したときは relist せず watch を張り直すだけなので、普段は再送が起きない
4. 旧実装は `UpdateFunc` で変化の有無を見ずに通知処理を呼び、通知処理（`slack.Handle`）は現在の conditions だけで判定していた
5. 同じ現象は 2026-08-14 18:27:41 UTC にも発生していた（Job の watch 失敗の 2 秒後に `#ws_warn_prod` へ 2 件再送）。そのときは起動からの日数が浅く、対象の失敗 Job が少なかった
6. 旧実装は、起動時刻での絞り込みを Update にも適用していた。そのため、起動前に作られ、起動後に失敗した Job は通知されていなかった（コードから確認）

### apiserver が 503 を返した背景

- 同じ時刻に coredns（6 Pod 全部）、ambassador traffic-manager、job-notifier が watch の張り直しで 503 を受けている。その直前（20:17:27.9 頃）には cluster-autoscaler などの watch がサーバ側から一斉に切られており、クラスタ全体のコントロールプレーン側の事象だった
- `kubectl -n kube-system get leases` の apiserver identity lease を見ると、旧 2 台（2026-09-26 09:36 作成）は 20:17:22 / 20:17:24 で更新が止まり、新 2 台が 20:11:45 / 20:11:54 に作成されている。apiserver インスタンスの入れ替わりと同時刻である
- `aws eks list-updates` に 2026-07-27 以降の更新はなく、AWS Health にも該当イベントはない。AWS 側の自動の入れ替わりと推測されるが、未確認
- EKS のコントロールプレーンログ（api / audit 等）はすべて無効。503 を返したのが停止中の旧 apiserver か、手前のロードバランサかは特定できない

## 根本原因

informer は「イベント」ではなく「オブジェクトの最新状態」を配信する仕組みで、relist のたびに変化のない Job も Update として届く。job-notifier はこれを状態変化の通知とみなし、現在の状態（Failed であること）だけで通知を判定していた。relist 自体は apiserver の入れ替わりなどで定期的に起こりうる正常な挙動であり、きっかけの 503 をなくしても根本的な解決にはならない。

## 対処

1. `event.Event` に変更前の Job（`OldResource`）を追加し、Update のときに渡すようにした（Add のときは nil）
2. `slack.NotifiableCondition()` で、通知条件に一致する condition のうち、変更前の Job では True でなかったものがあるときだけ通知するようにした。通知条件が複数ある場合（例: `FailureTarget,Failed`）は先に True になったものが残り続けるため、一致した最初の condition ではなく、新たに True になったものを探す。Add（初めて見えた Job）はこれまでどおり通知する。これにより、relist で見逃していた Job（watch が切れている間に作られて失敗した Job）は通知され、変化のない再配信は通知されない。再配信を抑止したときは `<namespace>/<name> skip, Failed was already true` のログを出し、次に relist が起きたときに修正が効いたことを本番ログで確かめられるようにした
3. 起動時刻での絞り込みを Add のみに適用するようにした。Update の重複は 2 の遷移判定で防ぐ
4. controller が通知処理（`handler.Handler`）を外から受け取るようにし、job informer の組み立てを `newJobInformer()` に切り出した。これにより、偽の ListerWatcher で今回の流れ（watch が 503 で失敗 → relist）を再現するテストを書けるようにした
5. job informer の DeleteFunc で `cache.DeletedFinalStateUnknown` を展開するようにした。relist の時点で消えていた Job（watch 断の間に CronJob の履歴上限などで削除された Job）はこの型で届くため、旧実装の `obj.(*batchv1.Job)` は panic し、プロセスごと落ちていた（テストで再現を確認）。落ちると再起動の間に失敗した Job は通知されない
6. テストを追加した
   - `TestNotifiableCondition`: 遷移の判定（初検出の Failed / 実行中→Failed / FailureTarget→Failed は通知し、Failed→Failed の再配信は通知しない。通知条件が `FailureTarget,Failed` のとき、FailureTarget が残ったまま Failed が True になれば通知する）
   - `TestJobInformerNotifiesTransitionsOnlyAcrossRelist`: 今回の障害の再現。修正前の挙動を再現したコードでは、J が重複して送られ L が漏れて `[J J K]` となり失敗することを確認済み。relist の時点で消えていた Job を含め、修正前の DeleteFunc では panic することも確認済み

## 検討して採用しなかった案

- **`ResourceVersion` が同じなら Update を無視する**: relist での再配信は防げるが、失敗済みの Job にラベル変更など無関係な更新が入ると再通知される。遷移での判定はこのケースも含む
- **通知済みの印を Job のアノテーションや ConfigMap に記録する**: 再起動をまたいでも重複を防げるが、全 namespace の Job への書き込み権限と追加の API 書き込みが必要になる。今回の問題に対して過大
- **client-go の更新**: relist 時に Update として配信する契約は新しい版でも変わらないため、単独では解決にならない（EKS とのバージョン差の解消は別途の課題）

## 残る制約（今回の変更では変わらない）

- job-notifier が停止している間に失敗した Job は通知されない（起動時の Add は起動時刻での絞り込みで除外される）
- ローリングアップデート中に新旧 Pod が同時に動く間に失敗した Job は、2 回通知されうる（README の「制限事項」のとおり、replica=1 前提）
- watch 断の間に同じ名前の Job が削除・再作成され、両方とも失敗していた場合、relist では同じ Job の更新として届くため通知されない。CronJob が作る Job は名前にスケジュール時刻が入り重複しないため、手動で同名の Job を数秒以内に作り直した場合に限られる

## 影響範囲

- Production: 2026-08-14 18:27 UTC（2 件）と 2026-09-29 20:17 UTC（45 件）に、対応済みの失敗の再通知が発生した。再通知に伴う通知の欠落はない
- 起動前に作られ起動後に失敗した Job の通知漏れは、job-notifier の再起動をまたいで実行された Job にだけ起こりうる。発生件数は確認していない
- DeleteFunc の panic は、現在の Pod（2026-08-06 起動）の再起動回数が 0 なので、少なくともこの Pod では起きていない。relist が数秒で終わり、その間に削除された Job が無かったためと考えられる

## フォローアップ

この PR の範囲外で、別途検討する事項。

- **EKS コントロールプレーンログの有効化の要否**: 今回は api / audit ログがすべて無効だったため、503 を返したのが停止中の旧 apiserver か手前のロードバランサかを特定できなかった。保存コストと引き換えに、次回の同種事象で発生元を追えるようにするかを判断する
- **client-go と EKS のバージョン差の解消**: job-notifier は client-go v0.21.2 を使っており、クラスタのバージョンとの差が大きい。今回の再通知の原因ではないが、サポートされる組み合わせに戻す
- **修正後の relist で抑止ログを確認する**: デプロイ後に relist が起きたら、`skip, Failed was already true` のログが出て、再通知が無いことを確かめる

## 改訂履歴

| 日付 | 内容 |
|---|---|
| 2026-09-30 | 初版作成 |
