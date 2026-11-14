package ui

import (
	"strings"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"
	"github.com/vishwa/dm/internal/model"
)

// row is one download row. GTK recycles these widgets: bind fills it from the
// store, unbind clears the association. The row never owns download state.
type row struct {
	w    *window
	root *gtk.Box

	fileIcon     *gtk.Image
	title        *gtk.Label
	context      *gtk.Label
	icon         *gtk.Image
	status       *gtk.Label
	spinner      *gtk.Spinner
	progress     *gtk.ProgressBar
	progressSlot *gtk.Box

	pause     *gtk.Button
	pauseSlot *gtk.Box
	menu      *gtk.MenuButton
	popover   *gtk.Popover

	cancelItem *gtk.Button
	openItem   *gtk.Button
	removeItem *gtk.Button
	deleteItem *gtk.Button

	id             string
	lastAccessible string
	dimmed         bool
}

// newRow builds the row widget tree once; setDownload updates it in place.
func newRow(w *window) *row {
	r := &row{w: w}

	// Compact three-part row: type icon, a two-line text column, and the
	// state/action column. Keep padding tight so more downloads fit on screen.
	r.root = gtk.NewBox(gtk.OrientationVertical, 0)
	r.root.SetMarginTop(8)
	r.root.SetMarginBottom(8)
	r.root.SetMarginStart(12)
	r.root.SetMarginEnd(12)

	head := gtk.NewBox(gtk.OrientationHorizontal, 12)

	r.fileIcon = gtk.NewImage()
	r.fileIcon.SetPixelSize(24)
	r.fileIcon.SetVAlign(gtk.AlignCenter)
	r.fileIcon.AddCSSClass("dim-label")

	textCol := gtk.NewBox(gtk.OrientationVertical, 2)
	textCol.SetHExpand(true)
	r.title = newEllipsizedLabel("")
	r.title.SetHExpand(true)
	r.title.AddCSSClass("heading")
	r.context = newEllipsizedLabel("")
	r.context.AddCSSClass("dim-label")
	textCol.Append(r.title)
	textCol.Append(r.context)

	statusCol := gtk.NewBox(gtk.OrientationHorizontal, 8)
	statusCol.SetVAlign(gtk.AlignCenter)
	r.spinner = gtk.NewSpinner()
	r.spinner.SetVAlign(gtk.AlignCenter)
	r.spinner.SetVisible(false)
	r.icon = gtk.NewImage()
	r.icon.SetPixelSize(16)
	r.icon.SetVisible(false)
	r.status = newLabel("")
	r.status.SetSingleLineMode(true)
	r.status.SetEllipsize(pango.EllipsizeEnd)
	// A fixed character width keeps the state column aligned across rows even
	// when the action buttons differ.
	r.status.SetWidthChars(15)
	r.status.SetXAlign(1)
	r.status.AddCSSClass("dim-label")
	r.dimmed = true
	r.pause = newIconButton("media-playback-pause-symbolic", "Pause")
	// A fixed slot keeps the state label aligned across rows whether or not
	// the pause control applies to that state.
	r.pauseSlot = gtk.NewBox(gtk.OrientationHorizontal, 0)
	r.pauseSlot.SetSizeRequest(40, -1)
	r.pauseSlot.SetHAlign(gtk.AlignCenter)
	r.pauseSlot.Append(r.pause)
	r.menu = gtk.NewMenuButton()
	r.menu.SetIconName("view-more-symbolic")
	r.menu.SetHasFrame(false)
	r.menu.SetTooltipText("More actions")
	setAccessibleLabel(r.menu.Accessible, "More actions")
	statusCol.Append(r.spinner)
	statusCol.Append(r.icon)
	statusCol.Append(r.status)
	statusCol.Append(r.pauseSlot)
	statusCol.Append(r.menu)

	head.Append(r.fileIcon)
	head.Append(textCol)
	head.Append(statusCol)

	// The bar lines up under the filename, past the type icon. The slot stays
	// reserved for every in-flight state so rows do not jump in height when a
	// transfer moves between queued, running, and paused.
	r.progress = gtk.NewProgressBar()
	r.progress.SetShowText(false)
	r.progress.SetHExpand(true)
	r.progress.SetVisible(false)
	r.progressSlot = gtk.NewBox(gtk.OrientationVertical, 0)
	r.progressSlot.SetMarginStart(36)
	r.progressSlot.SetMarginTop(6)
	r.progressSlot.SetVisible(false)
	r.progressSlot.Append(r.progress)

	r.root.Append(head)
	r.root.Append(r.progressSlot)

	// Row menu: only the actions that apply to the current state are shown,
	// so unavailable actions never appear as active controls.
	r.popover = gtk.NewPopover()
	menuBox := gtk.NewBox(gtk.OrientationVertical, 2)
	menuBox.SetMarginTop(6)
	menuBox.SetMarginBottom(6)
	menuBox.SetMarginStart(6)
	menuBox.SetMarginEnd(6)

	r.cancelItem = newMenuItem("Cancel")
	r.openItem = newMenuItem("Open File")
	r.removeItem = newMenuItem("Remove from List")
	r.deleteItem = newMenuItem("Delete File\u2026")
	for _, item := range []*gtk.Button{r.cancelItem, r.openItem, r.removeItem, r.deleteItem} {
		menuBox.Append(item)
	}
	r.popover.SetChild(menuBox)
	r.menu.SetPopover(r.popover)

	r.pause.ConnectClicked(func() {
		if r.id == "" {
			return
		}
		if d, ok := r.w.store.get(r.id); ok {
			switch d.Status {
			case model.StatusPaused:
				r.w.resumeDownload(r.id)
				return
			case model.StatusFailed, model.StatusCanceled:
				r.w.retryDownload(r.id)
				return
			}
		}
		r.w.pauseDownload(r.id)
	})
	r.cancelItem.ConnectClicked(func() {
		r.popover.Popdown()
		r.w.cancelDownload(r.id)
	})
	r.openItem.ConnectClicked(func() {
		r.popover.Popdown()
		r.w.openFile(r.id)
	})
	r.removeItem.ConnectClicked(func() {
		r.popover.Popdown()
		r.w.removeDownload(r.id, false)
	})
	r.deleteItem.ConnectClicked(func() {
		r.popover.Popdown()
		r.w.removeDownload(r.id, true)
	})

	return r
}

