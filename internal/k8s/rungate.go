package k8s

import (
	"context"
	"fmt"
	"log/slog"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// RunGate reports whether a Renovate run is currently active in the CronJob
// namespace, so the collector can avoid starting an overlapping run. It counts
// both our triggered Jobs (by managed-by label) and the source CronJob's
// scheduled Jobs (by owner reference).
type RunGate struct {
	client           kubernetes.Interface
	cronJobName      string
	cronJobNamespace string
	logger           *slog.Logger
}

func NewRunGate(client kubernetes.Interface, cronJobName, cronJobNamespace string, logger *slog.Logger) *RunGate {
	return &RunGate{
		client:           client,
		cronJobName:      cronJobName,
		cronJobNamespace: cronJobNamespace,
		logger:           logger,
	}
}

// Active reports whether any Renovate run is executing. A list error is returned
// to the caller, which treats it as active (postpone) rather than racing.
func (g *RunGate) Active(ctx context.Context) (bool, error) {
	jobs, err := g.client.BatchV1().Jobs(g.cronJobNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return false, fmt.Errorf("listing jobs in %s: %w", g.cronJobNamespace, err)
	}
	for i := range jobs.Items {
		job := &jobs.Items[i]
		if jobFinished(job) {
			continue
		}
		if g.isRenovateRun(job) {
			g.logger.Debug("active renovate run detected, flush will postpone", "job", job.Name)
			return true, nil
		}
	}
	return false, nil
}

// jobFinished reports whether a Job has completed or failed (and so is no longer
// an active run).
func jobFinished(job *batchv1.Job) bool {
	for _, c := range job.Status.Conditions {
		if c.Status != corev1.ConditionTrue {
			continue
		}
		if c.Type == batchv1.JobComplete || c.Type == batchv1.JobFailed {
			return true
		}
	}
	return false
}

// isRenovateRun reports whether a Job is a Renovate run — either one we created
// or one the source CronJob scheduled.
func (g *RunGate) isRenovateRun(job *batchv1.Job) bool {
	if job.Labels[managedByLabel] == managedByValue {
		return true
	}
	for _, ref := range job.OwnerReferences {
		if ref.Kind == "CronJob" && ref.Name == g.cronJobName {
			return true
		}
	}
	return false
}
