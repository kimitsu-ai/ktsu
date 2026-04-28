## GitHub Issues
- All changes must reference a GitHub issue. Before starting work, confirm an issue exists; if not, ask the user to provide one or offer to create it.
- Include the issue number in commit messages (e.g., `fixes #123` or `refs #123`).

## Approach
- Think before acting. Read existing files before writing code.
- Be concise in output but thorough in reasoning.
- Prefer editing over rewriting whole files.
- Do not re-read files you have already read.
- Test your code before declaring done.
- No sycophantic openers or closing fluff.
- Keep solutions simple and direct.
- User instructions always override this file.

## Documentation
- When adding new features or modifying existing features, update documentation in docs/*.md
- Any changes to schema validation or workflow graphs must update docs/yaml-spec/*.md
- When updating a doc, also update its frontmatter `description` to reflect any changes in scope or content.

## Specs
- When creating spec files, use the .md format
- Spec files should be placed in the specs/ directory
