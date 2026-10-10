package app

import (
	"slices"
	"testing"

	"github.com/marrasen/gunim/filemanager"

	"github.com/marrasen/kakel/secrets"
)

// zipAsk is a question for the password of secret.zip.
var zipAsk = filemanager.PasswordAsk{FS: "", Path: "/data/secret.zip", Where: "/data/secret.zip"}

// askZip asks for the password of ask on a goroutine, as the file
// manager does, and hands back what it gives.
func askZip(t *testing.T, a *app, ask filemanager.PasswordAsk) chan filemanager.Password {
	t.Helper()
	got := make(chan filemanager.Password, 1)
	go func() {
		pw, err := a.zipPassword(t.Context(), nil, ask)
		if err != nil {
			pw.Text = "error: " + err.Error()
		}
		got <- pw
	}()
	return got
}

// awaitZip waits for the answer to askZip while the program runs.
func awaitZip(t *testing.T, a *app, got chan filemanager.Password) filemanager.Password {
	t.Helper()
	var pw filemanager.Password
	waitFor(t, a, "the password", func() bool {
		select {
		case pw = <-got:
			return true
		default:
			return false
		}
	})
	return pw
}

// A password typed with its box ticked is kept under the zip's name once
// it worked, and opens the zip from then on without asking.
func TestAZipsTypedPasswordIsKeptAndUsedAgain(t *testing.T) {
	a, _ := secretsApp(t)
	startVault(t, a)
	got := askZip(t, a, zipAsk)
	waitFor(t, a, "the question", func() bool { return len(a.st.Asks) > 0 })
	if q := a.st.Asks[0]; q.Also == "" || len(q.Prompts) != 1 || !q.Secret[0] {
		t.Fatalf("the question is %+v", q)
	}
	answer(t, a, "Password for secret.zip", true, "hunter2", "yes")
	pw := awaitZip(t, a, got)
	if pw.Text != "hunter2" || pw.Worked == nil {
		t.Fatalf("answered %q, worked %v", pw.Text, pw.Worked != nil)
	}
	pw.Worked()
	waitFor(t, a, "the password kept", func() bool { return a.keptZipPassword("secret.zip") == "hunter2" })

	again := awaitZip(t, a, askZip(t, a, zipAsk))
	if again.Text != "hunter2" || len(a.st.Asks) != 0 {
		t.Fatalf("the next time it gave %q, asking %+v", again.Text, a.st.Asks)
	}
	// Wrong after all, it asks, and says so.
	wrong := zipAsk
	wrong.Wrong = true
	got = askZip(t, a, wrong)
	waitFor(t, a, "the question", func() bool { return len(a.st.Asks) > 0 })
	if a.st.Asks[0].Problem == "" {
		t.Fatal("asked again, the question doesn't say why")
	}
	a.handle(AskAnswered{ID: a.st.Asks[0].ID})
	if pw := awaitZip(t, a, got); pw.Text == "" || pw.Worked != nil {
		t.Fatalf("turned down, it gave %+v", pw)
	}
}

// A saved secret picked answers for the zip.
func TestAPickedSecretOpensAZip(t *testing.T) {
	a, _ := secretsApp(t)
	startVault(t, a)
	if _, err := a.secrets.Put(secrets.Item{Name: "archives"}, "opensesame"); err != nil {
		t.Fatal(err)
	}
	got := askZip(t, a, zipAsk)
	waitFor(t, a, "the question", func() bool { return len(a.st.Asks) > 0 })
	q := a.st.Asks[0]
	i := slices.Index(q.Saved, "archives")
	if i < 0 {
		t.Fatalf("the question offers %v", q.Saved)
	}
	a.handle(AskAnswered{ID: q.ID, Yes: true, Answers: []string{"", "", itoa(i)}})
	if pw := awaitZip(t, a, got); pw.Text != "opensesame" {
		t.Fatalf("answered %q", pw.Text)
	}
}

// A password for a zip being made is typed twice, the same both times.
func TestAZipBeingMadeTakesThePasswordTwice(t *testing.T) {
	a, _ := secretsApp(t)
	made := zipAsk
	made.Make = true
	got := askZip(t, a, made)
	waitFor(t, a, "the question", func() bool { return len(a.st.Asks) > 0 })
	q := a.st.Asks[0]
	if len(q.Prompts) != 2 || q.Yes != "Protect" {
		t.Fatalf("the question is %+v", q)
	}
	a.handle(AskAnswered{ID: q.ID, Yes: true, Answers: []string{"one", "two", ""}})
	waitFor(t, a, "the question again", func() bool { return len(a.st.Asks) > 0 && a.st.Asks[0].ID != q.ID })
	if a.st.Asks[0].Problem == "" {
		t.Fatal("two different passwords were taken without a word")
	}
	a.handle(AskAnswered{ID: a.st.Asks[0].ID, Yes: true, Answers: []string{"same", "same", ""}})
	if pw := awaitZip(t, a, got); pw.Text != "same" {
		t.Fatalf("answered %q", pw.Text)
	}
}

// Secrets that are there but locked are offered behind a button that
// unlocks them.
func TestLockedSecretsAreOfferedToUnlock(t *testing.T) {
	a, _ := secretsApp(t)
	startVault(t, a)
	a.handle(LockSecrets{})
	if a.vaultInHand() != nil {
		t.Skip("the secrets open on a key in hand here")
	}
	got := askZip(t, a, zipAsk)
	waitFor(t, a, "the question", func() bool { return len(a.st.Asks) > 0 })
	q := a.st.Asks[0]
	if !slices.Contains(q.Choose, unlockSecrets) || len(q.Saved) != 0 {
		t.Fatalf("locked, the question offers %v and %v", q.Choose, q.Saved)
	}
	a.handle(AskAnswered{ID: q.ID})
	if pw := awaitZip(t, a, got); pw.Text == "" {
		t.Fatal("turned down, it gave a password")
	}
}
