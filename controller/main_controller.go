package controller

import (
	"context"
	"time"

	"github.com/FiNCDeveloper/k8s-job-notifier/event"
	"github.com/FiNCDeveloper/k8s-job-notifier/handler"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

type MainController struct {
	client    kubernetes.Interface
	handler   handler.Handler
	startTime time.Time
}

func NewMainController(client kubernetes.Interface, h handler.Handler) MainController {
	return MainController{
		client:  client,
		handler: h,
	}
}

func (c *MainController) Run() {

	c.startTime = time.Now().Local()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	jobListWatcher := cache.NewListWatchFromClient(c.client.BatchV1().RESTClient(), "jobs", v1.NamespaceAll, fields.Everything())
	jobInformer := c.newJobInformer(ctx, jobListWatcher)

	cronjobListWatcher := cache.NewListWatchFromClient(c.client.BatchV1().RESTClient(), "cronjobs", v1.NamespaceAll, fields.Everything())
	_, cronjobInformer := cache.NewIndexerInformer(cronjobListWatcher, &batchv1.CronJob{}, 0, cache.ResourceEventHandlerFuncs{

		AddFunc: func(obj interface{}) {
			c.cronjobEvent(ctx, obj.(*batchv1.CronJob))
		},
		UpdateFunc: func(old interface{}, new interface{}) {
			c.cronjobEvent(ctx, new.(*batchv1.CronJob))
		},
		DeleteFunc: func(obj interface{}) {
		},
	}, cache.Indexers{})

	stopCh := make(chan struct{})
	defer close(stopCh)
	go jobInformer.Run(stopCh)
	go cronjobInformer.Run(stopCh)
	select {} // Block all
}

func (c *MainController) newJobInformer(ctx context.Context, lw cache.ListerWatcher) cache.Controller {
	_, jobInformer := cache.NewIndexerInformer(lw, &batchv1.Job{}, 0, cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c.addEvent(ctx, obj.(*batchv1.Job))
		},
		UpdateFunc: func(old interface{}, new interface{}) {
			c.updateEvent(ctx, old.(*batchv1.Job), new.(*batchv1.Job))
		},
		DeleteFunc: func(obj interface{}) {
			// relist で見つかった削除は *batchv1.Job ではなく cache.DeletedFinalStateUnknown で届くため、
			// そのまま型アサーションすると panic してプロセスが落ちる。
			if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
				obj = tombstone.Obj
			}
			if job, ok := obj.(*batchv1.Job); ok {
				c.deleteEvent(ctx, job)
			}
		},
	}, cache.Indexers{})
	return jobInformer
}

func (c *MainController) deleteEvent(ctx context.Context, job *batchv1.Job) {
	// log.Printf("job event: %s, %s", job.GetName(), job.Status.String())
}

func (c *MainController) addEvent(ctx context.Context, job *batchv1.Job) {
	// 起動時に過去のJobもAddとして送信されてくるので、CreationTimestampをチェックして起動前に作られたJobは送信しない。
	// Updateには適用しない。起動前に作られ起動後に失敗したJobも通知対象であり、
	// 再配信による重複はhandlerが状態遷移で判定して防ぐ。
	if job.CreationTimestamp.Sub(c.startTime).Seconds() <= 0 {
		return
	}
	c.sendEvent(ctx, event.Event{
		Namespace: job.Namespace,
		Type:      job.TypeMeta.Kind,
		Resource:  job,
	})
}

func (c *MainController) updateEvent(ctx context.Context, old *batchv1.Job, new *batchv1.Job) {
	c.sendEvent(ctx, event.Event{
		Namespace:   new.Namespace,
		Type:        new.TypeMeta.Kind,
		Resource:    new,
		OldResource: old,
	})
}

func (c *MainController) sendEvent(ctx context.Context, e event.Event) {
	go c.handler.Handle(e)
}

func (c *MainController) cronjobEvent(ctx context.Context, cj *batchv1.CronJob) {
	// 今の所cronjobのイベントに対しては何もしない
}
