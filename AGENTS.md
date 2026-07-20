# AGENTS.md

## Project

This repository is for building a US stock trading bot with Go and the Alpaca API.

The project is also a way to learn trading systems and AI-assisted development.

## Current Stage

The project is in an early design and exploration stage.

Do not assume the final architecture, trading strategy, or package structure unless it has already been decided.

## Working Style

- Use English for code, documentation, plans, and commit messages.
- Keep changes small and easy to review.
- Inspect the repository before proposing changes.
- Explain important design choices.
- Do not make unrelated changes.
- Prefer simple solutions over premature abstractions.

## Planning

When asked to plan:

- Do not modify files.
- Summarize the current state.
- Propose a small next step.
- List assumptions and unresolved decisions.
- Explain what I need to decide.

## Implementation

When asked to implement:

- Implement only the requested scope.
- Do not add features that were not requested.
- Add appropriate tests.
- Run formatting and tests when possible.
- Report what changed and what remains unverified.

## Safety

- Use Alpaca Paper Trading by default.
- Do not enable live trading unless explicitly requested.
- Never hard-code or expose API credentials.
- Do not silently ignore errors.

## Validation

Run when applicable:

```bash
gofmt -w .
go vet ./...
go test ./...
```

Never claim that a command passed unless it was actually executed.
