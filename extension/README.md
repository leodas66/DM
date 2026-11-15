# dm browser extension

A Manifest V3 WebExtension (Chrome and Firefox) that routes browser downloads
through a Native Messaging host into the native GTK desktop app. The app shows
a review dialog before the request is submitted to the local [dm](../) daemon.
The extension holds no port and no bearer token.

## What it does

- Intercepts `downloads.onCreated` and opens the DM desktop **Add Download**
  review dialog with the URL, source page, filename, size/type and browser
  request context.
- The browser's own copy is cancelled only after the desktop confirms that the
  review dialog is visible. If the daemon, native host or desktop app is
  unavailable, the browser download continues normally.
- The user chooses the destination folder and confirms **Add Download** in DM.
  Closing the review dialog cancels the captured download without starting it.
- Context menu **Download with DM** opens the same review dialog for any link.
- Popup to paste a URL and open it in the DM review dialog, and check daemon
  connectivity.
- Options page: enable capture, minimum size, host allow/deny lists.
- Shows `!` on the toolbar button for a few seconds when a capture fails, with
  the reason in the button tooltip.

## How it talks to the daemon

```
extension  --Native Messaging-->  dm-native-host  --HTTP + token-->  dm daemon
```

The host name is `io.github.vishwa.dm`. Manifests are per browser because Chrome
allowlists extension origins while Firefox allowlists extension IDs.

## Build

Requires Node.js and npm. esbuild and TypeScript are dev dependencies.

```sh
cd extension
npm install        # once
npm run build      # builds dist/chrome and dist/firefox
npm test           # unit tests for capture rules and host messaging
npm run lint       # tsc --noEmit
```

`dist/` is gitignored — it is a build artifact. Each target directory is
self-contained, so there is no shared `dist` root to load by mistake.

## Install the native host

**Arch Linux package installs already include the native host and Firefox
manifest.** Install/upgrade DM with the repository PKGBUILD, then start the
daemon:

```sh
systemctl --user enable --now dm.service
```

For Chrome/Chromium, load the extension first, copy its ID from
`chrome://extensions`, then register the per-user manifest:

```sh
# Chrome
mkdir -p ~/.config/google-chrome/NativeMessagingHosts
dm-native-host -manifest chrome -extension-id <your-extension-id> \
  > ~/.config/google-chrome/NativeMessagingHosts/io.github.vishwa.dm.json

# Chromium (use this directory instead)
mkdir -p ~/.config/chromium/NativeMessagingHosts
dm-native-host -manifest chromium -extension-id <your-extension-id> \
  > ~/.config/chromium/NativeMessagingHosts/io.github.vishwa.dm.json
```

The native host starts `dm-desktop` when needed and hands the request to the
running app over a 0600 Unix socket in the user's DM runtime directory. URLs,
cookies and headers are not put in command-line arguments or temporary files.

## Load unpacked

### Chrome

1. Build the extension: `cd extension && npm run build`.
2. Open `chrome://extensions` and enable **Developer mode**.
3. Click **Load unpacked** and select `extension/dist/chrome`.
4. Copy the extension ID and install the host manifest as above.
5. Reload the extension after writing the manifest.

### Firefox

1. Build the extension: `cd extension && npm run build`.
2. Open `about:debugging#/runtime/this-firefox`.
3. Click **Load Temporary Add-on…** and select
   `extension/dist/firefox/manifest.json`. Do not pick the `dist/chrome`
   manifest: it declares a service worker, which Firefox rejects with
   `background.service_worker is currently disabled`.
4. Install the host manifest as above.

> Temporary add-ons are cleared when Firefox restarts. Firefox identifies this
> extension as `dm@vishwa.github.io`; the host manifest allowlists that ID, so no
> extra configuration is needed. For a permanent install, package the
> `dist/firefox` directory as a `.xpi` and load it through `about:addons`.

## Settings

- **Capture browser downloads with DM** — turn interception off without
  removing the extension.
- **Minimum size** — downloads smaller than this many bytes are left to the
  browser. `0` captures everything. An unknown size is never filtered out.
- **Allow hosts** — only these hosts are captured. Empty allows all.
- **Deny hosts** — these hosts are never captured. Deny wins over allow.
- **Test connection** — asks the native host to check the daemon endpoint and
  token file. It does not add a download.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| Firefox: `background.service_worker is currently disabled. Add background.scripts.` | `dist/chrome/manifest.json` was selected; only the Firefox target uses event pages | Select `extension/dist/firefox/manifest.json` |
| Tooltip says the native host is not installed | No manifest in the browser's NativeMessagingHosts directory | Install the manifest for your browser |
| Tooltip says the daemon is not running | `$XDG_RUNTIME_DIR/dm/endpoint` is missing or stale | `systemctl --user start dm` |
| Tooltip says the token is missing | The daemon has never run | Start the daemon once; it writes `~/.config/dm/token` |
| Daemon rejects the download | The URL is not `http(s)`, or the header/cookie set is invalid | Check the daemon log |

## Files

- `manifest.json` — MV3 base manifest; `build.mjs` derives the browser variants.
- `src/background.ts` — interception, context menu, badge errors, popup/options
  messages.
- `src/popup.ts` + `popup.html` — paste-a-URL dialog and host check.
- `src/options.ts` + `options.html` — capture settings.
- `src/types.ts` — capture rules, payload building, Native Messaging client.
- `src/storage.ts` — `storage.local` wrapper for settings.
- `src/types.test.ts` — unit tests (run with `node --test`).
- `build.mjs` — esbuild bundler and per-browser manifest generation.

## Security notes

- The extension cannot reach the daemon directly: it has no token. It uses the
  review-only `open` and health-check `ping` commands; the host retains a legacy
  direct-add command for trusted local clients. The user confirms in the desktop
  app before the extension's browser-captured request is submitted.
- The browser request is passed to the desktop over a per-user Unix socket with
  mode 0600 in a 0700 runtime directory. The socket is removed on clean exit.
- The host refuses anything but `http://` and `https://` URLs, so the browser
  cannot make the daemon read a local file, and it drops header names or values
  containing CR/LF before forwarding.
- Cookies, headers, and the referrer are collected at capture time and forwarded
  in the request body; the daemon keeps them in memory only (`json:"-"`), and
  the extension does not persist them.
- `host_permissions: <all_urls>` is needed to read the cookies and referrer of
  whatever site starts the download. Nothing is sent anywhere except the local
  daemon.
