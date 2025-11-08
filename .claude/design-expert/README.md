# Design Expert Documentation

This directory contains comprehensive documentation about the pgEdge AI
Workbench design philosophy, architecture, and implementation guidance.

## Purpose

These documents serve as the knowledge base for the Design Compliance Validator
role - an AI assistant that ensures all implementation work aligns with the
architectural vision and design goals documented in DESIGN.md.

## Documents

### Core Design Documentation

#### [design-philosophy.md](design-philosophy.md)
**Purpose**: Captures the core design philosophy, goals, and principles that
guide the architecture.

**Contents**:
- Primary mission and design goals
- Architectural principles
- Design patterns
- Technology choices and rationale
- Quality standards
- Success criteria

**Use When**: Understanding "why" decisions were made, evaluating whether new
features align with design intent.

#### [architecture-decisions.md](architecture-decisions.md)
**Purpose**: Records major architectural decisions and their rationales.

**Contents**:
- Component architecture decisions
- Data storage decisions
- Authentication & authorization decisions
- Protocol & API design decisions
- Configuration management decisions
- Migration strategy decisions
- Testing strategy decisions

**Use When**: Understanding specific architectural choices, evaluating
alternatives, making new architectural decisions.

#### [security-model.md](security-model.md)
**Purpose**: Describes the security design, threat model, and secure coding
practices.

**Contents**:
- Security goals and threat model
- Authentication architecture (tokens, sessions, passwords)
- Authorization architecture (RBAC, privileges)
- Secure coding practices
- Network security
- Database security
- Security testing requirements

**Use When**: Implementing security-sensitive features, reviewing code for
security issues, understanding privilege system.

#### [component-responsibilities.md](component-responsibilities.md)
**Purpose**: Defines clear boundaries and responsibilities for each component.

**Contents**:
- Component overview (Collector, Server, CLI, Client)
- Core responsibilities for each component
- What each component does NOT do
- Communication between components
- Boundary rules and deployment patterns

**Use When**: Deciding which component should implement a feature, reviewing
for boundary violations, understanding system architecture.

### Implementation Guidance

#### [development-guidelines.md](development-guidelines.md)
**Purpose**: Provides guidelines for maintaining design consistency and
architectural integrity.

**Contents**:
- Design compliance guidelines with examples
- Pattern library (adding probes, tools, migrations, privilege checks)
- Code review checklist
- Common anti-patterns to avoid
- Error handling patterns

**Use When**: Implementing new features, reviewing code, unsure about correct
implementation approach.

#### [recent-changes.md](recent-changes.md)
**Purpose**: Tracks significant architectural changes and their implications.

**Contents**:
- Migration 6: RBAC system (detailed)
- Migration 5: User tokens
- Migration 4: Configuration tracking probes
- Migration 3: pg_settings probe
- Migration 2: User sessions
- Migration 1: Schema consolidation
- Lessons learned
- Future anticipated changes

**Use When**: Understanding recent architecture evolution, implementing similar
features, avoiding past mistakes.

## How to Use These Documents

### For Design Compliance Validation

When reviewing code or proposals:

1. **Identify Relevant Design Goals**: Check design-philosophy.md for
   applicable principles
2. **Check Architectural Decisions**: Review architecture-decisions.md for
   precedents
3. **Verify Component Boundaries**: Use component-responsibilities.md to
   ensure proper separation
4. **Validate Security**: Check security-model.md for security requirements
5. **Review Patterns**: Use development-guidelines.md for implementation
   patterns
6. **Consider Recent Changes**: Check recent-changes.md for context

### For Feature Implementation

When implementing new features:

1. **Understand Design Intent**: Read design-philosophy.md to understand "why"
2. **Find Precedents**: Check architecture-decisions.md for similar decisions
3. **Determine Component**: Use component-responsibilities.md to identify
   owner
4. **Follow Patterns**: Use development-guidelines.md pattern library
5. **Check Security**: Review security-model.md for security requirements
6. **Test Comprehensively**: Follow testing guidelines in
   development-guidelines.md

### For Code Review

When reviewing code:

