package ui

import (
	"net/url"
	"strings"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/vishwa/dm/desktop/internal/dmapi"
)

// addDialog is the focused Add Download surface: a URL first, with destination
// and filename available without crowding the primary flow.
type addDialog struct {
	w   *window
	win *gtk.Window

	urlEntry  *gtk.Entry
	dirEntry  *gtk.Entry
	nameEntry *gtk.Entry

	urlErrorBox    *gtk.Box
	urlErrorLabel  *gtk.Label
	formErrorBox   *gtk.Box
	formErrorLabel *gtk.Label

	addButton  *gtk.Button
	urlHint    *gtk.Label
	submitting bool
	prefilling bool
}

func newAddDialog(w *window) *addDialog {
	d := &addDialog{w: w}

	d.win = gtk.NewWindow()
	d.win.SetTitle("Add Download")
	d.win.SetModal(true)
	d.win.SetTransientFor(&w.win.Window)
	d.win.SetDestroyWithParent(true)
	d.win.SetDefaultSize(560, -1)

	header := gtk.NewHeaderBar()
	cancel := gtk.NewButtonWithLabel("Cancel")
	cancel.ConnectClicked(func() { d.win.Close() })
	header.PackStart(cancel)

	d.addButton = gtk.NewButtonWithLabel("Add Download")
	d.addButton.AddCSSClass("suggested-action")
	d.addButton.SetSensitive(false)
	d.addButton.ConnectClicked(func() { d.submit() })
	header.PackEnd(d.addButton)
	d.win.SetTitlebar(header)

	content := gtk.NewBox(gtk.OrientationVertical, 8)
	content.SetMarginTop(18)
	content.SetMarginBottom(18)
	content.SetMarginStart(18)
	content.SetMarginEnd(18)

	d.urlEntry = gtk.NewEntry()
	d.urlEntry.SetPlaceholderText("https://example.com/file.zip")
	d.urlEntry.SetHExpand(true)
	d.urlEntry.SetActivatesDefault(true)
	content.Append(fieldLabel("_URL", d.urlEntry))
	content.Append(d.urlEntry)

	d.urlHint = newDimLabel("Link taken from the clipboard.")
	d.urlHint.AddCSSClass("caption")
	d.urlHint.SetVisible(false)
	content.Append(d.urlHint)

	d.urlErrorBox, d.urlErrorLabel = errorRow()
	content.Append(d.urlErrorBox)

	content.Append(gtk.NewSeparator(gtk.OrientationHorizontal))

	d.dirEntry = gtk.NewEntry()
	d.dirEntry.SetPlaceholderText("Daemon default folder")
	d.dirEntry.SetText(w.defaultDir)
	d.dirEntry.SetHExpand(true)
	content.Append(fieldLabel("_Folder", d.dirEntry))
	folderRow := gtk.NewBox(gtk.OrientationHorizontal, 8)
	folderRow.Append(d.dirEntry)
	choose := gtk.NewButtonWithLabel("Choose\u2026")
	choose.ConnectClicked(func() { d.chooseFolder() })
	folderRow.Append(choose)
	content.Append(folderRow)

	d.nameEntry = gtk.NewEntry()
	d.nameEntry.SetPlaceholderText("From the URL or server")
	d.nameEntry.SetHExpand(true)
	d.nameEntry.SetTooltipText("Optional. Leave empty to use the server's filename.")
	content.Append(fieldLabel("File_name", d.nameEntry))
	content.Append(d.nameEntry)

	d.formErrorBox, d.formErrorLabel = errorRow()
	content.Append(d.formErrorBox)

	d.win.SetChild(content)
	d.win.SetDefaultWidget(d.addButton)

	// Escape dismisses the dialog, as GTK users expect.
	controller := gtk.NewShortcutController()
	controller.AddShortcut(gtk.NewShortcut(
		gtk.NewShortcutTriggerParseString("Escape"),
		gtk.NewNamedAction("window.close"),
	))
	d.win.AddController(controller)

	d.urlEntry.ConnectChanged(func() {
		d.hideClipboardHint()
		d.validate()
	})
	d.dirEntry.ConnectChanged(func() { d.clearFormError() })
	d.nameEntry.ConnectChanged(func() { d.clearFormError() })

	return d
}

