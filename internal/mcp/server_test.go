package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sharkusk/side-quest/internal/gitcmd"
	"github.com/sharkusk/side-quest/internal/quest"
	"github.com/sharkusk/side-quest/internal/store"
)

// newTestStore makes a throwaway git repo with an identity and an opened store.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	g := gitcmd.New(dir)
	for _, a := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "Tester"},
	} {
		if _, err := g.Run(a...); err != nil {
			t.Fatal(err)
		}
	}
	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// dialTest wires an in-memory client to a server backed by s.
func dialTest(t *testing.T, s *store.Store) (*sdk.ClientSession, context.Context) {
	t.Helper()
	ctx := context.Background()
	srv := NewServer(s)
	serverT, clientT := sdk.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, serverT, nil) // server connects first
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs, ctx
}

func contentText(t *testing.T, res *sdk.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatal("no content in result")
	}
	tc, ok := res.Content[0].(*sdk.TextContent)
	if !ok {
		t.Fatalf("content[0] is %T, want *sdk.TextContent", res.Content[0])
	}
	return tc.Text
}

func TestListToolsExposesTen(t *testing.T) {
	cs, ctx := dialTest(t, newTestStore(t))
	lt, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(lt.Tools) != 10 {
		names := make([]string, len(lt.Tools))
		for i, tl := range lt.Tools {
			names[i] = tl.Name
		}
		t.Fatalf("want 10 tools, got %d: %v", len(lt.Tools), names)
	}
}

func TestQuestNewThenShow(t *testing.T) {
	cs, ctx := dialTest(t, newTestStore(t))

	res, err := cs.CallTool(ctx, &sdk.CallToolParams{
		Name:      "quest_new",
		Arguments: map[string]any{"title": "Fix parser", "type": "bug", "context": "saw a stack trace"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("quest_new tool error: %s", contentText(t, res))
	}
	var created quest.Quest
	if err := json.Unmarshal([]byte(contentText(t, res)), &created); err != nil {
		t.Fatalf("json: %v\n%s", err, contentText(t, res))
	}
	if created.Title != "Fix parser" || created.Type != quest.TypeBug {
		t.Fatalf("bad created quest: %+v", created)
	}
	if created.Context == "" {
		t.Fatal("expected mechanical+narrative context to be recorded")
	}

	res, err = cs.CallTool(ctx, &sdk.CallToolParams{
		Name:      "quest_show",
		Arguments: map[string]any{"id": created.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	var shown quest.Quest
	if err := json.Unmarshal([]byte(contentText(t, res)), &shown); err != nil {
		t.Fatal(err)
	}
	if shown.ID != created.ID {
		t.Fatalf("show returned %q, want %q", shown.ID, created.ID)
	}
}

func TestQuestListFilterAndInvalid(t *testing.T) {
	cs, ctx := dialTest(t, newTestStore(t))
	cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_new", Arguments: map[string]any{"title": "a", "type": "bug"}})
	cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_new", Arguments: map[string]any{"title": "b", "type": "feature"}})

	res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_list", Arguments: map[string]any{"type": "bug"}})
	if err != nil {
		t.Fatal(err)
	}
	var bugs []quest.Quest
	if err := json.Unmarshal([]byte(contentText(t, res)), &bugs); err != nil {
		t.Fatal(err)
	}
	if len(bugs) != 1 || bugs[0].Type != quest.TypeBug {
		t.Fatalf("filter wrong: %+v", bugs)
	}

	res, err = cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_list", Arguments: map[string]any{"type": "bugg"}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("invalid filter should be a tool error")
	}
}

func TestGetCurrentEmpty(t *testing.T) {
	cs, ctx := dialTest(t, newTestStore(t))
	res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_get_current", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("get_current errored: %s", contentText(t, res))
	}
}

func TestSetStatusAndReclassify(t *testing.T) {
	cs, ctx := dialTest(t, newTestStore(t))
	res, _ := cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_new", Arguments: map[string]any{"title": "x"}})
	var q quest.Quest
	json.Unmarshal([]byte(contentText(t, res)), &q)

	res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_set_status", Arguments: map[string]any{"id": q.ID, "status": "done"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("set_status error: %s", contentText(t, res))
	}
	var done quest.Quest
	json.Unmarshal([]byte(contentText(t, res)), &done)
	if done.Status != quest.StatusDone {
		t.Fatalf("status not set: %+v", done)
	}

	res, _ = cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_reclassify", Arguments: map[string]any{"id": q.ID, "priority": "high"}})
	var re quest.Quest
	json.Unmarshal([]byte(contentText(t, res)), &re)
	if re.Priority != quest.PriorityHigh {
		t.Fatalf("reclassify failed: %+v", re)
	}

	res, _ = cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_set_status", Arguments: map[string]any{"id": q.ID, "status": "nope"}})
	if !res.IsError {
		t.Fatal("invalid status should be a tool error")
	}
	res, _ = cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_reclassify", Arguments: map[string]any{"id": q.ID}})
	if !res.IsError {
		t.Fatal("reclassify with no field should be a tool error")
	}
}

func TestUpdateAndNote(t *testing.T) {
	cs, ctx := dialTest(t, newTestStore(t))
	res, _ := cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_new", Arguments: map[string]any{"title": "orig", "tags": map[string]any{"keep": "yes"}}})
	var q quest.Quest
	json.Unmarshal([]byte(contentText(t, res)), &q)

	res, _ = cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_update", Arguments: map[string]any{"id": q.ID, "title": "renamed", "tags": map[string]any{"area": "mcp", "keep": ""}}})
	var up quest.Quest
	json.Unmarshal([]byte(contentText(t, res)), &up)
	if up.Title != "renamed" || up.Tags["area"] != "mcp" {
		t.Fatalf("update wrong: %+v", up)
	}
	if _, ok := up.Tags["keep"]; ok {
		t.Fatalf("empty tag value should delete: %+v", up.Tags)
	}

	res, _ = cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_note", Arguments: map[string]any{"id": q.ID, "text": "learned something"}})
	var noted quest.Quest
	json.Unmarshal([]byte(contentText(t, res)), &noted)
	if !strings.Contains(noted.Body, "learned something") {
		t.Fatalf("note not appended: body=%q", noted.Body)
	}

	res, _ = cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_update", Arguments: map[string]any{"id": q.ID}})
	if !res.IsError {
		t.Fatal("update with nothing to change should be a tool error")
	}
}

func TestSetCurrentAndLink(t *testing.T) {
	cs, ctx := dialTest(t, newTestStore(t))
	res, _ := cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_new", Arguments: map[string]any{"title": "cur"}})
	var q quest.Quest
	json.Unmarshal([]byte(contentText(t, res)), &q)

	if _, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_set_current", Arguments: map[string]any{"id": q.ID}}); err != nil {
		t.Fatal(err)
	}
	res, _ = cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_get_current", Arguments: map[string]any{}})
	if !strings.Contains(contentText(t, res), q.ID) {
		t.Fatalf("current not set: %s", contentText(t, res))
	}
	// clear
	cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_set_current", Arguments: map[string]any{"clear": true}})
	res, _ = cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_get_current", Arguments: map[string]any{}})
	if strings.Contains(contentText(t, res), q.ID) {
		t.Fatalf("current not cleared: %s", contentText(t, res))
	}
	// link_commit tolerates an unknown/most-any sha argument shape (Link is tolerant of unknown ids)
	if _, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_link_commit", Arguments: map[string]any{"sha": "HEAD"}}); err != nil {
		t.Fatal(err)
	}
}
