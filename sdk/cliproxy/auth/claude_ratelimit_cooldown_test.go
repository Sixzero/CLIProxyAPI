package auth

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
)

func TestAuthManager_ConcurrentSuccessDoesNotClearActiveCredentialCooldown(t *testing.T) {
	now := time.Now()
	sevenDayReset := now.Add(7 * 24 * time.Hour)

	manager := NewManager(nil, nil, nil)

	baseID := uuid.NewString()
	auth := &Auth{
		ID:       baseID + "-claude-concurrent",
		Provider: "claude",
		Attributes: map[string]string{
			"api_key": "test-key",
		},
	}

	reg := registry.GetGlobalRegistry()
	reg.RegisterClient(auth.ID, "claude", []*registry.ModelInfo{
		{ID: "claude-3-5-sonnet-20241022"},
		{ID: "claude-3-opus-20240229"},
	})
	t.Cleanup(func() {
		reg.UnregisterClient(auth.ID)
	})

	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	// 1. Request A fails with 7d credential-scoped cooldown
	sevenDayDuration := 7 * 24 * time.Hour
	manager.MarkResult(context.Background(), Result{
		AuthID:          auth.ID,
		Provider:        "claude",
		Model:           "claude-3-5-sonnet-20241022",
		Success:         false,
		RetryAfter:      &sevenDayDuration,
		CredentialScope: true,
		Error:           &Error{HTTPStatus: http.StatusTooManyRequests, Message: "7d limit rejected"},
	})

	// 2. An earlier in-flight request on opus returns 200 OK after the 429
	manager.MarkResult(context.Background(), Result{
		AuthID:   auth.ID,
		Provider: "claude",
		Model:    "claude-3-opus-20240229",
		Success:  true,
	})

	// 3. The credential MUST still be blocked for all models
	updatedAuth, ok := manager.GetByID(auth.ID)
	if !ok || updatedAuth == nil {
		t.Fatal("auth not found")
	}
	if !updatedAuth.Quota.Exceeded || !updatedAuth.Quota.NextRecoverAt.After(now.Add(6*24*time.Hour)) {
		t.Fatalf("auth quota was cleared or shortened by concurrent success: quota=%+v", updatedAuth.Quota)
	}

	// Selecting any model on this credential must be blocked locally
	for _, m := range []string{"claude-3-5-sonnet-20241022", "claude-3-opus-20240229", "claude-3-7-sonnet-20250219"} {
		blocked, reason, next := isAuthBlockedForModel(updatedAuth, m, time.Now())
		if !blocked {
			t.Fatalf("model %q was unblocked despite active 7d credential cooldown", m)
		}
		if reason != blockReasonCooldown || next.Before(sevenDayReset.Add(-time.Minute)) {
			t.Fatalf("model %q block reason=%v next=%v, want cooldown ~7d", m, reason, next)
		}
	}
}

func TestAuthManager_UpdatePreservesActiveCredentialCooldown(t *testing.T) {
	now := time.Now()
	manager := NewManager(nil, nil, nil)

	baseID := uuid.NewString()
	auth := &Auth{
		ID:       baseID + "-claude-update",
		Provider: "claude",
		Attributes: map[string]string{
			"api_key": "test-key",
		},
	}

	reg := registry.GetGlobalRegistry()
	reg.RegisterClient(auth.ID, "claude", []*registry.ModelInfo{
		{ID: "claude-3-5-sonnet-20241022"},
	})
	t.Cleanup(func() {
		reg.UnregisterClient(auth.ID)
	})

	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	sevenDayDuration := 7 * 24 * time.Hour
	manager.MarkResult(context.Background(), Result{
		AuthID:          auth.ID,
		Provider:        "claude",
		Model:           "claude-3-5-sonnet-20241022",
		Success:         false,
		RetryAfter:      &sevenDayDuration,
		CredentialScope: true,
		Error:           &Error{HTTPStatus: http.StatusTooManyRequests, Message: "7d limit rejected"},
	})

	// Reload/update auth (e.g. config reload or token refresh)
	updatedAuth := &Auth{
		ID:       auth.ID,
		Provider: "claude",
		Attributes: map[string]string{
			"api_key": "test-key-updated",
		},
	}
	if _, err := manager.Update(context.Background(), updatedAuth); err != nil {
		t.Fatalf("update auth: %v", err)
	}

	persistedAuth, ok := manager.GetByID(auth.ID)
	if !ok || persistedAuth == nil {
		t.Fatal("auth not found after update")
	}
	if !persistedAuth.Quota.Exceeded || persistedAuth.Quota.Reason != "credential_quota" || !persistedAuth.Quota.NextRecoverAt.After(now.Add(6*24*time.Hour)) {
		t.Fatalf("credential cooldown was lost after Update: quota=%+v", persistedAuth.Quota)
	}

	blocked, reason, _ := isAuthBlockedForModel(persistedAuth, "claude-3-5-sonnet-20241022", time.Now())
	if !blocked || reason != blockReasonCooldown {
		t.Fatalf("model unblocked after Update: blocked=%v reason=%v", blocked, reason)
	}
}

