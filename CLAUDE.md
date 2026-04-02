# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## gstack

Use the `/browse` skill for all web browsing. Never use `mcp__claude-in-chrome__*` tools.

Available skills:

| Skill | Purpose |
|---|---|
| `/office-hours` | YC-style office hours for startup/builder mode |
| `/plan-ceo-review` | CEO/founder-mode plan review |
| `/plan-eng-review` | Eng manager-mode plan review |
| `/plan-design-review` | Designer's eye plan review |
| `/design-consultation` | Full design system proposal |
| `/design-shotgun` | Generate multiple design variants for comparison |
| `/design-html` | Convert approved mockup to production HTML/CSS |
| `/review` | Pre-landing PR review |
| `/ship` | Ship workflow: merge base, run tests, bump version, create PR |
| `/land-and-deploy` | Merge PR, wait for CI/deploy, verify production |
| `/canary` | Post-deploy canary monitoring |
| `/benchmark` | Performance regression detection |
| `/browse` | Headless browser for QA testing and site dogfooding |
| `/connect-chrome` | Launch real Chrome controlled by gstack |
| `/qa` | Systematically QA test and fix bugs |
| `/qa-only` | QA report only, no fixes |
| `/design-review` | Designer's eye QA: find and fix visual issues |
| `/setup-browser-cookies` | Import cookies from real browser into headless session |
| `/setup-deploy` | Configure deployment settings |
| `/retro` | Weekly engineering retrospective |
| `/investigate` | Systematic debugging with root cause investigation |
| `/document-release` | Post-ship documentation update |
| `/codex` | OpenAI Codex CLI: code review, challenge, consult modes |
| `/cso` | Chief Security Officer infrastructure security audit |
| `/autoplan` | Auto-review pipeline (CEO + design + eng reviews) |
| `/careful` | Safety guardrails for destructive commands |
| `/freeze` | Restrict file edits to a specific directory |
| `/guard` | Full safety mode: destructive warnings + directory-scoped edits |
| `/unfreeze` | Clear the freeze boundary |
| `/gstack-upgrade` | Upgrade gstack to the latest version |
| `/learn` | Manage project learnings across sessions |

## Skill routing

When the user's request matches an available skill, ALWAYS invoke it using the Skill
tool as your FIRST action. Do NOT answer directly, do NOT use other tools first.
The skill has specialized workflows that produce better results than ad-hoc answers.

Key routing rules:
- Product ideas, "is this worth building", brainstorming → invoke office-hours
- Bugs, errors, "why is this broken", 500 errors → invoke investigate
- Ship, deploy, push, create PR → invoke ship
- QA, test the site, find bugs → invoke qa
- Code review, check my diff → invoke review
- Update docs after shipping → invoke document-release
- Weekly retro → invoke retro
- Design system, brand → invoke design-consultation
- Visual audit, design polish → invoke design-review
- Architecture review → invoke plan-eng-review