// newMenuItem builds a flat, full-width button suitable for a row popover.
func newMenuItem(text string) *gtk.Button {
	b := gtk.NewButton()
	b.SetHasFrame(false)
	b.SetHExpand(true)
	l := newLabel(text)
	l.SetMarginStart(6)
	l.SetMarginEnd(6)
	b.SetChild(l)
	return b
}

// setDownload renders d and the sampled transfer rate.
func (r *row) setDownload(d model.Download, bytesPerSecond float64) {
	r.id = d.ID
	text := describe(d, bytesPerSecond)

	r.fileIcon.SetFromIconName(fileIcon(text.Title))
	r.title.SetLabel(text.Title)
	r.title.SetTooltipText(text.Title)
	r.context.SetLabel(text.Context)
	r.context.SetVisible(text.Context != "")
	if text.Detail != "" {
		r.context.SetTooltipText(text.Detail)
	} else {
		r.context.SetTooltipText("")
	}
	r.status.SetLabel(text.Status)
	r.status.SetTooltipText(text.Status)

	// A failed row is the one state that should read at normal emphasis; the
	// status icon carries the alarm, not an app-defined colour.
	wantDim := d.Status != model.StatusFailed
	if wantDim != r.dimmed {
		for _, l := range []*gtk.Label{r.status, r.context} {
			if wantDim {
				l.AddCSSClass("dim-label")
			} else {
				l.RemoveCSSClass("dim-label")
			}
		}
		r.dimmed = wantDim
	}

	if text.StatusIcon != "" {
		r.icon.SetFromIconName(text.StatusIcon)
		r.icon.SetVisible(true)
	} else {
		r.icon.SetVisible(false)
	}

	if text.Spinning {
		r.spinner.SetVisible(true)
		r.spinner.Start()
	} else {
		r.spinner.Stop()
		r.spinner.SetVisible(false)
	}

	inFlightState := d.Status == model.StatusRunning || d.Status == model.StatusProbing ||
		d.Status == model.StatusQueued || d.Status == model.StatusScheduled ||
		d.Status == model.StatusPaused
	r.progressSlot.SetVisible(inFlightState)
	if text.Determinate {
		r.progress.SetVisible(true)
		r.progress.SetFraction(text.Fraction)
	} else {
		r.progress.SetVisible(false)
	}

	switch d.Status {
	case model.StatusRunning, model.StatusProbing:
		r.pause.SetVisible(true)
		r.pause.SetIconName("media-playback-pause-symbolic")
		r.setButtonAccessible(r.pause, "Pause "+text.Title)
	case model.StatusPaused:
		r.pause.SetVisible(true)
		r.pause.SetIconName("media-playback-start-symbolic")
		r.setButtonAccessible(r.pause, "Resume "+text.Title)
	case model.StatusFailed, model.StatusCanceled:
		// Retry is a fresh attempt, not a continuation: the icon says so and
		// the control is the same slot the in-flight states use.
		r.pause.SetVisible(true)
		r.pause.SetIconName("view-refresh-symbolic")
		r.setButtonAccessible(r.pause, "Retry "+text.Title)
	default:
		r.pause.SetVisible(false)
	}

	terminal := d.Status == model.StatusCompleted || d.Status == model.StatusFailed ||
		d.Status == model.StatusCanceled

	r.cancelItem.SetVisible(inFlightState)
	r.openItem.SetVisible(d.Status == model.StatusCompleted)
	r.removeItem.SetVisible(terminal)
	r.deleteItem.SetVisible(terminal)

	accessible := strings.Join(nonEmpty(text.Title, text.Status, text.Context), ", ")
	if accessible != r.lastAccessible {
		setAccessibleLabel(r.root.Accessible, accessible)
		r.lastAccessible = accessible
	}
}

func (r *row) setButtonAccessible(b *gtk.Button, text string) {
	b.SetTooltipText(text)
	setAccessibleLabel(b.Accessible, text)
}

func nonEmpty(parts ...string) []string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
