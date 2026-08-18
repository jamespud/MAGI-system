package assistant_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/jamespud/magi/backend/application/assistant"
	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// --- fakes ---

type memConvRepo struct {
	mu       sync.Mutex
	convs    map[string]*entity.Conversation
	msgs     map[string][]*entity.ConversationMessage
	deleted  map[string]bool
	lastList int
}

func newMemConvRepo() *memConvRepo {
	return &memConvRepo{convs: map[string]*entity.Conversation{}, msgs: map[string][]*entity.ConversationMessage{}, deleted: map[string]bool{}}
}

func (r *memConvRepo) Create(ctx context.Context, conv *entity.Conversation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.convs[conv.ID] = conv
	return nil
}

func (r *memConvRepo) Get(ctx context.Context, id string) (*entity.Conversation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.convs[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return c, nil
}

func (r *memConvRepo) ListByUser(ctx context.Context, userID int64, limit, offset int) ([]*entity.Conversation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*entity.Conversation
	for _, c := range r.convs {
		if userID != 0 && c.UserID != 0 && c.UserID != userID {
			continue
		}
		out = append(out, c)
	}
	r.lastList = len(out)
	return out, nil
}

func (r *memConvRepo) Delete(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleted[id] = true
	delete(r.convs, id)
	delete(r.msgs, id)
	return nil
}

func (r *memConvRepo) AppendMessage(ctx context.Context, msg *entity.ConversationMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs[msg.ConversationID] = append(r.msgs[msg.ConversationID], msg)
	return nil
}

func (r *memConvRepo) ListMessages(ctx context.Context, conversationID string, limit int) ([]*entity.ConversationMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.msgs[conversationID], nil
}

type memResRepo struct {
	byCase map[string]*entity.Resolution
}

func (r *memResRepo) Create(ctx context.Context, res *entity.Resolution) error { return nil }
func (r *memResRepo) Get(ctx context.Context, caseID string) (*entity.Resolution, error) {
	res, ok := r.byCase[caseID]
	if !ok {
		return nil, errors.New("not found")
	}
	return res, nil
}

// --- tests ---

func newConvService(repo port.ConversationRepository, resRepo port.ResolutionRepository) *assistant.Service {
	dec := decision.NewService(askOrch{}, decision.ServiceConfig{MaxDebateRounds: 1},
		decision.WithCaseRepo(&askCaseRepo{}), decision.WithResolutionRepo(resRepo))
	return assistant.NewService(dec, assistant.WithConversationRepository(repo))
}

func TestConversation_StartsNewThreadAndRecordsTurns(t *testing.T) {
	repo := newMemConvRepo()
	svc := newConvService(repo, &memResRepo{byCase: map[string]*entity.Resolution{}})

	cs, conv, err := svc.AskInConversation(context.Background(), 7, "", "Should we adopt Rust?", "", nil)
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	if conv == nil || conv.ID == "" {
		t.Fatal("expected a new conversation")
	}
	if !strings.HasPrefix(conv.ID, "conv-") {
		t.Fatalf("conversation ID prefix: %q", conv.ID)
	}
	if conv.Title != "Should we adopt Rust?" {
		t.Fatalf("title: %q", conv.Title)
	}
	if conv.UserID != 7 {
		t.Fatalf("conversation owner: %d", conv.UserID)
	}
	msgs := repo.msgs[conv.ID]
	if len(msgs) != 2 {
		t.Fatalf("expected user+assistant messages, got %d", len(msgs))
	}
	if msgs[0].Role != entity.ConversationRoleUser || msgs[0].Content != "Should we adopt Rust?" {
		t.Fatalf("user message wrong: %+v", msgs[0])
	}
	if msgs[1].Role != entity.ConversationRoleAssistant || msgs[1].CaseID != cs.ID {
		t.Fatalf("assistant message wrong: %+v", msgs[1])
	}
}

func TestConversation_FollowUpHydratesPriorResolution(t *testing.T) {
	repo := newMemConvRepo()
	resRepo := &memResRepo{byCase: map[string]*entity.Resolution{}}
	svc := newConvService(repo, resRepo)

	// Turn 1: seeds the thread and a linked case.
	_, conv, err := svc.AskInConversation(context.Background(), 7, "", "Should we adopt Rust?", "", nil)
	if err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	msgs := repo.msgs[conv.ID]
	firstCaseID := msgs[1].CaseID
	resRepo.byCase[firstCaseID] = &entity.Resolution{
		CaseID:        firstCaseID,
		FinalDecision: entity.VoteDecisionApprove,
		Consensus:     entity.ConsensusResult{Outcome: entity.ConsensusStrongApproval, Round: 1},
	}

	// Turn 2: follow-up inside the same thread.
	cs2, conv2, err := svc.AskInConversation(context.Background(), 7, conv.ID, "What about the migration cost?", "budget cap 50k", nil)
	if err != nil {
		t.Fatalf("turn 2: %v", err)
	}
	if conv2.ID != conv.ID {
		t.Fatalf("expected same conversation, got %q", conv2.ID)
	}
	if !strings.Contains(cs2.Context, "[Conversation history]") {
		t.Fatalf("expected hydrated history, got %q", cs2.Context)
	}
	if !strings.Contains(cs2.Context, "Should we adopt Rust?") {
		t.Fatalf("history missing first question: %q", cs2.Context)
	}
	if !strings.Contains(cs2.Context, firstCaseID) {
		t.Fatalf("history missing linked case: %q", cs2.Context)
	}
	if !strings.Contains(cs2.Context, "budget cap 50k") {
		t.Fatalf("extra background dropped: %q", cs2.Context)
	}
}

func TestConversation_RejectsForeignThread(t *testing.T) {
	repo := newMemConvRepo()
	svc := newConvService(repo, &memResRepo{byCase: map[string]*entity.Resolution{}})

	_, conv, err := svc.AskInConversation(context.Background(), 7, "", "mine?", "", nil)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, _, err := svc.AskInConversation(context.Background(), 8, conv.ID, "hijack?", "", nil); err != assistant.ErrForbidden {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
	if _, _, err := svc.GetConversation(context.Background(), 8, conv.ID); err != assistant.ErrForbidden {
		t.Fatalf("get expected ErrForbidden, got %v", err)
	}
}

func TestConversation_GetListDelete(t *testing.T) {
	repo := newMemConvRepo()
	svc := newConvService(repo, &memResRepo{byCase: map[string]*entity.Resolution{}})

	_, conv, err := svc.AskInConversation(context.Background(), 9, "", "first question", "", nil)
	if err != nil {
		t.Fatalf("ask: %v", err)
	}

	got, msgs, err := svc.GetConversation(context.Background(), 9, conv.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != conv.ID || len(msgs) != 2 {
		t.Fatalf("get wrong: %+v %d", got, len(msgs))
	}

	list, err := svc.ListConversations(context.Background(), 9, 10, 0)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %d", err, len(list))
	}

	if err := svc.DeleteConversation(context.Background(), 9, conv.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, _, err := svc.GetConversation(context.Background(), 9, conv.ID); err != assistant.ErrConversationNotFound {
		t.Fatalf("expected not found after delete, got %v", err)
	}
}
