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

// CreateSlackClient builds the *slack.Slack config from environment
// variables. It is exposed (in addition to CreateHandler) so callers that
// need Slack-specific behavior not covered by the Handler interface (e.g.
// heartbeat notifications) can reuse the same configuration.
func CreateSlackClient() *slack.Slack {
	dc := os.Getenv("DEFAULT_CHANNEL")
	if len(dc) == 0 {
		dc = "#bot_sandbox"
	}

	hc := os.Getenv("HEARTBEAT_CHANNEL")
	if len(hc) == 0 {
		hc = "#system_alert_info"
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
		HeartbeatChannel: hc,
		Title:            "job notify",
		NotifyCondisions: []string{"Failed"},
		DefaultEnabled:   enabled,
	}
}

func createSlackHandler() (Handler, error) {
	return CreateSlackClient(), nil
}
