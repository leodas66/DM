package ui

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/vishwa/dm/desktop/internal/dmapi"
	"github.com/vishwa/dm/internal/model"
)

// window is the main DM window: header bar, filter sidebar, and the virtualized
// download list. All GTK access happens on the main loop; network work runs in
// goroutines and posts results back through glib.IdleAdd.
type window struct {
	app       *gtk.Application
	connectFn func() (*dmapi.Client, error)
	win       *gtk.ApplicationWindow

	ctx    context.Context
	cancel context.CancelFunc

	store       *viewStore
	filter      filter
	client      *dmapi.Client
	loaded      bool
	categories  []model.Category
	defaultDir  string
	settings    dmapi.Settings
	addDialog   *addDialog
	prefsDialog *preferencesDialog
	catsDialog  *categoriesDialog
	rowTimer    coreglib.SourceHandle
	toastSource coreglib.SourceHandle
	retryCh     chan struct{}

	// GTK widgets.
	titleLabel      *gtk.Label
	subtitleLabel   *gtk.Label
	sidebarToggle   *gtk.Button
	sidebarRevealer *gtk.Revealer
	filterList      *gtk.ListBox
	categoryBox     *gtk.Box
	categoryList    *gtk.ListBox
	stack           *gtk.Stack
	model           *gtk.StringList
	listView        *gtk.ListView
	loadingSpinner  *gtk.Spinner
	offlineDetail   *gtk.Label
	emptyTitle      *gtk.Label
	emptyBody       *gtk.Label
	emptyAdd        *gtk.Button
	banner          *gtk.Box
	bannerLabel     *gtk.Label
	toastBar        *gtk.Box
	toastLabel      *gtk.Label

	sidebarAuto       bool
	filterRows        []*filterRow
	categoryRows      []*categoryRow
	filterByRow       map[*gtk.ListBoxRow]*filterRow
	categoryByRow     map[*gtk.ListBoxRow]filter
	updatingSelection bool

	// Row bookkeeping for coalesced progress updates.
	rowWidgets  map[uintptr]*row
	visibleRows map[string]*row
	dirty       map[string]bool

	// Event coalescing; mu guards the fields below from the stream goroutine.
	mu           sync.Mutex
	pending      map[string]model.Event
	order        []string
	needsRebuild bool
	flushQueued  bool
}

type filterRow struct {
	filter filter
	row    *gtk.ListBoxRow
	count  *gtk.Label
}

type categoryRow struct {
	filter filter
	row    *gtk.ListBoxRow
	count  *gtk.Label
}

func newWindow(app *gtk.Application, connect func() (*dmapi.Client, error)) *window {
	ctx, cancel := context.WithCancel(context.Background())
	w := &window{
		app:           app,
		connectFn:     connect,
		ctx:           ctx,
		cancel:        cancel,
		store:         newViewStore(),
		filter:        filter{kind: filterAll, label: "All Downloads"},
		retryCh:       make(chan struct{}, 1),
		filterByRow:   make(map[*gtk.ListBoxRow]*filterRow),
		categoryByRow: make(map[*gtk.ListBoxRow]filter),
		rowWidgets:    make(map[uintptr]*row),
		visibleRows:   make(map[string]*row),
		dirty:         make(map[string]bool),
	}
	w.build()
	return w
}

