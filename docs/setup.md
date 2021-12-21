# Installation

manifestを用意したので適宜修正して利用してください。

[RBAC and Deployment example manifests](https://github.com/FiNCDeveloper/k8s-job-notifier/blob/main/manifest_examples)

## 手順

1. Setup slack app
1. Setup k8s RBAC
1. Deploy workload

## Setup slack app

### Create Slack App

https://api.slack.com/apps

### Create Slack App and Grant Scope

Slack API > OAuth & Permissions > Bot User OAuth Token

Slack AppのBot Token Scopeに以下のscopeを与えます。

scope
- `chat:write`
- `chat:write.public `

See: https://api.slack.com/methods/chat.postMessage

## Setup k8s RBAC

`k8s-job-notifier`は以下のk8s権限を要求します。

```rbac.yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: k8s-job-notifier
rules:
- apiGroups:
  - ""
  resources:
  - pods
  - pods/log
  verbs: ["get", "list", "watch"]
- apiGroups: ["batch"]
  resources:
  - jobs
  - cronjobs
  verbs: ["get", "list", "watch"]
```


## 🚀 Deploy workload

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: notify
  labels:
    app: notify
    app.kubernetes.io/name: notify
    app.kubernetes.io/version: "1.0"
spec:
  replicas: 1 # 必ず '1'
  selector:
    matchLabels:
      app: "notify"
  template:
    metadata:
      labels:
        app: "notify"
    spec:
      serviceAccountName: k8s-job-notifier-sa
      containers:
      - name: notify
        image: 759549166074.dkr.ecr.ap-northeast-1.amazonaws.com/job_notifier
        imagePullPolicy: Always
        env:
          - name: DEFAULT_CHANNEL
            value: "#bot_sandbox"
          - name: SLACK_DEFAULT_ENABLED
            value: "false"

```
