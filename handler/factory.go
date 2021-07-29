package handler

import (
	"os"

	"github.com/FiNCDeveloper/k8s-job-notifier/slack"
)

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

	return &slack.Slack{
		Token:            os.Getenv("SLACK_TOKEN"),
		DefaultChannel:   dc,
		Title:            "job notify",
		NotifyCondisions: []string{"Failed"},
	}, nil
}
