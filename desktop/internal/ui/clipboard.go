package ui

import (
	"strings"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
)

// clipboardURL accepts a clipboard value only when the whole value is one
// http(s) URL. Copying a paragraph or a URL with tracking text around it must
// not silently become a download.
func clipboardURL(text string) (string, bool) {
	text = strings.TrimSpace(text)
	if text == "" || len(text) > 4096 {
		return "", false
	}
	if strings.ContainsAny(text, " \t\r\n") {
		return "", false
	}
	if !validURL(text) {
		return "", false
	}
	return text, true
}

// prefillFromClipboard offers a copied link in an empty URL field. It never
// submits anything: the user still has to confirm. Reading happens
// asynchronously because the X11/Wayland clipboard is owned by another
// process, and a slow owner must not block the dialog.
func (d *addDialog) prefillFromClipboard() {
	if strings.TrimSpace(d.urlEntry.Text()) != "" {
		return
	}
	display := gdk.DisplayGetDefault()
	if display == nil {
		return
	}
	clipboard := display.Clipboard()
	if clipboard == nil {
		return
	}
	clipboard.ReadTextAsync(d.w.ctx, func(res gio.AsyncResulter) {
		text, err := clipboard.ReadTextFinish(res)
		if err != nil {
			return
		}
		link, ok := clipboardURL(text)
		if !ok || strings.TrimSpace(d.urlEntry.Text()) != "" {
			return
		}
		// The change handler would otherwise treat this as typing and hide
		// the hint immediately.
		d.prefilling = true
		d.urlEntry.SetText(link)
		d.urlEntry.SetPosition(-1)
		d.prefilling = false
		d.urlHint.SetVisible(true)
		d.validate()
	})
}

// hideClipboardHint clears the "taken from the clipboard" note once the value
// no longer comes from there.
func (d *addDialog) hideClipboardHint() {
	if d.prefilling || !d.urlHint.Visible() {
		return
	}
	d.urlHint.SetVisible(false)
}
