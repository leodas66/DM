package ui

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/vishwa/dm/desktop/internal/dmapi"
)

// Limits the preferences form exposes. They mirror what the daemon keeps
// meaningful for a desktop session: more parallel files than this starves the
// disk, and more connections than this starves the server.
const (
	maxConcurrentLimit  = 16
	maxConnectionsLimit = 32
	maxMiBPerSecond     = 1024
)

// editableSettings holds the values the preferences form owns. The daemon
// replaces the whole settings record on PUT, so anything the form does not show
// is carried over from the base record instead of being reset to zero.
type editableSettings struct {
	Dir                 string
	MaxConcurrent       int
	Connections         int
	GlobalMiBLimit      float64
	PerDownloadMiBLimit float64
}

// mergeSettings validates the form and returns the complete record to send.
func mergeSettings(base dmapi.Settings, edit editableSettings) (dmapi.Settings, error) {
	dir := strings.TrimSpace(edit.Dir)
	if dir == "" {
		return dmapi.Settings{}, errors.New("Choose a default download folder.")
	}
	if edit.MaxConcurrent < 1 || edit.MaxConcurrent > maxConcurrentLimit {
		return dmapi.Settings{}, fmt.Errorf("Simultaneous downloads must be between 1 and %d.", maxConcurrentLimit)
	}
	if edit.Connections < 1 || edit.Connections > maxConnectionsLimit {
		return dmapi.Settings{}, fmt.Errorf("Connections per download must be between 1 and %d.", maxConnectionsLimit)
	}
	if math.IsNaN(edit.GlobalMiBLimit) || math.IsInf(edit.GlobalMiBLimit, 0) || edit.GlobalMiBLimit < 0 ||
		edit.GlobalMiBLimit > maxMiBPerSecond {
		return dmapi.Settings{}, errors.New("The overall speed limit must be between 0 and 1024 MiB/s.")
	}
	if math.IsNaN(edit.PerDownloadMiBLimit) || math.IsInf(edit.PerDownloadMiBLimit, 0) || edit.PerDownloadMiBLimit < 0 ||
		edit.PerDownloadMiBLimit > maxMiBPerSecond {
		return dmapi.Settings{}, errors.New("The per-download speed limit must be between 0 and 1024 MiB/s.")
	}

	base.Dir = dir
	base.MaxConcurrent = edit.MaxConcurrent
	base.Connections = edit.Connections
	base.GlobalLimit = bytesFromMiBLimit(edit.GlobalMiBLimit)
	base.PerDownloadLimit = bytesFromMiBLimit(edit.PerDownloadMiBLimit)
	return base, nil
}

// mibLimit turns a stored byte rate into the MiB/s value the form shows.
func mibLimit(bytesPerSecond int64) float64 {
	if bytesPerSecond <= 0 {
		return 0
	}
	return float64(bytesPerSecond) / (1024 * 1024)
}

// bytesFromMiBLimit converts a MiB/s form value back to bytes per second.
func bytesFromMiBLimit(mib float64) int64 {
	if mib <= 0 {
		return 0
	}
	return int64(math.Round(mib * 1024 * 1024))
}

// preferencesDialog edits the daemon-wide settings. It is modal: settings
// changes affect running downloads, so there is one editor at a time.
type preferencesDialog struct {
	w   *window
	win *gtk.Window

	base dmapi.Settings

	dirEntry        *gtk.Entry
	concurrencySpin *gtk.SpinButton
	connectionsSpin *gtk.SpinButton
	globalLimitSpin *gtk.SpinButton
	perDownloadSpin *gtk.SpinButton
	appearanceDrop  *gtk.DropDown
	densityDrop     *gtk.DropDown
	errorBox        *gtk.Box
	errorLabel      *gtk.Label
	saveButton      *gtk.Button

	saving bool
}

