//go:build nogui

package main

import "errors"

const hasGUI = false

func runGUI(string) error { return errors.New("this cellar is built without the GUI (-tags nogui)") }
