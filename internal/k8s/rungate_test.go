package k8s

import (
	"context"
	"errors"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"

	"k8s.io/client-go/kubernetes/fake"
)

const gateNS = "renovate"

func mkJob(name string, ours, cronOwned, finished bool, cond batchv1.JobConditionType) batchv1.Job {
	j := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: gateNS},
	}
	if ours {
		j.Labels = map[string]string{managedByLabel: managedByValue}
	}
	if cronOwned {
		j.OwnerReferences = []metav1.OwnerReference{{Kind: "CronJob", Name: "renovate"}}
	}
	if finished {
		j.Status.Conditions = []batchv1.JobCondition{{Type: cond, Status: corev1.ConditionTrue}}
	}
	return j
}

func gateWith(t *testing.T, jobs ...batchv1.Job) *RunGate {
	t.Helper()
	objs := make([]runtime.Object, 0, len(jobs))
	for i := range jobs {
		objs = append(objs, &jobs[i])
	}
	client := fake.NewSimpleClientset(objs...)
	return NewRunGate(client, "renovate", gateNS, testLogger())
}

func TestRunGateActiveMatrix(t *testing.T) {
	tests := []struct {
		name string
		job  batchv1.Job
		want bool
	}{
		{"active ours", mkJob("a", true, false, false, ""), true},
		{"active cronjob-owned", mkJob("b", false, true, false, ""), true},
		{"active unrelated", mkJob("c", false, false, false, ""), false},
		{"completed ours", mkJob("d", true, false, true, batchv1.JobComplete), false},
		{"failed ours", mkJob("e", true, false, true, batchv1.JobFailed), false},
		{"completed cronjob-owned", mkJob("f", false, true, true, batchv1.JobComplete), false},
		{"failed cronjob-owned", mkJob("g", false, true, true, batchv1.JobFailed), false},
		{"completed unrelated", mkJob("h", false, false, true, batchv1.JobComplete), false},
		{"failed unrelated", mkJob("i", false, false, true, batchv1.JobFailed), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gateWith(t, tt.job)
			active, err := g.Active(context.Background())
			if err != nil {
				t.Fatalf("Active error: %v", err)
			}
			if active != tt.want {
				t.Errorf("Active() = %v, want %v", active, tt.want)
			}
		})
	}
}

func TestRunGateNoJobs(t *testing.T) {
	g := gateWith(t)
	active, err := g.Active(context.Background())
	if err != nil {
		t.Fatalf("Active error: %v", err)
	}
	if active {
		t.Error("Active() = true, want false when no jobs exist")
	}
}

// An active Renovate run alongside finished/unrelated jobs still reports active.
func TestRunGateActiveAmongFinished(t *testing.T) {
	g := gateWith(t,
		mkJob("done", true, false, true, batchv1.JobComplete),
		mkJob("other", false, false, false, ""),
		mkJob("running", false, true, false, ""),
	)
	active, err := g.Active(context.Background())
	if err != nil {
		t.Fatalf("Active error: %v", err)
	}
	if !active {
		t.Error("Active() = false, want true (a cronjob-owned run is in progress)")
	}
}

// A different CronJob's Jobs do not count.
func TestRunGateIgnoresOtherCronJob(t *testing.T) {
	job := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "x",
			Namespace:       gateNS,
			OwnerReferences: []metav1.OwnerReference{{Kind: "CronJob", Name: "some-other-cronjob"}},
		},
	}
	g := gateWith(t, job)
	active, err := g.Active(context.Background())
	if err != nil {
		t.Fatalf("Active error: %v", err)
	}
	if active {
		t.Error("Active() = true, want false for a different CronJob's job")
	}
}

func TestRunGateListErrorSurfaces(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("list", "jobs", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("api down")
	})
	g := NewRunGate(client, "renovate", gateNS, testLogger())

	if _, err := g.Active(context.Background()); err == nil {
		t.Fatal("expected error when listing jobs fails (collector treats it as active)")
	}
}
