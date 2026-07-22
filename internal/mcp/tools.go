package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sharkusk/side-quest/internal/brief"
	"github.com/sharkusk/side-quest/internal/capture"
	"github.com/sharkusk/side-quest/internal/config"
	"github.com/sharkusk/side-quest/internal/quest"
	"github.com/sharkusk/side-quest/internal/voice"
)

// register wires every tool onto the server. Task 4 appends the mutation tools.
func (h *handlers) register(s *sdk.Server) {
	// Closed string domains, derived from the quest constants so a rename breaks
	// the build. Applied as JSON-Schema enums below (see enumSchema).
	statuses := []string{
		string(quest.StatusOpen), string(quest.StatusPartial), string(quest.StatusConfirm),
		string(quest.StatusDone), string(quest.StatusDeferred), string(quest.StatusDiscarded),
	}
	types := []string{string(quest.TypeBug), string(quest.TypeFeature)}
	prios := []string{string(quest.PriorityHigh), string(quest.PriorityLow)}

	sdk.AddTool(s, &sdk.Tool{Name: "quest_new", Description: "Capture a new quest — an issue, task, or follow-up. Note a tangent that surfaced mid-task without derailing: restate the idea in a line. Mechanical git context (branch/head/cwd/current) is recorded automatically; pass a one-sentence narrative in context. Set type/priority only when the request makes them obvious (a crash or regression is a bug; explicit \"urgent\"/\"critical\"/\"blocking\" is high), else omit them.",
		InputSchema: enumSchema[newIn](map[string][]string{"type": types, "priority": prios})}, h.questNew)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_list", Description: "List quests, optionally filtered by status/type/priority/tags (AND).",
		InputSchema: enumSchema[listIn](map[string][]string{"status": statuses, "type": types, "priority": prios})}, h.questList)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_show", Description: "Show one quest by id."}, h.questShow)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_history", Description: "Return a quest's change history to answer historical questions about it: one entry per commit that touched the quest, oldest first, each with the date, author (who/email), and what changed (created, status x→y, type/priority x→y, note added, linked/unlinked commit, title/tags/body edited)."}, h.questHistory)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_get_current", Description: "Return this worktree's current quest id (empty if none)."}, h.questGetCurrent)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_brief", Description: "Return a resume-oriented snapshot of the project's state in one call: the current quest (full), the outstanding backlog, and the most-recently-closed quests (with a total count). Call it at the start of a session — especially on a fresh clone or new machine — to orient without reading quests one by one. Read-only."}, h.questBrief)
	sdk.AddTool(s, &sdk.Tool{Name: "server_info", Description: "Report the running side-quest MCP server's build version. Use it to verify the server is current after a plugin install or update: compare this version to the latest release (or to `side-quest version` for the provisioned binary). If it's older, the server is still running the previous binary — restart it from /mcp so it reloads the new build."}, h.serverInfo)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_set_status", Description: "Set a quest's lifecycle status (open|partial|confirm|done|deferred|discarded). Use confirm when you've finished a change but want the user to confirm it before the quest is done — it stays outstanding until they close it.",
		InputSchema: enumSchema[statusIn](map[string][]string{"status": statuses})}, h.questSetStatus)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_reclassify", Description: "Change a quest's type and/or priority.",
		InputSchema: enumSchema[reclassifyIn](map[string][]string{"type": types, "priority": prios})}, h.questReclassify)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_update", Description: "Update a quest's title and/or tags (a tag with an empty value is deleted)."}, h.questUpdate)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_note", Description: "Append a timestamped note to a quest's body (non-destructive)."}, h.questNote)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_set_current", Description: "Set this worktree's current quest by id (the quest you're actively working on), or clear it with clear:true. While a quest is current, the git hooks link the commits you make to it automatically."}, h.questSetCurrent)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_link_commit", Description: "Apply a commit's Quest:/Confirm:/Completes: trailers to the referenced quests."}, h.questLinkCommit)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_relink_commit", Description: "Repoint a recorded commit after a rebase rewrote its hash: replace old_sha (matched by prefix, never resolved — it may be dangling) with new_sha."}, h.questRelinkCommit)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_unlink_commit", Description: "Remove a recorded commit from a quest (sha matched by prefix)."}, h.questUnlinkCommit)

	sdk.AddTool(s, &sdk.Tool{Name: "cli_status", Description: "Report whether the terminal side-quest CLI is enabled (a launcher on the user's PATH) and whether the one-time enable offer has been made. Call early in a plugin session to decide whether to offer."}, h.cliStatus)
	sdk.AddTool(s, &sdk.Tool{Name: "cli_install", Description: "Enable the terminal side-quest CLI: write a read-only launcher onto the user's PATH so they can run side-quest from their own terminal (and have their own git commits link). Also installs a project-level /sq slash command in the current repo (bare /sq; the plugin's own is /side-quest:sq). Runs in-process; safe to re-run. Offer before calling."}, h.cliInstall)
	sdk.AddTool(s, &sdk.Tool{Name: "cli_uninstall", Description: "Disable the terminal side-quest CLI by removing the launcher this tool installed (never touches a side-quest it did not install)."}, h.cliUninstall)
	sdk.AddTool(s, &sdk.Tool{Name: "cli_dismiss", Description: "Record that the user declined the terminal-CLI offer, so it is not offered again for this install."}, h.cliDismiss)
}

