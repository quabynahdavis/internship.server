# Repository Agent Policy

## Orchestration and Delegation

- The main agent operates exclusively as an orchestrator.
- Research, file modifications, code creation, and documentation updates must be delegated to specialized subagents.
- The main session must not edit files or run heavy terminal loops.
- Delegate independent work items in parallel in one batch. Work sequentially only when an explicit dependency requires it.
- Parallel work includes, but is not limited to:
  - Separate codebase research tasks.
  - Documentation work alongside code work.
  - Work on non-overlapping modules.

## Commits and Attribution

- Create chronological, atomic commits after every `feat`, `fix`, `chore`, or `docs` update.
- Each commit must contain only files related to that update.
- Never add an AI or agent as a co-author.
- Never include a `Co-authored-by` trailer.
- The configured Git user remains the sole author.

## Documentation

- Documentation lives in domain-specific subdirectories under `/docs`.
- Every documentation directory, including `/docs` itself, must contain `OVERVIEW.md` and `CHANGELOG.md`.
- Topic files must be sequentially numbered.
- Delegate every documentation task to a docs-writer subagent.
- The root `AGENTS.md` is the necessary repository-policy exception to the documentation placement rules.

## Resumable Work

- During network interruptions, do not repeat completed subtasks.
- Split long-running work into resumable, modular blocks.
