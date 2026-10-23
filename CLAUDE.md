# Nylzo

## Pull requests

- Write the PR title and body in English, whatever language the conversation uses.
- Keep the body short and easy to scan, 30 lines at most.
- Open with two or three sentences on what the PR does and why.
- Use only the sections a reviewer needs, usually Changes, Testing and Open items.
- Give each change one bullet of one line. Say what changed and why, and leave the how to the diff.
- Cite acceptance criteria by id, for example "Covered: AK1 to AK9, AK11". Link the spec or design doc instead of copying from it.
- Keep review logs, finding lists and long reasoning out of the body. Put them in a PR comment or a linked file.
- Update the body whenever the PR changes, so it always describes the current state.

## Svelte

- Before you create, edit or review a `.svelte`, `.svelte.ts` or `.svelte.js` file, load the `svelte-core-bestpractices` skill and follow it. Write Svelte 5 in runes mode only.
- Use the `svelte-code-writer` skill for documentation lookups and code checks, but run its CLI at the version pinned in `web/package.json`: `pnpm --dir web exec svelte-mcp <command>` instead of `npx @sveltejs/mcp`, so nothing unpinned is downloaded. File paths are relative to `web/`.
- Before you finish, run `svelte-autofixer` on every Svelte file you changed and fix what it reports until it reports nothing.
- Subagents follow these rules too, and a brief that hands Svelte work to a subagent says so.
- The skills are vendored from `sveltejs/ai-tools` and pinned in `skills-lock.json`. Do not edit them; update them on purpose with `npx skills update` and review the diff.