func (w *window) build() {
	w.win = gtk.NewApplicationWindow(w.app)
	w.win.SetTitle("DM")
	w.win.SetDefaultSize(1024, 640)
	w.win.ConnectCloseRequest(func() bool {
		w.shutdown()
		return false
	})

	header := gtk.NewHeaderBar()

	w.sidebarToggle = newIconButton("view-list-symbolic", "Hide filters")
	w.sidebarToggle.ConnectClicked(func() {
		// A manual toggle wins over the width-driven default.
		w.sidebarAuto = false
		w.setSidebarVisible(!w.sidebarRevealer.RevealChild())
	})
	header.PackStart(w.sidebarToggle)

	// Title plus a one-line summary: the header says which view is open and
	// what is happening, without adding another toolbar.
	titleBox := gtk.NewBox(gtk.OrientationVertical, 0)
	titleBox.SetMarginStart(8)
	w.titleLabel = newLabel(w.filter.label)
	w.titleLabel.AddCSSClass("title")
	w.titleLabel.SetHAlign(gtk.AlignStart)
	w.subtitleLabel = newDimLabel("")
	w.subtitleLabel.AddCSSClass("caption")
	w.subtitleLabel.SetHAlign(gtk.AlignStart)
	titleBox.Append(w.titleLabel)
	titleBox.Append(w.subtitleLabel)
	// Start-aligned next to the sidebar toggle, so the view title reads as the
	// heading of the content below it instead of floating in the middle.
	header.PackStart(titleBox)
	// An empty title widget suppresses the header bar's built-in centred
	// window title; the WM title still names the window.
	header.SetTitleWidget(gtk.NewBox(gtk.OrientationHorizontal, 0))

	addButton := gtk.NewButtonWithLabel("Add Download")
	addButton.AddCSSClass("suggested-action")
	addButton.ConnectClicked(func() { w.showAddDialog() })
	header.PackEnd(addButton)

	menu := gio.NewMenu()
	primary := gio.NewMenu()
	primary.Append("Preferences", "win.preferences")
	primary.Append("Categories\u2026", "win.categories")
	menu.AppendSection("", primary)
	secondary := gio.NewMenu()
	secondary.Append("About DM", "app.about")
	secondary.Append("Quit", "app.quit")
	menu.AppendSection("", secondary)
	menuButton := gtk.NewMenuButton()
	menuButton.SetIconName("open-menu-symbolic")
	menuButton.SetHasFrame(false)
	menuButton.SetTooltipText("Main menu")
	setAccessibleLabel(menuButton.Accessible, "Main menu")
	menuButton.SetMenuModel(menu)
	header.PackEnd(menuButton)

	w.win.SetTitlebar(header)
	w.win.NotifyProperty("default-width", w.adaptSidebar)

	content := gtk.NewBox(gtk.OrientationHorizontal, 0)
	content.SetVExpand(true)

	w.sidebarRevealer = gtk.NewRevealer()
	w.sidebarRevealer.SetTransitionType(gtk.RevealerTransitionTypeSlideRight)
	w.sidebarRevealer.SetTransitionDuration(150)
	w.sidebarRevealer.SetChild(w.buildSidebar())
	w.sidebarRevealer.SetRevealChild(true)
	content.Append(w.sidebarRevealer)
	content.Append(gtk.NewSeparator(gtk.OrientationVertical))

	mainArea := gtk.NewBox(gtk.OrientationVertical, 0)
	mainArea.SetHExpand(true)
	mainArea.SetVExpand(true)
	mainArea.Append(w.buildBanner())
	mainArea.Append(w.buildStack())
	mainArea.Append(w.buildToast())
	content.Append(mainArea)

	root := gtk.NewBox(gtk.OrientationVertical, 0)
	root.Append(content)
	w.win.SetChild(root)

	// Ctrl+N adds a download from anywhere in the window.
	addAction := gio.NewSimpleAction("add", nil)
	addAction.ConnectActivate(func(*glib.Variant) { w.showAddDialog() })
	w.win.AddAction(addAction)

	prefsAction := gio.NewSimpleAction("preferences", nil)
	prefsAction.ConnectActivate(func(*glib.Variant) { w.showPreferences() })
	w.win.AddAction(prefsAction)

	catsAction := gio.NewSimpleAction("categories", nil)
	catsAction.ConnectActivate(func(*glib.Variant) { w.showCategories() })
	w.win.AddAction(catsAction)

	controller := gtk.NewShortcutController()
	controller.AddShortcut(gtk.NewShortcut(
		gtk.NewShortcutTriggerParseString("<Control>n"),
		gtk.NewNamedAction("win.add"),
	))
	controller.AddShortcut(gtk.NewShortcut(
		gtk.NewShortcutTriggerParseString("<Control>comma"),
		gtk.NewNamedAction("win.preferences"),
	))
	w.win.AddController(controller)

	// Coalesce high-frequency progress events into bounded row updates.
	w.rowTimer = coreglib.TimeoutAdd(120, func() bool {
		w.flushRows()
		return true
	})
}

