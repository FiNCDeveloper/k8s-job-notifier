package controller

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/FiNCDeveloper/k8s-job-notifier/event"
	"github.com/FiNCDeveloper/k8s-job-notifier/slack"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
)

// recordingHandler applies the real Slack notification decision and records
// the names of the Jobs that would have been posted, instead of posting.
// handled receives one value per completed Handle call, notified or not.
type recordingHandler struct {
	decider  *slack.Slack
	mu       sync.Mutex
	notified []string
	handled  chan struct{}
}

func (h *recordingHandler) Handle(e event.Event) {
	defer func() { h.handled <- struct{}{} }()
	if h.decider.NotifiableCondition(e) == nil {
		return
	}
	h.mu.Lock()
	h.notified = append(h.notified, e.Resource.(*batchv1.Job).Name)
	h.mu.Unlock()
}

func testJob(name string, created time.Time, resourceVersion string, failed bool) batchv1.Job {
	j := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:         "test",
			Name:              name,
			ResourceVersion:   resourceVersion,
			CreationTimestamp: metav1.NewTime(created),
			Annotations:       map[string]string{slack.EnabledAnnotation: "true"},
		},
	}
	if failed {
		j.Status.Conditions = []batchv1.JobCondition{
			{Type: "FailureTarget", Status: corev1.ConditionTrue},
			{Type: batchv1.JobFailed, Status: corev1.ConditionTrue},
		}
	}
	return j
}

// 2026-09-29 の本番障害の再現: watch が 503 で失敗して relist が走っても、既に通知済みの
// 失敗 Job は再通知されず、状態遷移と relist で初めて見えた失敗 Job だけが通知される。
// watch 断の間に削除された Job（relist では DeletedFinalStateUnknown で届く）で落ちないことも確認する。
func TestJobInformerNotifiesTransitionsOnlyAcrossRelist(t *testing.T) {
	startTime := time.Now()
	before := startTime.Add(-time.Hour)
	after := startTime.Add(time.Minute)

	// 初回 list: 起動後に作られ実行中(J)、起動前に作られ実行中(L)、起動前に失敗済み(M)、watch 断の間に削除される(D)
	initialList := []batchv1.Job{
		testJob("J", after, "10", false),
		testJob("L", before, "11", false),
		testJob("M", before, "12", true),
		testJob("D", before, "9", true),
	}
	// watch: J と L が失敗に遷移する
	firstWatch := watch.NewFakeWithChanSize(2, false)
	jFailed := testJob("J", after, "13", true)
	lFailed := testJob("L", before, "14", true)
	firstWatch.Modify(&jFailed)
	firstWatch.Modify(&lFailed)
	firstWatch.Stop()
	// relist: J/L/M は変化なし、D は消えており、watch 断の間に作られ失敗した K が初めて見える
	relist := []batchv1.Job{jFailed, lFailed, initialList[2], testJob("K", after.Add(time.Minute), "15", true)}

	var mu sync.Mutex
	listCalls, watchCalls := 0, 0
	lw := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			mu.Lock()
			defer mu.Unlock()
			listCalls++
			if listCalls == 1 {
				return &batchv1.JobList{ListMeta: metav1.ListMeta{ResourceVersion: "12"}, Items: initialList}, nil
			}
			return &batchv1.JobList{ListMeta: metav1.ListMeta{ResourceVersion: "15"}, Items: relist}, nil
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			mu.Lock()
			defer mu.Unlock()
			watchCalls++
			switch watchCalls {
			case 1:
				return firstWatch, nil
			case 2:
				// 本番ログと同じ "the server is currently unable to handle the request"
				return nil, apierrors.NewServiceUnavailable("the server is currently unable to handle the request")
			default:
				return watch.NewFake(), nil
			}
		},
	}

	h := &recordingHandler{
		decider: &slack.Slack{NotifyCondisions: []string{"Failed"}},
		handled: make(chan struct{}, 20),
	}
	c := &MainController{handler: h, startTime: startTime}

	stopCh := make(chan struct{})
	defer close(stopCh)
	go c.newJobInformer(context.Background(), lw).Run(stopCh)

	// handler は goroutine で呼ばれるので、handler に渡るイベントがすべて処理し終わるまで待つ。
	// 渡るのは次の 7 件（L/M/D の初回 Add は起動前に作られたので渡らず、relist で消えた D の削除も渡らない）:
	// 初回 list の Add J / watch の Update J, L / relist の Update J, L, M と Add K
	const wantHandled = 7
	timeout := time.After(10 * time.Second)
	for i := 0; i < wantHandled; i++ {
		select {
		case <-h.handled:
		case <-timeout:
			t.Fatalf("handled %d of %d events; notified so far: %v", i, wantHandled, h.snapshot())
		}
	}
	// 遅れて届く余分な呼び出し（重複通知の元）が無いことも確かめる。
	select {
	case <-h.handled:
		t.Fatalf("handled more than %d events; notified so far: %v", wantHandled, h.snapshot())
	case <-time.After(200 * time.Millisecond):
	}

	got := h.snapshot()
	want := []string{"J", "K", "L"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("notified %v, want %v", got, want)
	}
	mu.Lock()
	defer mu.Unlock()
	if listCalls < 2 {
		t.Fatalf("relist did not happen (list calls: %d)", listCalls)
	}
}

func (h *recordingHandler) snapshot() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	got := append([]string(nil), h.notified...)
	sort.Strings(got)
	return got
}