func TestAuthManager_DisableCoolingDoesNotPermanentlyBlock(t *testing.T) {
	SetQuotaCooldownDisabled(true)
	t.Cleanup(func() { SetQuotaCooldownDisabled(false) })

	manager := NewManager(nil, nil, nil)

	baseID := uuid.NewString()
	auth := &Auth{
		ID:       baseID + "-claude-disable-cooling",
		Provider: "claude",
		Attributes: map[string]string{
			"api_key": "test-key",
		},
	}

	reg := registry.GetGlobalRegistry()
	reg.RegisterClient(auth.ID, "claude", []*registry.ModelInfo{
		{ID: "claude-3-5-sonnet-20241022"},
		{ID: "claude-3-opus-20240229"},
	})
	t.Cleanup(func() {
		reg.UnregisterClient(auth.ID)
	})

	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	// 429 arrives while cooling is disabled
	sevenDayDuration := 7 * 24 * time.Hour
	manager.MarkResult(context.Background(), Result{
		AuthID:          auth.ID,
		Provider:        "claude",
		Model:           "claude-3-5-sonnet-20241022",
		Success:         false,
		RetryAfter:      &sevenDayDuration,
		CredentialScope: true,
		Error:           &Error{HTTPStatus: http.StatusTooManyRequests, Message: "7d limit rejected"},
	})

	// Must NOT be blocked when cooling is disabled
	for _, m := range []string{"claude-3-5-sonnet-20241022", "claude-3-opus-20240229"} {
		updatedAuth, _ := manager.GetByID(auth.ID)
		blocked, _, _ := isAuthBlockedForModel(updatedAuth, m, time.Now())
		if blocked {
			t.Fatalf("model %q was blocked even though cooling is disabled", m)
		}
	}
}

func TestAuthManager_NonClaudeProvider_Model429DoesNotBlockSiblingModels(t *testing.T) {
	manager := NewManager(nil, nil, nil)

	baseID := uuid.NewString()
	auth := &Auth{
		ID:       baseID + "-openai-auth",
		Provider: "openai",
		Attributes: map[string]string{
			"api_key": "test-key",
		},
	}

	reg := registry.GetGlobalRegistry()
	reg.RegisterClient(auth.ID, "openai", []*registry.ModelInfo{
		{ID: "gpt-4o"},
		{ID: "gpt-4o-mini"},
	})
	t.Cleanup(func() {
		reg.UnregisterClient(auth.ID)
	})

	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	// Regular model 429 on gpt-4o (CredentialScope is false)
	manager.MarkResult(context.Background(), Result{
		AuthID:          auth.ID,
		Provider:        "openai",
		Model:           "gpt-4o",
		Success:         false,
		CredentialScope: false,
		Error:           &Error{HTTPStatus: http.StatusTooManyRequests, Message: "rate limit"},
	})

	// gpt-4o should be blocked
	updatedAuth, _ := manager.GetByID(auth.ID)
	blocked4o, _, _ := isAuthBlockedForModel(updatedAuth, "gpt-4o", time.Now())
	if !blocked4o {
		t.Fatal("gpt-4o should be blocked after 429")
	}

	// gpt-4o-mini MUST remain selectable (unaffected by sibling model 429)
	blockedMini, _, _ := isAuthBlockedForModel(updatedAuth, "gpt-4o-mini", time.Now())
	if blockedMini {
		t.Fatal("gpt-4o-mini was incorrectly blocked by sibling model 429")
	}
}