func (w *window) buildSidebar() *gtk.Box {
	box := gtk.NewBox(gtk.OrientationVertical, 0)
	box.SetSizeRequest(208, -1)

	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	scroll.SetVExpand(true)

	inner := gtk.NewBox(gtk.OrientationVertical, 0)
	inner.SetMarginTop(6)
	inner.SetMarginBottom(6)

	// navigation-sidebar is the standard GTK class for a compact navigation
	// list: it gives theme-default selection styling instead of a full-width
	// accent block.
	w.filterList = gtk.NewListBox()
	w.filterList.SetSelectionMode(gtk.SelectionSingle)
	w.filterList.AddCSSClass("navigation-sidebar")
	w.filterList.SetMarginStart(6)
	w.filterList.SetMarginEnd(6)
	sectionLabel := newDimLabel("Downloads")
	sectionLabel.AddCSSClass("caption")
	sectionLabel.SetMarginTop(8)
	sectionLabel.SetMarginBottom(2)
	sectionLabel.SetMarginStart(10)
	sectionLabel.SetMarginEnd(10)
	inner.Append(sectionLabel)
	for _, dest := range []struct {
		kind  filterKind
		icon  string
		label string
	}{
		{filterAll, "view-list-symbolic", "All Downloads"},
		{filterActive, "media-playback-start-symbolic", "Active"},
		{filterQueued, "appointment-soon-symbolic", "Queued"},
		{filterPaused, "media-playback-pause-symbolic", "Paused"},
		{filterCompleted, "object-select-symbolic", "Completed"},
		{filterFailed, "dialog-error-symbolic", "Failed"},
	} {
		row, count := addSidebarRow(w.filterList, dest.icon, dest.label)
		fr := &filterRow{filter: filter{kind: dest.kind, label: dest.label}, row: row, count: count}
		w.filterRows = append(w.filterRows, fr)
		w.filterByRow[row] = fr
	}
	inner.Append(w.filterList)

	w.categoryBox = gtk.NewBox(gtk.OrientationVertical, 0)
	categoryHeader := gtk.NewBox(gtk.OrientationHorizontal, 4)
	categoryHeader.SetMarginTop(12)
	categoryHeader.SetMarginBottom(2)
	categoryHeader.SetMarginStart(10)
	categoryHeader.SetMarginEnd(6)
	categoryLabel := newDimLabel("Categories")
	categoryLabel.AddCSSClass("caption")
	categoryLabel.SetHExpand(true)
	categoryHeader.Append(categoryLabel)
	newCategory := newIconButton("list-add-symbolic", "New category")
	newCategory.ConnectClicked(func() { w.showCategories() })
	categoryHeader.Append(newCategory)
	w.categoryBox.Append(categoryHeader)

	w.categoryList = gtk.NewListBox()
	w.categoryList.SetSelectionMode(gtk.SelectionSingle)
	w.categoryList.AddCSSClass("navigation-sidebar")
	w.categoryList.SetMarginStart(6)
	w.categoryList.SetMarginEnd(6)
	w.categoryBox.Append(w.categoryList)
	w.categoryBox.SetVisible(false)
	inner.Append(w.categoryBox)

	scroll.SetChild(inner)
	box.Append(scroll)

	w.filterList.ConnectRowSelected(func(row *gtk.ListBoxRow) {
		if row == nil || w.updatingSelection {
			return
		}
		if fr, ok := w.filterByRow[row]; ok {
			w.applyFilter(fr.filter)
		}
	})
	w.categoryList.ConnectRowSelected(func(row *gtk.ListBoxRow) {
		if row == nil || w.updatingSelection {
			return
		}
		if f, ok := w.categoryByRow[row]; ok {
			w.applyFilter(f)
		}
	})

	return box
}

// addSidebarRow appends one navigation row and returns it with its count label.
func addSidebarRow(list *gtk.ListBox, icon, text string) (*gtk.ListBoxRow, *gtk.Label) {
	row := gtk.NewListBoxRow()
	row.SetActivatable(true)

	content := gtk.NewBox(gtk.OrientationHorizontal, 8)
	content.SetMarginTop(6)
	content.SetMarginBottom(6)
	content.SetMarginStart(8)
	content.SetMarginEnd(8)

	image := gtk.NewImageFromIconName(icon)
	image.SetPixelSize(16)
	content.Append(image)

	name := newLabel(text)
	name.SetHExpand(true)
	count := newDimLabel("")
	count.SetHAlign(gtk.AlignEnd)

	content.Append(name)
	content.Append(count)
	row.SetChild(content)
	list.Append(row)
	return row, count
}

func (w *window) buildBanner() *gtk.Box {
	banner := gtk.NewBox(gtk.OrientationHorizontal, 10)
	banner.SetMarginTop(8)
	banner.SetMarginBottom(8)
	banner.SetMarginStart(12)
	banner.SetMarginEnd(12)
	banner.SetVisible(false)

	icon := gtk.NewImageFromIconName("dialog-warning-symbolic")
	banner.Append(icon)

	w.bannerLabel = newLabel("")
	w.bannerLabel.SetWrap(true)
	w.bannerLabel.SetHExpand(true)
	banner.Append(w.bannerLabel)

	reconnect := gtk.NewButtonWithLabel("Reconnect")
	reconnect.ConnectClicked(func() { w.retryNow() })
	banner.Append(reconnect)

	w.banner = banner
	return banner
}

