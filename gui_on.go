//go:build !nogui

package main

import "github.com/jing2uo/cellar/internal/gui"

const hasGUI = true

func runGUI(view string) error { return gui.Run(version, view) }
