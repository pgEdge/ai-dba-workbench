# Documentation Writer Knowledge Base

This directory contains documentation standards and templates for the pgEdge
AI DBA Workbench project.

## Purpose

This knowledge base provides:

- Templates for common document types.
- Formatting rules and conventions stored in CLAUDE.md
  and the documentation-writer agent prompt.

## Documents

### [templates.md](templates.md)

Ready-to-use templates:

- README template
- API documentation template
- Feature documentation template
- Changelog entry format

## Quick Reference

### Critical Rules

1. **Line wrap at 79 characters** for all markdown files
2. **Active voice** throughout
3. **7-20 word sentences** that are grammatically complete
4. **Blank line before every list** (including sub-lists)
5. **No emojis** unless explicitly requested
6. **Four-space indentation** in code blocks

### Document Location

| Document Type | Location |
|---------------|----------|
| Sub-project docs | `/docs/<subproject>/` |
| Sub-project README | `/<subproject>/README.md` |
| Top-level README | `/README.md` |
| Changelog | `/docs/changelog.md` |

### File Naming

- Use **lowercase** for all files in `/docs/`
- Use **hyphens** for multi-word names: `api-reference.md`
- Each sub-project docs has an `index.md` entry point

## Document Updates

This knowledge base is the source of truth for documentation standards.
Update these documents when:

- Style guide changes
- New templates needed
- New patterns established

Last Updated: 2026-02-26
