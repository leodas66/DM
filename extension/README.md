# dm browser extension

A Manifest V3 WebExtension (Chrome and Firefox) that sends browser downloads to
the local [dm](../) daemon through a Native Messaging host. The extension holds
no port and no bearer token: the host finds the daemon through its runtime
endpoint file and reads the token itself.

## What it does

- Intercepts `downloads.onCreated` and asks the daemon to take the download,
  carrying the URL, referrer, headers, and the cookies the browser would send.
- Cancels the browser's own copy **only after** the daemon accepts it; if the
  daemon or host is unavailable the browser download proceeds normally.
- Context menu **Download with DM** on any link.
- Popup to paste a URL and check whether the daemon is reachable.
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

Build and install the host from the repository root:

```sh
make build
./bin/dm-native-host -version
```

Register it with the browser you use. Chrome needs the extension's ID, which is
shown on `chrome://extensions` after you load the extension (step 5 below).

```sh
# Chrome / Chromium
./bin/dm-native-host -manifest chrome -extension-id <your-extension-id> \
  > ~/.config/google-chrome/NativeMessagingHosts/io.github.vishwa.dm.json
# Chromium uses ~/.config/chromium/NativeMessagingHosts/ instead.

# Firefox
./bin/dm-native-host -manifest firefox \
  > ~/.mozilla/native-messaging-hosts/io.github.vishwa.dm.json
```

Create the directory first if it does not exist. The manifest records the
absolute path of `dm-native-host`, so keep the binary where you built it (or
re-run the command after moving it). A packaged install puts the host in
`/usr/lib/dm/` and writes the manifest from the package.

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
   `extension/dist/firefox/manifest.json`.
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

- The extension cannot reach the daemon directly: it has no token and the host
  only implements `add` and `ping`. It cannot read, pause, or delete downloads.
- The host refuses anything but `http://` and `https://` URLs, so the browser
  cannot make the daemon read a local file, and it drops header names or values
  containing CR/LF before forwarding.
- Cookies, headers, and the referrer are collected at capture time and forwarded
  in the request body; the daemon keeps them in memory only (`json:"-"`), and
  the extension does not persist them.
- `host_permissions: <all_urls>` is needed to read the cookies and referrer of
  whatever site starts the download. Nothing is sent anywhere except the local
  daemon.
