// Command dm-desktop is the native GTK4 client for the dm download manager.
//
// It discovers the running daemon through its runtime endpoint metadata,
// authenticates with the existing bearer token, and drives downloads through
// the daemon's REST API and WebSocket events. It never starts its own engine.
package main

import (
	"os"

	"github.com/vishwa/dm/desktop/internal/dmapi"
	"github.com/vishwa/dm/desktop/internal/ui"
)

const version = "0.2.0"

func main() {
	os.Exit(ui.Run(version, dmapi.Connect))
}
