package k8s

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func createFakeCronJob(client *fake.Clientset, name, namespace string) error {
	cronJob := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: batchv1.CronJobSpec{
			Schedule: "0 3 * * *",
			JobTemplate: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "renovate",
									Image: "renovate/renovate:latest",
									Env: []corev1.EnvVar{
										{Name: "RENOVATE_PLATFORM", Value: "github"},
										{Name: "RENOVATE_REPOSITORIES", Value: `["default/repo"]`},
									},
								},
							},
							RestartPolicy: corev1.RestartPolicyNever,
						},
					},
				},
			},
		},
	}
	_, err := client.BatchV1().CronJobs(namespace).Create(context.Background(), cronJob, metav1.CreateOptions{})
	return err
}

func TestVerifyCronJobExists(t *testing.T) {
	client := fake.NewSimpleClientset()
	if err := createFakeCronJob(client, "renovate", "renovate"); err != nil {
		t.Fatal(err)
	}

	jc := NewJobCreator(client, "renovate", "renovate", testLogger())
	if err := jc.Verify(context.Background()); err != nil {
		t.Errorf("Verify error for existing cronjob: %v", err)
	}
}

func TestVerifyCronJobMissing(t *testing.T) {
	client := fake.NewSimpleClientset()

	jc := NewJobCreator(client, "nonexistent", "renovate", testLogger())
	if err := jc.Verify(context.Background()); err == nil {
		t.Fatal("expected error for missing cronjob")
	}
}

func TestCreateJobForRepos(t *testing.T) {
	client := fake.NewSimpleClientset()
	if err := createFakeCronJob(client, "renovate", "renovate"); err != nil {
		t.Fatal(err)
	}

	jc := NewJobCreator(client, "renovate", "renovate", testLogger())

	repos := []string{"org/repo-a", "org/repo-b"}
	_, err := jc.CreateJobForRepos(context.Background(), repos)
	if err != nil {
		t.Fatalf("CreateJobForRepos error: %v", err)
	}

	jobs, err := client.BatchV1().Jobs("renovate").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs.Items) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs.Items))
	}

	job := jobs.Items[0]

	// Check labels
	if job.Labels["app.kubernetes.io/managed-by"] != "renovate-trigger" {
		t.Error("missing managed-by label")
	}

	// Check annotations
	if job.Annotations["renovate-trigger/repos"] != "org/repo-a,org/repo-b" {
		t.Errorf("unexpected repos annotation: %s", job.Annotations["renovate-trigger/repos"])
	}

	// Check env override
	container := job.Spec.Template.Spec.Containers[0]
	var foundEnv string
	for _, e := range container.Env {
		if e.Name == "RENOVATE_REPOSITORIES" {
			foundEnv = e.Value
			break
		}
	}

	var envRepos []string
	if err := json.Unmarshal([]byte(foundEnv), &envRepos); err != nil {
		t.Fatalf("failed to parse RENOVATE_REPOSITORIES: %v", err)
	}
	if len(envRepos) != 2 || envRepos[0] != "org/repo-a" || envRepos[1] != "org/repo-b" {
		t.Errorf("unexpected RENOVATE_REPOSITORIES: %v", envRepos)
	}
}

func TestCreateJobForReposCronJobNotFound(t *testing.T) {
	client := fake.NewSimpleClientset()
	jc := NewJobCreator(client, "nonexistent", "renovate", testLogger())

	_, err := jc.CreateJobForRepos(context.Background(), []string{"org/repo"})
	if err == nil {
		t.Fatal("expected error for nonexistent cronjob")
	}
}

func TestCreateJobAppendEnvWhenMissing(t *testing.T) {
	client := fake.NewSimpleClientset()

	// Create CronJob without RENOVATE_REPOSITORIES env
	cronJob := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "renovate",
			Namespace: "renovate",
		},
		Spec: batchv1.CronJobSpec{
			Schedule: "0 3 * * *",
			JobTemplate: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "renovate",
									Image: "renovate/renovate:latest",
									Env: []corev1.EnvVar{
										{Name: "RENOVATE_PLATFORM", Value: "github"},
									},
								},
							},
							RestartPolicy: corev1.RestartPolicyNever,
						},
					},
				},
			},
		},
	}
	client.BatchV1().CronJobs("renovate").Create(context.Background(), cronJob, metav1.CreateOptions{})

	jc := NewJobCreator(client, "renovate", "renovate", testLogger())
	_, err := jc.CreateJobForRepos(context.Background(), []string{"org/repo"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	jobs, _ := client.BatchV1().Jobs("renovate").List(context.Background(), metav1.ListOptions{})
	container := jobs.Items[0].Spec.Template.Spec.Containers[0]

	found := false
	for _, e := range container.Env {
		if e.Name == "RENOVATE_REPOSITORIES" {
			found = true
			break
		}
	}
	if !found {
		t.Error("RENOVATE_REPOSITORIES env var should be appended when missing")
	}
}

func TestOverrideEnvReplacesExisting(t *testing.T) {
	envs := []corev1.EnvVar{
		{Name: "FOO", Value: "bar"},
		{Name: "TARGET", Value: "old"},
		{Name: "BAZ", Value: "qux"},
	}

	result := overrideEnv(envs, "TARGET", "new")

	if len(result) != 3 {
		t.Fatalf("expected 3 env vars, got %d", len(result))
	}
	for _, e := range result {
		if e.Name == "TARGET" && e.Value != "new" {
			t.Errorf("TARGET = %q, want new", e.Value)
		}
	}
}

func TestOverrideEnvAppendsNew(t *testing.T) {
	envs := []corev1.EnvVar{
		{Name: "FOO", Value: "bar"},
	}

	result := overrideEnv(envs, "NEW_VAR", "value")

	if len(result) != 2 {
		t.Fatalf("expected 2 env vars, got %d", len(result))
	}
	if result[1].Name != "NEW_VAR" || result[1].Value != "value" {
		t.Errorf("unexpected appended env: %+v", result[1])
	}
}
