package app

import (
	"context"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/marrasen/gunim/filemanager"

	"github.com/marrasen/kakel/secrets"
)

// unlockSecrets is the button a zip's password question offers while the
// secrets are locked, to offer them too.
const unlockSecrets = "Unlock secrets"

// zipSecrets is what the secrets offer a zip's password question: the
// passwords and passphrases in them, and whether they are there but
// locked.
type zipSecrets struct {
	items  []secrets.Item
	locked bool
}

// zipPassword asks for the password of a zip, as the file manager asks
// through its Options.Password: to open one a password protects, or to
// protect one being made. A password the secrets keep under the zip's
// name opens it without asking, the first time. The question offers the
// secrets to answer with, and a box to keep what is typed in them, under
// the zip's name, once it has worked. It runs on a goroutine of the file
// manager's.
func (a *app) zipPassword(ctx context.Context, _ *filemanager.Window, ask filemanager.PasswordAsk) (filemanager.Password, error) {
	name := zipBase(ask)
	if !ask.Make && !ask.Wrong {
		if kept := a.onLoop(ctx, func() string { return a.keptZipPassword(name) }); kept != "" {
			return filemanager.Password{Text: kept}, nil
		}
	}
	q := Ask{Title: "Password for " + name, Icon: "lock", Prompts: []string{"Password"}, Secret: []bool{true}, Yes: "Open",
		Text:  "What this zip holds is protected with a password.",
		Facts: []AskFact{{Label: "Zip", Name: name, Note: ask.Where}},
		Also:  "Save this password in the secrets"}
	if ask.Make {
		q.Title, q.Yes = "Protect "+name+" with a password", "Protect"
		q.Text = "What the zip holds opens only with this password. The names of the files in it stay readable."
		q.Prompts, q.Secret = []string{"Password", "The same again"}, []bool{true, true}
	}
	wrong := ""
	if ask.Wrong {
		wrong = "The last password didn't open it. Try again."
	}
	q.Problem = wrong
	for {
		var offer zipSecrets
		got := make(chan zipSecrets, 1)
		if !a.onLoopDo(ctx, func() { got <- a.zipOffer() }) {
			return filemanager.Password{}, context.Canceled
		}
		select {
		case offer = <-got:
		case <-ctx.Done():
			return filemanager.Password{}, ctx.Err()
		}
		q.Saved, q.Choose = nil, nil
		for _, it := range offer.items {
			q.Saved = append(q.Saved, it.Name)
		}
		if offer.locked {
			q.Choose = []string{q.Yes, unlockSecrets}
		}
		ans, err := a.ask(ctx, q)
		if err != nil {
			return filemanager.Password{}, err
		}
		fields, rest := ans.Answers[:len(q.Prompts)], ans.Answers[len(q.Prompts):]
		if q.Choose != nil {
			choice := rest[0]
			rest = rest[1:]
			if choice == unlockSecrets {
				q.Problem = wrong
				if err := a.unlockForZip(ctx, name); err != nil {
					q.Problem = "The secrets stayed locked."
				}
				continue
			}
		}
		ticked := len(rest) > 0 && rest[0] == "yes"
		if len(q.Saved) > 0 && len(rest) > 1 {
			if i, err := strconv.Atoi(rest[len(rest)-1]); err == nil && i >= 0 && i < len(offer.items) {
				id := offer.items[i].ID
				value := a.onLoop(ctx, func() string { return a.secretValue(id) })
				if value == "" {
					q.Problem = "That saved secret couldn't be read. Type the password, or pick another."
					continue
				}
				return filemanager.Password{Text: value}, nil
			}
		}
		typed := fields[0]
		switch {
		case typed == "":
			q.Problem = "Type the password, or pick a saved one."
			continue
		case ask.Make && fields[1] != typed:
			q.Problem = "The two passwords aren't the same."
			continue
		}
		pw := filemanager.Password{Text: typed}
		if ticked {
			pw.Worked = func() { a.later(func() { a.keepZipPassword(name, ask.Where, typed) }) }
		}
		return pw, nil
	}
}

// zipBase is the name of the zip ask is about, from its path as the
// window writes it, on any machine.
func zipBase(ask filemanager.PasswordAsk) string {
	where := ask.Where
	if where == "" {
		where = ask.Path
	}
	return path.Base(strings.ReplaceAll(where, `\`, "/"))
}

// zipOffer is what the secrets offer a zip's password question now.
func (a *app) zipOffer() zipSecrets {
	if v := a.vaultInHand(); v != nil {
		return zipSecrets{items: a.offered()}
	}
	v, err := a.vault()
	return zipSecrets{locked: err == nil && v.Exists()}
}

// keptZipPassword is the password the secrets keep under the zip's name,
// when they open without asking, or "".
func (a *app) keptZipPassword(name string) string {
	v := a.vaultInHand()
	if v == nil {
		return ""
	}
	items, err := v.Items()
	if err != nil {
		return ""
	}
	i := slices.IndexFunc(items, func(it secrets.Item) bool { return it.Kind == secrets.Password && it.Name == name })
	if i < 0 {
		return ""
	}
	value, err := v.Secret(items[i].ID)
	if err != nil {
		return ""
	}
	return value
}

// unlockForZip unlocks the secrets, asking, for the zip called name. It
// asks, so it runs off the program's goroutine; the vault is found on it.
func (a *app) unlockForZip(ctx context.Context, name string) error {
	type found struct {
		v   *secrets.Vault
		err error
	}
	got := make(chan found, 1)
	if !a.onLoopDo(ctx, func() {
		v, err := a.vault()
		got <- found{v, err}
	}) {
		return context.Canceled
	}
	var f found
	select {
	case f = <-got:
	case <-ctx.Done():
		return ctx.Err()
	}
	v, err := f.v, f.err
	if err != nil {
		return err
	}
	if err := a.openVault(v, &AskFact{Label: "For the zip", Name: name}); err != nil {
		return err
	}
	a.later(a.showVault)
	return nil
}

// keepZipPassword keeps value in the secrets as the password of the zip
// called name, at where: in place of the one kept under that name, or as
// a new secret.
func (a *app) keepZipPassword(name, where, value string) {
	a.withSecrets("Couldn't keep the zip's password in the secrets", func(v *secrets.Vault) error {
		items, err := v.Items()
		if err != nil {
			return err
		}
		it := secrets.Item{Name: name, Kind: secrets.Password, Notes: "The password of the zip " + where}
		said := name + " saved in the secrets"
		if i := slices.IndexFunc(items, func(it secrets.Item) bool { return it.Kind == secrets.Password && it.Name == name }); i >= 0 {
			it, said = items[i], name+" changed in the secrets"
		}
		if _, err := v.Put(it, value); err != nil {
			return err
		}
		a.worked(said, "Opens "+name+" from now on, without asking.", "")
		return nil
	})
}

// onLoop runs f on the program's goroutine and returns what it returns,
// or "" once ctx ends.
func (a *app) onLoop(ctx context.Context, f func() string) string {
	got := make(chan string, 1)
	if !a.onLoopDo(ctx, func() { got <- f() }) {
		return ""
	}
	select {
	case s := <-got:
		return s
	case <-ctx.Done():
		return ""
	}
}

// onLoopDo hands f to the program's goroutine, and reports false when
// ctx or kakel ends first.
func (a *app) onLoopDo(ctx context.Context, f func()) bool {
	select {
	case a.events <- f:
		return true
	case <-ctx.Done():
		return false
	case <-a.ctx.Done():
		return false
	}
}