func newPreferencesDialog(w *window) *preferencesDialog {
	d := &preferencesDialog{w: w, base: w.settings}

	d.win = gtk.NewWindow()
	d.win.SetTitle("Preferences")
	d.win.SetModal(true)
	d.win.SetTransientFor(&w.win.Window)
	d.win.SetDestroyWithParent(true)
	d.win.SetDefaultSize(560, -1)

	header := gtk.NewHeaderBar()
	cancel := gtk.NewButtonWithLabel("Cancel")
	cancel.ConnectClicked(func() { d.win.Close() })
	header.PackStart(cancel)

	d.saveButton = gtk.NewButtonWithLabel("Save")
	d.saveButton.AddCSSClass("suggested-action")
	d.saveButton.ConnectClicked(func() { d.save() })
	header.PackEnd(d.saveButton)
	d.win.SetTitlebar(header)

	content := gtk.NewBox(gtk.OrientationVertical, 10)
	content.SetMarginTop(18)
	content.SetMarginBottom(18)
	content.SetMarginStart(18)
	content.SetMarginEnd(18)

	content.Append(sectionHeading("Appearance"))
	d.appearanceDrop = gtk.NewDropDownFromStrings([]string{"Follow system", "Dark", "Light"})
	d.appearanceDrop.SetSelected(themeChoiceIndex(w.prefs.Theme))
	content.Append(dropdownRow("_Appearance", d.appearanceDrop, "Controls DM's palette without changing your desktop theme."))

	d.densityDrop = gtk.NewDropDownFromStrings([]string{"Comfortable", "Compact"})
	d.densityDrop.SetSelected(densityChoiceIndex(w.prefs.Density))
	content.Append(dropdownRow("_List density", d.densityDrop, "Compact rows show more downloads at once."))

	content.Append(gtk.NewSeparator(gtk.OrientationHorizontal))
	content.Append(sectionHeading("Downloads"))

	d.dirEntry = gtk.NewEntry()
	d.dirEntry.SetText(d.base.Dir)
	d.dirEntry.SetPlaceholderText("Folder for new downloads")
	d.dirEntry.SetHExpand(true)
	content.Append(fieldLabel("_Default folder", d.dirEntry))
	folderRow := gtk.NewBox(gtk.OrientationHorizontal, 8)
	folderRow.Append(d.dirEntry)
	choose := gtk.NewButtonWithLabel("Choose\u2026")
	choose.ConnectClicked(func() { d.chooseFolder() })
	folderRow.Append(choose)
	content.Append(folderRow)

	d.concurrencySpin = numberSpin(d.base.MaxConcurrent, 1, maxConcurrentLimit)
	content.Append(optionRow(
		"Simultaneous _downloads",
		d.concurrencySpin,
		"",
		"How many downloads transfer at the same time.",
	))
	d.connectionsSpin = numberSpin(d.base.Connections, 1, maxConnectionsLimit)
	content.Append(optionRow(
		"Connections per down_load",
		d.connectionsSpin,
		"",
		"Segments opened per file. Servers may cap this.",
	))

	content.Append(gtk.NewSeparator(gtk.OrientationHorizontal))
	content.Append(sectionHeading("Speed limits"))

	d.globalLimitSpin = limitSpin(mibLimit(d.base.GlobalLimit))
	content.Append(optionRow(
		"Overall _limit",
		d.globalLimitSpin,
		"MiB/s",
		"Applies to all downloads together. 0 means unlimited.",
	))
	d.perDownloadSpin = limitSpin(mibLimit(d.base.PerDownloadLimit))
	content.Append(optionRow(
		"Per-download lim_it",
		d.perDownloadSpin,
		"MiB/s",
		"Applies to each download on its own. 0 means unlimited.",
	))

	d.errorBox, d.errorLabel = errorRow()
	content.Append(d.errorBox)

	d.appearanceDrop.NotifyProperty("selected", d.persistAppearance)
	d.densityDrop.NotifyProperty("selected", d.persistAppearance)

	// Large text or a small screen must not clip the form: the dialog caps its
	// natural height and scrolls instead.
	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	scroll.SetMaxContentHeight(560)
	scroll.SetChild(content)
	d.win.SetChild(scroll)
	d.win.SetDefaultWidget(d.saveButton)

	controller := gtk.NewShortcutController()
	controller.AddShortcut(gtk.NewShortcut(
		gtk.NewShortcutTriggerParseString("Escape"),
		gtk.NewNamedAction("window.close"),
	))
	d.win.AddController(controller)

	return d
}

// sectionHeading is a small group title, using the theme's own caption style.
func sectionHeading(text string) *gtk.Label {
	l := newLabel(text)
	l.AddCSSClass("heading")
	return l
}

func themeChoiceIndex(choice themeChoice) uint {
	switch choice {
	case themeDark:
		return 1
	case themeLight:
		return 2
	default:
		return 0
	}
}

func densityChoiceIndex(choice densityChoice) uint {
	if choice == densityCompact {
		return 1
	}
	return 0
}

func (d *preferencesDialog) persistAppearance() {
	prefs := d.w.prefs
	switch d.appearanceDrop.Selected() {
	case 1:
		prefs.Theme = themeDark
	case 2:
		prefs.Theme = themeLight
	default:
		prefs.Theme = themeSystem
	}
	if d.densityDrop.Selected() == 1 {
		prefs.Density = densityCompact
	} else {
		prefs.Density = densityComfortable
	}
	if err := d.w.setClientPrefs(prefs); err != nil {
		d.showError("Appearance changed but could not be saved: " + err.Error())
		return
	}
	if d.errorBox != nil {
		d.errorBox.SetVisible(false)
	}
}

