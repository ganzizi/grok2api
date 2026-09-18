package gateway

import (
	"testing"

	accountdomain "github.com/chenyme/grok2api/backend/internal/domain/account"
	"github.com/chenyme/grok2api/backend/internal/domain/audit"
	modeldomain "github.com/chenyme/grok2api/backend/internal/domain/model"
)

func TestApplyTUICompactionQualitySkip(t *testing.T) {
	t.Parallel()

	t.Run("claude code messages compact skips hold without changing operation", func(t *testing.T) {
		t.Parallel()
		input := Input{
			Operation:   audit.OperationMessages,
			Streaming:   true,
			PublicModel: "grok-4.6",
			Body:        []byte(`{"messages":[{"role":"user","content":"` + claudeCodeCompactionPrompt + `"}]}`),
		}
		applyTUICompactionQualitySkip(&input)
		if input.Operation != audit.OperationMessages {
			t.Fatalf("Operation = %q, want %q", input.Operation, audit.OperationMessages)
		}
		if input.auditOperation != audit.OperationCompaction {
			t.Fatalf("auditOperation = %q, want %q", input.auditOperation, audit.OperationCompaction)
		}
		if !input.skipQualityHold {
			t.Fatal("skipQualityHold = false, want true")
		}
	})

	t.Run("normal messages coding turn does not skip", func(t *testing.T) {
		t.Parallel()
		input := Input{
			Operation: audit.OperationMessages,
			Body:      []byte(`{"messages":[{"role":"user","content":"fix the nil panic in service.go"}]}`),
		}
		applyTUICompactionQualitySkip(&input)
		if input.skipQualityHold {
			t.Fatal("normal coding turn must not skip quality hold")
		}
		if input.auditOperation != "" {
			t.Fatalf("auditOperation = %q, want empty", input.auditOperation)
		}
		if input.Operation != audit.OperationMessages {
			t.Fatalf("Operation = %q, want unchanged", input.Operation)
		}
	})

	t.Run("single claude marker does not skip", func(t *testing.T) {
		t.Parallel()
		input := Input{
			Operation: audit.OperationMessages,
			Body:      []byte(`{"messages":[{"role":"user","content":"` + claudeCodeFirstMarkerOnly + `"}]}`),
		}
		applyTUICompactionQualitySkip(&input)
		if input.skipQualityHold {
			t.Fatal("partial compact marker must not skip quality hold")
		}
	})

	t.Run("compaction trigger on messages does not rewrite operation", func(t *testing.T) {
		t.Parallel()
		input := Input{
			Operation: audit.OperationMessages,
			Body:      []byte(`{"input":[{"type":"compaction_trigger"}]}`),
		}
		applyTUICompactionQualitySkip(&input)
		if input.Operation != audit.OperationMessages {
			t.Fatalf("Operation = %q, want messages", input.Operation)
		}
		if input.skipQualityHold {
			t.Fatal("compaction_trigger must not skip via TUI helper")
		}
	})
}

func TestClaudeCodeCompactMessagesQualityHoldComposition(t *testing.T) {
	t.Parallel()
	cfg := QualityRetryRuntime{Enabled: true, MaxAttempts: 2, MinOutputTokens: 32}
	route := modeldomain.Route{Provider: accountdomain.ProviderBuild, UpstreamModel: "grok-4.6", PublicID: "grok-4.6"}
	compactBody := []byte(`{"messages":[{"role":"user","content":"` + claudeCodeCompactionPrompt + `"}]}`)

	held := Input{Streaming: true, PublicModel: "grok-4.6", Body: compactBody}
	if !shouldHoldQualityStream(held, nil, route, audit.OperationMessages, cfg) {
		t.Fatal("claude code compact body without skipQualityHold must still hold")
	}

	skipped := held
	applyTUICompactionQualitySkip(&skipped)
	if shouldHoldQualityStream(skipped, nil, route, audit.OperationMessages, cfg) {
		t.Fatal("claude code compact messages must not hold after TUI skip")
	}

	coding := Input{Streaming: true, PublicModel: "grok-4.6", Body: []byte(`{"messages":[{"role":"user","content":"fix the nil panic in service.go"}]}`)}
	applyTUICompactionQualitySkip(&coding)
	if !shouldHoldQualityStream(coding, nil, route, audit.OperationMessages, cfg) {
		t.Fatal("normal messages coding turn must still hold")
	}
}
