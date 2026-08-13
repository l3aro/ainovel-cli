package host

import (
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/store"
)

func TestBuildResumePrompt_PhaseInitIsFreshNovel(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("init store: %v", err)
	}
	if err := s.Progress.Init("Smoke Novel", 0); err != nil {
		t.Fatalf("init progress: %v", err)
	}

	prompt, label, err := buildResumePrompt(s)
	if err != nil {
		t.Fatalf("buildResumePrompt: %v", err)
	}
	if prompt != "" || label != "" {
		t.Fatalf("phase %q is fresh and must not resume: prompt=%q label=%q", domain.PhaseInit, prompt, label)
	}
}