func dropdownRow(label string, control *gtk.DropDown, help string) *gtk.Box {
	row := gtk.NewBox(gtk.OrientationVertical, 4)
	l := gtk.NewLabel("")
	l.SetUseUnderline(true)
	l.SetTextWithMnemonic(label)
	l.SetHAlign(gtk.AlignStart)
	l.SetMnemonicWidget(control)
	setAccessibleLabel(control.Accessible, strings.ReplaceAll(label, "_", ""))
	row.Append(l)
	control.SetHExpand(true)
	row.Append(control)
	if help != "" {
		h := newDimLabel(help)
		h.AddCSSClass("caption")
		h.SetWrap(true)
		row.Append(h)
	}
	return row
}

// numberSpin builds an integer spinner with no decimals.
func numberSpin(value, min, max int) *gtk.SpinButton {
	s := gtk.NewSpinButtonWithRange(float64(min), float64(max), 1)
	s.SetValue(float64(value))
	s.SetNumeric(true)
	s.SetHAlign(gtk.AlignStart)
	s.SetSizeRequest(96, -1)
	return s
}

// limitSpin builds a MiB/s spinner. Half steps keep limits that were set
// through the API representable instead of silently rounding them.
func limitSpin(value float64) *gtk.SpinButton {
	s := gtk.NewSpinButtonWithRange(0, maxMiBPerSecond, 0.5)
	s.SetDigits(1)
	s.SetNumeric(true)
	s.SetValue(value)
	s.SetHAlign(gtk.AlignStart)
	s.SetSizeRequest(110, -1)
	return s
}

// optionRow lays out one labelled preference: control, unit, help text.
func optionRow(label string, control *gtk.SpinButton, unit, help string) *gtk.Box {
	row := gtk.NewBox(gtk.OrientationVertical, 4)

	g := gtk.NewGrid()
	g.SetColumnSpacing(8)
	g.SetRowSpacing(0)
	g.Attach(labelToControl(label, control, control.Accessible), 0, 0, 1, 1)
	g.Attach(control, 1, 0, 1, 1)
	if unit != "" {
		unitLabel := newDimLabel(unit)
		g.Attach(unitLabel, 2, 0, 1, 1)
	}
	g.SetHExpand(true)
	row.Append(g)

	if help != "" {
		h := newDimLabel(help)
		h.AddCSSClass("caption")
		h.SetWrap(true)
		row.Append(h)
	}
	return row
}

// labelToControl builds a mnemonic label and points it at control, so the
// association is programmatic.
func labelToControl(mnemonic string, control gtk.Widgetter, accessible gtk.Accessible) *gtk.Label {
	plain := strings.ReplaceAll(mnemonic, "_", "")
	l := gtk.NewLabel("")
	l.SetXAlign(0)
	l.SetHAlign(gtk.AlignStart)
	l.SetUseUnderline(true)
	l.SetTextWithMnemonic(mnemonic)
	l.SetMnemonicWidget(control)
	setAccessibleLabel(accessible, plain)
	return l
}

func (d *preferencesDialog) present() {
	d.win.Present()
	d.dirEntry.GrabFocus()
}

func (d *preferencesDialog) chooseFolder() {
	dialog := gtk.NewFileDialog()
	dialog.SetTitle("Choose Download Folder")
	dialog.SetModal(true)
	dialog.SelectFolder(d.w.ctx, d.win, func(res gio.AsyncResulter) {
		file, err := dialog.SelectFolderFinish(res)
		if err != nil || file == nil {
			return
		}
		if path := file.Path(); path != "" {
			d.dirEntry.SetText(path)
		}
	})
}

func (d *preferencesDialog) read() editableSettings {
	return editableSettings{
		Dir:                 strings.TrimSpace(d.dirEntry.Text()),
		MaxConcurrent:       d.concurrencySpin.ValueAsInt(),
		Connections:         d.connectionsSpin.ValueAsInt(),
		GlobalMiBLimit:      d.globalLimitSpin.Value(),
		PerDownloadMiBLimit: d.perDownloadSpin.Value(),
	}
}

func (d *preferencesDialog) showError(message string) {
	d.errorLabel.SetLabel(message)
	d.errorBox.SetVisible(true)
}

func (d *preferencesDialog) save() {
	if d.saving {
		return
	}
	next, err := mergeSettings(d.base, d.read())
	if err != nil {
		d.showError(err.Error())
		return
	}

	c := d.w.client
	if c == nil {
		d.showError("Not connected to the DM daemon.")
		return
	}

	d.errorBox.SetVisible(false)
	d.saving = true
	d.saveButton.SetSensitive(false)
	go func() {
		stored, err := c.UpdateSettings(d.w.ctx, next)
		d.w.post(func() {
			d.saving = false
			d.saveButton.SetSensitive(true)
			if err != nil {
				d.showError(err.Error())
				return
			}
			d.w.settings = stored
			d.w.defaultDir = stored.Dir
			d.w.toast("Preferences saved.")
			d.win.Close()
		})
	}()
}
