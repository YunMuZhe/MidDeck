package main

import (
	"errors"
	"path/filepath"

	"github.com/YunMuZhe/MidDeck/internal/changeset"
	"github.com/YunMuZhe/MidDeck/internal/i18n"
)

// The changeset library is a folder of drafts on this machine. AI tools save
// proposals here; a person opens, plans and applies them in the app.

func (a *App) library() changeset.Library {
	return changeset.Library{Dir: filepath.Join(a.dataDir, "changesets")}
}

// ChangesetLibrary lists the saved changesets, newest first.
func (a *App) ChangesetLibrary() ([]changeset.File, error) { return a.library().List() }

func (a *App) ChangesetLibraryRead(name string) (string, error) { return a.library().Read(name) }

// ChangesetLibrarySave stores a changeset and returns its file name.
func (a *App) ChangesetLibrarySave(name, content string, overwrite bool) (string, error) {
	return a.saveLibrary(name, content, overwrite)
}

func (a *App) saveLibrary(name, content string, overwrite bool) (string, error) {
	p, err := a.library().Write(name, content, overwrite)
	if errors.Is(err, changeset.ErrExists) {
		return "", errors.New(i18n.T("library.exists", filepath.Base(p)))
	}
	return filepath.Base(p), err
}

func (a *App) ChangesetLibraryDelete(name string) error { return a.library().Delete(name) }
