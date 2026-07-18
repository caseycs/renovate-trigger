package batch

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/ilia/renovate-trigger/internal/ghapp"
	"github.com/ilia/renovate-trigger/internal/k8s"
	"github.com/ilia/renovate-trigger/internal/resolve"
	"github.com/ilia/renovate-trigger/internal/webhook"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// This is the ticket #5 tracer-bullet integration test: a signed tag webhook on
// a dependency, through the real handler -> collector -> real Resolver (GitHub
// faked over httptest) -> client-go fake, produces one Renovate Job whose target
// repos are the deduplicated dependents. The flush is driven deterministically
// by calling attemptFlush directly (the window is long enough that the timer
// never fires).
//
// It lives white-box in package batch so it can drive the unexported
// attemptFlush deterministically. If more cross-package flow tests accrue,
// relocate them to a dedicated internal/integration package so batch stays
// black-box.

// fakeGitHub serves the installation, token, and contents endpoints for org/lib.
func fakeGitHub(t *testing.T, triggerJSON string, contentsStatus int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/org/lib/installation", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"id": 42}`)
	})
	mux.HandleFunc("/app/installations/42/access_tokens", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, `{"token":"ghs_test","expires_at":%q}`, time.Now().Add(time.Hour).Format(time.RFC3339))
	})
	mux.HandleFunc("/repos/org/lib/contents/renovate.trigger.json", func(w http.ResponseWriter, _ *http.Request) {
		if contentsStatus != http.StatusOK {
			w.WriteHeader(contentsStatus)
			return
		}
		enc := base64.StdEncoding.EncodeToString([]byte(triggerJSON))
		fmt.Fprintf(w, `{"encoding":"base64","content":%q}`, enc)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func flowTestKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	return key
}

func flowCronJob(t *testing.T, kube *fake.Clientset, name, ns string) {
	t.Helper()
	cj := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: batchv1.CronJobSpec{
			Schedule: "0 3 * * *",
			JobTemplate: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Name:  "renovate",
								Image: "renovate/renovate:latest",
							}},
							RestartPolicy: corev1.RestartPolicyNever,
						},
					},
				},
			},
		},
	}
	if _, err := kube.BatchV1().CronJobs(ns).Create(context.Background(), cj, metav1.CreateOptions{}); err != nil {
		t.Fatalf("creating cronjob: %v", err)
	}
}

// postTag delivers a correctly-signed tag create webhook for repo through h.
func postTag(t *testing.T, h http.Handler, secret, repo, tag string) {
	t.Helper()
	body, _ := json.Marshal(webhook.CreateEvent{
		Ref:        tag,
		RefType:    "tag",
		Repository: webhook.Repository{FullName: repo},
	})
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	req.Header.Set("X-GitHub-Event", "create")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("webhook status = %d, want 202", rr.Code)
	}
}

func listJobs(t *testing.T, kube *fake.Clientset, ns string) []batchv1.Job {
	t.Helper()
	jobs, err := kube.BatchV1().Jobs(ns).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("listing jobs: %v", err)
	}
	return jobs.Items
}

func renovateRepos(t *testing.T, job batchv1.Job) []string {
	t.Helper()
	for _, e := range job.Spec.Template.Spec.Containers[0].Env {
		if e.Name != "RENOVATE_REPOSITORIES" {
			continue
		}
		var repos []string
		if err := json.Unmarshal([]byte(e.Value), &repos); err != nil {
			t.Fatalf("parsing RENOVATE_REPOSITORIES: %v", err)
		}
		sort.Strings(repos)
		return repos
	}
	t.Fatal("RENOVATE_REPOSITORIES not set on job")
	return nil
}

func newFlowCollector(t *testing.T, kube *fake.Clientset, gh *httptest.Server) *Collector {
	t.Helper()
	ghClient := ghapp.NewClient("Iv23liTEST", flowTestKey(t), ghapp.WithBaseURL(gh.URL))
	resolver := resolve.New(ghClient, testLogger())
	jc := k8s.NewJobCreator(kube, "renovate", "renovate", testLogger())
	// A long window means the timer never fires during the test; we drive the
	// flush ourselves via attemptFlush.
	return NewCollector(time.Hour, openGate{}, resolver, jc, testLogger())
}

func TestFlushCreatesRenovateRunFromWebhook(t *testing.T) {
	gh := fakeGitHub(t, `{"tags":["org/app-2","org/app-1","org/app-1"]}`, http.StatusOK)
	kube := fake.NewSimpleClientset()
	flowCronJob(t, kube, "renovate", "renovate")

	c := newFlowCollector(t, kube, gh)
	defer c.Stop()

	secret := "webhook-secret"
	h := webhook.NewHandler(secret, c, testLogger())

	postTag(t, h, secret, "org/lib", "v1.0.0")
	c.attemptFlush()

	jobs := listJobs(t, kube, "renovate")
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	got := renovateRepos(t, jobs[0])
	if len(got) != 2 || got[0] != "org/app-1" || got[1] != "org/app-2" {
		t.Errorf("RENOVATE_REPOSITORIES = %v, want [org/app-1 org/app-2] (deduped)", got)
	}
}

func TestFlushNoDependentsCreatesNoRun(t *testing.T) {
	gh := fakeGitHub(t, "", http.StatusNotFound) // source has no trigger declaration
	kube := fake.NewSimpleClientset()
	flowCronJob(t, kube, "renovate", "renovate")

	c := newFlowCollector(t, kube, gh)
	defer c.Stop()

	secret := "webhook-secret"
	h := webhook.NewHandler(secret, c, testLogger())

	postTag(t, h, secret, "org/lib", "v1.0.0")
	c.attemptFlush()

	if jobs := listJobs(t, kube, "renovate"); len(jobs) != 0 {
		t.Errorf("expected no job for empty resolution, got %d", len(jobs))
	}
}
