package gateway

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	accountapp "github.com/chenyme/grok2api/backend/internal/application/account"
	clientkeyapp "github.com/chenyme/grok2api/backend/internal/application/clientkey"
	egressapp "github.com/chenyme/grok2api/backend/internal/application/egress"
	"github.com/chenyme/grok2api/backend/internal/domain/account"
	"github.com/chenyme/grok2api/backend/internal/domain/audit"
	"github.com/chenyme/grok2api/backend/internal/domain/clientkey"
	modeldomain "github.com/chenyme/grok2api/backend/internal/domain/model"
	infraegress "github.com/chenyme/grok2api/backend/internal/infra/egress"
	"github.com/chenyme/grok2api/backend/internal/infra/persistence/relational"
	"github.com/chenyme/grok2api/backend/internal/infra/provider"
	"github.com/chenyme/grok2api/backend/internal/infra/runtime/memory"
)

func TestQualityProbeSelectionFailureCanBeIdentifiedByCaller(t *testing.T) {
	err := normalizeQualityProbeRequestError(errors.Join(ErrNoAvailableAccount, &SelectionUnavailableError{Reason: SelectionCooling}))
	if !errors.Is(err, egressapp.ErrQualityProbeNoAccount) {
		t.Fatalf("error = %v", err)
	}
}

func TestQualityProbeModelIsPinnedToBuildNamespace(t *testing.T) {
	got, ok := qualityProbeBuildPublicModel("grok-shared")
	if !ok || got != "Build/grok-shared" {
		t.Fatalf("Build probe model = %q, valid=%v", got, ok)
	}
	if _, ok := qualityProbeBuildPublicModel("Console/grok-shared"); ok {
		t.Fatal("quality probe must reject an explicitly non-Build model")
	}
}

func TestQualityProbeOutputTokensPerSecondMatchesAuditPanel(t *testing.T) {
	got := qualityProbeOutputTokensPerSecond(1335, 1200, 17320, 17100)
	// 17100ms wait vs 220ms tail: use full duration so buffered thinking is
	// not crushed into the flush window.
	want := float64(1335) * 1000 / 17320
	if got != want {
		t.Fatalf("output TPS = %v, want %v", got, want)
	}
}

func TestQualityProbeOutputTokensPerSecondIncludesReasoningTokens(t *testing.T) {
	// The panel reports completion/output tokens, including reasoning tokens.
	// 100ms tail after 1000ms wait is a short flush, so the window is duration.
	got := qualityProbeOutputTokensPerSecond(1050, 1000, 1100, 1000)
	want := float64(1050) * 1000 / 1100
	if got != want {
		t.Fatalf("output TPS = %v, want %v", got, want)
	}
}

func TestQualityProbeOutputTokensPerSecondKeepsNormalTail(t *testing.T) {
	got := qualityProbeOutputTokensPerSecond(200, 0, 2200, 200)
	if got != 100 {
		t.Fatalf("output TPS = %v, want 100", got)
	}
}

func TestQualityProbeOutputTokensPerSecondKeepsBurstWithoutReasoning(t *testing.T) {
	got := qualityProbeOutputTokensPerSecond(2000, 0, 10100, 10000)
	if got != 20000 {
		t.Fatalf("output TPS = %v, want 20000", got)
	}
}

func TestQualityProbeCountsThinkingContentAsFirstToken(t *testing.T) {
	if qualityProbeHasGeneratedDelta("", "", "", "") {
		t.Fatal("empty deltas must not mark first token")
	}
	if !qualityProbeHasGeneratedDelta("", "", "", "hmm") {
		t.Fatal("thinking_content must mark first token so thinking time stays in the generation window")
	}
	if !qualityProbeHasGeneratedDelta("", "", "hmm", "") {
		t.Fatal("reasoning_content must still mark first token")
	}
}

func TestOmitQualityProbeCoolingAuditOnlyForProbeCooling(t *testing.T) {
	t.Parallel()
	cooling := &SelectionUnavailableError{Reason: SelectionCooling}
	probe := infraegress.WithQualityProbe(context.Background())
	if !omitQualityProbeCoolingAudit(probe, cooling) {
		t.Fatal("quality probe cooling must not be audited")
	}
	if omitQualityProbeCoolingAudit(context.Background(), cooling) {
		t.Fatal("ordinary cooling must still be audited")
	}
	quota := &SelectionUnavailableError{Reason: SelectionQuotaExhausted}
	if omitQualityProbeCoolingAudit(probe, quota) {
		t.Fatal("quality probe quota exhaustion must still be audited")
	}
}

