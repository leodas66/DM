package ui

import (
	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"
)

// newLabel builds a left-aligned label that wraps when space is tight.
func newLabel(text string) *gtk.Label {
	l := gtk.NewLabel(text)
	l.SetXAlign(0)
	l.SetHAlign(gtk.AlignStart)
	return l
}

// newDimLabel is secondary text using GTK's own dim-label style class, so the
// active theme decides how it looks.
func newDimLabel(text string) *gtk.Label {
	l := newLabel(text)
	l.AddCSSClass("dim-label")
	return l
}

// newEllipsizedLabel is a single-line label that shortens in the middle.
func newEllipsizedLabel(text string) *gtk.Label {
	l := newLabel(text)
	l.SetEllipsize(pango.EllipsizeMiddle)
	l.SetSingleLineMode(true)
	return l
}

// newIconButton builds an icon-only button with an accessible name and a
// tooltip, as required for every icon-only control.
func newIconButton(icon, accessible string) *gtk.Button {
	b := gtk.NewButtonFromIconName(icon)
	b.SetHasFrame(false)
	b.SetTooltipText(accessible)
	setAccessibleLabel(b.Accessible, accessible)
	return b
}

// setAccessibleLabel sets the GTK accessible label property (not a visual
// label) so screen readers announce the control.
func setAccessibleLabel(a gtk.Accessible, text string) {
	a.UpdateProperty(
		[]gtk.AccessibleProperty{gtk.AccessiblePropertyLabel},
		[]coreglib.Value{*coreglib.NewValue(text)},
	)
}

// newEmptyPage builds a centred state page: icon, title, detail, action.
func newEmptyPage(icon string) (page *gtk.Box, title, detail *gtk.Label) {
	page = gtk.NewBox(gtk.OrientationVertical, 12)
	page.SetHAlign(gtk.AlignCenter)
	page.SetVAlign(gtk.AlignCenter)
	page.SetHExpand(true)
	page.SetVExpand(true)
	page.SetMarginTop(24)
	page.SetMarginBottom(24)
	page.SetMarginStart(24)
	page.SetMarginEnd(24)

	image := gtk.NewImageFromIconName(icon)
	image.SetPixelSize(48)
	image.AddCSSClass("dim-label")
	page.Append(image)

	title = newLabel("")
	title.SetHAlign(gtk.AlignCenter)
	title.SetJustify(gtk.JustifyCenter)
	title.AddCSSClass("title-4")
	page.Append(title)

	detail = newLabel("")
	detail.SetHAlign(gtk.AlignCenter)
	detail.SetJustify(gtk.JustifyCenter)
	detail.SetWrap(true)
	detail.SetMaxWidthChars(52)
	detail.AddCSSClass("dim-label")
	page.Append(detail)

	return page, title, detail
}
