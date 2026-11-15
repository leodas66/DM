# GTK4 Desktop Design Specification

Status: approved design baseline v2 — Full v3 workspace selected by the author (D040), superseding the native-theme-only baseline
Scope: `desktop/` GTK4 application only. This document does not define the daemon, CLI, browser extension, or the removed web/Wails interfaces (D035).

## 1. Product and platform contract

DM is a lightweight download manager. The desktop application is a native GTK4 client of the existing Go daemon, not a second download engine.

- The daemon remains the sole owner of download lifecycle, queue scheduling, workers, and persistence.
- The desktop client consumes the daemon's authenticated REST API and WebSocket events. It must not import Manager internals, open SQLite, or start a private Manager.
- Closing the desktop window must not stop or cancel daemon downloads.
- Use GTK4 widgets and the already-approved `gotk4` bindings. Keep the desktop Go module isolated under `desktop/`.
- Do not add Libadwaita or another UI toolkit as an implementation dependency without author approval. Prefer standard GTK4 widgets and platform behavior.
- The design must work on KDE Plasma as well as GNOME. Do not assume GNOME Shell, GNOME-only services, or a Libadwaita runtime is present.

## 2. Design direction: native GTK with the DM v3 visual system

DM remains a native GTK4 application. The approved v3 direction changes the visual system and workspace composition, not the widget toolkit or daemon boundary.

- Use standard GTK4 widgets, list models, menus, dialogs, focus behavior and accessibility semantics. Do not embed the HTML prototype or introduce a WebView, React, Electron, Libadwaita, Qt or another UI toolkit.
- Translate the v3 reference into GTK-native widget composition. The target is the visual hierarchy, information density, palette, and responsive behavior—not a literal port of browser DOM or CSS.
- DM owns its application palette: dark, light, dark high-contrast and light high-contrast variants. The user can choose Follow system, Dark or Light; Follow system is the default. The application palette does not modify global GTK or desktop settings.
- Centralize all colors in desktop/internal/ui/palette-*.css and all shared widget/layout rules in desktop/internal/ui/style.css. Widget Go files must not contain raw color values. Palette variants must define the same named colors.
- Use the system font stack; prefer Inter when installed, with Noto Sans, Cantarell and the system UI font as fallbacks. Do not bundle a font. Text scaling remains governed by GTK.
- Comfortable and Compact row densities are application preferences, saved locally by the desktop client and never sent to the daemon.
- Keep GTK's native focus, selection, hover, disabled and accessibility behavior. Styling changes appearance, never behavior.

### 2.1 KDE Plasma is a first-class host

- GTK4 remains the toolkit on KDE Plasma and GNOME. Do not add KDE/Qt libraries or assume GNOME Shell, GNOME-only services or Libadwaita.
- Preserve the host's windowing, font scaling, clipboard, file chooser and portal integration. The DM palette is an application-level skin, not a replacement desktop theme.
- Use GTK and XDG Desktop Portal APIs for file/folder selection and opening. Do not shell out to Dolphin or another file manager.
- Verify dialogs, clipboard, keyboard focus, file choosers and window behavior on KDE Wayland.

### 2.2 Token and appearance boundaries

- All four palettes define the same semantic tokens: background, window, sidebar, surface, raised surface, hover, separators, text, muted text, subtle text, accent, accent foreground, accent hover, success, warning, danger, selection and progress track.
- The dark palette follows the prototype's charcoal surfaces and warm amber accent. The light palette is a true light counterpart rather than an inverted dark palette.
- High-contrast variants increase text, border and focus contrast; state must never be communicated by color alone.
- The GTK theme may still provide platform behavior and fallback rendering, but it must not replace DM's authored semantic palette.


## 3. Main window structure

The desktop workspace follows the approved DM v3 composition.

1. A compact GTK header bar with the view title, search, appearance toggle, Preferences and the primary New download action.
2. A 232 px navigation sidebar with the DM brand/version, lifecycle filters, user categories and a network summary card.
3. A main content area with a page title and metadata, a bulk-action/sort toolbar, and the virtualized download list.
4. A download inspector at the right on wide windows. It is visible by default at widths of 1180 px and above; below that width it becomes an on-demand bottom sheet. It must not permanently steal list space on narrow windows.

The sidebar exposes All, Active/Downloading, Queued, Paused, Completed and Failed. User categories appear as a separate group when available. Canceled items remain discoverable in All/history.

The header search matches filenames, source URLs and destination paths. The toolbar offers Pause all, Resume all, Clear completed and sorting by date added, name, file size or progress. Clear completed removes history records only and never deletes downloaded files.

