package main

import (
	"github.com/FiNCDeveloper/k8s-job-notifier/controller"
	"github.com/FiNCDeveloper/k8s-job-notifier/utils"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func main() {
	var kubeClient kubernetes.Interface

	if _, err := rest.InClusterConfig(); err != nil {
		kubeClient = utils.GetClientOutOfCluster()
	} else {
		kubeClient = utils.GetClient()
	}

	c := controller.NewMainController(kubeClient)
	c.Run()
}
