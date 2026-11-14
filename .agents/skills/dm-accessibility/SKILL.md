---
name: dm-accessibility
description: Implement or audit accessibility in DM's GTK4 desktop client, including names, keyboard use, focus, dialogs, scaling, and status announcements.
---

# DM GTK accessibility

Use when adding or changing GTK controls, forms, dialogs, menus, keyboard shortcuts, focus behavior, or status feedback.

## Priority order

1. Accessible names and labels.
2. Keyboard reachability and visible focus.
3. Dialog focus, dismissal, and restoration.
4. State and progress announcements.
5. Text scaling, high contrast, and non-color status.
6. Pointer/touch target usability.

## GTK rules

- Prefer GTK's native widgets and semantics. Do not recreate keyboard behavior with custom drawing or event handlers unless required.
- Give every icon-only button an accessible name and a useful tooltip.
- Give every input a programmatic label; placeholder text is not a label.
- Ensure all actions are reachable and operable by keyboard. Preserve visible focus.
- Dialogs should establish sensible initial focus, support Escape when appropriate, and return focus to the invoking control when closed.
- Associate validation feedback with the relevant field. Do not rely on color or a transient toast alone.
- Expose meaningful download state and progress to assistive technology. Do not announce every tiny progress tick; choose useful update intervals.
- Status must not rely on color alone. Support high-contrast themes and large text without clipping essential information.
- Use GTK accessibility APIs/properties. Do not add HTML ARIA attributes to GTK code.
- Avoid custom controls when a standard GTK widget already provides the expected semantics.

## Verification

Use keyboard-only navigation and GTK Inspector accessibility tooling. Test with Orca on a supported Linux desktop. Check light, dark, high-contrast, and enlarged text settings. On KDE Plasma, verify that dialogs and portal-backed operations remain usable under Wayland.

## Upstream inspiration

Adapted from ibelick/ui-skills fixing-accessibility. HTML/ARIA examples are intentionally translated to GTK accessibility semantics.
