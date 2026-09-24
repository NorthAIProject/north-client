package conversations_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/conversations"
)

// Origin and label survive the round trip, and a reply stays a reply.
func TestProvenanceIsStored(t *testing.T) {
	svc, c := newConversation(t)
	ctx := context.Background()

	reply, err := svc.AppendModelMessage(ctx, c.ID, "A reply.", nil, "m", "p", nil, conversations.Provenance{})
	if err != nil {
		t.Fatal(err)
	}
	proactive, err := svc.AppendModelMessage(ctx, c.ID, "Morning.", nil, "m", "p", nil,
		conversations.Proactive(conversations.SourceDailyBriefing))
	if err != nil {
		t.Fatal(err)
	}

	history, err := svc.History(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[uuid.UUID]conversations.Message{}
	for _, m := range history {
		byID[m.ID] = m
	}
	if got := byID[reply.ID]; got.Origin != conversations.OriginReply || got.SourceLabel != "" || got.IsProactive() {
		t.Errorf("reply stored as %q/%q", got.Origin, got.SourceLabel)
	}
	if got := byID[proactive.ID]; !got.IsProactive() || got.SourceLabel != conversations.SourceDailyBriefing {
		t.Errorf("proactive stored as %q/%q", got.Origin, got.SourceLabel)
	}
}

// Nobody asked for a proactive turn, so it must not break the user/model
// alternation providers expect: folded into the model turn before it, or given
// a stand-in user turn when it opens the thread.
func TestProactiveTurnsKeepAlternation(t *testing.T) {
	briefing := conversations.Message{
		Role: ai.RoleModel, Content: "Morning.",
		Origin: conversations.OriginProactive, SourceLabel: conversations.SourceDailyBriefing,
	}

	opening := conversations.ToAIMessages([]conversations.Message{briefing})
	if len(opening) != 2 || opening[0].Role != ai.RoleUser || opening[1].Role != ai.RoleModel {
		t.Fatalf("thread opened by a briefing = %+v", opening)
	}
	if !strings.Contains(opening[1].Text(), "Daily briefing") || !strings.Contains(opening[1].Text(), "Morning.") {
		t.Errorf("proactive turn is not marked: %q", opening[1].Text())
	}

	afterReply := conversations.ToAIMessages([]conversations.Message{
		{Role: ai.RoleUser, Content: "How did I sleep?"},
		{Role: ai.RoleModel, Content: "Fine."},
		briefing,
		{Role: ai.RoleUser, Content: "Thanks."},
	})
	roles := make([]ai.Role, 0, len(afterReply))
	for _, m := range afterReply {
		roles = append(roles, m.Role)
	}
	want := []ai.Role{ai.RoleUser, ai.RoleModel, ai.RoleUser}
	if len(roles) != len(want) {
		t.Fatalf("roles = %v, want %v", roles, want)
	}
	for i := range want {
		if roles[i] != want[i] {
			t.Fatalf("roles = %v, want %v", roles, want)
		}
	}
	if !strings.Contains(afterReply[1].Text(), "Fine.") || !strings.Contains(afterReply[1].Text(), "Morning.") {
		t.Errorf("merged model turn = %q", afterReply[1].Text())
	}
}

// A briefing never lands in a reflection or in a thread waiting on an
// approval card; it goes to the next-latest chat instead.
func TestProactiveTargetSkipsReflectionsAndPendingApprovals(t *testing.T) {
	svc, chat := newConversation(t)
	ctx := context.Background()

	if _, err := svc.AppendUserMessage(ctx, chat.ID, "hello", nil); err != nil {
		t.Fatal(err)
	}
	waiting, err := svc.Start(ctx, chat.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.AppendToolCalls(ctx, waiting.ID, []ai.ToolCall{{ID: "c1", Name: "create_watch"}}); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.StartKind(ctx, chat.UserID, conversations.KindReflection); err != nil {
		t.Fatal(err)
	}

	msg, err := svc.PostProactive(ctx, chat.UserID, waiting.ID, "Morning.", conversations.SourceDailyBriefing)
	if err != nil {
		t.Fatal(err)
	}
	if msg.ConversationID != chat.ID {
		t.Errorf("posted into %s, want the plain chat %s", msg.ConversationID, chat.ID)
	}
}
