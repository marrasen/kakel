package term

import (
	"strings"
	"testing"
	"time"

	"github.com/marrasen/kakel/input"
)

// typeAnswer types one letter and presses return, which answers an ask.
func typeAnswer(t *testing.T, term *Terminal) {
	t.Helper()
	if _, err := term.HandleKey(input.Event{Kind: input.Text, Rune: 'x', NormalText: true}); err != nil {
		t.Fatalf("typing: %v", err)
	}
	if _, err := term.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyEnter}); err != nil {
		t.Fatalf("pressing return: %v", err)
	}
}

// ended reports whether an ask has ended.
func ended(ask *SecretAsk) bool {
	select {
	case <-ask.Done():
		return true
	default:
		return false
	}
}

func TestAskingAgainForTheSameSecretWaitsOnTheSameAsk(t *testing.T) {
	term, _ := newTestTerm(t, 80, 10, Config{})
	first := term.WaitForSecret("-- the password --", time.Minute)
	second := term.WaitForSecret("-- the password --", time.Minute)
	if first != second {
		t.Fatal("asking again for the same thing started a second ask")
	}
	if n := strings.Count(term.Text(), "-- the password --"); n != 1 {
		t.Errorf("the pane says the ask %d times:\n%s", n, term.Text())
	}
	typeAnswer(t, term)
	if !ended(first) || !first.Answered() {
		t.Fatal("the user typed, and the ask did not end answered")
	}
}

func TestAskingForAnotherSecretEndsTheFirstAsk(t *testing.T) {
	term, _ := newTestTerm(t, 80, 10, Config{})
	first := term.WaitForSecret("-- the password --", time.Minute)
	second := term.WaitForSecret("-- the passphrase --", time.Minute)
	if !ended(first) || first.Answered() {
		t.Fatal("the first ask is still waiting after another replaced it")
	}
	if ended(second) || !term.AskedForASecret() {
		t.Fatal("the second ask is not waiting")
	}
	if !strings.Contains(term.Text(), secretEnded) {
		t.Errorf("the pane does not say the first ask ended:\n%s", term.Text())
	}
}

func TestAnAskNobodyAnswersEndsOnItsOwn(t *testing.T) {
	term, _ := newTestTerm(t, 80, 10, Config{})
	ask := term.WaitForSecret("-- the password --", 20*time.Millisecond)
	select {
	case <-ask.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the ask was still waiting long after its time ran out")
	}
	if ask.Answered() || term.AskedForASecret() {
		t.Fatal("the ask ran out of time and still counts as answered or waiting")
	}
}
