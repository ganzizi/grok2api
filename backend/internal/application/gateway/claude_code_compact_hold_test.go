package gateway

import (
	"os"
	"strings"
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

	t.Run("claude code chat compact skips hold without changing operation", func(t *testing.T) {
		t.Parallel()
		input := Input{
			Operation:   audit.OperationChat,
			Streaming:   true,
			PublicModel: "grok-4.6",
			Body:        []byte(`{"messages":[{"role":"user","content":"` + claudeCodeCompactionPrompt + `"}]}`),
		}
		applyTUICompactionQualitySkip(&input)
		if input.Operation != audit.OperationChat {
			t.Fatalf("Operation = %q, want %q", input.Operation, audit.OperationChat)
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

func TestClaudeCodeCompactChatQualityHoldComposition(t *testing.T) {
	t.Parallel()
	cfg := QualityRetryRuntime{Enabled: true, MaxAttempts: 2, MinOutputTokens: 32}
	route := modeldomain.Route{Provider: accountdomain.ProviderBuild, UpstreamModel: "grok-4.6", PublicID: "grok-4.6"}
	compactBody := []byte(`{"messages":[{"role":"user","content":"` + claudeCodeCompactionPrompt + `"}]}`)

	held := Input{Streaming: true, PublicModel: "grok-4.6", Body: compactBody}
	if !shouldHoldQualityStream(held, nil, route, audit.OperationChat, cfg) {
		t.Fatal("claude code compact chat body without skipQualityHold must still hold")
	}

	skipped := held
	applyTUICompactionQualitySkip(&skipped)
	if shouldHoldQualityStream(skipped, nil, route, audit.OperationChat, cfg) {
		t.Fatal("claude code compact chat must not hold after TUI skip")
	}

	coding := Input{Streaming: true, PublicModel: "grok-4.6", Body: []byte(`{"messages":[{"role":"user","content":"fix the nil panic in service.go"}]}`)}
	applyTUICompactionQualitySkip(&coding)
	if !shouldHoldQualityStream(coding, nil, route, audit.OperationChat, cfg) {
		t.Fatal("normal chat coding turn must still hold")
	}
}

func TestCreateChatCompletionWiresTUICompactionQualitySkip(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("service.go")
	if err != nil {
		t.Fatalf("read service.go: %v", err)
	}
	fn, ok := extractGoFunction(string(src), "func (s *Service) CreateChatCompletion")
	if !ok {
		t.Fatal("CreateChatCompletion not found in service.go")
	}
	if !strings.Contains(fn, "applyTUICompactionQualitySkip(&input)") {
		t.Fatal("CreateChatCompletion must call applyTUICompactionQualitySkip so New API compact via /v1/chat/completions does not 503 quality_degraded")
	}
	if strings.Contains(fn, "input.Operation = audit.OperationCompaction") {
		t.Fatal("CreateChatCompletion must not rewrite Operation; chat converter keys on OperationChat")
	}
}

func extractGoFunction(src, signature string) (string, bool) {
	start := strings.Index(src, signature)
	if start < 0 {
		return "", false
	}
	brace := strings.Index(src[start:], "{")
	if brace < 0 {
		return "", false
	}
	i := start + brace
	depth := 0
	for ; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[start : i+1], true
			}
		}
	}
	return "", false
}
