package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"

	"github.com/jing2uo/cellar/internal/runner"
)

// runPackaged runs a FlatPark package's app. Started from the menu it has
// no terminal to print to, so a missing Wine, which the package does not
// bring, is shown in a dialog too.
func runPackaged(args []string) int {
	err := cmdRun(args)
	if errors.Is(err, runner.ErrNone) {
		alertNoWine()
	}
	return exit(err)
}

// alertNoWine shows how to install Wine, with zenity (in the Freedesktop
// and GNOME runtimes), else as a notification.
func alertNoWine() {
	title, text := "Wine is not installed",
		"This app runs with Wine from Flathub. Install it, then start the app again:\n\nflatpak install "+runner.FlatpakWine
	if strings.HasPrefix(os.Getenv("LANG"), "zh") {
		title, text = "没有安装 Wine",
			"这个应用要用 Flathub 上的 Wine 运行。安装后再打开它：\n\nflatpak install "+runner.FlatpakWine
	}
	if _, err := exec.LookPath("zenity"); err == nil {
		exec.Command("zenity", "--error", "--no-markup", "--title", title, "--text", text).Run()
		return
	}
	exec.Command("notify-send", "--icon", "dialog-error", title, text).Run()
}
