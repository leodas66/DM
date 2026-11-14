---
name: dm-motion
description: Review or implement purposeful, low-cost motion in DM's GTK4 desktop client.
---

# DM motion and rendering

Default to no animation. Motion is allowed only when it communicates a meaningful state transition or materially improves feedback.

- Prefer GTK's built-in transitions and feedback over custom animation systems.
- Keep transitions short and non-blocking. Never delay an action for an animation.
- Do not animate layout dimensions, row positions, or large surfaces for decoration.
- Avoid blur, backdrop effects, gradients, glow, and continuously running decorative motion.
- Respect reduced-motion and accessibility preferences. Provide a non-motion equivalent.
- Avoid repeated animation for frequently updating download progress. Progress should remain accurate and calm.
- Do not perform expensive work on the GTK main thread. Keep UI updates bounded and coalesce high-frequency progress events when appropriate.
- Measure before optimizing. Do not add caching, timers, or complexity without evidence.
- Keep GTK object access on the appropriate main context; do not update widgets directly from worker goroutines.

## Review

For each animation, explain what state change it communicates, whether GTK already provides the behavior, how it behaves with reduced motion, and what it costs during a long download list. Remove motion that has no user benefit.

## Upstream inspiration

Adapted from ibelick/ui-skills fixing-motion-performance. Browser compositor/CSS animation rules do not map literally to GTK; prioritize GTK frame cost and main-context discipline.
