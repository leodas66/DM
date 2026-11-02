# GTK4 Desktop Design Specification

Status: approved design baseline for the GTK4 client
Scope: `desktop/` GTK4 application only. This document does not define the daemon, CLI, browser extension, or the transitional Svelte/Wails interfaces.

## 1. Product and platform contract

DM is a lightweight download manager. The desktop application is a native GTK4 client of the existing Go daemon, not a second download engine.

- The daemon remains the sole owner of download lifecycle, queue scheduling, workers, and persistence.
- The desktop client consumes the daemon's authenticated REST API and WebSocket events. It must not import Manager internals, open SQLite, or start a private Manager.
- Closing the desktop window must not stop or cancel daemon downloads.
- Use GTK4 widgets and the already-approved `gotk4` bindings. Keep the desktop Go module isolated under `desktop/`.
- Do not add Libadwaita or another UI toolkit as an implementation dependency without author approval. Prefer standard GTK4 widgets and platform behavior.
- The design must work on KDE Plasma as well as GNOME. Do not assume GNOME Shell, GNOME-only services, or a Libadwaita runtime is present.

## 2. Design direction

The interface is native, calm, practical, and information-led. The download list is the primary surface; controls should be easy to discover without turning every row into a toolbar.

Use the active GTK theme and system color-scheme preference. Support light and dark appearances without maintaining a separate hard-coded palette. Do not force a specific desktop theme or accent color.

Use GTK's system typography and text styles. Respect user font scaling; avoid fixed pixel font sizes, all-caps labels, and unnecessary font variants.

Use native widget spacing, alignment, focus, selection, and state styling as the baseline. Custom CSS is exceptional and must solve a demonstrated GTK limitation rather than restyle the entire application.

## 3. Main window structure

The primary window has three functional regions:

1. A GTK header bar with the current view title, the primary **Add Download** action, and a small app menu for secondary actions such as Preferences and About.
2. A navigation area for download filters and user categories.
3. A main content area containing the download list for the selected filter or category.

Keep the header bar sparse. The Add Download action is the primary action; secondary actions belong in the app menu or the relevant row menu. Preserve clear space in the header bar for window dragging.

The navigation area exposes the useful lifecycle filters: All, Active, Queued, Paused, Completed, and Failed. Canceled items remain discoverable in All/history. User categories appear as a separate group when categories are available. Do not add a navigation destination for every individual download.

The main area uses a virtualized GTK list view for the dynamic download collection. Do not use a grid of oversized cards for ordinary downloads.

The first layout target is a desktop window at 1024×600. As width decreases, collapse or replace the sidebar with a compact native navigation control; keep the download list usable and retain all actions. Avoid permanently visible multi-pane inspectors.

## 4. Download list and row contract

Each download row presents the information needed to identify and monitor a file:

- Filename as the primary label.
- Source host or URL as secondary context, shortened safely when necessary.
- A progress indicator for active transfers.
- Transferred bytes and total size when known.
- Current status, transfer speed, and estimated time remaining when applicable.
- Row actions appropriate to the current state.

Keep the row visually regular and scannable. Use a compact two-line hierarchy rather than a collection of unrelated badges and buttons. Use symbolic GTK icons where an icon improves recognition.

Actions are state-aware:
- Active: Pause and Cancel.
- Paused: Resume and Cancel.
- Queued: Cancel.
- Completed: history/removal actions; do not show transfer controls.
- Failed: Retry when the API supports it, otherwise show the available recovery action.
- Canceled: history/removal actions.

Do not show unavailable actions as active controls. Put less-frequent actions in a row menu rather than permanently displaying multiple icon buttons.

### Progress and status

For known file sizes, show a determinate progress bar with a readable percentage or byte count. Keep speed and ETA adjacent to the download they describe.

For unknown-size or indeterminate work, do not invent a percentage or ETA. Use GTK activity indication and a concise status label instead.