func (w *window) buildToast() *gtk.Box {
	bar := gtk.NewBox(gtk.OrientationHorizontal, 10)
	bar.SetMarginTop(6)
	bar.SetMarginBottom(6)
	bar.SetMarginStart(12)
	bar.SetMarginEnd(12)
	bar.SetVisible(false)

	icon := gtk.NewImageFromIconName("dialog-information-symbolic")
	bar.Append(icon)

	w.toastLabel = newLabel("")
	w.toastLabel.SetWrap(true)
	w.toastLabel.SetHExpand(true)
	bar.Append(w.toastLabel)

	dismiss := newIconButton("window-close-symbolic", "Dismiss message")
	dismiss.ConnectClicked(func() {
		bar.SetVisible(false)
		if w.toastSource != 0 {
			coreglib.SourceRemove(w.toastSource)
			w.toastSource = 0
		}
	})
	bar.Append(dismiss)

	w.toastBar = bar
	return bar
}

func (w *window) buildStack() *gtk.Stack {
	w.stack = gtk.NewStack()
	w.stack.SetTransitionType(gtk.StackTransitionTypeNone)
	w.stack.SetHExpand(true)
	w.stack.SetVExpand(true)

	// Loading: local indication near the content, never a blocking dialog.
	loading := gtk.NewBox(gtk.OrientationVertical, 10)
	loading.SetHAlign(gtk.AlignCenter)
	loading.SetVAlign(gtk.AlignCenter)
	loading.SetHExpand(true)
	loading.SetVExpand(true)
	w.loadingSpinner = gtk.NewSpinner()
	loading.Append(w.loadingSpinner)
	loading.Append(newDimLabel("Loading downloads\u2026"))
	w.stack.AddNamed(loading, "loading")

	// Daemon unavailable.
	offlinePage, offlineTitle, offlineDetail := newEmptyPage("network-offline-symbolic")
	offlineTitle.SetLabel("Cannot reach the DM daemon")
	w.offlineDetail = offlineDetail
	offlineRetry := gtk.NewButtonWithLabel("Retry")
	offlineRetry.SetHAlign(gtk.AlignCenter)
	offlineRetry.ConnectClicked(func() { w.retryNow() })
	offlinePage.Append(offlineRetry)
	w.stack.AddNamed(offlinePage, "offline")

	// Empty collection or empty filter.
	emptyPage, emptyTitle, emptyBody := newEmptyPage("folder-download-symbolic")
	w.emptyTitle = emptyTitle
	w.emptyBody = emptyBody
	w.emptyAdd = gtk.NewButtonWithLabel("Add Download")
	w.emptyAdd.SetHAlign(gtk.AlignCenter)
	w.emptyAdd.AddCSSClass("suggested-action")
	w.emptyAdd.ConnectClicked(func() { w.showAddDialog() })
	emptyPage.Append(w.emptyAdd)
	w.stack.AddNamed(emptyPage, "empty")

	// The download list itself.
	w.model = gtk.NewStringList(nil)
	// The list is not a selection surface: rows are acted on through their own
	// controls, so a selected-row highlight would suggest state that does not
	// exist.
	selection := gtk.NewNoSelection(w.model)
	factory := gtk.NewSignalListItemFactory()
	factory.ConnectSetup(func(obj *coreglib.Object) {
		item, ok := obj.Cast().(*gtk.ListItem)
		if !ok {
			return
		}
		r := newRow(w)
		// Key by the native GObject pointer: gotk4 hands out a fresh Go
		// wrapper every time GTK returns the child, so pointer identity of
		// the wrapper is not stable across setup and bind.
		w.rowWidgets[objectKey(r.root)] = r
		item.SetChild(r.root)
	})
	factory.ConnectBind(func(obj *coreglib.Object) {
		item, ok := obj.Cast().(*gtk.ListItem)
		if !ok {
			return
		}
		r := w.rowFor(item)
		if r == nil {
			return
		}
		id := itemID(item)
		if id == "" {
			return
		}
		d, ok := w.store.get(id)
		if !ok {
			return
		}
		r.setDownload(d, w.store.rate(id))
		w.visibleRows[id] = r
	})
	factory.ConnectUnbind(func(obj *coreglib.Object) {
		item, ok := obj.Cast().(*gtk.ListItem)
		if !ok {
			return
		}
		r := w.rowFor(item)
		if r == nil {
			return
		}
		if r.id != "" {
			delete(w.visibleRows, r.id)
			r.id = ""
		}
	})

	w.listView = gtk.NewListView(selection, &factory.ListItemFactory)
	w.listView.SetShowSeparators(true)

	scroller := gtk.NewScrolledWindow()
	scroller.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	scroller.SetHExpand(true)
	scroller.SetVExpand(true)
	scroller.SetChild(w.listView)
	w.stack.AddNamed(scroller, "list")

	w.stack.SetVisibleChildName("loading")
	return w.stack
}

