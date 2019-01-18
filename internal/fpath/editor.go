// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information.

package fpath

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

//EditFile opens the best OS-specific text editor we can find
func EditFile(fileToEdit string) error {
	editorPath := getEditorPath()
	if editorPath == "" {
		return fmt.Errorf("Unable to find suitable editor for file %s", fileToEdit)
	}
	cmd := exec.Command(editorPath, fileToEdit)
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func getEditorPath() string {
	// we currently only attempt to open TTY-friendly editors here
	// we could consider using https://github.com/mattn/go-isatty
	// alongside "start" / "open" / "xdg-open"

	//look for a preference in environment variables
	for _, eVar := range [...]string{"EDITOR", "VISUAL", "GIT_EDITOR"} {
		path := os.Getenv(eVar)
		if len(path) > 0 {
			return path
		}
	}
	//look for a preference via 'git config'
	git, err := exec.LookPath("git")
	if err == nil {
		out, err := exec.Command(git, "config", "core.editor").Output()
		cmd := strings.TrimSpace(string(out))
		if err == nil && len(cmd) > 0 {
			return cmd
		}
	}
	//heck, just try a bunch of options
	for _, exe := range [...]string{"nvim", "vim", "vi", "emacs", "nano", "pico"} {
		path, err := exec.LookPath(exe)
		if err == nil {
			return path
		}
	}
	return ""
}
