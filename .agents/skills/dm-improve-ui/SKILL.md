---
name: dm-improve-ui
description: Audit one DM GTK4 desktop surface against its approved design contract and prepare a grounded, actionable improvement plan.
---

# DM improve UI

Use for review, refinement planning, or design-system drift investigation. This skill is read-only on product source unless the user explicitly asks to implement selected findings.

## Scope and evidence

1. Select one coherent GTK desktop surface, such as the main download list or Add Download dialog.
2. Read docs/GTK4-DESIGN-SPEC.md and trace the actual widget construction and state path.
3. Inspect the GTK widgets, properties, CSS, theme assumptions, accessibility metadata, and layout branches that reach the selected surface.
4. Separate a documented contract violation from a subjective preference. Do not report visual hierarchy, density, discoverability, or perceived quality as a proven defect without rendered evidence or explicit user feedback.
5. For each finding, state the exact source location, governing rule, runtime path, user impact, and one deterministic correction.
6. Try to falsify every finding. Remove unsupported, duplicate, or ambiguous findings.

Do not infer that a file governs a screen from its name or repository proximity. Do not invent a new design system or token set.

## GTK-specific checks

- Standard GTK controls are preferred over hand-built imitations.
- System theme, light/dark preference, font scaling, and high-contrast settings must remain authoritative.
- GTK CSS overrides must be narrowly scoped and justified by a demonstrated GTK limitation.
- Check keyboard navigation, visible focus, accessible names, labels, and screen-reader announcements.
- Check list virtualization and avoid per-row control clutter.
- Check responsive behavior as the window narrows.
- Check that daemon connection failures and download states are represented without misleading progress.
- On KDE Plasma Wayland, check portal/file chooser behavior and avoid GNOME-only assumptions.

## Output

Return:
- Audited surface and design sources.
- Up to three verified findings with evidence, impact, and one correction each.
- Positive patterns worth preserving.
- The highest-leverage next change, or “No supported recommendation.”

If the user asks for a plan, make it self-contained and identify exact files/widgets. Do not change product code unless implementation is explicitly requested. Follow PROCESS.md for any code changes.

## Upstream inspiration

Adapted from ibelick/ui-skills improve-ui. Its web route/component/CSS tracing is replaced by GTK widget construction, properties, and runtime state tracing.
