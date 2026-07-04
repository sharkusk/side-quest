package mcp

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sharkusk/side-quest/internal/config"
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
	srv := NewServer(s, "test")
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

// TestServerAdvertisesGivenVersion (SQ-0044): the version NewServer is handed —
// the build's main.version — is exactly what the server advertises to clients, so
// the MCP-advertised version can never drift from what `side-quest version` reports.
func TestServerAdvertisesGivenVersion(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	srv := NewServer(s, "9.9.9-test")
	serverT, clientT := sdk.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, serverT, nil)
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

	info := cs.InitializeResult().ServerInfo
	if info == nil || info.Version != "9.9.9-test" {
		t.Fatalf("advertised ServerInfo = %+v, want version 9.9.9-test", info)
	}
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

// enumOf marshals a tool's InputSchema and reads the enum values declared on
// one property (empty if the property has no enum).
func enumOf(t *testing.T, schema any, prop string) []string {
	t.Helper()
	raw, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	var sch jsonschema.Schema
	if err := json.Unmarshal(raw, &sch); err != nil {
		t.Fatal(err)
	}
	p := sch.Properties[prop]
	if p == nil {
		return nil
	}
	out := make([]string, len(p.Enum))
	for i, v := range p.Enum {
		out[i], _ = v.(string)
	}
	sort.Strings(out)
	return out
}

func TestToolSchemasExposeEnums(t *testing.T) {
	cs, ctx := dialTest(t, newTestStore(t))
	lt, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]*sdk.Tool{}
	for _, tl := range lt.Tools {
		byName[tl.Name] = tl
	}

	statuses := []string{"deferred", "discarded", "done", "open", "partial"} // sorted
	types := []string{"bug", "feature"}
	prios := []string{"high", "low"}
	cases := []struct {
		tool, prop string
		want       []string
	}{
		{"quest_new", "type", types},
		{"quest_new", "priority", prios},
		{"quest_list", "status", statuses},
		{"quest_list", "type", types},
		{"quest_list", "priority", prios},
		{"quest_set_status", "status", statuses},
		{"quest_reclassify", "type", types},
		{"quest_reclassify", "priority", prios},
	}
	for _, c := range cases {
		tl := byName[c.tool]
		if tl == nil {
			t.Fatalf("tool %s not registered", c.tool)
		}
		got := enumOf(t, tl.InputSchema, c.prop)
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("%s.%s enum = %v, want %v", c.tool, c.prop, got, c.want)
		}
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

func TestQuestListTagFilter(t *testing.T) {
	cs, ctx := dialTest(t, newTestStore(t))
	cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_new", Arguments: map[string]any{"title": "a", "tags": map[string]any{"area": "cli", "phase": "5"}}})
	cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_new", Arguments: map[string]any{"title": "b", "tags": map[string]any{"area": "mcp"}}})

	res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_list", Arguments: map[string]any{"tags": map[string]any{"area": "cli"}}})
	if err != nil {
		t.Fatal(err)
	}
	var got []quest.Quest
	if err := json.Unmarshal([]byte(contentText(t, res)), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Tags["area"] != "cli" {
		t.Fatalf("tag filter wrong, want one area=cli quest: %+v", got)
	}
}

// TestMutationVoiceBlock (SQ-0028): a mutation response keeps a neutral JSON
// content[0] for parsers, but under a flavored tone appends a SECOND text block
// carrying a voice line that names the quest. Under plain there is no second block,
// so machine consumers that select plain see exactly the JSON. Reads never voice.
func TestMutationVoiceBlock(t *testing.T) {
	// dcc (the default tone): quest_new gets a second, flavored block.
	cs, ctx := dialTest(t, newTestStore(t))
	res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_new", Arguments: map[string]any{"title": "ship it"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("quest_new error: %s", contentText(t, res))
	}
	var created quest.Quest
	if err := json.Unmarshal([]byte(contentText(t, res)), &created); err != nil {
		t.Fatalf("content[0] must stay neutral JSON: %v", err)
	}
	if len(res.Content) != 2 {
		t.Fatalf("dcc mutation should append a voice block; got %d content block(s)", len(res.Content))
	}
	flavor, ok := res.Content[1].(*sdk.TextContent)
	if !ok || !strings.Contains(flavor.Text, created.ID) {
		t.Errorf("voice block should name %q, got %+v", created.ID, res.Content[1])
	}

	// A read (quest_show) never appends a voice block.
	res, _ = cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_show", Arguments: map[string]any{"id": created.ID}})
	if len(res.Content) != 1 {
		t.Errorf("reads must stay single-block; got %d", len(res.Content))
	}

	// plain tone: no second block on a mutation either.
	s := newTestStore(t)
	if err := s.SetTone(config.TonePlain); err != nil {
		t.Fatal(err)
	}
	csP, ctxP := dialTest(t, s)
	resP, err := csP.CallTool(ctxP, &sdk.CallToolParams{Name: "quest_new", Arguments: map[string]any{"title": "quiet"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resP.Content) != 1 {
		t.Fatalf("plain mutation must stay single-block; got %d", len(resP.Content))
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

	res, _ = cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_update", Arguments: map[string]any{"id": q.ID, "tags": map[string]any{}}})
	if !res.IsError {
		t.Fatal("update with empty tags object and no title should be a tool error")
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
