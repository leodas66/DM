---
name: dm-impeccable
description: Run a systematic native quality review or design critique of a DM GTK4 desktop surface, adapted from Impeccable for GTK.
---

# DM Impeccable: native GTK adaptation

Use for a broad review of one GTK4 desktop surface. DM is an “Operate” product: task completion, scanability, consistency, native expectations, and real usage context outrank decorative expression.

Read docs/GTK4-DESIGN-SPEC.md first. This adaptation does not install Impeccable's CLI, browser automation, detector, or web commands. Those tools target web/native mobile workflows and are not a GTK quality gate.

## Review dimensions

Score each 0–4 and support the score with concrete evidence. If source or runtime evidence is unavailable, mark “not assessed” rather than guessing.

1. **Accessibility:** accessible names, keyboard reachability, focus order, Orca output, large text, high contrast, reduced motion.
2. **Runtime performance:** startup work, virtualized download list, GTK main-thread responsiveness, image/file metadata work, unnecessary redraws, goroutine-to-main-context updates.
3. **Appearance and theming:** active GTK theme, light/dark preference, system fonts, high contrast, absence of forced palette and broad CSS overrides.
4. **GTK/platform conformance:** standard GTK controls, native dialogs and menus, expected keyboard behavior, no web-shaped controls, no GNOME-only service dependency.
5. **Adaptivity:** useful layout at 1024×600 and narrower sizes, no clipped controls, navigation adapts while retaining essential actions.

## KDE Plasma host audit

Treat KDE Plasma as a first-class host, not an exception. On Arch + Plasma Wayland, check:
- configured GTK theme (Breeze GTK only if the user selected it);
- GTK light/dark and font settings;
- XDG Desktop Portal backend and file/folder chooser behavior;
- clipboard, dialog, focus, and window interactions;
- no dependency on GNOME Shell, GNOME services, or Qt libraries.

Do not assume that KDE's Qt theme settings automatically theme GTK. Do not change the user's desktop settings.

## Report format

- Surface and build/revision audited.
- Environment and GTK theme used.
- Five-dimension scorecard (or not-assessed labels).
- Critical findings first, each with source location, impact, and one actionable fix.
- Positive findings and next steps.
- Exact checks performed and checks not performed.

Do not claim visual verification without a rendered application and stated environment. Do not claim accessibility verification without actual keyboard/screen-reader checks. Do not use Impeccable's web detector or CSS rules to score GTK code.

## Upstream inspiration and limits

Inspired by pbakaus/impeccable's Operate mode and native audit rubric. The upstream native references primarily target iOS/Android; this file translates the useful review dimensions to GTK4 and KDE Plasma rather than treating those platform-specific rules as GTK requirements.