func TestAuthManager_CooldownPersistenceAcrossRestore(t *testing.T) {
	manager := NewManager(nil, nil, nil)

	baseID := uuid.NewString()
	auth := &Auth{
		ID:       baseID + "-persistence-test",
		Provider: "claude",
		Attributes: map[string]string{
			"api_key": "k",
		},
	}

	reg := registry.GetGlobalRegistry()
	reg.RegisterClient(auth.ID, "claude", []*registry.ModelInfo{{ID: "claude-3-5-sonnet-20241022"}})
	t.Cleanup(func() {
		reg.UnregisterClient(auth.ID)
	})

	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	futureCooldown := 7 * 24 * time.Hour
	manager.MarkResult(context.Background(), Result{
		AuthID:          auth.ID,
		Provider:        "claude",
		Model:           "claude-3-5-sonnet-20241022",
		Success:         false,
		RetryAfter:      &futureCooldown,
		CredentialScope: true,
		Error:           &Error{HTTPStatus: http.StatusTooManyRequests, Message: "7d rejected"},
	})

	records := manager.cooldownStateRecordsSnapshot()
	if len(records) == 0 {
		t.Fatal("expected cooldown state records to be captured")
	}

	// Create a new manager instance and restore state
	newManager := NewManager(nil, nil, nil)
	newAuth := &Auth{
		ID:       auth.ID,
		Provider: "claude",
	}
	if _, err := newManager.Register(context.Background(), newAuth); err != nil {
		t.Fatalf("register new auth: %v", err)
	}

	newManager.SetCooldownStateStore(&mockCooldownStateStore{records: records})
	if err := newManager.RestoreCooldownStates(context.Background()); err != nil {
		t.Fatalf("RestoreCooldownStates error: %v", err)
	}

	restoredAuth, ok := newManager.GetByID(auth.ID)
	if !ok || restoredAuth == nil {
		t.Fatal("restored auth not found")
	}
	if !restoredAuth.Quota.Exceeded || restoredAuth.Quota.NextRecoverAt.Before(time.Now().Add(6*24*time.Hour)) {
		t.Fatalf("restored auth quota was not preserved: quota=%+v", restoredAuth.Quota)
	}
}

type mockCooldownStateStore struct {
	records []CooldownStateRecord
}

func (s *mockCooldownStateStore) Load(context.Context) ([]CooldownStateRecord, error) {
	return s.records, nil
}

func (s *mockCooldownStateStore) Save(context.Context, []CooldownStateRecord) error {
	return nil
}

// claude-fable-5 carries a smaller subscription quota than the rest of the Claude
// line-up, yet Anthropic rejects it with the same unified 5h/7d headers used for a
// genuine account-wide limit. Widening that rejection to the credential cooled
// sibling models that were still answering 200.
func TestAuthManager_ClaudeFable429DoesNotCoolSiblingModels(t *testing.T) {
	manager := NewManager(nil, nil, nil)

	auth := &Auth{
		ID:         uuid.NewString() + "-claude-fable",
		Provider:   "claude",
		Attributes: map[string]string{"api_key": "test-key"},
	}

	reg := registry.GetGlobalRegistry()
	reg.RegisterClient(auth.ID, "claude", []*registry.ModelInfo{
		{ID: "claude-fable-5"},
		{ID: "claude-opus-5"},
	})
	t.Cleanup(func() { reg.UnregisterClient(auth.ID) })

	if _, err := manager.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	// opus-5 answers normally, then a fable-5 request is rejected with the
	// unified 7d headers that mark the failure credential-scoped.
	manager.MarkResult(context.Background(), Result{
		AuthID:   auth.ID,
		Provider: "claude",
		Model:    "claude-opus-5",
		Success:  true,
	})

	sevenDays := 7 * 24 * time.Hour
	manager.MarkResult(context.Background(), Result{
		AuthID:          auth.ID,
		Provider:        "claude",
		Model:           "claude-fable-5(high)",
		Success:         false,
		RetryAfter:      &sevenDays,
		CredentialScope: true,
		Error:           &Error{HTTPStatus: http.StatusTooManyRequests, Message: "7d limit rejected"},
	})

	updated, ok := manager.GetByID(auth.ID)
	if !ok || updated == nil {
		t.Fatal("auth not found")
	}

	now := time.Now()
	if blocked, _, _ := isAuthBlockedForModel(updated, "claude-fable-5", now); !blocked {
		t.Fatal("claude-fable-5 must cool down after its own 429")
	}
	if blocked, reason, next := isAuthBlockedForModel(updated, "claude-opus-5", now); blocked {
		t.Fatalf("claude-opus-5 was blocked by a fable-5 rate limit: reason=%v next=%v", reason, next)
	}

	// The rejection must stay model-scoped in the stored state too, so a later
	// selector change cannot resurrect the credential-wide block.
	if updated.Quota.Reason == "credential_quota" {
		t.Fatalf("credential marked credential_quota by a fable-5 rate limit: %+v", updated.Quota)
	}
	if fable := updated.ModelStates["claude-fable-5"]; fable == nil || !fable.Unavailable {
		t.Fatalf("claude-fable-5 state must be cooled down, got %+v", fable)
	}
	if opus := updated.ModelStates["claude-opus-5"]; opus != nil && (opus.Unavailable || opus.Quota.Reason == "credential_quota") {
		t.Fatalf("claude-opus-5 state was cooled by a fable-5 rate limit: %+v", opus)
	}
}
