# AGENTS.md

## Project

This repository is for building a US stock trading bot with Go and the Alpaca API.

The project is also a way to learn trading systems and AI-assisted development.

## Direction and Current Capabilities

The long-term direction is continuous server operation, a dashboard showing current account status, and an automated daily trading cycle. These goals do not imply that those capabilities are implemented.

See [README.md](README.md) for current capabilities, setup, and limitations.

Do not assume the final architecture, trading strategy, or package structure. Distinguish documented decisions from proposals and unresolved choices.

## Working Style

- Use English for code, documentation, plans, and commit messages.
- Keep changes small and easy to review.
- Inspect the repository before proposing changes.
- Explain important design choices.
- Do not make unrelated changes.
- Prefer simple solutions over premature abstractions.

## Selecting the Next Goal

- Ground the next goal in the user's request, current code, tests, and relevant documentation.
- Choose a small, independently verifiable step toward the project direction; address prerequisites before features that depend on them.
- State the intended outcome, scope, non-goals, and unresolved decisions. Do not bundle unrelated infrastructure or trading features.
- Do not invent trading rules, architecture decisions, or requirements. Identify decisions the user needs to make when they materially affect the work.

## Version Control

- Normally use a short-lived branch for implementation work instead of committing directly to `main`.
- Review the complete branch diff against its target branch before merging.
- Write commit messages as `<type>: <imperative summary>`.
- Use `feat`, `fix`, `test`, `docs`, `refactor`, or `chore` when one of these types fits.
- Keep each commit focused on one logical change.

## Planning

When asked to plan:

- Do not modify files.
- Summarize the current state.
- Propose a small next step.
- Define observable acceptance criteria and how each will be verified, including relevant failure cases.
- List assumptions and unresolved decisions.
- Explain what I need to decide.

For implementation work, establish scope and acceptance criteria before editing.

## Implementation

When asked to implement:

- Implement only the requested scope.
- Do not add features that were not requested.
- Add appropriate tests.
- Follow the validation guidance below and check the result against the acceptance criteria.
- Review the complete task diff for correctness, safety, scope, and unintended changes before reporting completion.
- Report what changed, verification results, remaining limitations, and unresolved conflicts. Distinguish completed work from follow-up work.

## Documentation Maintenance

- Before reporting a task complete, review whether its changes make applicable AGENTS.md instructions or relevant project documentation stale. When committing, do this before the commit so related documentation updates are included.
- Correct instructions and documentation made stale by the task. Reference existing documents instead of duplicating their contents.
- Add guidance only when it is durable, project-specific, and useful for future tasks. Prefer clarifying or replacing an existing rule over adding another.
- Keep task progress, temporary workarounds, and development logs outside AGENTS.md.
- Do not change project goals, trading policy, or acceptance criteria merely to match the implementation.
- If no documentation change is needed, leave the files unchanged. Briefly report documentation changes and why they were needed.

## Safety

- Use Alpaca Paper Trading by default.
- Do not enable live trading unless explicitly requested.
- Never hard-code or expose API credentials.
- Store real credentials only in process environment variables or ignored local `.env` files.
- Keep only placeholder values in `.env.example`.
- Use fake credentials and temporary files in tests.
- Never log secret values or an entire configuration object that contains them.
- Do not silently ignore errors.

## Validation

Run commands from the repository root using a Go version compatible with `go.mod`.
For Go changes, format each changed `.go` file with `gofmt -w <changed-file.go>`, then run static analysis and tests:

```bash
go vet ./...
go test ./...
```

Automated tests use fake credentials and transports and require no Alpaca connection; see the README for optional manual verification. Do not treat `go run .` as an automated test.

For documentation-only changes, review the diff and run `git diff --check`; Go formatting is unnecessary unless Go files changed.

Report any skipped or failed check and distinguish code failures from execution-environment failures.
Never claim that a command passed unless it was actually executed.
