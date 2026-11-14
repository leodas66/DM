package ui

import (
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// showConfirm presents a modal confirmation for a destructive action. The
// message names the object and the destination, and Cancel is the default.
func showConfirm(parent *gtk.Window, title, message, detail, confirmLabel string, onConfirm func()) {
	win := gtk.NewWindow()
	win.SetTitle(title)
	win.SetModal(true)
	win.SetTransientFor(parent)
	win.SetDestroyWithParent(true)
	win.SetResizable(false)
	win.SetDefaultSize(460, -1)

	header := gtk.NewHeaderBar()
	cancel := gtk.NewButtonWithLabel("Cancel")
	cancel.ConnectClicked(func() { win.Close() })
	header.PackStart(cancel)

	confirm := gtk.NewButtonWithLabel(confirmLabel)
	confirm.AddCSSClass("destructive-action")
	confirm.ConnectClicked(func() {
		win.Close()
		onConfirm()
	})
	header.PackEnd(confirm)
	win.SetTitlebar(header)

	content := gtk.NewBox(gtk.OrientationVertical, 10)
	content.SetMarginTop(18)
	content.SetMarginBottom(18)
	content.SetMarginStart(18)
	content.SetMarginEnd(18)

	// The header bar deliberately has no title widget, so this heading is the
	// only visible title; the window title names the dialog to the WM.
	titleLabel := newLabel(title)
	titleLabel.AddCSSClass("title-4")
	content.Append(titleLabel)

	messageLabel := newLabel(message)
	messageLabel.SetWrap(true)
	content.Append(messageLabel)

	if detail != "" {
		detailLabel := newDimLabel(detail)
		detailLabel.SetWrap(true)
		content.Append(detailLabel)
	}

	// Escape cancels, matching the Cancel default.
	controller := gtk.NewShortcutController()
	controller.AddShortcut(gtk.NewShortcut(
		gtk.NewShortcutTriggerParseString("Escape"),
		gtk.NewNamedAction("window.close"),
	))
	win.AddController(controller)

	win.SetChild(content)
	// Cancel is the default: Enter must not delete a file.
	win.SetDefaultWidget(cancel)

	win.Present()
}
