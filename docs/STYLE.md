# Documentation style guide

A short reference for writers and reviewers. Goal: keep voice, naming
and examples consistent across every page on this site.

## Product naming

| Context | Use | Don't use |
|---|---|---|
| Prose, headings, marketing | **Docker Agent** (two words, both capitalised — the proper name of the product) | docker-agent, Docker-Agent, docker agent (in prose) |
| The CLI command | `docker agent` (lower-case, two words, in monospace) | `docker-agent`, `Docker Agent run` |
| The repository / module path | `docker/docker-agent` | docker/Docker-Agent |
| Internal identifiers / package names | as defined in code (e.g. `cagent`) — never invent new spellings in prose | mixing internal identifiers into user-facing copy |

A simple rule of thumb:

- **Talking about the product?** → "Docker Agent"
- **Showing a command the user types?** → `docker agent run agent.yaml`

## Voice

- Address the reader as **you**, not "we" or "the user".
- Prefer present tense and active voice ("the agent reads files",
  not "files will be read by the agent").
- Keep sentences short. Two short sentences usually beat one compound
  one.
- Avoid "simply", "just", "easily" — they're rarely accurate and
  often condescending.

## Code samples

- All shell prompts use a dollar sign followed by a space (`$`) and the
  command on the same line. Output, when shown, has no prompt.
- YAML/HCL examples should be runnable as-is when reasonable, or end
  in `# ...` to make truncation explicit.
- The canonical example agent uses `model: anthropic/claude-sonnet-4-5`.
  Use a different model only when the example is *about* that model.
- File names in prose are in `monospace` (`agent.yaml`, not "agent.yaml").

## Callouts

Callouts are written as portable GitHub-style alerts so the same
Markdown renders correctly on docs.docker.com (Hugo), GitHub, and this
site (a blockquote render hook upgrades them to the styled panels):

```markdown
> [!TIP]
> **When to use it**
>
> Body text.
```

- `[!NOTE]` — neutral context
- `[!TIP]` — positive, "consider this"
- `[!IMPORTANT]` — must-read to succeed
- `[!WARNING]` — caution, breaking, security

The bold line directly after the marker is an optional title; omit it
when the default label (Note, Tip, …) is enough. Don't prefix the
title with an emoji — the icon badge already provides one.

## Links

Internal links are plain relative Markdown paths to the target file,
including the `index.md` filename:

```markdown
See the [Quick Start](../../getting-started/quickstart/index.md).
```

Both this site and docs.docker.com render them through a Hugo link
render hook that resolves the path to the target page's URL. Never
use absolute `/path/` links in `docs/**` content — they break when
the page is mounted on docs.docker.com.

## Canonical URLs

Every content page is mounted on docs.docker.com, and this site
tracks `main` while docs.docker.com is pinned to a release — so the
stable page is the authoritative copy. Each
`docs/<section>/<page>/index.md` sets `canonical:` in its front
matter (after `weight:`), derived from its path:

```yaml
canonical: https://docs.docker.com/ai/docker-agent/<section>/<page>/
```

The github.io layout renders it as the page's `rel=canonical` link;
docs.docker.com ignores the value and self-canonicalizes. CI
(`docs-lint` / `scripts/docs-check-canonical.sh`) fails when the
value is missing or doesn't match the page path — mind it when
scaffolding a new page from an existing one. The homepage, `404.md`
and section `_index.md` files are not mirrored pages and don't set
one.

## Availability badges

When a page documents a feature that is merged on `main` but not yet
in a tagged release, mark it so readers of the stable docs know what
to expect:

```markdown
> [!NOTE]
> **Coming in v1.99**
>
> This feature is available on `main` and ships in v1.99.
```

Remove the badge in the release PR that tags the version (the
CHANGELOG update is a good reminder to sweep for `Coming in` markers).

## Glossary one-liners

When a page first introduces a term, link to its concept page or use
one of these standard one-liners:

- **Agent** — an LLM with instructions, tools, and (optionally)
  sub-agents, defined in YAML or HCL.
- **Toolset** — a group of related tools the agent can call (e.g.
  `filesystem`, `shell`, `mcp`).
- **MCP** — Model Context Protocol, an open standard for tool servers.
- **A2A** — Agent-to-Agent protocol, used to talk to other agents
  over HTTP.
- **TUI** — Terminal User Interface, the default interactive front end
  Docker Agent ships with.
- **OCI** — Open Container Initiative; the same registry format used
  for Docker images. Docker Agent reuses it for sharing agents.

## Accessibility (site chrome: `css/style.css`, `js/app.js`, `layouts/`)

The github.io layout targets WCAG 2.1 AA in both themes. When touching
tokens, templates, or JS, keep these conventions:

- **Never de-emphasize text with `opacity`.** It multiplies through to
  contrast against the background and silently drops already-muted
  text below AA (this bit both `.sidebar-subheading` and
  `how-it-works.svg`'s secondary labels). Use a token with a real,
  computed contrast ratio instead — the muted-but-legible look comes
  from choosing the right gray, not from fading a legible one.
- **Recompute contrast for both themes whenever a shared token or rule
  changes.** Dark is `:root`'s default; light is the `[data-theme="light"]`
  override block. A value that passes on one background can fail badly
  on the other — see the token table below.
- **`--bg-content`** is the content column's background (`.main`):
  white in light mode, the same `--bg-dark` as the page in dark mode
  (so dark is unaffected). Header/sidebar/footer chrome stays on
  `--bg`/`--bg-sidebar` (gray-100 in light) — don't apply `--bg-content`
  to chrome elements, or the light chrome-vs-content contrast is lost.
- **`--accent-on-card`** is for brand-blue text sitting on `--bg-card`
  (elevator labels, ecosystem tile headings, usecase links, glossary
  terms): blue-300 in dark (blue-400 only reads 4.08:1 on the dark
  card), blue-500 in light (already 5.0:1 there). Use it instead of
  the bare `--accent` for any *new* text-on-card component.
- **Static, theme-unaware assets** (SVGs embedded via `<img>`, so they
  can't inherit `currentColor` from the page) need one hardcoded
  palette that clears AA against *both* a near-black dark card
  (`#1E2129`) and a white light card simultaneously. A neutral gray
  can't do much better than ~4:1 against both at once (see
  `assets/how-it-works.svg`'s header comment for the derivation) — if
  a new diagram needs true per-theme colors, inline the SVG in the
  Markdown instead so CSS custom properties apply.
- **Watch CSS specificity when a rule sets `color` on `code`/`pre`
  elements.** A same-specificity, theme-unconditional rule that comes
  *later* in the stylesheet silently wins over an earlier
  `[data-theme="light"]` override in both themes — this is exactly how
  the old "Rouge / highlighter-rouge overrides" block broke every
  untyped Chroma syntax token (`.l`, `.sd`, `.cl`, ...) in light mode
  (1.47:1) despite the correctly-themed rule sitting right above it.
  Caught by the `pa11y-ci` gate below — when in doubt, let the scanner
  decide rather than eyeballing the cascade.

### Contrast reference (light / dark, WCAG AA: 4.5:1 normal text, 3:1 large text / non-text UI)

| Token / use | Light | Dark |
|---|---|---|
| `--text-muted` on chrome (`--bg`) / content (`--bg-content`) | gray-600, 4.87:1 / 5.88:1 | gray-400, 6.37:1 |
| Tiny uppercase labels (sidebar/footer/page-nav headings) | gray-700, 7.14:1 / 8.61:1 | gray-100 (unchanged) |
| `--accent` on `.content a` | blue-500, 5.00:1 (on white) | blue-400 + `--accent-hover` blue, 7.2:1 |
| `--accent-on-card` | blue-500, 5.0:1 | blue-300, 5.89:1 |
| Dark syntax comments | n/a | gray-400, 5.59:1 |
| `kbd` text | `--text-muted` (as above) | gray-300, 6.07:1 |
| `.copy-btn` text | gray-600, 5.52:1 | gray-400, 5.59:1 |
| `.pain-x` badge (white text) | `--error-strong` `#DC2626`, 4.83:1 | same |

### Motion

Every animation/transition (fade-in, hover transforms, sidebar slide,
smooth scroll) is disabled under `@media (prefers-reduced-motion: reduce)`
in `style.css`; `js/app.js`'s TOC scroll gates `scrollIntoView`'s
`behavior` on the same query. Animated media (the homepage demo) must
stay pausable/stoppable (WCAG 2.2.2) — it's a `<video controls>`, not
an auto-looping GIF with no user control.

### Running the accessibility scan locally

CI (`docs-a11y` workflow) runs [pa11y-ci](https://github.com/pa11y/pa11y-ci)
against the URLs in `docs/.pa11yci.json` at the `WCAG2AA` standard. To
reproduce locally:

```bash
# Serve the site the same way the Dockerfile does
docker build -t docs docs/
docker run --rm -p 1313:1313 -v $(pwd)/docs:/src docs

# In another terminal, once it's serving at http://localhost:1313/docker-agent/
cd docs && npx --yes pa11y-ci@3.1.0 --config .pa11yci.json
```

`pa11y-ci` only fails the build on HTML_CodeSniffer `error`-level
results (warnings/notices are informational and don't fail CI), so a
local failure here is a real regression worth fixing before pushing.
