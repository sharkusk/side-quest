package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sharkusk/side-quest/internal/capture"
	"github.com/sharkusk/side-quest/internal/quest"
)

// register wires every tool onto the server. Task 4 appends the mutation tools.
func (h *handlers) register(s *sdk.Server) {
	sdk.AddTool(s, &sdk.Tool{Name: "quest_new", Description: "Capture a new quest. Mechanical git context (branch/head/cwd/current) is recorded automatically; pass a one-sentence narrative in context."}, h.questNew)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_list", Description: "List quests, optionally filtered by status/type/priority (AND)."}, h.questList)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_show", Description: "Show one quest by id."}, h.questShow)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_get_current", Description: "Return this worktree's current quest id (empty if none)."}, h.questGetCurrent)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_set_status", Description: "Set a quest's lifecycle status (open|partial|done|deferred|discarded)."}, h.questSetStatus)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_reclassify", Description: "Change a quest's type and/or priority."}, h.questReclassify)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_update", Description: "Update a quest's title and/or tags (a tag with an empty value is deleted)."}, h.questUpdate)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_note", Description: "Append a timestamped note to a quest's body (non-destructive)."}, h.questNote)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_set_current", Description: "Set this worktree's current quest by id, or clear it with clear:true."}, h.questSetCurrent)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_link_commit", Description: "Apply a commit's Quest:/Completes: trailers to the referenced quests."}, h.questLinkCommit)
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
	Status   string `json:"status,omitempty" jsonschema:"filter by status"`
	Type     string `json:"type,omitempty" jsonschema:"filter by type (bug|feature)"`
	Priority string `json:"priority,omitempty" jsonschema:"filter by priority (high|low)"`
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

// --- handlers ---

func (h *handlers) questNew(ctx context.Context, req *sdk.CallToolRequest, in newIn) (*sdk.CallToolResult, any, error) {
	cur, _ := h.store.Current()
	dir, _ := os.Getwd()
	var parts []string
	if mech := capture.Mechanical(dir, cur); mech != "" {
		parts = append(parts, mech)
	}
	if in.Context != "" {
		parts = append(parts, in.Context)
	}
	q, err := h.store.Create(in.Title, strings.Join(parts, "\n\n"), quest.Type(in.Type), quest.Priority(in.Priority), in.Tags)
	if err != nil {
		return nil, nil, err
	}
	if in.SetCurrent {
		if err := h.store.SetCurrent(q.ID); err != nil {
			return nil, nil, err
		}
	}
	return jsonResult(q)
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

func (h *handlers) questSetStatus(ctx context.Context, req *sdk.CallToolRequest, in statusIn) (*sdk.CallToolResult, any, error) {
	if err := h.store.SetStatus(in.ID, quest.Status(in.Status)); err != nil {
		return nil, nil, err
	}
	return h.result(in.ID)
}

func (h *handlers) questReclassify(ctx context.Context, req *sdk.CallToolRequest, in reclassifyIn) (*sdk.CallToolResult, any, error) {
	if in.Type == "" && in.Priority == "" {
		return nil, nil, fmt.Errorf("reclassify needs type and/or priority")
	}
	if in.Type != "" {
		if err := h.store.SetType(in.ID, quest.Type(in.Type)); err != nil {
			return nil, nil, err
		}
	}
	if in.Priority != "" {
		if err := h.store.SetPriority(in.ID, quest.Priority(in.Priority)); err != nil {
			return nil, nil, err
		}
	}
	return h.result(in.ID)
}

func (h *handlers) questUpdate(ctx context.Context, req *sdk.CallToolRequest, in updateIn) (*sdk.CallToolResult, any, error) {
	if in.Title == "" && in.Tags == nil {
		return nil, nil, fmt.Errorf("update needs title and/or tags")
	}
	if in.Title != "" {
		if err := h.store.SetTitle(in.ID, in.Title); err != nil {
			return nil, nil, err
		}
	}
	if in.Tags != nil {
		if err := h.store.MergeTags(in.ID, in.Tags); err != nil {
			return nil, nil, err
		}
	}
	return h.result(in.ID)
}

func (h *handlers) questNote(ctx context.Context, req *sdk.CallToolRequest, in noteIn) (*sdk.CallToolResult, any, error) {
	if err := h.store.AppendNote(in.ID, in.Text); err != nil {
		return nil, nil, err
	}
	return h.result(in.ID)
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
