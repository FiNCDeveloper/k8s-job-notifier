package handler

import "github.com/FiNCDeveloper/k8s-job-notifier/event"

type Handler interface {
	Handle(e event.Event)
}
