package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/google/jsonschema-go/jsonschema"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
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
		string(quest.StatusOpen), string(quest.StatusPartial), string(quest.StatusDone),
		string(quest.StatusDeferred), string(quest.StatusDiscarded),
	}
	types := []string{string(quest.TypeBug), string(quest.TypeFeature)}
	prios := []string{string(quest.PriorityHigh), string(quest.PriorityLow)}

	sdk.AddTool(s, &sdk.Tool{Name: "quest_new", Description: "Capture a new quest — an issue, task, or follow-up. Note a tangent that surfaced mid-task without derailing: restate the idea in a line. Mechanical git context (branch/head/cwd/current) is recorded automatically; pass a one-sentence narrative in context. Set type/priority only when the request makes them obvious (a crash or regression is a bug; explicit \"urgent\"/\"critical\"/\"blocking\" is high), else omit them.",
		InputSchema: enumSchema[newIn](map[string][]string{"type": types, "priority": prios})}, h.questNew)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_list", Description: "List quests, optionally filtered by status/type/priority/tags (AND).",
		InputSchema: enumSchema[listIn](map[string][]string{"status": statuses, "type": types, "priority": prios})}, h.questList)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_show", Description: "Show one quest by id."}, h.questShow)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_get_current", Description: "Return this worktree's current quest id (empty if none)."}, h.questGetCurrent)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_set_status", Description: "Set a quest's lifecycle status (open|partial|done|deferred|discarded).",
		InputSchema: enumSchema[statusIn](map[string][]string{"status": statuses})}, h.questSetStatus)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_reclassify", Description: "Change a quest's type and/or priority.",
		InputSchema: enumSchema[reclassifyIn](map[string][]string{"type": types, "priority": prios})}, h.questReclassify)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_update", Description: "Update a quest's title and/or tags (a tag with an empty value is deleted)."}, h.questUpdate)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_note", Description: "Append a timestamped note to a quest's body (non-destructive)."}, h.questNote)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_set_current", Description: "Set this worktree's current quest by id (the quest you're actively working on), or clear it with clear:true. While a quest is current, the git hooks link the commits you make to it automatically."}, h.questSetCurrent)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_link_commit", Description: "Apply a commit's Quest:/Completes: trailers to the referenced quests."}, h.questLinkCommit)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_relink_commit", Description: "Repoint a recorded commit after a rebase rewrote its hash: replace old_sha (matched by prefix, never resolved — it may be dangling) with new_sha."}, h.questRelinkCommit)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_unlink_commit", Description: "Remove a recorded commit from a quest (sha matched by prefix)."}, h.questUnlinkCommit)
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

// result re-reads a quest by id and returns it — used by the mutating tools so
// the agent sees the post-mutation state.
func (h *handlers) result(id string) (*sdk.CallToolResult, any, error) {
	q, err := h.store.Get(id)
	if err != nil {
		return nil, nil, err
	}
	return jsonResult(q)
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

// resultVoiced re-reads a quest by id (post-mutation state) and returns it with an
// optional voice block (see voiced).
func (h *handlers) resultVoiced(id string, line func(*voice.Voice) string) (*sdk.CallToolResult, any, error) {
	res, meta, err := h.result(id)
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
			return nil, nil, err
		}
	}
	res, meta, err := jsonResult(q)
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
	filtered := make([]*quest.Quest, 0, len(all))
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
		filtered = append(filtered, q)
	}
	return jsonResult(filtered)
}

func (h *handlers) questShow(ctx context.Context, req *sdk.CallToolRequest, in idIn) (*sdk.CallToolResult, any, error) {
	q, err := h.store.Get(in.ID)
	if err != nil {
		return nil, nil, err
	}
	return jsonResult(q)
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

type statusIn struct {
	ID     string `json:"id" jsonschema:"the quest id"`
	Status string `json:"status" jsonschema:"open|partial|done|deferred|discarded"`
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
	return h.resultVoiced(in.ID, func(v *voice.Voice) string { return v.StatusSet(in.ID, quest.Status(in.Status)) })
}

func (h *handlers) questReclassify(ctx context.Context, req *sdk.CallToolRequest, in reclassifyIn) (*sdk.CallToolResult, any, error) {
	if in.Type == "" && in.Priority == "" {
		return nil, nil, fmt.Errorf("reclassify needs type and/or priority")
	}
	if err := h.store.Reclassify(in.ID, quest.Type(in.Type), quest.Priority(in.Priority)); err != nil {
		return nil, nil, err
	}
	return h.result(in.ID)
}

func (h *handlers) questUpdate(ctx context.Context, req *sdk.CallToolRequest, in updateIn) (*sdk.CallToolResult, any, error) {
	if in.Title == "" && len(in.Tags) == 0 {
		return nil, nil, fmt.Errorf("update needs title and/or tags")
	}
	if err := h.store.Modify(in.ID, in.Title, in.Tags); err != nil {
		return nil, nil, err
	}
	return h.result(in.ID)
}

func (h *handlers) questNote(ctx context.Context, req *sdk.CallToolRequest, in noteIn) (*sdk.CallToolResult, any, error) {
	if err := h.store.AppendNote(in.ID, in.Text); err != nil {
		return nil, nil, err
	}
	return h.resultVoiced(in.ID, func(v *voice.Voice) string { return v.NoteAdded(in.ID) })
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
	return jsonResult(struct {
		OK      bool   `json:"ok"`
		Current string `json:"current"`
	}{true, in.ID})
}

func (h *handlers) questLinkCommit(ctx context.Context, req *sdk.CallToolRequest, in shaIn) (*sdk.CallToolResult, any, error) {
	if err := h.store.Link(in.SHA); err != nil {
		return nil, nil, err
	}
	return jsonResult(struct {
		OK  bool   `json:"ok"`
		SHA string `json:"sha"`
	}{true, in.SHA})
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
	return h.result(in.ID)
}

func (h *handlers) questUnlinkCommit(ctx context.Context, req *sdk.CallToolRequest, in unlinkIn) (*sdk.CallToolResult, any, error) {
	if err := h.store.RemoveCommit(in.ID, in.SHA); err != nil {
		return nil, nil, err
	}
	return h.result(in.ID)
}
