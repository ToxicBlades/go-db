# Workspace instructions

These instructions apply only to this repository.

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