The list remains virtualized with GTK's list view. It is a table-like information surface, not a grid of oversized cards. The initial target window size is 1280×820. At narrower widths the sidebar collapses to a rail, then secondary table columns are progressively hidden while status, progress and actions remain accessible.

The inspector is persistent on wide windows and shows an empty prompt until a row is selected. On narrow windows, selecting a row opens it as a bottom sheet with a dismissible scrim. It reports daemon-backed values only: status, bytes, rate, ETA, source, destination and actual segment count.


## 4. Download table and row contract

Each row is a compact, scannable record with these columns at wide widths:

- Name: file-type icon, filename and source host/context.
- Progress: determinate bar and transferred bytes/percentage when known; an activity indicator for indeterminate work.
- Size: total size, or an explicit unknown value.
- Speed: current rate for active transfers.
- Left: ETA only when it can be calculated from daemon-backed size and rate.
- Actions: the primary state-appropriate action and a secondary menu.

The table header labels are Name, Progress, Size, Speed, Left and Actions. Column widths respond to the window width; hiding a narrow column must not remove its value from the inspector or accessible row description.

Actions are state-aware:
- Active: Pause and Cancel.
- Paused: Resume and Cancel.
- Queued: Cancel.
- Completed: history/removal actions and Open File; no transfer controls.
- Failed: Retry when the API supports it, otherwise the available recovery action.
- Canceled: history/removal actions.

Keep less-frequent actions in a row menu. Do not show unavailable actions as active controls. Rows are selectable; selection updates the inspector. Keyboard up/down moves the selection and keeps the selected row visible.

### Progress and parallel connections

For known sizes, show a determinate progress bar with readable progress. For unknown sizes, do not invent a percentage or ETA.

The selected-download inspector may show a segmented connection map and connection chips, but only from actual daemon segment records. Segment spans, transferred bytes and connection counts must be derived from model.Download.Segments; never simulate segment ownership, invent connection rates or fabricate progress. When segment detail is unavailable, say so plainly.

Use text and, where useful, a symbolic icon in addition to color for every state. Use tabular figures for byte counts, rates, percentages and ETA where the system font supports them.


## 5. Add Download flow

Add Download is the primary entry point and must be reachable from the header bar and a clear empty-state action.

The initial dialog is focused on one task:
- A labelled URL entry is the first control and receives initial keyboard focus.
- The destination folder and optional filename are available without overwhelming the primary URL flow.
- Validation errors appear next to the relevant field.
- The affirmative action is labelled **Add** or **Add Download**, not a generic **OK**.
- Cancel is available and Escape dismisses the dialog.
- Disable the affirmative action until the required URL is valid.

On successful submission, close the dialog and show the accepted download in the list. If the daemon rejects the request, keep the dialog state where practical and show the error next to the relevant input or action.

Do not put browser-captured cookies, authorization headers, or other secrets into visible fields, logs, or persisted UI state.

## 6. Empty, loading, offline, and error states

- Empty collection: explain that there are no downloads yet and provide one clear **Add Download** action.
- Empty filter/category: explain that no downloads match the current view; do not show a second Add action if the global action is already visible.
- Initial loading: use a small, local loading indication near the content being loaded. Avoid a full-window blocking spinner.
- Daemon unavailable: show a clear connection state and a retry/reconnect action. Do not imply that closing or losing the UI stops existing daemon work.
- Per-download failure: show a concise human-readable reason in the row or its details, with a relevant recovery action.
- Form errors: place each message beside the field or action that caused it.

## 7. Preferences

Preferences contain two distinct groups.

### Appearance (desktop-local)

- Appearance: Follow system, Dark or Light. Follow system is the default.
- List density: Comfortable or Compact.
- Changes apply immediately and are saved in the desktop client's local desktop.json preferences file under the user's DM configuration directory. These values are never sent to the daemon.
- If the local preference file is missing, use defaults. If it is malformed, keep defaults and report a recoverable error; writes must be atomic and use owner-only file permissions.

### Downloads (daemon settings)

- Default download directory.
- Maximum concurrent downloads.
- Per-download connection/segment limit, where exposed by the daemon.
- Download speed limits, where exposed by the daemon.

Explain settings that affect future downloads separately from settings that affect active transfers. Do not invent settings that the daemon API does not support. Appearance remains usable when the daemon is offline; daemon settings remain disabled or report that the daemon is unavailable.


## 8. Destructive and lifecycle interactions

Pause, Resume, and Cancel are distinct actions and must map to the corresponding daemon operations. Cancel must not be presented as Remove.