func (w *window) rowFor(item *gtk.ListItem) *row {
	child := item.Child()
	if child == nil {
		return nil
	}
	return w.rowWidgets[objectKey(child)]
}

// objectKey identifies a widget by its native GObject pointer. Two Go wrappers
// for the same GTK object produce the same key, unlike wrapper pointer identity.
func objectKey(obj coreglib.Objector) uintptr {
	return coreglib.InternObject(obj).Native()
}

func itemID(item *gtk.ListItem) string {
	obj := item.Item()
	if obj == nil {
		return ""
	}
	str, ok := obj.Cast().(*gtk.StringObject)
	if !ok {
		return ""
	}
	return str.String()
}

func (w *window) present() {
	w.win.Present()
	w.loadingSpinner.Start()
	go w.loop()
}

// ---- daemon connection loop ----

// loop keeps one REST snapshot plus one WebSocket stream alive. Any failure
// re-runs discovery, because a restarted daemon publishes a new ephemeral port.
func (w *window) loop() {
	backoff := time.Second
	for {
		if w.ctx.Err() != nil {
			return
		}

		client, err := w.connectFn()
		if err != nil {
			w.post(func() { w.onConnectFailure(err) })
			if !w.wait(backoff) {
				return
			}
			backoff = nextBackoff(backoff)
			continue
		}

		list, err := client.List(w.ctx)
		if err != nil {
			w.post(func() { w.onConnectFailure(err) })
			if !w.wait(backoff) {
				return
			}
			backoff = nextBackoff(backoff)
			continue
		}

		categories, err := client.Categories(w.ctx)
		if err != nil {
			categories = nil
		}
		settings, err := client.Settings(w.ctx)
		if err != nil {
			settings = dmapi.Settings{}
		}

		w.post(func() { w.onSnapshot(client, list, categories, settings) })

		events, err := client.Events(w.ctx)
		if err != nil {
			w.post(func() { w.onStreamLost(err) })
			if !w.wait(backoff) {
				return
			}
			backoff = nextBackoff(backoff)
			continue
		}

		backoff = time.Second
		for ev := range events {
			w.enqueue(ev)
		}
		if w.ctx.Err() != nil {
			return
		}
		w.post(func() { w.onStreamLost(nil) })
		if !w.wait(backoff) {
			return
		}
		backoff = nextBackoff(backoff)
	}
}

func nextBackoff(d time.Duration) time.Duration {
	if d >= 15*time.Second {
		return 15 * time.Second
	}
	return d * 2
}

// wait sleeps for d, waking early on window close or a manual retry.
func (w *window) wait(d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-w.ctx.Done():
		return false
	case <-timer.C:
		return true
	case <-w.retryCh:
		return true
	}
}

func (w *window) retryNow() {
	select {
	case w.retryCh <- struct{}{}:
	default:
	}
	w.bannerLabel.SetLabel("Reconnecting to the DM daemon\u2026")
}

// post runs f on the GTK main loop. Safe to call from any goroutine.
func (w *window) post(f func()) {
	coreglib.IdleAdd(f)
}

func (w *window) onSnapshot(client *dmapi.Client, list []model.Download, categories []model.Category, settings dmapi.Settings) {
	w.client = client
	w.loaded = true
	w.settings = settings
	w.defaultDir = settings.Dir
	w.store.replaceAll(list)
	w.updateCategories(categories)
	w.banner.SetVisible(false)
	w.rebuild()
}

func (w *window) onConnectFailure(err error) {
	w.client = nil
	if w.loaded {
		w.showBanner("Lost connection to the DM daemon. Reconnecting\u2026")
		return
	}
	w.loadingSpinner.Stop()
	w.offlineDetail.SetLabel(describeConnectError(err))
	w.stack.SetVisibleChildName("offline")
	w.banner.SetVisible(false)
}

