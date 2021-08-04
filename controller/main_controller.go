package controller

import (
	"context"

	"github.com/FiNCDeveloper/k8s-job-notifier/event"
	"github.com/FiNCDeveloper/k8s-job-notifier/handler"
	batchv1 "k8s.io/api/batch/v1"
	batchv1beta1 "k8s.io/api/batch/v1beta1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

type MainController struct {
	client kubernetes.Interface
}

func NewMainController(client kubernetes.Interface) MainController {
	return MainController{
		client: client,
	}
}

func (c *MainController) Run() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	jobListWatcher := cache.NewListWatchFromClient(c.client.BatchV1().RESTClient(), "jobs", v1.NamespaceAll, fields.Everything())
	_, jobInformer := cache.NewIndexerInformer(jobListWatcher, &batchv1.Job{}, 0, cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c.sendEvent(ctx, obj.(*batchv1.Job))
		},
		UpdateFunc: func(old interface{}, new interface{}) {
			c.sendEvent(ctx, new.(*batchv1.Job))
		},
		DeleteFunc: func(obj interface{}) {
			c.deleteEvent(ctx, obj.(*batchv1.Job))
		},
	}, cache.Indexers{})

	cronjobListWatcher := cache.NewListWatchFromClient(c.client.BatchV1beta1().RESTClient(), "cronjobs", v1.NamespaceAll, fields.Everything())
	_, cronjobInformer := cache.NewIndexerInformer(cronjobListWatcher, &batchv1beta1.CronJob{}, 0, cache.ResourceEventHandlerFuncs{

		AddFunc: func(obj interface{}) {
			c.cronjobEvent(ctx, obj.(*batchv1beta1.CronJob))
		},
		UpdateFunc: func(old interface{}, new interface{}) {
			c.cronjobEvent(ctx, new.(*batchv1beta1.CronJob))
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

func (c *MainController) deleteEvent(ctx context.Context, job *batchv1.Job) {
	// log.Printf("job event: %s, %s", job.GetName(), job.Status.String())
}

func (c *MainController) sendEvent(ctx context.Context, job *batchv1.Job) {
	e := event.Event{
		Namespace: job.Namespace,
		Type:      job.TypeMeta.Kind,
		Resource:  job,
	}

	// TODO: 起動時に過去のイベントも送信されてくるので日付をチェックして過去のイベントは送信しないように実装

	h, err := handler.CreateHandler()
	if err != nil {
		return
	}
	go h.Handle(e)
}

func (c *MainController) cronjobEvent(ctx context.Context, cj *batchv1beta1.CronJob) {
	// 今の所cronjobのイベントに対しては何もしない
}
