package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/vishwa/dm/desktop/internal/dmapi"
	"github.com/vishwa/dm/desktop/internal/format"
	"github.com/vishwa/dm/internal/model"
)

func (w *window) buildInspector() *gtk.Box {
	panel := gtk.NewBox(gtk.OrientationVertical, 10)
	panel.AddCSSClass("dm-inspector")
	panel.SetSizeRequest(336, -1)
	panel.SetMarginTop(14)
	panel.SetMarginBottom(14)
	panel.SetMarginStart(14)
	panel.SetMarginEnd(14)

	header := gtk.NewBox(gtk.OrientationHorizontal, 6)
	titleBox := gtk.NewBox(gtk.OrientationVertical, 2)
	titleBox.SetHExpand(true)
	w.inspectorTitle = newLabel("Download details")
	w.inspectorTitle.AddCSSClass("heading")
	w.inspectorTitle.SetWrap(true)
	w.inspectorHost = newDimLabel("")
	w.inspectorHost.AddCSSClass("caption")
	w.inspectorHost.SetWrap(true)
	titleBox.Append(w.inspectorTitle)
	titleBox.Append(w.inspectorHost)
	header.Append(titleBox)
	closeButton := newIconButton("window-close-symbolic", "Close download details")
	closeButton.ConnectClicked(func() { w.hideDetails() })
	header.Append(closeButton)
	panel.Append(header)

	w.inspectorStatus = newDimLabel("Select a download to see its progress, transfer details and saved location.")
	w.inspectorStatus.SetWrap(true)
	panel.Append(w.inspectorStatus)
	w.inspectorProgress = gtk.NewProgressBar()
	w.inspectorProgress.SetShowText(true)
	w.inspectorProgress.SetHExpand(true)
	panel.Append(w.inspectorProgress)

	panel.Append(sectionHeading("Transfer"))
	w.inspectorStats = newLabel("")
	w.inspectorStats.SetWrap(true)
	panel.Append(w.inspectorStats)

	panel.Append(gtk.NewSeparator(gtk.OrientationHorizontal))
	panel.Append(sectionHeading("Source"))
	w.inspectorSource = newLabel("")
	w.inspectorSource.SetWrap(true)
	w.inspectorSource.SetSelectable(true)
	panel.Append(w.inspectorSource)

	panel.Append(sectionHeading("Saved to"))
	w.inspectorDestination = newLabel("")
	w.inspectorDestination.SetWrap(true)
	w.inspectorDestination.SetSelectable(true)
	panel.Append(w.inspectorDestination)

	panel.Append(sectionHeading("Segments"))
	w.inspectorSegments = newDimLabel("")
	w.inspectorSegments.SetWrap(true)
	panel.Append(w.inspectorSegments)

	panel.Append(gtk.NewSeparator(gtk.OrientationHorizontal))
	actions := gtk.NewBox(gtk.OrientationHorizontal, 6)
	w.inspectorPrimary = gtk.NewButtonWithLabel("Pause")
	w.inspectorPrimary.AddCSSClass("suggested-action")
	w.inspectorPrimary.ConnectClicked(func() { w.inspectorPrimaryAction() })
	actions.Append(w.inspectorPrimary)
	w.inspectorCancel = gtk.NewButtonWithLabel("Cancel")
	w.inspectorCancel.ConnectClicked(func() {
		if w.selectedID != "" {
			w.cancelDownload(w.selectedID)
		}
	})
	actions.Append(w.inspectorCancel)
	panel.Append(actions)

	w.inspectorOpen = gtk.NewButtonWithLabel("Open File")
	w.inspectorOpen.ConnectClicked(func() {
		if w.selectedID != "" {
			w.openFile(w.selectedID)
		}
	})
	panel.Append(w.inspectorOpen)

	w.inspectorRemove = gtk.NewButtonWithLabel("Remove from list")
	w.inspectorRemove.ConnectClicked(func() {
		if w.selectedID != "" {
			w.removeDownload(w.selectedID, false)
		}
	})
	panel.Append(w.inspectorRemove)

	w.inspectorDelete = gtk.NewButtonWithLabel("Delete file…")
	w.inspectorDelete.AddCSSClass("destructive-action")
	w.inspectorDelete.ConnectClicked(func() {
		if w.selectedID != "" {
			w.removeDownload(w.selectedID, true)
		}
	})
	panel.Append(w.inspectorDelete)

	w.inspectorPrimary.SetVisible(false)
	w.inspectorCancel.SetVisible(false)
	w.inspectorOpen.SetVisible(false)
	w.inspectorRemove.SetVisible(false)
	w.inspectorDelete.SetVisible(false)
	w.inspectorSource.SetLabel("—")
	w.inspectorDestination.SetLabel("—")
	return panel
}