func (w *window) onStreamLost(err error) {
	w.client = nil
	if !w.loaded {
		if err != nil {
			w.offlineDetail.SetLabel(describeConnectError(err))
		}
		w.stack.SetVisibleChildName("offline")
		return
	}
	message := "Lost connection to the DM daemon. Reconnecting\u2026"
	if err != nil {
		message = "Lost connection to the DM daemon (" + err.Error() + "). Reconnecting\u2026"
	}
	w.showBanner(message)
}

func (w *window) showBanner(message string) {
	w.bannerLabel.SetLabel(message)
	w.banner.SetVisible(true)
}

func describeConnectError(err error) string {
	switch {
	case errors.Is(err, dmapi.ErrDaemonNotRunning):
		return "Start the daemon with `dm --serve` and choose Retry. Downloads already running in the daemon are not affected."
	case errors.Is(err, dmapi.ErrNoToken):
		return "The daemon token file is missing. Start the daemon once so it can create it, then choose Retry."
	default:
		return err.Error()
	}
}

// ---- event coalescing ----

// enqueue records the newest event per download and schedules one flush. Called
// from the WebSocket goroutine; never touches GTK state directly.
func (w *window) enqueue(ev model.Event) {
	key := ev.DownloadID
	if key == "" {
		key = string(ev.Type)
	}

	w.mu.Lock()
	if w.pending == nil {
		w.pending = make(map[string]model.Event)
	}
	if _, ok := w.pending[key]; !ok {
		w.order = append(w.order, key)
	}
	w.pending[key] = ev
	if ev.Type != model.EventProgress {
		w.needsRebuild = true
	}
	schedule := !w.flushQueued
	w.flushQueued = true
	w.mu.Unlock()

	if schedule {
		w.post(w.flushEvents)
	}
}

// flushEvents applies queued events on the main loop and rebuilds the list at
// most once per batch.
func (w *window) flushEvents() {
	w.mu.Lock()
	pending, order := w.pending, w.order
	rebuild := w.needsRebuild
	w.pending, w.order = nil, nil
	w.needsRebuild = false
	w.flushQueued = false
	w.mu.Unlock()

	for _, id := range order {
		ev, ok := pending[id]
		if !ok {
			continue
		}
		if w.store.apply(ev) {
			w.dirty[id] = true
		}
	}

	if rebuild {
		w.rebuild()
		return
	}
	// Progress-only batches wait for the row timer, keeping row updates
	// bounded no matter how chatty the daemon is.
}

// flushRows updates the rows currently bound to visible list items.
func (w *window) flushRows() {
	for id := range w.dirty {
		if r, ok := w.visibleRows[id]; ok {
			if d, ok := w.store.get(id); ok {
				r.setDownload(d, w.store.rate(id))
			}
		}
		delete(w.dirty, id)
	}
}

// ---- list and sidebar state ----

func (w *window) rebuild() {
	ids := w.store.ids(w.filter)
	w.model.Splice(0, w.model.NItems(), ids)

	w.updateCounts()
	if !w.loaded {
		return
	}
	w.updateEmptyState()
	if len(ids) > 0 {
		w.stack.SetVisibleChildName("list")
	} else {
		w.stack.SetVisibleChildName("empty")
	}
	w.flushRows()
}

func (w *window) updateCounts() {
	counts := w.store.counts()
	for _, fr := range w.filterRows {
		fr.count.SetLabel(strconv.Itoa(counts[fr.filter.kind]))
	}
	for _, cr := range w.categoryRows {
		cr.count.SetLabel(strconv.Itoa(w.store.countCategory(cr.filter.categoryID)))
	}
	w.updateHeaderSummary(counts)
}

// updateHeaderSummary keeps the one-line subtitle under the view title honest:
// totals always, active and failed counts only when they are non-zero.
func (w *window) updateHeaderSummary(counts map[filterKind]int) {
	total := counts[filterAll]
	if total == 0 {
		w.subtitleLabel.SetLabel("")
		return
	}
	parts := []string{fmt.Sprintf("%d download", total)}
	if total != 1 {
		parts[0] += "s"
	}
	if n := counts[filterActive]; n > 0 {
		parts = append(parts, fmt.Sprintf("%d active", n))
	}
	if n := counts[filterFailed]; n > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", n))
	}
	w.subtitleLabel.SetLabel(strings.Join(parts, " \u00b7 "))
}

