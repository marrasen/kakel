package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marrasen/kakel/agenthost"
	"github.com/marrasen/kakel/settings"
)

// writeOldSkill puts a skill where an earlier kakel wrote one, and gives back its path.
func writeOldSkill(t *testing.T) string {
	t.Helper()
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	paths, err := agenthost.OldSkills()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(paths[0]), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths[0], []byte(oldSkillStart+"description: x\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return paths[0]
}

func oldSkillApp(t *testing.T) *app {
	t.Helper()
	a, _ := agentApp(t)
	set, err := settings.Load(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	a.settings = set
	return a
}

func TestAnOldSkillIsOfferedOnceAndRemoved(t *testing.T) {
	a := oldSkillApp(t)
	path := writeOldSkill(t)
	a.offerOldSkillRemoval()
	waitFor(t, a, "the question", func() bool { return len(a.st.Asks) == 1 })
	if q := a.st.Asks[0]; q.Title != "Remove kakel's old skill?" {
		t.Fatalf("the question is %+v", q)
	}
	a.handle(AskAnswered{ID: a.st.Asks[0].ID, Yes: true})
	waitFor(t, a, "the skill removed", func() bool {
		_, err := os.Stat(path)
		return os.IsNotExist(err)
	})
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Errorf("the empty skill folder is still there: %v", err)
	}
	waitFor(t, a, "the offer kept", a.settings.OldSkillOffered)

	// Asked once: a skill written again is not offered again.
	writeOldSkill(t)
	a.offerOldSkillRemoval()
	if len(a.st.Asks) != 0 {
		t.Fatalf("it asked again: %+v", a.st.Asks)
	}
}

func TestAnOldSkillKeptStaysAndIsNotOfferedAgain(t *testing.T) {
	a := oldSkillApp(t)
	path := writeOldSkill(t)
	a.offerOldSkillRemoval()
	waitFor(t, a, "the question", func() bool { return len(a.st.Asks) == 1 })
	a.handle(AskAnswered{ID: a.st.Asks[0].ID, Yes: false})
	waitFor(t, a, "the offer kept", a.settings.OldSkillOffered)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("a skill the user kept is gone: %v", err)
	}
}

func TestASkillKakelDidNotWriteIsLeftAlone(t *testing.T) {
	a := oldSkillApp(t)
	path := writeOldSkill(t)
	if err := os.WriteFile(path, []byte("---\nname: mine\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.offerOldSkillRemoval()
	if len(a.st.Asks) != 0 {
		t.Fatalf("it offered to remove a skill it did not write: %+v", a.st.Asks)
	}
}