func (w *window) showDetails(id string) {
	if _, ok := w.store.get(id); !ok {
		return
	}
	w.selectedID = id
	w.inspectorRevealer.SetRevealChild(true)
	w.refreshInspector()
}

func (w *window) hideDetails() {
	w.selectedID = ""
	w.refreshInspector()
	width, _ := w.win.ObjectProperty("default-width").(int)
	w.inspectorRevealer.SetRevealChild(width >= 1180)
}

func (w *window) inspectorPrimaryAction() {
	d, ok := w.store.get(w.selectedID)
	if !ok {
		return
	}
	switch d.Status {
	case model.StatusRunning, model.StatusProbing, model.StatusQueued:
		w.pauseDownload(d.ID)
	case model.StatusPaused:
		w.resumeDownload(d.ID)
	case model.StatusFailed, model.StatusCanceled:
		w.retryDownload(d.ID)
	}
}

func (w *window) refreshInspector() {
	if w.inspectorTitle == nil {
		return
	}
	if w.selectedID == "" {
		w.inspectorTitle.SetLabel("Download details")
		w.inspectorHost.SetLabel("")
		w.inspectorStatus.SetLabel("Select a download from the list to inspect its progress, speed, source and saved location.")
		w.inspectorProgress.SetVisible(false)
		w.inspectorStats.SetLabel("")
		w.inspectorSource.SetLabel("—")
		w.inspectorDestination.SetLabel("—")
		w.inspectorSegments.SetLabel("")
		for _, control := range []*gtk.Button{w.inspectorPrimary, w.inspectorCancel, w.inspectorOpen, w.inspectorRemove, w.inspectorDelete} {
			control.SetVisible(false)
		}
		return
	}
	d, ok := w.store.get(w.selectedID)
	if !ok {
		w.hideDetails()
		return
	}

	text := describe(d, w.store.rate(d.ID))
	w.inspectorTitle.SetLabel(text.Title)
	w.inspectorHost.SetLabel(text.Context)
	w.inspectorStatus.SetLabel(text.Status)
	downloaded := dmapi.Downloaded(d)
	if d.Size > 0 {
		fraction := float64(downloaded) / float64(d.Size)
		if fraction > 1 {
			fraction = 1
		}
		w.inspectorProgress.SetFraction(fraction)
		w.inspectorProgress.SetText(fmt.Sprintf("%d%%", int(fraction*100)))
		w.inspectorProgress.SetVisible(true)
	} else {
		w.inspectorProgress.SetVisible(false)
	}
	transfer := []string{"Downloaded: " + format.Bytes(downloaded)}
	if d.Size > 0 {
		transfer = append(transfer, "Total: "+format.Bytes(d.Size))
	}
	if activeStatus(d.Status) {
		rate := w.store.rate(d.ID)
		if rate > 0 {
			transfer = append(transfer, "Speed: "+format.Rate(rate))
			if d.Size > downloaded {
				if eta, ok := format.ETA(d.Size-downloaded, rate); ok {
					transfer = append(transfer, "Time left: "+eta)
				}
			}
		}
	}
	w.inspectorStats.SetLabel(strings.Join(transfer, "\n"))

	source := d.URL
	if d.FinalURL != "" {
		source = d.FinalURL
	}
	w.inspectorSource.SetLabel(source)
	w.inspectorSource.SetTooltipText(source)
	destination := d.Dir
	if d.Filename != "" {
		destination = filepath.Join(d.Dir, d.Filename)
	}
	w.inspectorDestination.SetLabel(destination)
	w.inspectorDestination.SetTooltipText(destination)
	if len(d.Segments) == 0 {
		w.inspectorSegments.SetLabel("Single-stream or not yet segmented.")
	} else {
		w.inspectorSegments.SetLabel(fmt.Sprintf("%d segments · %s transferred", len(d.Segments), format.Bytes(downloaded)))
	}

	primary := d.Status == model.StatusRunning || d.Status == model.StatusProbing ||
		d.Status == model.StatusQueued || d.Status == model.StatusPaused ||
		d.Status == model.StatusFailed || d.Status == model.StatusCanceled
	w.inspectorPrimary.SetVisible(primary)
	switch d.Status {
	case model.StatusPaused:
		w.inspectorPrimary.SetLabel("Resume")
	case model.StatusFailed, model.StatusCanceled:
		w.inspectorPrimary.SetLabel("Retry")
	default:
		w.inspectorPrimary.SetLabel("Pause")
	}
	terminal := d.Status == model.StatusCompleted || d.Status == model.StatusFailed || d.Status == model.StatusCanceled
	w.inspectorCancel.SetVisible(!terminal)
	w.inspectorOpen.SetVisible(d.Status == model.StatusCompleted)
	w.inspectorRemove.SetVisible(terminal)
	w.inspectorDelete.SetVisible(terminal)
}
