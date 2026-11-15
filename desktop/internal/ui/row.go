package ui

import (
	"fmt"
	"strings"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/vishwa/dm/desktop/internal/dmapi"
	"github.com/vishwa/dm/desktop/internal/format"
	"github.com/vishwa/dm/internal/model"
)

// row is one virtualized table row. GTK recycles these widgets; bind fills it
// from the store, and unbind clears the association. The row never owns state.
type row struct {
	w    *window
	root *gtk.Box

	fileCell      *gtk.Box
	fileIcon      *gtk.Image
	title         *gtk.Label
	context       *gtk.Label
	progressCell  *gtk.Box
	progressTop   *gtk.Box
	icon          *gtk.Image
	status        *gtk.Label
	spinner       *gtk.Spinner
	progress      *gtk.ProgressBar
	progressBytes *gtk.Label
	sizeLabel     *gtk.Label
	speedLabel    *gtk.Label
	etaLabel      *gtk.Label

	actionCell *gtk.Box
	pause      *gtk.Button
	menu       *gtk.MenuButton
	popover    *gtk.Popover

	detailsItem *gtk.Button
	cancelItem  *gtk.Button
	openItem    *gtk.Button
	removeItem  *gtk.Button
	deleteItem  *gtk.Button

	id             string
	lastAccessible string
	dimmed         bool
}