Use text and, where useful, a symbolic icon in addition to color for every state. Status must remain understandable in high-contrast mode and for users who cannot distinguish colors.

Use tabular figures for byte counts, rates, percentages, and ETA wherever the selected system font supports them. Format values consistently across rows.

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

Preferences are a secondary surface, not part of the main download list. Group related controls and keep labels and inputs consistently aligned.

The initial preferences scope follows existing daemon settings:
- Default download directory.
- Maximum concurrent downloads.
- Per-download connection/segment limit, where exposed by the daemon.
- Download speed limit, where exposed by the daemon.

Do not invent settings that the daemon API does not support. Explain settings that affect future downloads separately from settings that affect active transfers.

## 8. Destructive and lifecycle interactions

Pause, Resume, and Cancel are distinct actions and must map to the corresponding daemon operations. Cancel must not be presented as Remove.

Removing an item from history and deleting its downloaded file are different operations. State clearly which one is being performed. Deleting a file must use a confirmation step or a clear undo opportunity; the confirmation must name the file and destination. Prefer reversible actions when possible.

Closing the application window only closes the client. It must not silently quit the daemon or cancel active downloads.

## 9. Accessibility and input

- Prefer standard GTK controls because they provide platform keyboard and accessibility behavior.
- Every icon-only control needs an explicit accessible name and a tooltip.
- Every input must have a programmatic label; placeholder text is not a label.
- All actions must be usable with keyboard navigation. Focus must remain visible.
- Support large text and high-contrast themes without clipping or hiding essential information.
- Do not communicate status through color alone.
- Keep hit targets and row actions usable with pointer, touch, and keyboard input.
- Test the application with Orca and GTK Inspector's accessibility tools.

## 10. Motion and visual effects

Do not add animation unless it communicates a real state transition or the user explicitly requests it. Keep interaction feedback short and avoid motion that delays an action.

Do not use gradients, glow effects, decorative blur, or large backdrop effects. Do not animate layout dimensions or position. Respect reduced-motion preferences where animation is present.

## 11. UI Skills application rules

The UI Skills router is the selection layer for this UI phase. Use the smallest relevant skill set; do not load unrelated web/metadata skills.

Apply the useful principles from `baseline-ui` with GTK-native equivalents:
- Use existing GTK widgets and accessible behavior instead of introducing React, Tailwind, Base UI, Radix, or hand-built web primitives.
- Use system theme/style tokens instead of hard-coded colors.
- Keep one clear primary action per view and keep secondary actions visually quiet.
- Give empty states one clear next action.
- Show validation errors beside the relevant field or action.
- Confirm destructive operations or offer undo.
- Avoid unnecessary animation, gradients, blur, glow, and excessive accent colors.
- Use tabular figures for dense numeric information where supported.

The skill's Tailwind classes, React animation libraries, ARIA-specific instructions, and browser safe-area rules do not apply literally to this GTK4 application. Use GTK accessibility properties, native focus handling, GTK style classes, and platform conventions instead.

## 12. Implementation review checklist

Before calling the GTK UI phase complete, verify:

- The window and download list remain usable at 1024×600 and at narrower widths.
- Light, dark, and high-contrast system appearances work without hard-coded foreground/background colors.
- Large text does not clip filenames, controls, status, or dialogs.
- Keyboard-only use covers navigation, adding, pausing, resuming, canceling, and removing.
- Orca announces controls and download status meaningfully.
- Progress is accurate for known sizes and never fabricates a percentage for unknown sizes.
- Empty, loading, daemon-disconnected, failed, paused, and completed states are represented.
- Destructive file deletion is distinguishable from removing a history entry.
- Closing the client leaves daemon downloads running.
- GTK4 remains isolated in `desktop/`; daemon packages remain independent of GTK/cgo.

## 13. Governing references

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

This specification defines the design baseline. It does not claim that a GTK screen has been implemented or visually audited.
