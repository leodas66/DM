package ui

import "testing"

// TestDropVisibleKeepsReplacementRow covers the ordering GTK uses when a model
// splice rebinds a visible row: the new row is bound before the old row is
// unbound. The unbind must not delete the new row's mapping.
func TestDropVisibleKeepsReplacementRow(t *testing.T) {
	w := &window{visibleRows: make(map[string]*row)}
	oldRow := &row{id: "d1"}
	newRow := &row{id: "d1"}

	w.visibleRows["d1"] = oldRow
	w.visibleRows["d1"] = newRow
	w.dropVisible(oldRow)

	if got, ok := w.visibleRows["d1"]; !ok || got != newRow {
		t.Fatalf("visibleRows[d1] = %p (present %v), want replacement row %p", got, ok, newRow)
	}
	if oldRow.id != "" {
		t.Fatalf("old row id = %q, want it cleared", oldRow.id)
	}
}

// TestDropVisibleRemovesOwnRow is the ordinary unbind: the row that is leaving
// the screen owns the mapping, so it is removed.
func TestDropVisibleRemovesOwnRow(t *testing.T) {
	w := &window{visibleRows: make(map[string]*row)}
	r := &row{id: "d1"}
	w.visibleRows["d1"] = r

	w.dropVisible(r)

	if _, ok := w.visibleRows["d1"]; ok {
		t.Fatal("visibleRows[d1] still present after unbinding its own row")
	}
	if r.id != "" {
		t.Fatalf("row id = %q, want it cleared", r.id)
	}
}

func TestDropVisibleIgnoresUnboundRow(t *testing.T) {
	w := &window{visibleRows: map[string]*row{"d1": {id: "d1"}}}
	bound := w.visibleRows["d1"]

	w.dropVisible(&row{})

	if got, ok := w.visibleRows["d1"]; !ok || got != bound {
		t.Fatalf("visibleRows[d1] = %p (present %v), want the bound row %p", got, ok, bound)
	}
}
