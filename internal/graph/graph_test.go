package graph

import (
	"encoding/json"
	"testing"
)

func TestDependencyString(t *testing.T) {
	tests := []struct {
		dep  Dependency
		want string
	}{
		{Dependency{Kind: KindGitHub, Name: "org/lib"}, "github:org/lib"},
		{
			Dependency{Kind: KindECR, Name: "arn:aws:ecr:us-west-2:123456789012:repository/api"},
			"ecr:arn:aws:ecr:us-west-2:123456789012:repository/api",
		},
	}
	for _, tt := range tests {
		if got := tt.dep.String(); got != tt.want {
			t.Errorf("String() = %q, want %q", got, tt.want)
		}
	}
}

// A Dependency is logged as a slog attribute value, and slog's JSON handler
// marshals values with encoding/json — which ignores Stringer. Without
// MarshalText the flush log would degrade to {"Kind":...,"Name":...}, so this
// guards the log shape.
func TestDependencyMarshalsAsStringInSlice(t *testing.T) {
	deps := []Dependency{
		{Kind: KindGitHub, Name: "org/lib"},
		{Kind: KindECR, Name: "arn:aws:ecr:us-west-2:123456789012:repository/api"},
	}
	got, err := json.Marshal(deps)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	want := `["github:org/lib","ecr:arn:aws:ecr:us-west-2:123456789012:repository/api"]`
	if string(got) != want {
		t.Errorf("Marshal = %s, want %s", got, want)
	}
}

// The same Name under two Kinds is two distinct Dependencies. This encodes the
// deduplication contract the Batch relies on.
func TestSameNameDifferentKindAreDistinctKeys(t *testing.T) {
	set := map[Dependency]struct{}{
		{Kind: KindGitHub, Name: "api"}: {},
		{Kind: KindECR, Name: "api"}:    {},
	}
	if len(set) != 2 {
		t.Errorf("set size = %d, want 2", len(set))
	}
}

func TestValidDependent(t *testing.T) {
	valid := []string{"org/repo", "o/r", "org/repo.name", "org/repo-name"}
	for _, s := range valid {
		if !ValidDependent(s) {
			t.Errorf("ValidDependent(%q) = false, want true", s)
		}
	}
	invalid := []string{"", "org", "/repo", "org/", "org/repo/extra", "/"}
	for _, s := range invalid {
		if ValidDependent(s) {
			t.Errorf("ValidDependent(%q) = true, want false", s)
		}
	}
}

func TestParseDependents(t *testing.T) {
	tests := []struct {
		name  string
		given string
		want  []string
	}{
		{"single", "org/app", []string{"org/app"}},
		{"comma separated", "org/app-a,org/app-b", []string{"org/app-a", "org/app-b"}},
		{"surrounding spaces", " org/app-a , org/app-b ", []string{"org/app-a", "org/app-b"}},
		{"trailing comma", "org/app-a,org/app-b,", []string{"org/app-a", "org/app-b"}},
		{"repeated commas", "org/app-a,,org/app-b", []string{"org/app-a", "org/app-b"}},
		{"whitespace separated", "org/app-a org/app-b", []string{"org/app-a", "org/app-b"}},
		{"empty", "", nil},
		{"only separators", " , , ", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseDependents(tt.given)
			if len(got) != len(tt.want) {
				t.Fatalf("ParseDependents(%q) = %v, want %v", tt.given, got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("ParseDependents(%q) = %v, want %v", tt.given, got, tt.want)
				}
			}
		})
	}
}
