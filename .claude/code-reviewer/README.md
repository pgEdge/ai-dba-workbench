# Code Reviewer Knowledge Base

This directory contains code quality guidelines and patterns for the pgEdge
AI DBA Workbench project.

## Purpose

This knowledge base provides:

- Code quality standards
- Common anti-patterns to avoid
- Review checklists by change type
- Project-specific patterns

## Documents

### [quality-standards.md](quality-standards.md)

Code quality standards for the project:

- Naming conventions
- Error handling patterns
- Code organization
- Complexity guidelines

### [common-issues.md](common-issues.md)

Frequently found issues during code review:

- Go anti-patterns
- React anti-patterns
- Database query issues
- Error handling problems

### [review-checklists.md](review-checklists.md)

Checklists for different types of changes:

- New feature checklist
- Bug fix checklist
- Refactoring checklist
- Performance change checklist

## Quick Reference

### Related Knowledge Bases

For language-specific guidance, consult:

- Go patterns: `/.claude/golang-expert/`
- React patterns: `/.claude/react-expert/`
- Testing patterns: `/.claude/testing-expert/`
- Security review: `/.claude/security-auditor/`

### Key Quality Metrics

| Metric | Target |
|--------|--------|
| Function length | < 50 lines |
| Cyclomatic complexity | < 10 |
| Nesting depth | < 4 levels |
| Test coverage | > 80% |

### Project Conventions

- Four-space indentation (all languages)
- No unused code committed
- All exports documented
- Tests for all new functionality

## Document Updates

Update these documents when:

- New patterns are established
- Common issues are identified
- Review standards change
- New anti-patterns discovered

Last Updated: 2026-01-09
