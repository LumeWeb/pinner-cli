# Pinner Architecture Overhaul

This directory contains the decision record and execution plans for separating
the Pinner CLI, the hosted MCP product, the Pinner-specific shared application
code, and the independently valuable agnostic libraries that emerged during
MVP development.

The plans are written for mechanical execution by a smaller coding model. Each
execution plan must therefore provide:

- fixed package and type names;
- explicit prerequisites and forbidden dependencies;
- ordered edits small enough for one pull request;
- compatibility behavior that must remain unchanged;
- exact verification commands;
- stop conditions that prevent a partial migration from being merged.

## Decision order

1. [High-level package boundaries and names](00-package-boundaries.md)
2. Agnostic library API designs
3. Pinner-specific shared package API designs
4. Characterization and architecture fitness tests
5. Incremental extraction and migration plans
6. Removal of compatibility bridges and obsolete packages

The package-boundary document is the current decision gate. Detailed file
moves and API migrations must not be written until its names and dependency
directions are accepted.

## Non-negotiable outcome

The hosted product and the CLI are sibling composition roots. The hosted
product must not import, construct, emulate, or embed the CLI. Both products
reuse the same Pinner operation set and Pinner MCP application through shared
packages.

