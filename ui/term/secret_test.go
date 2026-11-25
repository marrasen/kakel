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

// await waits for an ask up to d and reports whether it ended and was answered.
func await(term *Terminal, ask *SecretAsk, d time.Duration) (ended, answered bool) {
	return term.AwaitSecret(ask, d, nil)
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
	if ended, answered := await(term, first, 0); !ended || !answered {
		t.Fatal("the user typed, and the ask did not end answered")
	}
}

func TestAnAnswerTypedBetweenCallsReachesTheNextCall(t *testing.T) {
	term, _ := newTestTerm(t, 80, 10, Config{})
	first := term.WaitForSecret("-- the password --", time.Minute)
	if ended, _ := await(term, first, time.Millisecond); ended {
		t.Fatal("the ask ended before anybody typed")
	}
	typeAnswer(t, term)

	again := term.WaitForSecret("-- the password --", time.Minute)
	if ended, answered := await(term, again, 0); !ended || !answered {
		t.Fatal("the answer typed between two calls did not reach the second")
	}
	if n := strings.Count(term.Text(), "-- the password --"); n != 1 {
		t.Errorf("the pane says the ask %d times:\n%s", n, term.Text())
	}

	// Collected once, so the call after that asks afresh.
	third := term.WaitForSecret("-- the password --", time.Minute)
	if ended, _ := await(term, third, 0); ended {
		t.Fatal("an answer already collected was handed out again")
	}
}

func TestAnAnswerAWaitingCallTookIsNotHandedOutAgain(t *testing.T) {
	term, _ := newTestTerm(t, 80, 10, Config{})
	first := term.WaitForSecret("-- the password --", time.Minute)
	got := make(chan bool, 1)
	go func() {
		_, answered := await(term, first, 5*time.Second)
		got <- answered
	}()
	waitUntil(t, func() bool {
		term.secretMu.Lock()
		defer term.secretMu.Unlock()
		return first.waiting == 1
	})
	typeAnswer(t, term)
	if !<-got {
		t.Fatal("the waiting call was not told the user typed")
	}
	next := term.WaitForSecret("-- the password --", time.Minute)
	if ended, _ := await(term, next, 0); ended {
		t.Fatal("an answer a waiting call took was handed out again")
	}
}

// waitUntil polls ok for up to five seconds.
func waitUntil(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("waited five seconds")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestAskingForAnotherSecretEndsTheFirstAsk(t *testing.T) {
	term, _ := newTestTerm(t, 80, 10, Config{})
	first := term.WaitForSecret("-- the password --", time.Minute)
	second := term.WaitForSecret("-- the passphrase --", time.Minute)
	if ended, answered := await(term, first, 0); !ended || answered {
		t.Fatal("the first ask is still waiting after another replaced it")
	}
	if ended, _ := await(term, second, 0); ended || !term.AskedForASecret() {
		t.Fatal("the second ask is not waiting")
	}
	if !strings.Contains(term.Text(), secretEnded) {
		t.Errorf("the pane does not say the first ask ended:\n%s", term.Text())
	}
}

func TestAnAskNobodyAnswersEndsOnItsOwn(t *testing.T) {
	term, _ := newTestTerm(t, 80, 10, Config{})
	ask := term.WaitForSecret("-- the password --", 20*time.Millisecond)
	ended, answered := await(term, ask, 5*time.Second)
	if !ended {
		t.Fatal("the ask was still waiting long after its time ran out")
	}
	if answered || term.AskedForASecret() {
		t.Fatal("the ask ran out of time and still counts as answered or waiting")
	}
}

func TestAKeptAnswerGoesWhenTheAskEnds(t *testing.T) {
	term, _ := newTestTerm(t, 80, 10, Config{})
	first := term.WaitForSecret("-- the password --", time.Minute)
	if ended, _ := await(term, first, time.Millisecond); ended {
		t.Fatal("the ask ended before anybody typed")
	}
	typeAnswer(t, term)
	// The pane is taken back, or its program ends, before anybody collects the answer.
	term.EndSecret(nil)
	again := term.WaitForSecret("-- the password --", time.Minute)
	if ended, _ := await(term, again, 0); ended {
		t.Fatal("an answer kept from before the pane was taken back was handed out")
	}
}
