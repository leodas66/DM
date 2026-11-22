---
name: dm-ui-router
description: Route UI tasks for DM to the smallest relevant native GTK design skill. Use before changing or reviewing the GTK4 desktop client.
---

# DM UI skill router

Use this router only for the GTK4 desktop client under desktop/. The approved contract is docs/GTK4-DESIGN-SPEC.md.

## Authority order

1. Product and architecture constraints in AGENTS.md and PLAN.md.
2. The approved GTK4 design specification.
3. These skills and their upstream inspirations.
4. Individual implementation preference.

If a skill conflicts with the GTK4 spec, the spec wins. Do not import web UI assumptions into GTK.

## Route

- New layout, widget composition, visual cleanup: load dm-baseline-ui.
- Review an existing GTK screen and prepare a grounded improvement plan: load dm-improve-ui.
- Controls, forms, keyboard, screen reader, focus, labels: load dm-accessibility.
- Animation, transitions, rendering cost: load dm-motion.
- Broad native quality audit: load dm-impeccable.

Use at most three skills for one task. Usually one is enough. Do not load any UI skill for daemon, API, storage, CLI, or extension-only work.

## Before editing

Read the GTK4 spec and relevant existing implementation. Preserve the daemon/client boundary. Do not introduce dependencies, Libadwaita, Qt, a theme engine, or a new UI toolkit without author approval. Follow PROCESS.md: one phase, tests first, make check for code phases, and report actual evidence.

## Platform target

GTK4 is the toolkit. KDE Plasma (including Wayland on Arch Linux) and GNOME are supported hosts. Use the app-scoped semantic palette selected in DM preferences; keep native widget behavior, fonts and platform integration from the host GTK theme. Read system appearance only for Follow system, and never modify global GTK or desktop settings or force Breeze. Test desktop portal/file chooser behavior on KDE. Do not require GNOME Shell or GNOME-only services.