func (w *window) updateEmptyState() {
	if w.store.len() == 0 {
		w.emptyTitle.SetLabel("No downloads yet")
		w.emptyBody.SetLabel("Files you download with DM appear here. Add a download to get started.")
		w.emptyAdd.SetVisible(true)
		return
	}
	w.emptyTitle.SetLabel("No downloads match this view")
	w.emptyBody.SetLabel(emptyFilterBody(w.filter))
	w.emptyAdd.SetVisible(false)
}

func emptyFilterBody(f filter) string {
	switch f.kind {
	case filterActive:
		return "Nothing is downloading right now."
	case filterQueued:
		return "Nothing is waiting to start."
	case filterPaused:
		return "No downloads are paused."
	case filterCompleted:
		return "No downloads have completed yet."
	case filterFailed:
		return "No downloads have failed."
	case filterCategory:
		return "This category has no downloads yet."
	default:
		return "No downloads match the current filter."
	}
}

func (w *window) updateCategories(categories []model.Category) {
	w.categories = categories
	w.categoryList.RemoveAll()
	w.categoryRows = nil
	w.categoryByRow = make(map[*gtk.ListBoxRow]filter)

	for _, c := range categories {
		row, count := addSidebarRow(w.categoryList, "folder-symbolic", c.Name)
		f := filter{kind: filterCategory, categoryID: c.ID, label: c.Name}
		w.categoryRows = append(w.categoryRows, &categoryRow{filter: f, row: row, count: count})
		w.categoryByRow[row] = f
	}

	if w.filter.kind == filterCategory && !w.categoryExists(w.filter.categoryID) {
		w.setFilterSilently(filter{kind: filterAll, label: "All Downloads"})
	}
	w.syncSelection()
}

func (w *window) categoryExists(id string) bool {
	for _, c := range w.categories {
		if c.ID == id {
			return true
		}
	}
	return false
}

func (w *window) applyFilter(f filter) {
	w.filter = f
	w.titleLabel.SetLabel(f.label)
	w.win.SetTitle("DM \u2014 " + f.label)
	w.syncSelection()
	w.rebuild()
}

// setFilterSilently changes the filter without rebuilding, for callers that
// rebuild immediately afterwards (or are already rebuilding).
func (w *window) setFilterSilently(f filter) {
	w.filter = f
	w.titleLabel.SetLabel(f.label)
	w.win.SetTitle("DM \u2014 " + f.label)
}

func (w *window) syncSelection() {
	w.updatingSelection = true
	defer func() { w.updatingSelection = false }()

	matched := false
	for _, fr := range w.filterRows {
		if fr.filter == w.filter {
			w.filterList.SelectRow(fr.row)
			matched = true
		}
	}
	if !matched {
		w.filterList.UnselectAll()
	}

	matched = false
	for _, cr := range w.categoryRows {
		if cr.filter == w.filter {
			w.categoryList.SelectRow(cr.row)
			matched = true
		}
	}
	if !matched {
		w.categoryList.UnselectAll()
	}
}

// sidebarBreakpoint is the window width below which the filter sidebar is
// hidden automatically. The header toggle still opens it on demand.
const sidebarBreakpoint = 820

// adaptSidebar collapses the sidebar on narrow windows so the list keeps its
// room, and restores it when there is space again unless the user toggled it.
func (w *window) adaptSidebar() {
	width, ok := w.win.ObjectProperty("default-width").(int)
	if !ok || width <= 0 {
		return
	}
	switch {
	case width < sidebarBreakpoint && w.sidebarRevealer.RevealChild():
		w.setSidebarVisible(false)
		w.sidebarAuto = true
	case width >= sidebarBreakpoint && w.sidebarAuto:
		w.setSidebarVisible(true)
		w.sidebarAuto = false
	}
}

func (w *window) setSidebarVisible(visible bool) {
	w.sidebarRevealer.SetRevealChild(visible)
	label := "Show filters"
	if visible {
		label = "Hide filters"
	}
	w.sidebarToggle.SetTooltipText(label)
	setAccessibleLabel(w.sidebarToggle.Accessible, label)
}

func (w *window) toast(message string) {
	w.toastLabel.SetLabel(message)
	w.toastBar.SetVisible(true)
	if w.toastSource != 0 {
		coreglib.SourceRemove(w.toastSource)
	}
	w.toastSource = coreglib.TimeoutAdd(6000, func() bool {
		w.toastBar.SetVisible(false)
		w.toastSource = 0
		return false
	})
}

