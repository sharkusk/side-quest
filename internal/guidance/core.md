side-quest is a git-native tracker; a "quest" is just an issue, task, or
follow-up you manage through these tools, not by editing files.

- Capture without derailing. An idea surfaces mid-task? File it with quest_new
  (one-line restatement + why it came up) and resume. Set type/priority only
  when the request makes it obvious — a crash or regression is a bug;
  "urgent"/"critical"/"blocking" is high — else keep defaults.
- Work one at a time. Make the quest you're on current (quest_set_current) so the
  git hooks link your commits to it (you never touch hashes); clear it — or switch
  it — once that quest is done, so a later commit doesn't attach to a finished quest.
- Linking a commit (Quest: SQ-1234, or the current-quest auto-link) advances an
  open quest to partial — work has started; "Completes: SQ-1234" closes it, and
  quest_set_status sets any state directly.
- Want the user to confirm a change before it's done? Move the quest to confirm —
  quest_set_status, or a "Confirm: SQ-1234" commit trailer — it stays outstanding
  until the user closes it to done or reopens it to partial.
- Active work is open, partial, and confirm — treat them alike as outstanding. List
  it with quest_list; read one with quest_show.
- Relay the flavor. A tool may append a flavored line beside its JSON; show it to
  the user verbatim — it's the tracker's voice.
