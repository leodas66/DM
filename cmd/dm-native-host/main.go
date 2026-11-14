// Command dm-native-host is the browser's Native Messaging host. It reads one
// framed JSON request per message on stdin and writes one framed JSON response
// on stdout; diagnostics go to stderr only, because the browser owns both
// other streams.
//
// It holds no credentials: the daemon's bearer token is read from the same
// 0600 file every local client uses, and the daemon address comes from the
// runtime endpoint file.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/vishwa/dm/internal/nativehost"
)

const version = "0.2.1"

// hostName is the Native Messaging host name the extension asks for. Browser
// manifests must use exactly this value.
const hostName = "io.github.vishwa.dm"

func main() {
	manifest := flag.String("manifest", "", `print a Native Messaging manifest ("chrome" or "firefox") and exit`)
	extensionID := flag.String("extension-id", "", "Chrome extension ID for -manifest chrome")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	switch {
	case *showVersion:
		fmt.Printf("dm-native-host %s\n", version)
		return
	case *manifest != "":
		text, err := manifestJSON(*manifest, *extensionID)
		if err != nil {
			fmt.Fprintln(os.Stderr, "dm-native-host:", err)
			os.Exit(2)
		}
		fmt.Println(text)
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Stdin, os.Stdout); err != nil && !errors.Is(err, io.EOF) {
		fmt.Fprintln(os.Stderr, "dm-native-host:", err)
		os.Exit(1)
	}
}

// run serves messages until the browser closes stdin.
func run(ctx context.Context, in io.Reader, out io.Writer) error {
	host := nativehost.New(version)
	reader := bufio.NewReader(in)

	for {
		raw, err := nativehost.ReadMessage(reader)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil
			}
			// A framing error cannot be answered with a frame, but the browser
			// gets a clear diagnostic and the process exits instead of looping.
			return err
		}

		var req nativehost.Request
		if err := json.Unmarshal(raw, &req); err != nil {
			if err := reply(out, nativehost.Response{Error: "The extension sent an invalid message."}); err != nil {
				return err
			}
			continue
		}
		if err := reply(out, host.Handle(ctx, req)); err != nil {
			return err
		}
	}
}

func reply(out io.Writer, resp nativehost.Response) error {
	body, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	return nativehost.WriteMessage(out, body)
}

// manifestJSON builds the per-browser manifest. Chrome allowlists extension
// origins; Firefox allowlists extension IDs. Both point at this executable.
func manifestJSON(browser, extensionID string) (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	self, err = filepath.Abs(self)
	if err != nil {
		return "", err
	}

	manifest := struct {
		Name              string   `json:"name"`
		Description       string   `json:"description"`
		Path              string   `json:"path"`
		Type              string   `json:"type"`
		AllowedOrigins    []string `json:"allowed_origins,omitempty"`
		AllowedExtensions []string `json:"allowed_extensions,omitempty"`
	}{
		Name:        hostName,
		Description: "DM download manager native host",
		Path:        self,
		Type:        "stdio",
	}

	switch browser {
	case "chrome", "chromium":
		if extensionID == "" {
			return "", errors.New("-manifest chrome needs -extension-id")
		}
		manifest.AllowedOrigins = []string{"chrome-extension://" + extensionID + "/"}
	case "firefox":
		manifest.AllowedExtensions = []string{"dm@vishwa.github.io"}
	default:
		return "", fmt.Errorf("unknown browser %q: use chrome or firefox", browser)
	}

	body, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	return string(body), nil
}
