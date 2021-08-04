package handler

import (
	"os"

	"github.com/FiNCDeveloper/k8s-job-notifier/event"
	"github.com/FiNCDeveloper/k8s-job-notifier/slack"
)

type Handler interface {
	Handle(e event.Event)
}

func CreateHandler() (Handler, error) {
	h, err := createSlackHandler()

	if err != nil {
		return nil, err
	}

	return h, nil
}

func createSlackHandler() (Handler, error) {
	dc := os.Getenv("DEFAULT_CHANNEL")
	if len(dc) == 0 {
		dc = "#bot_sandbox"
	}

	enabled := false
	switch s := os.Getenv("SLACK_DEFAULT_ENABLED"); s {
	case "true":
		enabled = true
	case "false":
		enabled = false
	default:
		enabled = false
	}

	return &slack.Slack{
		Token:            os.Getenv("SLACK_TOKEN"),
		DefaultChannel:   dc,
		Title:            "job notify",
		NotifyCondisions: []string{"Failed"},
		DefaultEnabled:   enabled,
	}, nil
}
