package ui

import (
	"fmt"
	"strings"

	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/vishwa/dm/internal/model"
)

// categoriesDialog manages the user's categories. Categories only group
// downloads and give them a default folder; deleting one never deletes files,
// so the dialog says so before it asks for confirmation.
type categoriesDialog struct {
	w   *window
	win *gtk.Window

	list *gtk.ListBox

	nameEntry *gtk.Entry
	dirEntry  *gtk.Entry
	addButton *gtk.Button

	errorBox   *gtk.Box
	errorLabel *gtk.Label

	busy bool
}

func newCategoriesDialog(w *window) *categoriesDialog {
	d := &categoriesDialog{w: w}

	d.win = gtk.NewWindow()
	d.win.SetTitle("Categories")
	d.win.SetModal(true)
	d.win.SetTransientFor(&w.win.Window)
	d.win.SetDestroyWithParent(true)
	d.win.SetDefaultSize(560, 520)

	header := gtk.NewHeaderBar()
	closeButton := gtk.NewButtonWithLabel("Close")
	closeButton.ConnectClicked(func() { d.win.Close() })
	header.PackEnd(closeButton)
	d.win.SetTitlebar(header)

	content := gtk.NewBox(gtk.OrientationVertical, 12)
	content.SetMarginTop(18)
	content.SetMarginBottom(18)
	content.SetMarginStart(18)
	content.SetMarginEnd(18)

	content.Append(sectionHeading("Your categories"))

	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	scroll.SetVExpand(true)
	scroll.SetMinContentHeight(120)

	d.list = gtk.NewListBox()
	d.list.SetSelectionMode(gtk.SelectionNone)
	d.list.AddCSSClass("navigation-sidebar")
	scroll.SetChild(d.list)
	content.Append(scroll)

	content.Append(gtk.NewSeparator(gtk.OrientationHorizontal))
	content.Append(sectionHeading("New category"))

	d.nameEntry = gtk.NewEntry()
	d.nameEntry.SetPlaceholderText("Videos")
	d.nameEntry.SetHExpand(true)
	d.nameEntry.SetActivatesDefault(true)
	content.Append(fieldLabel("_Name", d.nameEntry))
	content.Append(d.nameEntry)

	d.dirEntry = gtk.NewEntry()
	d.dirEntry.SetPlaceholderText("Optional: separate folder for this category")
	d.dirEntry.SetHExpand(true)
	content.Append(fieldLabel("Default f_older", d.dirEntry))
	folderRow := gtk.NewBox(gtk.OrientationHorizontal, 8)
	folderRow.Append(d.dirEntry)
	choose := gtk.NewButtonWithLabel("Choose\u2026")
	choose.ConnectClicked(func() { d.chooseFolder() })
	folderRow.Append(choose)
	content.Append(folderRow)

	d.errorBox, d.errorLabel = errorRow()
	content.Append(d.errorBox)

	addRow := gtk.NewBox(gtk.OrientationHorizontal, 8)
	addRow.SetHAlign(gtk.AlignEnd)
	d.addButton = gtk.NewButtonWithLabel("Add Category")
	d.addButton.AddCSSClass("suggested-action")
	d.addButton.ConnectClicked(func() { d.add() })
	addRow.Append(d.addButton)
	content.Append(addRow)

	d.win.SetChild(content)
	d.win.SetDefaultWidget(d.addButton)

	controller := gtk.NewShortcutController()
	controller.AddShortcut(gtk.NewShortcut(
		gtk.NewShortcutTriggerParseString("Escape"),
		gtk.NewNamedAction("window.close"),
	))
	d.win.AddController(controller)

	d.nameEntry.ConnectChanged(func() { d.errorBox.SetVisible(false) })

	d.render(w.categories)
	return d
}

func (d *categoriesDialog) present() {
	d.win.Present()
	d.nameEntry.GrabFocus()
}