// ---- download actions ----

// onAdded shows a download the daemon accepted even when the current filter
// would hide it: a successful add must be visible.
func (w *window) onAdded(d model.Download) {
	w.store.apply(model.Event{Type: model.EventAdded, DownloadID: d.ID, Download: &d})
	if w.filter.kind != filterAll && !w.filter.matches(d) {
		w.setFilterSilently(filter{kind: filterAll, label: "All Downloads"})
		w.syncSelection()
	}
	w.rebuild()
}

func (w *window) pauseDownload(id string) {
	w.control("pause", id, "pause")
}

func (w *window) resumeDownload(id string) {
	w.control("resume", id, "resume")
}

// retryDownload re-queues a failed or canceled download. The daemon exposes it
// as resume: a retry is the same transition back to queued.
func (w *window) retryDownload(id string) {
	w.control("resume", id, "retry")
}

func (w *window) cancelDownload(id string) {
	w.control("cancel", id, "cancel")
}

func (w *window) control(action, id, verb string) {
	c := w.client
	if c == nil {
		w.toast("Not connected to the DM daemon.")
		return
	}
	go func() {
		var err error
		switch action {
		case "pause":
			err = c.Pause(w.ctx, id)
		case "resume":
			err = c.Resume(w.ctx, id)
		case "cancel":
			err = c.Cancel(w.ctx, id)
		}
		if err != nil {
			message := "Could not " + verb + " the download: " + err.Error()
			w.post(func() { w.toast(message) })
		}
	}()
}

func (w *window) removeDownload(id string, deleteFile bool) {
	if w.client == nil {
		w.toast("Not connected to the DM daemon.")
		return
	}
	d, ok := w.store.get(id)
	if !ok {
		return
	}
	if !deleteFile {
		w.doRemove(id, false)
		return
	}

	showConfirm(
		&w.win.Window,
		"Delete file?",
		fmt.Sprintf("%s will be deleted permanently.", d.Filename),
		"From "+d.Dir,
		"Delete",
		func() { w.doRemove(id, true) },
	)
}

func (w *window) doRemove(id string, deleteFile bool) {
	c := w.client
	if c == nil {
		return
	}
	go func() {
		if err := c.Remove(w.ctx, id, deleteFile); err != nil {
			message := "Could not remove the download: " + err.Error()
			w.post(func() { w.toast(message) })
		}
	}()
}

func (w *window) openFile(id string) {
	d, ok := w.store.get(id)
	if !ok {
		return
	}
	if d.Dir == "" || d.Filename == "" {
		w.toast("The file location is unknown.")
		return
	}
	path := filepath.Join(d.Dir, d.Filename)
	gtk.ShowURI(&w.win.Window, (&url.URL{Scheme: "file", Path: path}).String(), 0)
}

func (w *window) showAddDialog() {
	if w.client == nil {
		w.toast("Not connected to the DM daemon.")
		return
	}
	if w.addDialog != nil {
		w.addDialog.win.Present()
		w.addDialog.urlEntry.GrabFocus()
		return
	}
	d := newAddDialog(w)
	w.addDialog = d
	d.win.ConnectCloseRequest(func() bool {
		w.addDialog = nil
		return false
	})
	d.present()
}

func (w *window) showPreferences() {
	if w.client == nil {
		w.toast("Not connected to the DM daemon.")
		return
	}
	if w.prefsDialog != nil {
		w.prefsDialog.present()
		return
	}
	d := newPreferencesDialog(w)
	w.prefsDialog = d
	d.win.ConnectCloseRequest(func() bool {
		w.prefsDialog = nil
		return false
	})
	d.present()
}

func (w *window) showCategories() {
	if w.client == nil {
		w.toast("Not connected to the DM daemon.")
		return
	}
	if w.catsDialog != nil {
		w.catsDialog.present()
		return
	}
	d := newCategoriesDialog(w)
	w.catsDialog = d
	d.win.ConnectCloseRequest(func() bool {
		w.catsDialog = nil
		return false
	})
	d.present()
}

func (w *window) shutdown() {
	w.cancel()
	w.client = nil
	if w.rowTimer != 0 {
		coreglib.SourceRemove(w.rowTimer)
		w.rowTimer = 0
	}
	if w.toastSource != 0 {
		coreglib.SourceRemove(w.toastSource)
		w.toastSource = 0
	}
}
