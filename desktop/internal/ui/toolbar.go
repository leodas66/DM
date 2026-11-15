package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/vishwa/dm/desktop/internal/dmapi"
	"github.com/vishwa/dm/internal/model"
)

type sortMode uint8

const (
	sortNewest sortMode = iota
	sortName
	sortSize
	sortProgress
)

type bulkAction uint8

const (
	bulkPause bulkAction = iota
	bulkResume
	bulkClearCompleted
)

func (w *window) buildToolbar() *gtk.Box {
	bar := gtk.NewBox(gtk.OrientationHorizontal, 6)
	bar.AddCSSClass("dm-toolbar")
	bar.SetMarginTop(8)
	bar.SetMarginBottom(8)
	bar.SetMarginStart(12)
	bar.SetMarginEnd(12)

	w.pauseAllButton = gtk.NewButtonWithLabel("Pause all")
	w.pauseAllButton.SetTooltipText("Pause active and queued downloads")
	w.pauseAllButton.ConnectClicked(func() { w.runBulk(bulkPause) })
	bar.Append(w.pauseAllButton)

	w.resumeAllButton = gtk.NewButtonWithLabel("Resume all")
	w.resumeAllButton.SetTooltipText("Resume paused downloads")
	w.resumeAllButton.ConnectClicked(func() { w.runBulk(bulkResume) })
	bar.Append(w.resumeAllButton)

	w.clearCompletedButton = gtk.NewButtonWithLabel("Clear completed")
	w.clearCompletedButton.SetTooltipText("Remove completed downloads from the list; files remain on disk")
	w.clearCompletedButton.ConnectClicked(func() { w.runBulk(bulkClearCompleted) })
	bar.Append(w.clearCompletedButton)

	spacer := gtk.NewBox(gtk.OrientationHorizontal, 0)
	spacer.SetHExpand(true)
	bar.Append(spacer)

	bar.Append(newDimLabel("Sort"))
	sortMenu := gio.NewMenu()
	sortMenu.Append("Date added", "win.sort-newest")
	sortMenu.Append("Name", "win.sort-name")
	sortMenu.Append("File size", "win.sort-size")
	sortMenu.Append("Progress", "win.sort-progress")
	w.addSortAction("sort-newest", sortNewest)
	w.addSortAction("sort-name", sortName)
	w.addSortAction("sort-size", sortSize)
	w.addSortAction("sort-progress", sortProgress)

	sortButton := gtk.NewMenuButton()
	sortButton.SetIconName("view-sort-ascending-symbolic")
	sortButton.SetHasFrame(false)
	sortButton.SetTooltipText("Sort downloads")
	setAccessibleLabel(sortButton.Accessible, "Sort downloads")
	sortButton.SetMenuModel(sortMenu)
	bar.Append(sortButton)
	return bar
}

func (w *window) addSortAction(name string, mode sortMode) {
	action := gio.NewSimpleAction(name, nil)
	action.ConnectActivate(func(*glib.Variant) {
		w.sortMode = mode
		w.rebuild()
	})
	w.win.AddAction(action)
}

func (w *window) visibleIDs() []string {
	ids := w.store.ids(w.filter)
	query := strings.ToLower(strings.TrimSpace(w.searchEntry.Text()))
	if query != "" {
		filtered := ids[:0]
		for _, id := range ids {
			d, ok := w.store.get(id)
			if !ok {
				continue
			}
			haystack := strings.ToLower(strings.Join([]string{d.Filename, d.URL, d.FinalURL, d.Dir}, " "))
			if strings.Contains(haystack, query) {
				filtered = append(filtered, id)
			}
		}
		ids = filtered
	}
	sort.SliceStable(ids, func(i, j int) bool {
		a, _ := w.store.get(ids[i])
		b, _ := w.store.get(ids[j])
		switch w.sortMode {
		case sortName:
			return strings.ToLower(a.Filename) < strings.ToLower(b.Filename)
		case sortSize:
			return a.Size > b.Size
		case sortProgress:
			if a.Size <= 0 || b.Size <= 0 {
				if a.Size <= 0 && b.Size <= 0 {
					return a.CreatedAt.After(b.CreatedAt)
				}
				return a.Size > 0
			}
			return float64(dmapi.Downloaded(a))/float64(a.Size) > float64(dmapi.Downloaded(b))/float64(b.Size)
		default:
			if !a.CreatedAt.Equal(b.CreatedAt) {
				return a.CreatedAt.After(b.CreatedAt)
			}
			return a.ID < b.ID
		}
	})
	return ids
}

func (w *window) updateBulkSensitivity(counts map[filterKind]int) {
	if w.pauseAllButton == nil {
		return
	}
	w.pauseAllButton.SetSensitive(counts[filterActive]+counts[filterQueued] > 0)
	w.resumeAllButton.SetSensitive(counts[filterPaused] > 0)
	w.clearCompletedButton.SetSensitive(counts[filterCompleted] > 0)
}

func (w *window) updateNetworkSummary() {
	if w.networkActiveLabel == nil || w.networkSpeedLabel == nil {
		return
	}
	active := 0
	var totalRate float64
	for id, d := range w.store.items {
		if activeStatus(d.Status) {
			active++
			totalRate += w.store.rate(id)
		}
	}
	w.networkActiveLabel.SetLabel(fmt.Sprintf("%d active", active))
	w.networkSpeedLabel.SetLabel(fmt.Sprintf("%.1f MiB/s", totalRate/(1024*1024)))
}

func (w *window) runBulk(action bulkAction) {
	c := w.client
	if c == nil {
		w.toast("Not connected to the DM daemon.")
		return
	}
	ids := make([]string, 0)
	for _, id := range w.store.ids(filter{kind: filterAll}) {
		d, ok := w.store.get(id)
		if !ok {
			continue
		}
		switch action {
		case bulkPause:
			if d.Status == model.StatusRunning || d.Status == model.StatusProbing || d.Status == model.StatusQueued {
				ids = append(ids, id)
			}
		case bulkResume:
			if d.Status == model.StatusPaused {
				ids = append(ids, id)
			}
		case bulkClearCompleted:
			if d.Status == model.StatusCompleted {
				ids = append(ids, id)
			}
		}
	}
	if len(ids) == 0 {
		return
	}
	go func() {
		failed := 0
		for _, id := range ids {
			var err error
			switch action {
			case bulkPause:
				err = c.Pause(w.ctx, id)
			case bulkResume:
				err = c.Resume(w.ctx, id)
			case bulkClearCompleted:
				err = c.Remove(w.ctx, id, false)
			}
			if err != nil {
				failed++
			}
		}
		w.post(func() {
			if failed > 0 {
				w.toast(fmt.Sprintf("%d of %d actions failed.", failed, len(ids)))
				return
			}
			switch action {
			case bulkPause:
				w.toast(fmt.Sprintf("Paused %d downloads.", len(ids)))
			case bulkResume:
				w.toast(fmt.Sprintf("Resumed %d downloads.", len(ids)))
			case bulkClearCompleted:
				w.toast(fmt.Sprintf("Removed %d completed downloads from the list.", len(ids)))
			}
		})
	}()
}
