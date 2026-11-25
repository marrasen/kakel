package app

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/marrasen/kakel/agenthost"
)

// oldSkillStart is how every skill an earlier kakel wrote begins.
const oldSkillStart = "---\nname: kakel\n"

// oldSkills are the skill files an earlier kakel wrote that are still there.
func oldSkills() ([]string, error) {
	paths, err := agenthost.OldSkills()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range paths {
		body, err := os.ReadFile(p)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			return nil, err
		}
		if bytes.HasPrefix(bytes.ReplaceAll(body, []byte("\r\n"), []byte("\n")), []byte(oldSkillStart)) {
			out = append(out, p)
		}
	}
	return out, nil
}

// offerOldSkillRemoval asks once whether to remove the skill files an earlier kakel wrote.
func (a *app) offerOldSkillRemoval() {
	if a.settings == nil || a.settings.OldSkillOffered() {
		return
	}
	paths, err := oldSkills()
	if err != nil {
		a.failed("Couldn't look for kakel's old skill", err.Error())
		return
	}
	if len(paths) == 0 {
		a.keep("that there is no old skill", a.settings.PutOldSkillOffered())
		return
	}
	q := Ask{
		Title: "Remove kakel's old skill?",
		Text: "kakel no longer writes a skill for agents, because its MCP server tells them how to work in panes." +
			" This one is left over from an earlier kakel and will not be updated:\n\n" + strings.Join(paths, "\n"),
		Yes: "Remove",
		No:  "Keep",
	}
	go func() {
		ans, err := a.ask(a.ctx, q)
		if err != nil && !errors.Is(err, errDeclined) {
			return
		}
		select {
		case a.events <- func() { a.answerOldSkill(paths, err == nil && ans.Yes) }:
		case <-a.ctx.Done():
		}
	}()
}

// answerOldSkill removes the old skill files when the user said so, and keeps that they were asked.
func (a *app) answerOldSkill(paths []string, remove bool) {
	if remove {
		if err := removeSkills(paths); err != nil {
			a.failed("Couldn't remove kakel's old skill", err.Error())
			return
		}
		a.worked("Old skill removed", strings.Join(paths, "\n"), "")
	}
	a.keep("that the old skill was offered", a.settings.PutOldSkillOffered())
}

// removeSkills removes each skill file, and its folder when nothing else is in it.
func removeSkills(paths []string) error {
	for _, p := range paths {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		dir := filepath.Dir(p)
		left, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		if len(left) == 0 {
			if err := os.Remove(dir); err != nil {
				return err
			}
		}
	}
	return nil
}
