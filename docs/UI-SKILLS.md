# DM UI skills

This repository carries a small, project-local set of agent skills for the future GTK4 desktop client.

## Installation

Skills live under .agents/skills/ and are versioned with the project. No global agent configuration, npm dependency, CLI installation, browser automation, or desktop configuration is required. Agents that support the Agent Skills convention can discover each folder's SKILL.md.

## Skill set

| Skill | Use |
|---|---|
| dm-ui-router | Select the smallest relevant skill set for GTK UI work |
| dm-baseline-ui | Build or polish GTK4 screens using native controls |
| dm-improve-ui | Audit a surface and prepare evidence-based improvements |
| dm-accessibility | GTK accessible names, keyboard, focus, scaling, Orca |
| dm-motion | Purposeful motion and GTK rendering performance |
| dm-impeccable | Broad native quality audit adapted to GTK4 |

Start with dm-ui-router. Usually load only one skill; use no more than three for a task.

## Governing contract

docs/GTK4-DESIGN-SPEC.md is authoritative. In particular:
- GTK4 is the toolkit; gotk4 is the approved Go binding.
- DM's selected semantic color scheme is scoped to the app. The host GTK theme still owns native widget behavior, fonts, focus conventions and platform integration; DM never changes global GTK or desktop settings.
- KDE Plasma and Wayland are first-class test targets; GNOME remains supported.
- Keep the app-owned palette limited to the semantic tokens in the approved GTK4 design spec. Do not force a host GTK theme, change system settings, add a theme engine, or introduce Libadwaita or Qt.
- The daemon remains the sole owner of download state.

The skills do not authorize new dependencies, changes to project architecture, or work outside the requested phase. Follow AGENTS.md and PROCESS.md.

## Upstream provenance

These are original, DM-specific adaptations, not wholesale copies of upstream repositories.

- UI Skills: https://github.com/ibelick/ui-skills
  - Pinned reference revision: 65cc0edb4f64fe8bfe7e9c59466074de25bece76
  - Selected inspirations: ui-skills-root, baseline-ui, improve-ui, fixing-accessibility, fixing-motion-performance
  - Upstream license: MIT
- Impeccable: https://github.com/pbakaus/impeccable
  - Pinned reference revision: fb46cc6e356253a5d9945c4e3667c20c3d699cc8
  - Selected inspirations: Operate mode and native audit references
  - Upstream license: Apache-2.0

The upstream UI Skills web-specific instructions (Tailwind, React, ARIA, browser safe areas) and Impeccable web detector/commands are not treated as GTK implementation requirements. Impeccable's native references are iOS/Android-oriented; dm-impeccable is a separately authored GTK/KDE adaptation.

## Updating

When refreshing these skills, review upstream changes and re-check every adaptation against docs/GTK4-DESIGN-SPEC.md. Update the pinned revisions and this provenance document together. Do not run upstream installers blindly: they may install web tooling or change global agent configuration.