Removing an item from history and deleting its downloaded file are different operations. State clearly which one is being performed. Deleting a file must use a confirmation step or a clear undo opportunity; the confirmation must name the file and destination. Prefer reversible actions when possible.

Closing the application window only closes the client. It must not silently quit the daemon or cancel active downloads.

## 9. Accessibility and input

- Every icon-only control needs an explicit accessible name and tooltip.
- Every input has a programmatic label; placeholder text is not a label.
- All actions are usable with keyboard navigation and visible focus.
- Support large text and high-contrast appearances without clipping or hiding essential information.
- Do not communicate status through color alone. The dark, light and high-contrast palettes must meet contrast requirements for text and controls.
- Keep hit targets and row actions usable with pointer, touch and keyboard input.
- The selected row and inspector context must be announced meaningfully to screen readers. Test with Orca and GTK Inspector's accessibility tools.
- Space pauses/resumes only when the selected download currently exposes that action; Delete/Backspace must follow the same removal confirmation rules as pointer actions.


## 10. Motion and visual effects

Use short transitions only when they communicate a real state change. Progress movement may be eased but must remain visually tied to daemon updates. The inspector bottom-sheet transition should be no longer than 200 ms. Respect GTK's reduced-motion preference.

Do not add decorative blur, glow or large backdrop effects. Avoid animating layout dimensions or delaying a user action.


## 11. UI Skills application rules

Use the smallest relevant UI skill set. Apply native GTK equivalents of baseline UI, accessibility and motion guidance.

- Keep widget behavior, data and accessibility native GTK; the HTML prototype is a visual/interaction reference, not runtime code.
- Keep appearance centralized in the semantic token palettes and shared stylesheet. Do not hard-code color values in widget source.
- Keep one clear primary action per view; keep secondary actions visually quiet.
- Give empty states one clear next action and place validation errors beside their field or action.
- Confirm destructive operations or offer undo. Keep Cancel distinct from Remove.
- Keep list virtualization and the daemon as the only owner of download state.


## 12. Implementation review checklist

Before calling the v3 UI complete, verify:

- The workspace follows the v3 hierarchy at 1280×820 and remains usable at 1180, 1024, 900 and 700 px.
- The inspector is a right pane at 1180 px and above, and a bottom sheet below that breakpoint.
- Dark, light and both high-contrast palettes render without undefined tokens or CSS parsing warnings.
- Text and controls meet contrast requirements in each palette; state remains understandable without color.
- Comfortable and Compact density preferences survive restart and do not alter daemon settings.
- The list remains virtualized and all displayed progress, speed, ETA and segment information comes from daemon data.
- Search, filters, sort, bulk actions, add, pause, resume, cancel, retry, remove, delete and open-file actions work with mouse and keyboard.
- Empty, loading, offline and per-download failure states remain understandable.
- Keyboard navigation, focus, Orca announcements, large text and reduced motion are verified.
- KDE Plasma/Wayland file chooser, clipboard and window behavior work without GNOME services.
- The client does not require Qt, GNOME Shell, a WebView or a second download engine.


## 13. Governing references

- DM v3 visual reference: the author's dm-flow-prototype-v3.html design prototype (shared in the project Library; illustrative only, not shipped runtime code).

- Repository architecture: `docs/ARCHITECTURE.md`.
- Repository workflow and verification policy: `AGENTS.md` and `PROCESS.md`.
- Product feature scope and API outline: `PLAN.md`.
- GNOME Human Interface Guidelines: https://developer.gnome.org/hig/
- GNOME design principles: https://developer.gnome.org/hig/principles
- GTK styling and light/dark behavior: https://developer.gnome.org/hig/guidelines/ui-styling.html
- Typography: https://developer.gnome.org/hig/guidelines/typography.html
- Adaptive layouts: https://developer.gnome.org/hig/guidelines/adaptive.html
- Header bars: https://developer.gnome.org/hig/patterns/containers/header-bars.html
- Sidebars: https://developer.gnome.org/hig/patterns/nav/sidebars.html
- List and column views: https://developer.gnome.org/hig/patterns/containers/list-column-views.html
- Progress bars: https://developer.gnome.org/hig/patterns/feedback/progress-bars.html
- Dialogs: https://developer.gnome.org/hig/patterns/feedback/dialogs.html
- GTK accessibility: https://gnome.pages.gitlab.gnome.org/gtk/gtk4/section-accessibility.html
- XDG Desktop Portal: https://flatpak.github.io/xdg-desktop-portal/

This specification defines the design baseline. It does not claim that a GTK screen has been implemented or visually audited.
