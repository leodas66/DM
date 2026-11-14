// Package ui is the GTK4 desktop client for the dm daemon. It is a client
// only: downloads, scheduling, and persistence stay in the daemon.
package ui

import (
	"os"

	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/vishwa/dm/desktop/internal/dmapi"
)

// Run starts the GTK application and blocks until it exits. connect is called
// again on every reconnect so a restarted daemon is rediscovered.
func Run(version string, connect func() (*dmapi.Client, error)) int {
	app := gtk.NewApplication("io.github.vishwa.DM", gio.ApplicationDefaultFlags)

	app.ConnectActivate(func() {
		if existing := app.ActiveWindow(); existing != nil {
			existing.Present()
			return
		}
		newWindow(app, connect).present()
	})

	quit := gio.NewSimpleAction("quit", nil)
	quit.ConnectActivate(func(*glib.Variant) { app.Quit() })
	app.AddAction(quit)
	app.SetAccelsForAction("app.quit", []string{"<Control>q"})

	about := gio.NewSimpleAction("about", nil)
	about.ConnectActivate(func(*glib.Variant) { showAbout(app, version) })
	app.AddAction(about)

	// GTK must not try to parse the daemon-style flags; the client has none.
	return app.Run(os.Args[:1])
}

func showAbout(app *gtk.Application, version string) {
	dialog := gtk.NewAboutDialog()
	dialog.SetProgramName("DM")
	dialog.SetVersion(version)
	dialog.SetComments("A lightweight download manager.")
	dialog.SetLicenseType(gtk.LicenseMITX11)
	dialog.SetAuthors([]string{"DM contributors"})
	dialog.SetModal(true)
	if parent := app.ActiveWindow(); parent != nil {
		dialog.SetTransientFor(parent)
	}
	dialog.Present()
}
