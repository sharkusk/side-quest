---
description: Capture a side quest without derailing your current work
argument-hint: <the idea to capture>
---

A new idea just surfaced mid-task: **$ARGUMENTS**

Capture it as a side quest, then immediately return to what we were doing. Do
NOT start working on the idea.

1. Call the `quest_new` MCP tool (side-quest server) with:
   - `title`: a concise, self-contained restatement of the idea (not a verbatim echo).
   - `context`: one sentence on *why it came up now* — what we were doing when it
     surfaced — so it is recoverable later.
   - Do not set it current. Do not set `type`/`priority` unless the user stated them.
2. Confirm in one line: the returned quest id and its title. Nothing more.
3. Resume the previous task exactly where we left off.

If the side-quest MCP server or the `quest_new` tool is unavailable, tell the
user to install and enable the side-quest plugin (and its binary). Do not fall
back to editing files.
