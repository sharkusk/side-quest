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
