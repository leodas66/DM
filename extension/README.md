# dm browser extension

A Manifest V3 WebExtension (Chrome + Firefox) that sends downloads to the local
[dm](..) daemon.

## What it does

- Intercepts `downloads.onCreated` and POSTs the URL, referrer, cookies, and
  headers to `POST /api/downloads` on the local daemon.
- Cancels the browser's own download **only after** the daemon returns 2xx
  (accept-first; the browser download proceeds if the daemon is unreachable).
- Context menu "Download with DM" on any link.
- Popup to paste a URL and add it to the daemon.
- Options page: daemon port, bearer token, minimum capture size, host allow /
  deny lists.
- Never captures requests the daemon itself makes (avoids a loop).

## Build

Requires Node.js and npm. esbuild and TypeScript are dev dependencies.

```sh
cd extension
npm install        # once
npm run build      # bundles to dist/
npm test           # unit tests for the API client logic
npm run lint       # tsc --noEmit
```

`dist/` is gitignored — it is a build artifact.

## Load unpacked

### Chrome

1. Build the extension: `cd extension && npm run build`.
2. Open `chrome://extensions`.
3. Enable **Developer mode** (top-right toggle).
4. Click **Load unpacked** and select the `extension/dist` folder.
5. Open the extension's **Details** and note its ID.
6. Open the **Options** page to set the daemon port and bearer token.

### Firefox

1. Build the extension: `cd extension && npm run build`.
2. Open `about:debugging#/runtime/this-firefox`.
3. Click **Load Temporary Add-on…** and select `extension/dist/manifest.json`.
4. Open the **Options** page to set the daemon port and bearer token.

> Firefox temporary add-ons are cleared when the browser restarts. For a
> permanent install, package `dist/` into a `.xpi` (zip with `.xpi` extension)
> and load it via `about:addons`.

## Settings

Get the bearer token from the daemon's config file:

```sh
cat ~/.config/dm/token
```

Enter it in the extension's Options page along with the daemon port (the port
dm prints on startup, or the one you configured). The extension stores port and
token in Chrome/Firefox sync storage and sends `Authorization: Bearer <token>`
on every daemon request.

## Capture filters

- **Minimum size**: downloads smaller than this (in bytes) are left to the
  browser. Set `0` to capture everything.
- **Allow hosts**: only listed hosts are captured. Empty = allow all.
- **Deny hosts**: listed hosts are never captured. Empty = deny none.
- The daemon's own URL is never captured (prevents a capture loop).

## Files

- `manifest.json` — MV3 manifest, permissions, service worker entry.
- `src/background.ts` — service worker: intercepts downloads, context menu,
  forwards to the daemon.
- `src/popup.ts` + `popup.html` — paste-a-URL dialog.
- `src/options.ts` + `options.html` — settings page.
- `src/types.ts` — daemon client, capture filters, request building.
- `src/storage.ts` — chrome.storage.local wrapper for settings.
- `src/api.test.ts` — unit tests (runs with `node --test`).
- `build.mjs` — esbuild bundler config.

## Security notes

- The daemon binds to `127.0.0.1` only and requires a bearer token; the
  extension sends `Authorization: Bearer <token>` and an `Origin` header on
  every request.
- The extension declares `<all_urls>` host permission so it can POST to the
  (configurable) localhost daemon. It only actually posts to the configured
  daemon origin.
- Cookies, headers and referrer are read from the browser at capture time and
  forwarded to the daemon; they are not persisted by the extension.
