package slack

import (
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

func cond(t batchv1.JobConditionType, status corev1.ConditionStatus) batchv1.JobCondition {
	return batchv1.JobCondition{Type: t, Status: status, Message: "test message"}
}

func TestMatchCondition(t *testing.T) {
	tests := []struct {
		name             string
		conditions       []batchv1.JobCondition
		notifyConditions []string
		wantMatch        bool
		wantType         batchv1.JobConditionType
	}{
		{
			// Kubernetes 1.25+ の実挙動: Failed の前に FailureTarget が入る
			name: "k8s 1.25+ failed job with leading FailureTarget matches Failed",
			conditions: []batchv1.JobCondition{
				cond("FailureTarget", corev1.ConditionTrue),
				cond(batchv1.JobFailed, corev1.ConditionTrue),
			},
			notifyConditions: []string{"Failed"},
			wantMatch:        true,
			wantType:         batchv1.JobFailed,
		},
		{
			// Kubernetes 1.25+ の実挙動: Complete の前に SuccessCriteriaMet が入る
			name: "k8s 1.25+ succeeded job with leading SuccessCriteriaMet does not match Failed",
			conditions: []batchv1.JobCondition{
				cond("SuccessCriteriaMet", corev1.ConditionTrue),
				cond(batchv1.JobComplete, corev1.ConditionTrue),
			},
			notifyConditions: []string{"Failed"},
			wantMatch:        false,
		},
		{
			name: "succeeded job matches when Complete is explicitly requested",
			conditions: []batchv1.JobCondition{
				cond("SuccessCriteriaMet", corev1.ConditionTrue),
				cond(batchv1.JobComplete, corev1.ConditionTrue),
			},
			notifyConditions: []string{"Complete"},
			wantMatch:        true,
			wantType:         batchv1.JobComplete,
		},
		{
			// 旧 Kubernetes（中間 condition なし）でも従来どおり動く
			name: "legacy single Failed condition still matches",
			conditions: []batchv1.JobCondition{
				cond(batchv1.JobFailed, corev1.ConditionTrue),
			},
			notifyConditions: []string{"Failed"},
			wantMatch:        true,
			wantType:         batchv1.JobFailed,
		},
		{
			name: "condition with Status=False is ignored even if Type matches",
			conditions: []batchv1.JobCondition{
				cond(batchv1.JobFailed, corev1.ConditionFalse),
			},
			notifyConditions: []string{"Failed"},
			wantMatch:        false,
		},
		{
			name: "matching is case-insensitive and trims whitespace",
			conditions: []batchv1.JobCondition{
				cond(batchv1.JobFailed, corev1.ConditionTrue),
			},
			notifyConditions: []string{" failed "},
			wantMatch:        true,
			wantType:         batchv1.JobFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchCondition(tt.conditions, tt.notifyConditions)

			if tt.wantMatch && got == nil {
				t.Fatalf("expected a match, got nil")
			}
			if !tt.wantMatch && got != nil {
				t.Fatalf("expected no match, got %+v", got)
			}
			if tt.wantMatch && got.Type != tt.wantType {
				t.Fatalf("expected matched type %q, got %q", tt.wantType, got.Type)
			}
		})
	}
}
