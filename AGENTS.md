# Workspace instructions

These instructions apply only to this repository.

## Karpathy behavioral guidelines

Apply these guidelines when writing, reviewing, or refactoring code. They are
merged with the project-specific instructions below and bias toward caution
over speed; use judgment for trivial tasks.

### Think before coding

- State assumptions explicitly. If uncertain, ask rather than hiding confusion.
- Surface multiple interpretations and tradeoffs instead of choosing silently.
- Prefer simpler approaches and push back when warranted.
- If something is unclear, stop, name the ambiguity, and ask.

### Simplicity first

- Make the minimum change that solves the request.
- Do not add unrequested features, abstractions, flexibility, or configurability.
- Do not add handling for impossible scenarios.
- If an implementation is substantially more complex than necessary, simplify it.

### Surgical changes

- Touch only what is necessary; do not improve adjacent code or formatting.
- Do not refactor unrelated code or remove pre-existing dead code unless asked.
- Match the existing style.
- Remove only imports, variables, or functions made unused by the change.
- Every changed line should trace directly to the request.

### Goal-driven execution

- Define verifiable success criteria for the task.
- For fixes, reproduce the problem with a test before making it pass when practical.
- Verify the result and continue until the success criteria are met.

## Documentation synchronization

Whenever a change modifies the system architecture, component
responsibilities, data flow, persistence behavior, public interfaces, SQL
syntax, SQL semantics, supported commands, or SQL examples:

- Update the relevant Markdown files in the same change.
- Check `README.md` for architecture and public behavior changes.
- Check `SQL_COMMANDS.md` for SQL command or syntax changes.
- Keep diagrams, project-layout descriptions, command examples, and SQL
  examples consistent with the implementation.
- Review the documentation diff alongside the code diff before completing the
  task.

For unrelated changes, do not edit Markdown unnecessarily. Document only
behavior that actually exists in the code.