func TestQualityProbeCoolingDoesNotWriteRequestAudit(t *testing.T) {
	ctx := context.Background()
	database, err := relational.OpenSQLite(ctx, filepath.Join(t.TempDir(), "probe-cooling-audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.InitializeSchema(ctx); err != nil {
		t.Fatal(err)
	}
	accountRepo := relational.NewAccountRepository(database)
	modelRepo := relational.NewModelRepository(database)
	auditRepo := relational.NewAuditRepository(database)
	responseRepo := relational.NewResponseRepository(database)
	keyRepo := relational.NewClientKeyRepository(database)
	coolingUntil := time.Now().UTC().Add(time.Hour)
	credential, _, err := accountRepo.UpsertByIdentity(ctx, account.Credential{
		Provider: account.ProviderWeb, AuthType: account.AuthTypeSSO, WebTier: account.WebTierBasic,
		Name: "cooling-probe", SourceKey: "cooling-probe", EncryptedAccessToken: "encrypted",
		Enabled: true, AuthStatus: account.AuthStatusActive, MaxConcurrent: 1, CooldownUntil: &coolingUntil,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := modelRepo.UpsertRoutes(ctx, []modeldomain.Route{{
		PublicID: "grok-probe-cool", Provider: account.ProviderWeb, UpstreamModel: "grok-probe-cool",
		Capability: modeldomain.CapabilityChat, Enabled: true,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := modelRepo.ReplaceAccountCapabilities(ctx, credential.ID, []string{"grok-probe-cool"}, now); err != nil {
		t.Fatal(err)
	}
	key, err := keyRepo.Create(ctx, clientkey.Key{
		Name: "probe-key", Prefix: "probe-key", SecretHash: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		EncryptedSecret: "encrypted-key", Enabled: true, RPMLimit: 60, MaxConcurrent: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	registry := provider.NewRegistry(&webChatQuotaAdapter{synced: make(chan string, 1)})
	sticky := memory.NewStickyStore()
	accountService := accountapp.NewService(accountRepo, auditRepo, memory.NewDeviceSessionStore(), sticky, registry, testCipher(t), nil)
	selector := NewSelector(accountRepo, memory.NewConcurrencyLimiter(), sticky, registry, time.Hour, time.Second, time.Minute)
	service := NewService(modelRepo, auditRepo, accountService, clientkeyapp.NewService(keyRepo, nil, nil, 60, 4, nil), registry, selector, responseRepo, 1)
	input := Input{
		RequestID: "req-probe-cooling", ClientKey: key, PublicModel: "grok-probe-cool",
		Operation: audit.OperationChat, ForcedAccountID: credential.ID, ForcedEgressNodeID: 1,
		Body: []byte(`{"model":"grok-probe-cool","messages":[{"role":"user","content":"hi"}]}`),
	}
	if _, err := service.CreateChatCompletion(infraegress.WithQualityProbe(ctx), input); err == nil {
		t.Fatal("cooling probe must still fail")
	} else if !errors.Is(err, egressapp.ErrQualityProbeNoAccount) && !errors.Is(err, ErrNoAvailableAccount) {
		var unavailable *SelectionUnavailableError
		if !errors.As(err, &unavailable) || unavailable.Reason != SelectionCooling {
			t.Fatalf("probe error = %v", err)
		}
	}
	if logs, total, err := auditRepo.List(ctx, 0, 10); err != nil || total != 0 || len(logs) != 0 {
		t.Fatalf("probe cooling wrote an audit: total=%d logs=%#v err=%v", total, logs, err)
	}
	input.RequestID = "req-client-cooling"
	if _, err := service.CreateChatCompletion(ctx, input); err == nil {
		t.Fatal("ordinary cooling must still fail")
	}
	logs, total, err := auditRepo.List(ctx, 0, 10)
	if err != nil || total != 1 || len(logs) != 1 {
		t.Fatalf("ordinary cooling audit = %#v total=%d err=%v", logs, total, err)
	}
	if logs[0].StatusCode != 429 || logs[0].ErrorCode != "upstream_cooling" || logs[0].RequestID != "req-client-cooling" {
		t.Fatalf("ordinary cooling audit = %#v", logs[0])
	}
}
