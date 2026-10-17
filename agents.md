# REX — Agent Instructions

## 1. Project Overview

REX is a terminal-first application with a distinctive, polished CLI interface. Its exact purpose and feature set are still evolving.

The goal is to establish a strong terminal experience and a flexible technical foundation that can support future functionality.

Do not assume REX is exclusively an AI coding assistant, monitoring tool, or agent wrapper. Keep the architecture flexible until the product direction is finalized.

## 2. Core Design Principles

- **Terminal-native:** REX should feel like a thoughtfully designed CLI tool, not a web application squeezed into a terminal.
- **Visually distinctive:** Develop a recognizable visual identity using typography, spacing, Unicode characters, borders, symbols, and terminal colors.
- **Mascot-driven:** REX features a cute, expressive dinosaur mascot that can communicate different states through poses, expressions, and animations.
- **Information-dense but readable:** Use visual hierarchy and deliberate spacing. Avoid clutter, excessive borders, and unnecessary decoration.
- **Responsive:** The interface should remain usable across different terminal widths and heights.
- **Interactive:** Where appropriate, support keyboard navigation, clear prompts, progress indicators, and contextual help.
- **Extensible:** New commands, screens, animations, and features should fit naturally into the existing interface.

## 3. Visual References

The user will supply reference images showing the desired appearance of REX.

Treat supplied images as the primary source of truth for visual design.

When reference images are available:

- Study the layout, spacing, proportions, colors, typography, symbols, and overall aesthetic before implementing the interface.
- Reproduce the relevant visual characteristics as faithfully as practical in a real terminal.
- Translate visual elements into terminal-compatible text, Unicode art, ANSI styling, and interactive terminal components.
- Preserve the intended composition within the limitations of terminal character cells.
- If an image contains visual elements that cannot be reproduced exactly, create a deliberate terminal-native approximation rather than ignoring them.
- Do not substitute a generic CLI template when a reference image establishes a specific design direction.

Do not invent a finalized visual identity before reviewing the supplied references.

## 4. Terminal Interface

REX should provide a consistent CLI experience with the following potential building blocks:

- A recognizable REX header or identity mark.
- A dinosaur mascot rendered directly in the terminal using Unicode or other terminal-compatible characters.
- Clear section headings and status indicators.
- Consistent spacing, alignment, and visual hierarchy.
- Carefully chosen terminal colors, including true-color ANSI support where available.
- Contextual help, usage instructions, and useful error messages.
- Progress indicators or animated states where they add value.
- Interactive menus and keyboard controls when appropriate.
- Graceful fallback behavior for terminals with limited color, Unicode, or animation support.

These are design possibilities, not requirements to implement every component immediately.

Avoid unnecessary startup banners, giant ASCII logos, excessive emoji, and decorative output that makes real information harder to read.

## 5. Dinosaur Mascot

The dinosaur is an important part of REX's identity.

Design the mascot to work inside a terminal rather than as an external image.

Potential states include:

- Idle
- Working
- Thinking or waiting
- Success
- Error
- Warning
- Confused or stuck

These states are illustrative. Implement only the states that make sense for the current functionality.

Requirements:

- Keep the dinosaur visually recognizable across poses.
- Prefer compact, carefully constructed Unicode or pixel-art-style terminal characters.
- Use consistent proportions and alignment.
- Support static poses as a fallback when animation is unavailable.
- Avoid rapid, distracting animation.
- Never let animation interfere with user input or overwrite important terminal output.
- Keep mascot rendering separate from application logic so the design can evolve independently.

## 6. Architecture and Implementation

- Separate terminal rendering, application logic, input handling, and feature-specific functionality.
- Keep visual styling and reusable UI components centralized where practical.
- Prefer small, composable modules over a monolithic CLI implementation.
- Make terminal output deterministic and easy to test.
- Handle terminal resizing, interrupted processes, and cleanup of terminal state gracefully.
- Avoid unnecessary dependencies, abstractions, or frameworks.
- Do not build speculative product features before their purpose is defined.
- Do not introduce a large configuration system before there is a concrete need.

Before selecting a terminal UI framework or making major architectural decisions, inspect the existing repository and its dependencies. Follow established project conventions where reasonable.

## 7. Terminal Compatibility

- Support common macOS and Linux terminal environments.
- Detect terminal capabilities where necessary.
- Respect `NO_COLOR` and non-interactive execution contexts.
- Avoid requiring true-color support or Unicode rendering for essential functionality.
- Keep logs and redirected output free of unnecessary terminal control sequences.
- Ensure Ctrl+C exits cleanly and restores terminal settings.
- Do not assume a fixed terminal size.

## 8. CLI Behavior

- Provide clear command names and useful `--help` output.
- Make errors actionable and concise.
- Use consistent exit codes.
- Avoid surprising side effects.
- Keep machine-readable output separate from decorative presentation when appropriate.
- Do not require interactive input when running in a non-interactive context unless explicitly necessary.
- Do not add commands simply to make the CLI appear feature-rich.

The precise command structure will be established as the product direction becomes clearer.

## 9. Development Workflow

Before implementing substantial changes:

1. Inspect the existing repository, entry points, dependencies, and conventions.
2. Review all supplied visual references relevant to the task.
3. Identify the smallest useful implementation.
4. Preserve the existing design and architecture unless there is a concrete reason to change them.
5. Run relevant tests and verify terminal rendering where possible.

When presenting implementation decisions, explain significant trade-offs briefly and concretely.

## 10. Scope Boundaries

At this stage, prioritize:

- The CLI foundation.
- The terminal visual system.
- The dinosaur mascot and rendering system.
- Reusable terminal components.
- A clean, extensible architecture.
- A coherent experience across interactive and non-interactive execution.

Do not assume integrations with Claude Code, Cursor, other LLMs, shell monitoring, command interception, or agent observability are finalized requirements.

Those capabilities may be explored later as the product evolves.

## 11. Definition of Done

A CLI change is complete when:

- It follows the supplied visual references and established design conventions.
- It renders cleanly in a real terminal.
- It handles errors and interruptions appropriately.
- It does not break existing functionality.
- Relevant tests pass.
- It introduces no unnecessary product assumptions or complexity.