// newRow creates a six-column row whose fixed cell widths align with the
// table header. The filename column absorbs spare width and ellipsizes safely.
func newRow(w *window) *row {
	r := &row{w: w}

	r.root = gtk.NewBox(gtk.OrientationHorizontal, 14)
	r.root.AddCSSClass("dm-download-row")
	r.root.SetHExpand(true)
	r.root.SetFocusable(true)
	r.root.SetMarginStart(12)
	r.root.SetMarginEnd(12)

	// Name: file type icon plus filename and source context.
	r.fileCell = gtk.NewBox(gtk.OrientationHorizontal, 10)
	r.fileCell.SetSizeRequest(220, -1)
	r.fileCell.SetHExpand(true)
	r.fileCell.SetVAlign(gtk.AlignCenter)
	r.fileIcon = gtk.NewImage()
	r.fileIcon.SetPixelSize(24)
	r.fileIcon.AddCSSClass("dm-file-icon")
	r.fileCell.Append(r.fileIcon)

	fileText := gtk.NewBox(gtk.OrientationVertical, 2)
	fileText.SetHExpand(true)
	r.title = newEllipsizedLabel("")
	r.title.SetHExpand(true)
	r.title.AddCSSClass("dm-file-name")
	r.context = newEllipsizedLabel("")
	r.context.SetHExpand(true)
	r.context.AddCSSClass("dm-file-context")
	fileText.Append(r.title)
	fileText.Append(r.context)
	r.fileCell.Append(fileText)
	r.root.Append(r.fileCell)

	// Progress: status/percentage and downloaded bytes above the progress bar.
	r.progressCell = gtk.NewBox(gtk.OrientationVertical, 5)
	r.progressCell.SetSizeRequest(150, -1)
	r.progressCell.SetVAlign(gtk.AlignCenter)

	r.progressTop = gtk.NewBox(gtk.OrientationHorizontal, 5)
	r.progressTop.SetVAlign(gtk.AlignCenter)
	r.spinner = gtk.NewSpinner()
	r.spinner.SetVisible(false)
	r.icon = gtk.NewImage()
	r.icon.SetPixelSize(13)
	r.icon.SetVisible(false)
	r.status = newEllipsizedLabel("")
	r.status.SetHExpand(true)
	r.status.AddCSSClass("dm-progress-status")
	r.progressBytes = newEllipsizedLabel("")
	r.progressBytes.SetHAlign(gtk.AlignEnd)
	r.progressBytes.AddCSSClass("dm-progress-bytes")
	r.progressTop.Append(r.spinner)
	r.progressTop.Append(r.icon)
	r.progressTop.Append(r.status)
	r.progressTop.Append(r.progressBytes)
	r.progressCell.Append(r.progressTop)

	r.progress = gtk.NewProgressBar()
	r.progress.SetShowText(false)
	r.progress.SetHExpand(true)
	r.progress.AddCSSClass("dm-row-progress")
	r.progressCell.Append(r.progress)
	r.root.Append(r.progressCell)

	// Numeric columns use tabular alignment and never infer missing values.
	r.sizeLabel = tableValueLabel(92, gtk.AlignEnd)
	r.sizeLabel.AddCSSClass("dm-table-value")
	r.root.Append(r.sizeLabel)

	r.speedLabel = tableValueLabel(100, gtk.AlignEnd)
	r.speedLabel.AddCSSClass("dm-table-value")
	r.root.Append(r.speedLabel)

	r.etaLabel = tableValueLabel(84, gtk.AlignEnd)
	r.etaLabel.AddCSSClass("dm-table-value")
	r.root.Append(r.etaLabel)

	// Actions: one state-aware primary action and a quieter overflow menu.
	r.actionCell = gtk.NewBox(gtk.OrientationHorizontal, 2)
	r.actionCell.SetSizeRequest(84, -1)
	r.actionCell.SetHAlign(gtk.AlignEnd)
	r.actionCell.SetVAlign(gtk.AlignCenter)
	r.pause = newIconButton("media-playback-pause-symbolic", "Pause")
	r.actionCell.Append(r.pause)
	r.menu = gtk.NewMenuButton()
	r.menu.SetIconName("view-more-symbolic")
	r.menu.SetHasFrame(false)
	r.menu.SetTooltipText("More actions")
	setAccessibleLabel(r.menu.Accessible, "More actions")
	r.actionCell.Append(r.menu)
	r.root.Append(r.actionCell)

	// Row menu: only actions that apply to the current state are available.
	r.popover = gtk.NewPopover()
	menuBox := gtk.NewBox(gtk.OrientationVertical, 2)
	menuBox.SetMarginTop(6)
	menuBox.SetMarginBottom(6)
	menuBox.SetMarginStart(6)
	menuBox.SetMarginEnd(6)

	r.detailsItem = newMenuItem("Show details")
	r.cancelItem = newMenuItem("Cancel")
	r.openItem = newMenuItem("Open File")
	r.removeItem = newMenuItem("Remove from List")
	r.deleteItem = newMenuItem("Delete File…")
	for _, item := range []*gtk.Button{r.detailsItem, r.cancelItem, r.openItem, r.removeItem, r.deleteItem} {
		menuBox.Append(item)
	}
	r.detailsItem.ConnectClicked(func() {
		r.popover.Popdown()
		r.w.showDetails(r.id)
	})
	r.popover.SetChild(menuBox)
	r.menu.SetPopover(r.popover)

	// Clicking anywhere on the row selects it and opens its inspector.
	click := gtk.NewGestureClick()
	click.ConnectReleased(func(int, float64, float64) {
		if r.id != "" {
			r.w.showDetails(r.id)
		}
	})
	r.root.AddController(click)

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

func tableValueLabel(width int, align gtk.Align) *gtk.Label {
	label := newEllipsizedLabel("—")
	label.SetSizeRequest(width, -1)
	label.SetXAlign(1)
	label.SetHAlign(align)
	label.SetVAlign(gtk.AlignCenter)
	label.AddCSSClass("dm-table-value")
	return label
}

func tableHeaderLabel(text string, width int, expand bool) *gtk.Label {
	label := newEllipsizedLabel(text)
	label.SetSizeRequest(width, -1)
	label.SetHAlign(gtk.AlignStart)
	label.SetVAlign(gtk.AlignCenter)
	label.AddCSSClass("dm-table-heading")
	if text == "Size" || text == "Speed" || text == "Left" || text == "Actions" {
		label.SetXAlign(1)
		label.SetHAlign(gtk.AlignEnd)
	}
	if expand {
		label.SetHExpand(true)
	}
	return label
}

// setColumns keeps each virtualized row in sync with the responsive header.
func (r *row) setSelected(selected bool) {
	if selected {
		r.root.AddCSSClass("selected")
	} else {
		r.root.RemoveCSSClass("selected")
	}
}

func (r *row) setColumns(showSize, showSpeed, showETA bool) {
	r.sizeLabel.SetVisible(showSize)
	r.speedLabel.SetVisible(showSpeed)
	r.etaLabel.SetVisible(showETA)
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

// setDownload renders d and the sampled transfer rate. All numeric values come
// from the daemon snapshot/rate sampler; unknown values are rendered as dashes.
func (r *row) setDownload(d model.Download, bytesPerSecond float64) {
	r.id = d.ID
	r.setSelected(r.w.selectedID == d.ID)

	text := describe(d, bytesPerSecond)
	downloaded := dmapi.Downloaded(d)

	r.fileIcon.SetFromIconName(fileIcon(text.Title))
	r.title.SetLabel(text.Title)
	r.title.SetTooltipText(text.Title)
	context := text.Context
	if d.Status != model.StatusFailed && d.Status != model.StatusScheduled {
		context = hostOf(d)
		if activeStatus(d.Status) && len(d.Segments) > 0 {
			context = format.Context(context, formatCount(len(d.Segments), "connection"))
		}
	}
	r.context.SetLabel(context)
	r.context.SetVisible(context != "")
	r.context.SetTooltipText(text.Detail)

	r.status.SetLabel(text.Status)
	r.status.SetTooltipText(text.Status)

	wantDim := d.Status != model.StatusFailed
	if wantDim != r.dimmed {
		for _, label := range []*gtk.Label{r.status, r.context} {
			if wantDim {
				label.AddCSSClass("dim-label")
			} else {
				label.RemoveCSSClass("dim-label")
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

	showProgress := text.Determinate
	fraction := text.Fraction
	if d.Status == model.StatusCompleted && d.Size > 0 {
		showProgress = true
		fraction = 1
	}
	r.progress.SetVisible(showProgress)
	if showProgress {
		r.progress.SetFraction(fraction)
	}
	if d.Size > 0 {
		r.progressBytes.SetLabel(format.Bytes(downloaded))
		r.progressBytes.SetTooltipText(format.Bytes(downloaded) + " of " + format.Bytes(d.Size) + " downloaded")
	} else if downloaded > 0 {
		r.progressBytes.SetLabel(format.Bytes(downloaded))
		r.progressBytes.SetTooltipText(format.Bytes(downloaded) + " downloaded; total size unknown")
	} else {
		r.progressBytes.SetLabel("")
		r.progressBytes.SetTooltipText("")
	}

	if d.Size > 0 {
		r.sizeLabel.SetLabel(format.Bytes(d.Size))
		r.sizeLabel.SetTooltipText(format.Bytes(d.Size))
	} else {
		r.sizeLabel.SetLabel("—")
		r.sizeLabel.SetTooltipText("Total size unknown")
	}
	if activeStatus(d.Status) && bytesPerSecond >= 1 {
		r.speedLabel.SetLabel(format.Rate(bytesPerSecond))
		r.speedLabel.SetTooltipText(format.Rate(bytesPerSecond))
	} else {
		r.speedLabel.SetLabel("—")
		r.speedLabel.SetTooltipText("No active transfer rate")
	}
	if d.Status == model.StatusRunning && d.Size > downloaded {
		if eta, ok := format.ETA(d.Size-downloaded, bytesPerSecond); ok {
			r.etaLabel.SetLabel(eta)
			r.etaLabel.SetTooltipText(eta + " remaining")
		} else {
			r.etaLabel.SetLabel("—")
			r.etaLabel.SetTooltipText("ETA unavailable")
		}
	} else {
		r.etaLabel.SetLabel("—")
		r.etaLabel.SetTooltipText("ETA unavailable for this state or unknown size")
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
		r.pause.SetVisible(true)
		r.pause.SetIconName("view-refresh-symbolic")
		r.setButtonAccessible(r.pause, "Retry "+text.Title)
	default:
		r.pause.SetVisible(false)
	}

	inFlight := d.Status == model.StatusRunning || d.Status == model.StatusProbing ||
		d.Status == model.StatusQueued || d.Status == model.StatusScheduled || d.Status == model.StatusPaused
	terminal := d.Status == model.StatusCompleted || d.Status == model.StatusFailed ||
		d.Status == model.StatusCanceled
	r.cancelItem.SetVisible(inFlight)
	r.openItem.SetVisible(d.Status == model.StatusCompleted)
	r.removeItem.SetVisible(terminal)
	r.deleteItem.SetVisible(terminal)

	accessible := strings.Join(nonEmpty(
		text.Title, text.Status, context, r.sizeLabel.Text(), r.speedLabel.Text(), r.etaLabel.Text(),
	), ", ")
	if accessible != r.lastAccessible {
		setAccessibleLabel(r.root.Accessible, accessible)
		r.lastAccessible = accessible
	}
}

func (r *row) setButtonAccessible(b *gtk.Button, text string) {
	b.SetTooltipText(text)
	setAccessibleLabel(b.Accessible, text)
}

func formatCount(count int, singular string) string {
	if count == 1 {
		return "1 " + singular
	}
	return fmt.Sprintf("%d %ss", count, singular)
}

func nonEmpty(parts ...string) []string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