1. **Use Checklist**: development-guidelines.md has comprehensive checklist
2. **Verify Compliance**: Check against design-philosophy.md principles
3. **Check Boundaries**: Ensure component-responsibilities.md not violated
4. **Security Review**: Use security-model.md security checklist
5. **Pattern Matching**: Verify code follows established patterns

### For Learning the System

When getting started:

1. **Start with Philosophy**: Read design-philosophy.md to understand vision
2. **Review Architecture**: Read architecture-decisions.md for structure
3. **Understand Components**: Read component-responsibilities.md for organization
4. **Learn Security**: Read security-model.md for security approach
5. **Study Recent Changes**: Read recent-changes.md for recent evolution
6. **Follow Guidelines**: Read development-guidelines.md before coding

## Relationship to Other Documentation

### DESIGN.md (Root)
- **Primary authority** for architectural decisions
- These documents elaborate and explain DESIGN.md
- When conflict: DESIGN.md takes precedence

### CLAUDE.md (Root)
- Standing instructions for development practices
- Complements these documents with coding standards
- These documents explain "why", CLAUDE.md explains "how"

### Component READMEs
- Operational documentation (building, running, configuring)
- These documents explain design, READMEs explain operations

### /docs Directory
- User-facing documentation
- API reference and guides
- These documents are developer-facing, /docs is user-facing

## Maintaining These Documents

### When to Update

**design-philosophy.md**: Update when core design goals or principles change
(rare).

**architecture-decisions.md**: Update when making new major architectural
decisions.

**security-model.md**: Update when security model changes or new threats
identified.

**component-responsibilities.md**: Update when component boundaries change or
new components added.

**development-guidelines.md**: Update when discovering new patterns, anti-
patterns, or common mistakes.

**recent-changes.md**: Update when completing significant architectural changes
(migrations, major features).

### Update Process

1. Make code changes
2. Update relevant design document(s)
3. Ensure consistency across documents
4. Update "Last Updated" timestamp
5. Include in same commit as code changes (or separate doc commit)

### Document Evolution

These are **living documents** - they should evolve as the system evolves.

**Version History**: Tracked via git commits (not within documents)

**Consistency**: When updating one document, check if others need updates too.

**Obsolete Sections**: Mark as [OBSOLETE] rather than deleting (preserve
history).

## Design Compliance Validation Process

### Input
- Code changes (PR, commit, file)
- Feature proposals
- Architectural decisions

### Process
1. Identify applicable design goals from design-philosophy.md
2. Check relevant architectural decisions from architecture-decisions.md
3. Verify component boundaries per component-responsibilities.md
4. Validate security per security-model.md
5. Compare against patterns in development-guidelines.md
6. Consider context from recent-changes.md

### Output
- **Compliance Status**: COMPLIANT | MINOR DEVIATION | MODERATE CONCERN |
  DESIGN VIOLATION
- **Analysis**: How implementation aligns with or deviates from design
- **Findings**: Specific issues or successes
- **Recommendations**: Actionable suggestions for compliance
- **Impact Assessment**: Long-term architectural implications

### Compliance Levels

**COMPLIANT**: Implementation fully aligns with design goals
- No action required
- May highlight particularly good examples

**MINOR DEVIATION**: Small implementation choices that don't affect core
design goals
- Document the deviation
- Consider updating guidelines if pattern emerges

**MODERATE CONCERN**: Approaches that partially conflict with design intent
but could be reconciled
- Specific recommendations for alignment
- May require refactoring

**DESIGN VIOLATION**: Clear contradictions of documented design goals or
architectural principles
- Must be addressed before merge
- May require significant rework
- Consider if design documentation needs update

## Contributing

When you discover:
- Missing design documentation
- Inconsistencies between documents
- Outdated information
- Unclear guidelines
- Useful patterns not documented

**Please update the relevant document(s) and share your improvements.**

## Questions?

If these documents don't answer your question:

1. Check DESIGN.md (primary authority)
2. Check CLAUDE.md (development practices)
3. Look at existing code for examples
4. Ask in code review or design discussion
5. Consider adding your question and answer to relevant document

---

**Version**: 1.0
**Created**: 2025-11-08
**Status**: Living Documentation
