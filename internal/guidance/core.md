side-quest is a git-native tracker; a "quest" is just an issue, task, or
follow-up you manage through these tools, not by editing files.

- Capture without derailing. An idea surfaces mid-task? File it with quest_new
  (one-line restatement + why it came up) and resume. Set type/priority only
  when the request makes it obvious — a crash or regression is a bug;
  "urgent"/"critical"/"blocking" is high — else keep defaults.
- Work one at a time. Make the quest you're on current (quest_set_current); the
  git hooks then link your commits to it — you never touch hashes.
- Close it by committing "Completes: SQ-1234" (or "Quest: SQ-1234" to link
  only), or quest_set_status.
- List work with quest_list; read one with quest_show.
