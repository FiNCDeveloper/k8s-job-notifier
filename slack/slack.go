package slack

import (
	"fmt"
	"log"
	"strings"

	"github.com/FiNCDeveloper/k8s-job-notifier/event"
	"github.com/slack-go/slack"
	batchv1 "k8s.io/api/batch/v1"
)

var slackMsg = `
Name: %s/%s
Message: %s
Status: %s
CompletionTime: %s
Print log command: %s
Rerun command: %s
`

// Slack handler implements handler.Handler interface,
// Notify event to slack channel
type Slack struct {
	Token            string
	DefaultChannel   string
	Title            string
	NotifyCondisions []string
	DefaultEnabled   bool
}

const (
	// annotationPrefix = "notify.sho2010.dev/"
	annotationPrefix = "notify-slack.finc.com/"

	// ChannelAnnotation is annotation key of slack message destination channel
	//
	// const: 'notify.sho2010.dev/channel'
	ChannelAnnotation = annotationPrefix + "channel"

	// NotifyConditionAnnotation is
	//
	// value example: "Complete,Failed"
	NotifyConditionAnnotation = annotationPrefix + "conditions"

	// EnabledAnnotation is annotation key of enabled slack notification
	//
	// value: true/false
	EnabledAnnotation = annotationPrefix + "enabled"
)

// Handle handles the notification.
func (s *Slack) Handle(e event.Event) {

	//TODO: おそらく起動したときはConditions == 0 で判定できるはず
	// job createのときはconditionsが空でくる、他にいい判定方法があればそれに変える
	job := e.Resource.(*batchv1.Job)
	if len(job.Status.Conditions) == 0 {
		return
	}
	annotations := job.GetAnnotations()

	enabled := s.DefaultEnabled
	switch s := annotations[EnabledAnnotation]; s {
	case "true":
		enabled = true
	case "false":
		enabled = false
	}
	if !enabled {
		log.Printf("%s ignore, annotation value: %s", job.Name, annotations[EnabledAnnotation])
		return
	}

	var notifyCondisions []string
	if len(annotations[NotifyConditionAnnotation]) == 0 {
		notifyCondisions = s.NotifyCondisions
	} else {
		notifyCondisions = strings.Split(annotations[NotifyConditionAnnotation], ",")
	}

	isNotifyEvent := false
	jobCon := strings.ToLower(string(job.Status.Conditions[0].Type))

	for _, con := range notifyCondisions {
		con = strings.TrimSpace(con)
		con = strings.ToLower(con)

		if con == jobCon {
			isNotifyEvent = true
			break
		}
	}

	if !isNotifyEvent {
		return
	}

	channel := annotations[ChannelAnnotation]
	if len(channel) == 0 {
		channel = s.DefaultChannel
	} else {
		log.Printf("channel annotation find: %s\n", annotations[ChannelAnnotation])
	}
	attachment := buildAttachment(e, s)

	client := slack.New(s.Token)
	channelID, timestamp, err := client.PostMessage(channel,
		slack.MsgOptionAttachments(attachment),
		slack.MsgOptionAsUser(false),
		slack.MsgOptionIconEmoji(":sushi:"))
	if err != nil {
		log.Printf("slack error: %s\n", err)
		return
	}

	log.Printf("Message successfully sent to channel %s at %s", channelID, timestamp)
}

func slackColor(t batchv1.JobConditionType) string {
	switch t {
	case batchv1.JobSuspended:
		return "warning"
	case batchv1.JobComplete:
		return "good"
	case batchv1.JobFailed:
		return "danger"
	default:
		return "good"
	}
}

func buildMessage(e event.Event) string {
	job := e.Resource.(*batchv1.Job)

	logCommand := fmt.Sprintf("`kubectl logs -n %s job/%s`", job.Namespace, job.Name)

	var cronName string
	rerunCommand := "Unknown. Becuase base CronJob resource notfound."

	if len(job.OwnerReferences) > 0 {
		cronName = job.OwnerReferences[0].Name
		rerunCommand = fmt.Sprintf("`kubectl create job %s-debug -n %s --from cronjob/%s`", job.Name, job.Namespace, cronName)
	}

	// 同時実行数が1前提で作られるので仕様を考える
	s := fmt.Sprintf(slackMsg,
		job.Namespace, job.Name,
		job.Status.Conditions[0].Message,
		job.Status.Conditions[0].Type,
		job.Status.CompletionTime,
		logCommand,
		rerunCommand,
	)
	return s
}

func buildAttachment(e event.Event, s *Slack) slack.Attachment {
	mes := buildMessage(e)
	attachment := slack.Attachment{
		Fields: []slack.AttachmentField{
			{
				Title: s.Title,
				Value: mes,
			},
		},
	}

	// TODO: とりあえずここに書くがリファクタする
	job := e.Resource.(*batchv1.Job)
	attachment.Color = slackColor(job.Status.Conditions[0].Type)
	attachment.MarkdownIn = []string{"fields"}

	return attachment
}