// fieldLabel builds a visible label that is also the entry's mnemonic target,
// so the association is programmatic rather than implied by position.
func fieldLabel(mnemonic string, entry *gtk.Entry) *gtk.Label {
	plain := strings.ReplaceAll(mnemonic, "_", "")
	l := gtk.NewLabel("")
	l.SetXAlign(0)
	l.SetHAlign(gtk.AlignStart)
	l.SetUseUnderline(true)
	l.SetTextWithMnemonic(mnemonic)
	l.SetMnemonicWidget(entry)
	setAccessibleLabel(entry.Accessible, plain)
	return l
}

// errorRow builds a hidden inline error with an icon, so the message does not
// depend on color alone.
func errorRow() (*gtk.Box, *gtk.Label) {
	box := gtk.NewBox(gtk.OrientationHorizontal, 6)
	box.SetVisible(false)

	icon := gtk.NewImageFromIconName("dialog-warning-symbolic")
	icon.SetPixelSize(16)
	icon.SetVAlign(gtk.AlignStart)
	box.Append(icon)

	label := newLabel("")
	label.SetWrap(true)
	label.SetHExpand(true)
	box.Append(label)
	return box, label
}

func (d *addDialog) present() {
	d.win.Present()
	d.prefillFromClipboard()
	d.urlEntry.GrabFocus()
	d.validate()
}

func (d *addDialog) validate() {
	text := strings.TrimSpace(d.urlEntry.Text())
	valid := validURL(text)
	d.addButton.SetSensitive(valid && !d.submitting)

	if text != "" && !valid {
		d.urlErrorLabel.SetLabel("Enter a complete http:// or https:// address.")
		d.urlErrorBox.SetVisible(true)
		return
	}
	d.urlErrorBox.SetVisible(false)
}

func (d *addDialog) clearFormError() {
	d.formErrorBox.SetVisible(false)
}

func (d *addDialog) showFormError(message string) {
	d.formErrorLabel.SetLabel(message)
	d.formErrorBox.SetVisible(true)
}

func validURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func (d *addDialog) chooseFolder() {
	dialog := gtk.NewFileDialog()
	dialog.SetTitle("Choose Download Folder")
	dialog.SetModal(true)
	dialog.SelectFolder(d.w.ctx, d.win, func(res gio.AsyncResulter) {
		file, err := dialog.SelectFolderFinish(res)
		if err != nil || file == nil {
			// Dismissing the chooser is not an error worth showing.
			return
		}
		if path := file.Path(); path != "" {
			d.dirEntry.SetText(path)
		}
	})
}

func (d *addDialog) submit() {
	if d.submitting {
		return
	}
	raw := strings.TrimSpace(d.urlEntry.Text())
	if !validURL(raw) {
		d.validate()
		d.urlEntry.GrabFocus()
		return
	}

	c := d.w.client
	if c == nil {
		d.showFormError("Not connected to the DM daemon.")
		return
	}

	req := dmapi.AddRequest{
		URL:      raw,
		Dir:      strings.TrimSpace(d.dirEntry.Text()),
		Filename: strings.TrimSpace(d.nameEntry.Text()),
	}

	d.submitting = true
	d.addButton.SetSensitive(false)
	d.clearFormError()

	go func() {
		download, err := c.Add(d.w.ctx, req)
		coreglib.IdleAdd(func() {
			d.submitting = false
			d.addButton.SetSensitive(true)
			if err != nil {
				d.showFormError(err.Error())
				return
			}
			d.w.onAdded(download)
			d.win.Close()
		})
	}()
}