// render rebuilds the category list from the newest daemon snapshot.
func (d *categoriesDialog) render(categories []model.Category) {
	d.list.RemoveAll()

	if len(categories) == 0 {
		row := gtk.NewListBoxRow()
		row.SetActivatable(false)
		row.SetSelectable(false)
		empty := newDimLabel("No categories yet. The list above stays empty until you add one.")
		empty.AddCSSClass("caption")
		empty.SetMarginTop(10)
		empty.SetMarginBottom(10)
		empty.SetMarginStart(10)
		empty.SetMarginEnd(10)
		row.SetChild(empty)
		d.list.Append(row)
		return
	}

	for _, c := range categories {
		d.list.Append(d.categoryRow(c))
	}
}

func (d *categoriesDialog) categoryRow(c model.Category) *gtk.ListBoxRow {
	row := gtk.NewListBoxRow()
	row.SetActivatable(false)
	row.SetSelectable(false)

	box := gtk.NewBox(gtk.OrientationHorizontal, 8)
	box.SetMarginTop(6)
	box.SetMarginBottom(6)
	box.SetMarginStart(10)
	box.SetMarginEnd(6)

	text := gtk.NewBox(gtk.OrientationVertical, 0)
	text.SetHExpand(true)
	name := newLabel(c.Name)
	text.Append(name)
	if c.DefaultDir != "" {
		dir := newEllipsizedLabel(c.DefaultDir)
		dir.AddCSSClass("caption")
		dir.AddCSSClass("dim-label")
		dir.SetTooltipText(c.DefaultDir)
		text.Append(dir)
	}
	box.Append(text)

	// The name is part of the accessible label: an icon-only delete control
	// must say which category it deletes.
	remove := newIconButton("edit-delete-symbolic", "Delete "+c.Name)
	remove.ConnectClicked(func() {
		showConfirm(
			d.win,
			"Delete category?",
			fmt.Sprintf("%s will be removed.", c.Name),
			"Downloads in it and their files are kept.",
			"Delete",
			func() { d.delete(c) },
		)
	})
	box.Append(remove)

	row.SetChild(box)
	return row
}

func (d *categoriesDialog) chooseFolder() {
	dialog := gtk.NewFileDialog()
	dialog.SetTitle("Choose Category Folder")
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

func (d *categoriesDialog) showError(message string) {
	d.errorLabel.SetLabel(message)
	d.errorBox.SetVisible(true)
}

func (d *categoriesDialog) setBusy(busy bool) {
	d.busy = busy
	d.addButton.SetSensitive(!busy)
}

func (d *categoriesDialog) add() {
	if d.busy {
		return
	}
	name := strings.TrimSpace(d.nameEntry.Text())
	if name == "" {
		d.showError("Enter a category name.")
		d.nameEntry.GrabFocus()
		return
	}
	c := d.w.client
	if c == nil {
		d.showError("Not connected to the DM daemon.")
		return
	}

	d.errorBox.SetVisible(false)
	d.setBusy(true)
	dir := strings.TrimSpace(d.dirEntry.Text())
	go func() {
		_, err := c.AddCategory(d.w.ctx, name, dir)
		d.w.post(func() {
			d.setBusy(false)
			if err != nil {
				d.showError(err.Error())
				return
			}
			d.nameEntry.SetText("")
			d.dirEntry.SetText("")
			d.reload()
		})
	}()
}

func (d *categoriesDialog) delete(c model.Category) {
	client := d.w.client
	if client == nil {
		d.showError("Not connected to the DM daemon.")
		return
	}
	d.setBusy(true)
	go func() {
		err := client.DeleteCategory(d.w.ctx, c.ID)
		d.w.post(func() {
			d.setBusy(false)
			if err != nil {
				d.showError(err.Error())
				return
			}
			d.reload()
		})
	}()
}

// reload refreshes both the dialog and the main window's sidebar, so a new
// category is immediately usable for filtering.
func (d *categoriesDialog) reload() {
	c := d.w.client
	if c == nil {
		return
	}
	go func() {
		categories, err := c.Categories(d.w.ctx)
		d.w.post(func() {
			if err != nil {
				d.showError(err.Error())
				return
			}
			d.render(categories)
			d.w.updateCategories(categories)
			d.w.rebuild()
		})
	}()
}
