# k8s-job-notifier

# Permissions

### k8s RBAC

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

### Slack

https://api.slack.com/methods/chat.postMessage

scope

- `chat:write`
- `chat:write.public `
