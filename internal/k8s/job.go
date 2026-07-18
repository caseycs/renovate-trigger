package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// managedByLabel/managedByValue mark the Jobs we create, so the run gate can
	// tell our runs apart from unrelated Jobs.
	managedByLabel = "app.kubernetes.io/managed-by"
	managedByValue = "renovate-trigger"
)

type JobCreator struct {
	client           kubernetes.Interface
	cronJobName      string
	cronJobNamespace string
	logger           *slog.Logger
}

func NewJobCreator(client kubernetes.Interface, cronJobName, cronJobNamespace string, logger *slog.Logger) *JobCreator {
	return &JobCreator{
		client:           client,
		cronJobName:      cronJobName,
		cronJobNamespace: cronJobNamespace,
		logger:           logger,
	}
}

// Verify asserts the source CronJob is gettable, so a misconfigured
// RT_CRONJOB_NAME/RT_CRONJOB_NAMESPACE crash-loops the Deployment loudly at
// startup instead of silently dropping every trigger at flush time.
func (jc *JobCreator) Verify(ctx context.Context) error {
	_, err := jc.client.BatchV1().CronJobs(jc.cronJobNamespace).Get(ctx, jc.cronJobName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting cronjob %s/%s: %w", jc.cronJobNamespace, jc.cronJobName, err)
	}
	return nil
}

func (jc *JobCreator) CreateJobForRepos(ctx context.Context, repos []string) (string, error) {
	cronJob, err := jc.client.BatchV1().CronJobs(jc.cronJobNamespace).Get(ctx, jc.cronJobName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("getting cronjob %s/%s: %w", jc.cronJobNamespace, jc.cronJobName, err)
	}

	jobSpec := cronJob.Spec.JobTemplate.Spec.DeepCopy()

	repoJSON, err := json.Marshal(repos)
	if err != nil {
		return "", fmt.Errorf("marshaling repos: %w", err)
	}

	containers := make([]corev1.Container, len(jobSpec.Template.Spec.Containers))
	for i, c := range jobSpec.Template.Spec.Containers {
		c.Env = overrideEnv(c.Env, "RENOVATE_REPOSITORIES", string(repoJSON))
		containers[i] = c
	}
	jobSpec.Template.Spec.Containers = containers

	repoAnnotation := strings.Join(repos, ",")

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "renovate-trigger-",
			Namespace:    jc.cronJobNamespace,
			Labels: map[string]string{
				managedByLabel: managedByValue,
			},
			Annotations: map[string]string{
				"renovate-trigger/repos":        repoAnnotation,
				"renovate-trigger/triggered-at": time.Now().UTC().Format(time.RFC3339),
			},
		},
		Spec: *jobSpec,
	}

	created, err := jc.client.BatchV1().Jobs(jc.cronJobNamespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("creating job: %w", err)
	}

	jc.logger.Info("job created", "job", created.Name, "namespace", jc.cronJobNamespace, "repos", repos)
	return created.Name, nil
}

func overrideEnv(envs []corev1.EnvVar, name, value string) []corev1.EnvVar {
	result := make([]corev1.EnvVar, 0, len(envs)+1)
	found := false
	for _, e := range envs {
		if e.Name == name {
			result = append(result, corev1.EnvVar{Name: name, Value: value})
			found = true
		} else {
			result = append(result, e)
		}
	}
	if !found {
		result = append(result, corev1.EnvVar{Name: name, Value: value})
	}
	return result
}
