# Documentation Writer Knowledge Base

This directory records where documentation lives in the pgEdge AI DBA
Workbench repository. The style rules themselves are not kept here: the
`pgedge-skills:pgedge-docs` plugin skill is the authoritative pgEdge style
guide and is preloaded into the documentation-writer agent. The
repo-specific deviations from that skill are listed in the agent prompt at
`.claude/agents/documentation-writer.md` and in `CLAUDE.md`.

## Layout

Documentation is organised by audience rather than by sub-project, and the
navigation is defined in the `nav` section of `mkdocs.yml`:

| Content                          | Location                              |
|----------------------------------|---------------------------------------|
| Site entry point                 | `docs/index.md`                       |
| Installation and first steps     | `docs/getting-started/`               |
| Using the web client and tools   | `docs/user-guide/`                    |
| Configuration, API and operations| `docs/admin-guide/`                   |
| Architecture and contributing    | `docs/developer-guide/`               |
| Per-sub-project developer pages  | `docs/developer-guide/<subproject>/`  |
| Changelog                        | `docs/changelog.md`                   |
| Licence                          | `docs/LICENSE.md` and `/LICENSE.md`   |
| Sub-project README               | `/<subproject>/README.md`             |
| Top-level README                 | `/README.md`                          |

The static OpenAPI file at `docs/admin-guide/api/openapi.json` is generated
with `cd server && make openapi`; do not edit it by hand. The endpoint
summary table in `docs/admin-guide/api/reference.md` is maintained
manually alongside it.

## Conventions Specific to This Repository

- README footers link to `docs/developer-guide/contributing.md` for
  contributions, `https://docs.pgedge.com` for online documentation and
  `LICENSE.md` for the licence.
- Each README's table of contents mirrors the `mkdocs.yml` nav section.
- Filenames under `docs/` are lowercase with hyphens between words.
- Prose wraps at 79 characters; long URLs and nav paths are exempt.

## Maintenance

Update this file in the same change whenever the documentation layout,
generated files or README conventions change. A stale entry is worse than
none.