// enumSchema infers the JSON schema for input type T (the same inference the SDK
// would do) and stamps each named property with an enum of allowed values. The
// SDK's jsonschema struct tag only sets a property's description — google/jsonschema-go
// treats the tag as description text and rejects "key=" forms — so closed
// domains are constrained here, giving MCP clients hard validation before a
// request reaches the store. Absent optional properties still pass, so defaults
// and no-filter behavior are unchanged. Panics on a misconfigured property name,
// which is a programming error caught at startup.
func enumSchema[T any](enums map[string][]string) *jsonschema.Schema {
	sch, err := jsonschema.For[T](nil)
	if err != nil {
		panic(fmt.Sprintf("mcp: inferring schema: %v", err))
	}
	for prop, vals := range enums {
		p, ok := sch.Properties[prop]
		if !ok {
			panic(fmt.Sprintf("mcp: enum on unknown property %q", prop))
		}
		p.Enum = make([]any, len(vals))
		for i, v := range vals {
			p.Enum[i] = v
		}
	}
	return sch
}

// questSummary is the compact, list/triage projection of a quest for the MCP
// surface (SQ-0052): the light identifying fields only, with Context and Body
// dropped and Commits collapsed to a count, so an agent listing a backlog is
// not force-fed every quest's full body. Lowercase JSON keys match the ack
// style used elsewhere in this package (quest.Quest itself, carrying only yaml
// tags, still marshals with capitalised keys via quest_show).
type questSummary struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	Status      string            `json:"status"`
	Type        string            `json:"type"`
	Priority    string            `json:"priority"`
	Completed   *time.Time        `json:"completed,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
	CommitCount int               `json:"commit_count"`
}

func summarize(q *quest.Quest) questSummary {
	return questSummary{
		ID:          q.ID,
		Title:       q.Title,
		Status:      string(q.Status),
		Type:        string(q.Type),
		Priority:    string(q.Priority),
		Completed:   q.Completed,
		Tags:        q.Tags,
		CommitCount: len(q.Commits),
	}
}

// --- input types ---

type newIn struct {
	Title      string            `json:"title" jsonschema:"the quest title"`
	Context    string            `json:"context,omitempty" jsonschema:"a one-sentence note on why this quest was captured"`
	Type       string            `json:"type,omitempty" jsonschema:"bug or feature; defaults to feature"`
	Priority   string            `json:"priority,omitempty" jsonschema:"high or low; defaults to low"`
	Tags       map[string]string `json:"tags,omitempty" jsonschema:"optional key/value tags"`
	SetCurrent bool              `json:"set_current,omitempty" jsonschema:"also set this quest as the worktree's current quest"`
}

type listIn struct {
	Status   string            `json:"status,omitempty" jsonschema:"filter by status"`
	Type     string            `json:"type,omitempty" jsonschema:"filter by type (bug|feature)"`
	Priority string            `json:"priority,omitempty" jsonschema:"filter by priority (high|low)"`
	Tags     map[string]string `json:"tags,omitempty" jsonschema:"filter by tags; a quest matches only if it has every given key with the given value"`
}

type idIn struct {
	ID string `json:"id" jsonschema:"the quest id, e.g. SQ-0001"`
}

type emptyIn struct{}

// --- shared helpers ---

// jsonResult renders v as indented JSON text content — the neutral tool payload.
func jsonResult(v any) (*sdk.CallToolResult, any, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(b)}}}, nil, nil
}

// voiced appends an optional second content block carrying a tone-flavored line
// for a human reader, leaving content[0] (the neutral JSON) untouched for parsers
// (SQ-0028). It is silent for the plain tone and on any config read error, so a
// consumer that selects plain — and a misconfigured store — see exactly the JSON.
// line receives a *voice.Voice so the caller picks the matching confirmation.
func (h *handlers) voiced(res *sdk.CallToolResult, line func(*voice.Voice) string) *sdk.CallToolResult {
	if res == nil {
		return res
	}
	cfg, err := h.store.Config()
	if err != nil {
		return res
	}
	eff, _ := voice.EffectiveTone(cfg.Tone, true) // superfan -> dcc; a server prints no hint
	if eff == config.TonePlain {
		return res
	}
	res.Content = append(res.Content, &sdk.TextContent{Text: line(voice.New(eff))})
	return res
}

// jsonResultVoiced renders payload as the neutral JSON block and appends the
// optional tone-flavored line — the shorthand every voiced mutation handler uses.
func (h *handlers) jsonResultVoiced(payload any, line func(*voice.Voice) string) (*sdk.CallToolResult, any, error) {
	res, meta, err := jsonResult(payload)
	if err != nil {
		return res, meta, err
	}
	return h.voiced(res, line), meta, nil
}

// --- handlers ---

func (h *handlers) questNew(ctx context.Context, req *sdk.CallToolRequest, in newIn) (*sdk.CallToolResult, any, error) {
	cur, _ := h.store.Current()
	dir, _ := os.Getwd()
	body := capture.Body(dir, cur, in.Context)
	q, err := h.store.Create(in.Title, body, quest.Type(in.Type), quest.Priority(in.Priority), in.Tags)
	if err != nil {
		return nil, nil, err
	}
	if in.SetCurrent {
		if err := h.store.SetCurrent(q.ID); err != nil {
			// The quest EXISTS at this point — a bare error would hide that and
			// invite a retry that mints a duplicate (SQ-0122).
			return nil, nil, fmt.Errorf("quest %s was created, but setting it current failed: %w", q.ID, err)
		}
	}
	res, meta, err := jsonResult(summarize(q))
	if err != nil {
		return res, meta, err
	}
	return h.voiced(res, func(v *voice.Voice) string { return v.QuestCreated(q.ID) }), meta, nil
}

func (h *handlers) questList(ctx context.Context, req *sdk.CallToolRequest, in listIn) (*sdk.CallToolResult, any, error) {
	if in.Status != "" && !quest.Status(in.Status).Valid() {
		return nil, nil, fmt.Errorf("invalid status %q", in.Status)
	}
	if in.Type != "" && !quest.Type(in.Type).Valid() {
		return nil, nil, fmt.Errorf("invalid type %q", in.Type)
	}
	if in.Priority != "" && !quest.Priority(in.Priority).Valid() {
		return nil, nil, fmt.Errorf("invalid priority %q", in.Priority)
	}
	all, err := h.store.List()
	if err != nil {
		return nil, nil, err
	}
	summaries := make([]questSummary, 0, len(all))
	for _, q := range all {
		if in.Status != "" && string(q.Status) != in.Status {
			continue
		}
		if in.Type != "" && string(q.Type) != in.Type {
			continue
		}
		if in.Priority != "" && string(q.Priority) != in.Priority {
			continue
		}
		if !quest.MatchTags(q.Tags, in.Tags) {
			continue
		}
		summaries = append(summaries, summarize(q))
	}
	return jsonResult(summaries)
}

func (h *handlers) questShow(ctx context.Context, req *sdk.CallToolRequest, in idIn) (*sdk.CallToolResult, any, error) {
	q, err := h.store.Get(in.ID)
	if err != nil {
		return nil, nil, err
	}
	return jsonResult(q)
}

func (h *handlers) questHistory(ctx context.Context, req *sdk.CallToolRequest, in idIn) (*sdk.CallToolResult, any, error) {
	entries, err := h.store.History(in.ID)
	if err != nil {
		return nil, nil, err
	}
	return jsonResult(entries)
}

func (h *handlers) serverInfo(ctx context.Context, req *sdk.CallToolRequest, in emptyIn) (*sdk.CallToolResult, any, error) {
	return jsonResult(struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}{"side-quest", h.version})
}

func (h *handlers) questGetCurrent(ctx context.Context, req *sdk.CallToolRequest, in emptyIn) (*sdk.CallToolResult, any, error) {
	cur, err := h.store.Current()
	if err != nil {
		return nil, nil, err
	}
	return jsonResult(struct {
		Current string `json:"current"`
	}{cur})
}

type briefIn struct {
	ClosedShown *int `json:"closed_shown,omitempty" jsonschema:"how many recently-closed quests to include; defaults to 5, 0 for none"`
}

// briefPayload is the neutral machine shape for quest_brief: the current quest in
// full (an agent resuming wants its context/body) and the outstanding/closed
// quests as the same compact summaries quest_list returns, plus a total closed
// count and the last-activity time. Assembly is shared with the CLI via
// internal/brief. Read-only — reads never voice.
type briefPayload struct {
	Current      *quest.Quest   `json:"current"`
	Outstanding  []questSummary `json:"outstanding"`
	Closed       []questSummary `json:"closed"`
	ClosedTotal  int            `json:"closed_total"`
	LastActivity *time.Time     `json:"last_activity,omitempty"`
}

func (h *handlers) questBrief(ctx context.Context, req *sdk.CallToolRequest, in briefIn) (*sdk.CallToolResult, any, error) {
	quests, err := h.store.List()
	if err != nil {
		return nil, nil, err
	}
	cur, _ := h.store.Current() // best-effort: no pointer just means no current quest
	closedN := brief.DefaultClosedShown
	if in.ClosedShown != nil {
		closedN = *in.ClosedShown
	}
	d := brief.Build(quests, cur, time.Now(), closedN)
	payload := briefPayload{
		Current:     d.Current,
		Outstanding: summarizeAll(d.Outstanding),
		Closed:      summarizeAll(d.Closed),
		ClosedTotal: d.ClosedTotal,
	}
	if !d.LastActivity.IsZero() {
		la := d.LastActivity
		payload.LastActivity = &la
	}
	return jsonResult(payload)
}

func summarizeAll(qs []*quest.Quest) []questSummary {
	out := make([]questSummary, 0, len(qs))
	for _, q := range qs {
		out = append(out, summarize(q))
	}
	return out
}

type statusIn struct {
	ID     string `json:"id" jsonschema:"the quest id"`
	Status string `json:"status" jsonschema:"open|partial|confirm|done|deferred|discarded"`
}

type reclassifyIn struct {
	ID       string `json:"id" jsonschema:"the quest id"`
	Type     string `json:"type,omitempty" jsonschema:"new type (bug|feature)"`
	Priority string `json:"priority,omitempty" jsonschema:"new priority (high|low)"`
}

type updateIn struct {
	ID    string            `json:"id" jsonschema:"the quest id"`
	Title string            `json:"title,omitempty" jsonschema:"new title"`
	Tags  map[string]string `json:"tags,omitempty" jsonschema:"tags to merge; empty value deletes a key"`
}

type noteIn struct {
	ID   string `json:"id" jsonschema:"the quest id"`
	Text string `json:"text" jsonschema:"the note text to append"`
}

type setCurrentIn struct {
	ID    string `json:"id,omitempty" jsonschema:"the quest id to make current"`
	Clear bool   `json:"clear,omitempty" jsonschema:"clear the current quest instead of setting it"`
}

type shaIn struct {
	SHA string `json:"sha" jsonschema:"the commit sha whose trailers to apply"`
}

type relinkIn struct {
	ID     string `json:"id" jsonschema:"the quest id"`
	OldSHA string `json:"old_sha" jsonschema:"the recorded (old) commit sha to replace; matched by prefix"`
	NewSHA string `json:"new_sha" jsonschema:"the new commit sha to record in its place"`
}

type unlinkIn struct {
	ID  string `json:"id" jsonschema:"the quest id"`
	SHA string `json:"sha" jsonschema:"the recorded commit sha to remove; matched by prefix"`
}

func (h *handlers) questSetStatus(ctx context.Context, req *sdk.CallToolRequest, in statusIn) (*sdk.CallToolResult, any, error) {
	if err := h.store.SetStatus(in.ID, quest.Status(in.Status)); err != nil {
		return nil, nil, err
	}
	res, meta, err := jsonResult(struct {
		OK     bool   `json:"ok"`
		ID     string `json:"id"`
		Status string `json:"status"`
	}{true, in.ID, in.Status})
	if err != nil {
		return res, meta, err
	}
	return h.voiced(res, func(v *voice.Voice) string { return v.StatusSet(in.ID, quest.Status(in.Status)) }), meta, nil
}

func (h *handlers) questReclassify(ctx context.Context, req *sdk.CallToolRequest, in reclassifyIn) (*sdk.CallToolResult, any, error) {
	if in.Type == "" && in.Priority == "" {
		return nil, nil, fmt.Errorf("reclassify needs type and/or priority")
	}
	if err := h.store.Reclassify(in.ID, quest.Type(in.Type), quest.Priority(in.Priority)); err != nil {
		return nil, nil, err
	}
	return h.jsonResultVoiced(struct {
		OK       bool   `json:"ok"`
		ID       string `json:"id"`
		Type     string `json:"type,omitempty"`
		Priority string `json:"priority,omitempty"`
	}{true, in.ID, in.Type, in.Priority}, func(v *voice.Voice) string { return v.Reclassified(in.ID) })
}

func (h *handlers) questUpdate(ctx context.Context, req *sdk.CallToolRequest, in updateIn) (*sdk.CallToolResult, any, error) {
	if in.Title == "" && len(in.Tags) == 0 {
		return nil, nil, fmt.Errorf("update needs title and/or tags")
	}
	if err := h.store.Modify(in.ID, in.Title, in.Tags); err != nil {
		return nil, nil, err
	}
	q, err := h.store.Get(in.ID) // re-read: tags merge, so the result isn't knowable from inputs
	if err != nil {
		// The Modify above already landed — say so, or a failed re-read reads as
		// a failed update and invites a duplicate retry (SQ-0122).
		return nil, nil, fmt.Errorf("update to %s was saved, but re-reading it failed: %w", in.ID, err)
	}
	// tags is always present per SQ-0052: render {} (not null) when empty so an
	// agent can tell "tags are now empty" from "tags weren't touched".
	tags := q.Tags
	if tags == nil {
		tags = map[string]string{}
	}
	return h.jsonResultVoiced(struct {
		OK    bool              `json:"ok"`
		ID    string            `json:"id"`
		Title string            `json:"title,omitempty"`
		Tags  map[string]string `json:"tags"`
	}{true, in.ID, in.Title, tags}, func(v *voice.Voice) string { return v.Updated(in.ID) })
}

func (h *handlers) questNote(ctx context.Context, req *sdk.CallToolRequest, in noteIn) (*sdk.CallToolResult, any, error) {
	if err := h.store.AppendNote(in.ID, in.Text); err != nil {
		return nil, nil, err
	}
	res, meta, err := jsonResult(struct {
		OK bool   `json:"ok"`
		ID string `json:"id"`
	}{true, in.ID})
	if err != nil {
		return res, meta, err
	}
	return h.voiced(res, func(v *voice.Voice) string { return v.NoteAdded(in.ID) }), meta, nil
}

func (h *handlers) questSetCurrent(ctx context.Context, req *sdk.CallToolRequest, in setCurrentIn) (*sdk.CallToolResult, any, error) {
	if in.Clear {
		if err := h.store.ClearCurrent(); err != nil {
			return nil, nil, err
		}
		return jsonResult(struct {
			OK bool `json:"ok"`
		}{true})
	}
	if in.ID == "" {
		return nil, nil, fmt.Errorf("set_current needs an id (or clear:true)")
	}
	if err := h.store.SetCurrent(in.ID); err != nil {
		return nil, nil, err
	}
	return h.jsonResultVoiced(struct {
		OK      bool   `json:"ok"`
		Current string `json:"current"`
	}{true, in.ID}, func(v *voice.Voice) string { return v.QuestSelected(in.ID) })
}

func (h *handlers) questLinkCommit(ctx context.Context, req *sdk.CallToolRequest, in shaIn) (*sdk.CallToolResult, any, error) {
	res, err := h.store.Link(in.SHA)
	if err != nil {
		return nil, nil, err
	}
	// Surface skipped (unknown-quest) trailer ids so an agent can correct a typo
	// instead of assuming the link landed (SQ-0119).
	return h.jsonResultVoiced(struct {
		OK      bool     `json:"ok"`
		SHA     string   `json:"sha"`
		Linked  []string `json:"linked,omitempty"`
		Skipped []string `json:"skipped,omitempty"`
	}{true, in.SHA, res.Linked, res.Skipped}, func(v *voice.Voice) string { return v.Linked(in.SHA) })
}

// questRelinkCommit is the inverse-repair for a rebase: the old sha is matched by
// prefix against the recorded hashes (never git-resolved — it is typically
// dangling), while the new sha is resolved to its canonical hash (SQ-0049).
func (h *handlers) questRelinkCommit(ctx context.Context, req *sdk.CallToolRequest, in relinkIn) (*sdk.CallToolResult, any, error) {
	newSHA, err := h.store.ResolveCommit(in.NewSHA)
	if err != nil {
		return nil, nil, fmt.Errorf("new commit %q not found: %w", in.NewSHA, err)
	}
	if err := h.store.ReplaceCommit(in.ID, in.OldSHA, newSHA); err != nil {
		return nil, nil, err
	}
	return h.jsonResultVoiced(struct {
		OK     bool   `json:"ok"`
		ID     string `json:"id"`
		OldSHA string `json:"old_sha"`
		NewSHA string `json:"new_sha"`
	}{true, in.ID, in.OldSHA, newSHA}, func(v *voice.Voice) string { return v.Relinked(in.ID) })
}

func (h *handlers) questUnlinkCommit(ctx context.Context, req *sdk.CallToolRequest, in unlinkIn) (*sdk.CallToolResult, any, error) {
	if err := h.store.RemoveCommit(in.ID, in.SHA); err != nil {
		return nil, nil, err
	}
	return h.jsonResultVoiced(struct {
		OK  bool   `json:"ok"`
		ID  string `json:"id"`
		SHA string `json:"sha"`
	}{true, in.ID, in.SHA}, func(v *voice.Voice) string { return v.Unlinked(in.ID) })
}
