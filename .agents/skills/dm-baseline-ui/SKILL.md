---
name: dm-baseline-ui
description: Design or polish DM's GTK4 desktop UI using standard GTK widgets and the approved native GTK design specification.
---

# DM baseline UI

Use for GTK4 desktop UI implementation or a focused visual cleanup.

## Non-negotiable direction

DM is a native GTK application. Read docs/GTK4-DESIGN-SPEC.md before changing UI. Use standard GTK4 widgets and the approved gotk4 bindings. Do not build a web-style design system inside GTK.

- Prefer standard GTK controls, layout containers, dialogs, menus, list views, and native focus/selection behavior.
- Use DM's user-selected semantic color scheme and optional accent override only inside the app's scoped CSS provider. The host GTK theme continues to own native widget behavior, system fonts, text scaling, and accessibility settings.
- Do not modify global GTK or desktop settings, force a host theme, add a theme engine, or replace native widgets. Keep palette values in semantic token files and shared layout rules in style.css.
- Avoid Libadwaita APIs unless separately approved. GNOME HIG is guidance, not a runtime dependency.
- Never imitate Qt or require Qt libraries. GTK is expected to run on KDE Plasma too.
- Use symbolic GTK icons where appropriate. Give icon-only controls accessible names and tooltips.
- Keep the primary action obvious; keep secondary actions in menus or context-appropriate locations.
- Use clear hierarchy and compact, scannable rows. Downloads belong in a virtualized list, not oversized cards.
- Show empty states with one useful next action. Put validation errors beside the relevant control.
- Confirm destructive file deletion or provide a clear undo path. Distinguish deleting a file from removing history.
- Use tabular figures for numeric transfer data only when supported by the system font.

## CSS exception policy

DM's scoped semantic palette CSS is an approved part of the v3 visual system. Keep color overrides in palette token files and shared component rules in style.css; widget Go files must not hard-code colors. Beyond that contract, custom GTK CSS remains exceptional: prefer standard widget properties and native controls, and never change global GTK settings or broadly reset platform behavior.

## KDE and Wayland

On KDE Plasma, respect the GTK theme the user configured (Breeze GTK only if they selected it). Do not assume the Qt color scheme automatically configures GTK. Do not change desktop settings. Use GTK/portal APIs for file and folder selection; do not shell out to Dolphin. Verify file chooser, clipboard, dialog, focus, and window behavior under Plasma Wayland without GNOME services.

## Review checklist

- Does the change follow docs/GTK4-DESIGN-SPEC.md?
- Does it use standard GTK controls and preserve keyboard/focus behavior?
- Does it inherit theme, colors, fonts, and accessibility preferences?
- Is any custom CSS narrowly justified?
- Does it work at 1024×600 and narrower widths?
- Are loading, empty, error, offline, and lifecycle states understandable?
- Does it preserve the daemon as the sole owner of download state?

## Upstream inspiration

Adapted from ibelick/ui-skills baseline-ui. Its Tailwind, React, Base UI, Radix, ARIA-in-HTML, browser safe-area, and web animation prescriptions are not GTK implementation requirements.
