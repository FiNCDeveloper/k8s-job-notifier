package main

import (
	"fmt"
	"time"

	"github.com/FiNCDeveloper/k8s-job-notifier/controller"
	"github.com/FiNCDeveloper/k8s-job-notifier/handler"
	"github.com/FiNCDeveloper/k8s-job-notifier/utils"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// heartbeatInterval controls how often job-notifier reports itself alive to
// Slack. This exists to detect silent failures (e.g. the CronJob/Job
// informer failing to sync, or an expired Slack token) that leave the
// process Running with zero error logs and zero CronJob failure
// notifications, since nothing else would catch that state.
const heartbeatInterval = 24 * time.Hour

func main() {
	var kubeClient kubernetes.Interface

	if _, err := rest.InClusterConfig(); err != nil {
		kubeClient = utils.GetClientOutOfCluster()
	} else {
		kubeClient = utils.GetClient()
	}

	go runHeartbeat()

	c := controller.NewMainController(kubeClient)
	c.Run()
}

func runHeartbeat() {
	slackClient := handler.CreateSlackClient()

	post := func() {
		msg := fmt.Sprintf("job-notifier heartbeat: alive (%s)", time.Now().Local().Format(time.RFC3339))
		slackClient.PostHeartbeat(msg)
	}

	post() // 起動直後にも1回送り、デプロイ後の疎通確認を兼ねる

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	for range ticker.C {
		post()
	}
}
