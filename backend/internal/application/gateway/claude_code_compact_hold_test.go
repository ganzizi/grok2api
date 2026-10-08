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
	if strings.Contains(fn, "applyResponsesCompactionClassification") {
		t.Fatal("CreateChatCompletion must not use the Responses compaction helper")
	}
}

func TestCreateMessageWiresTUICompactionQualitySkip(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("service.go")
	if err != nil {
		t.Fatalf("read service.go: %v", err)
	}
	fn, ok := extractGoFunction(string(src), "func (s *Service) CreateMessage")
	if !ok {
		t.Fatal("CreateMessage not found in service.go")
	}
	if !strings.Contains(fn, "applyTUICompactionQualitySkip(&input)") {
		t.Fatal("CreateMessage must call applyTUICompactionQualitySkip so Anthropic compact does not 503 quality_degraded")
	}
	if strings.Contains(fn, "input.Operation = audit.OperationCompaction") {
		t.Fatal("CreateMessage must not rewrite Operation; messages converter keys on OperationMessages")
	}
	if strings.Contains(fn, "applyResponsesCompactionClassification") {
		t.Fatal("CreateMessage must not use the Responses compaction helper; that rewrite would skip the Anthropic converter")
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

func TestApplyResponsesCompactionClassification(t *testing.T) {
	t.Parallel()

	t.Run("codex trigger skips hold and sets compaction operation", func(t *testing.T) {
		t.Parallel()
		input := Input{
			Operation:   audit.OperationResponses,
			Streaming:   true,
			PublicModel: "grok-4.6",
			Body:        []byte(`{"input":[{"role":"user","content":"continue"},{"type":"compaction_trigger"}]}`),
		}
		applyResponsesCompactionClassification(&input)
		if input.Operation != audit.OperationCompaction {
			t.Fatalf("Operation = %q, want %q", input.Operation, audit.OperationCompaction)
		}
		if !input.skipQualityHold {
			t.Fatal("compaction_trigger on CreateResponse must skip quality hold")
		}
	})

	t.Run("tui last user skips hold without rewriting operation", func(t *testing.T) {
		t.Parallel()
		input := Input{
			Operation:   audit.OperationResponses,
			Streaming:   true,
			PublicModel: "grok-4.6",
			Body:        []byte(`{"input":[{"role":"user","content":"` + tuiCompactionPrompt + `"}]}`),
		}
		applyResponsesCompactionClassification(&input)
		if input.Operation != audit.OperationResponses {
			t.Fatalf("Operation = %q, want responses", input.Operation)
		}
		if input.auditOperation != audit.OperationCompaction {
			t.Fatalf("auditOperation = %q, want compaction", input.auditOperation)
		}
		if !input.skipQualityHold {
			t.Fatal("TUI compact must still skip quality hold")
		}
	})

	t.Run("normal coding turn does not skip", func(t *testing.T) {
		t.Parallel()
		input := Input{
			Operation: audit.OperationResponses,
			Body:      []byte(`{"input":[{"role":"user","content":"fix the nil panic in service.go"}]}`),
		}
		applyResponsesCompactionClassification(&input)
		if input.skipQualityHold {
			t.Fatal("normal coding turn must not skip quality hold")
		}
		if input.Operation != audit.OperationResponses {
			t.Fatalf("Operation = %q, want unchanged", input.Operation)
		}
		if input.auditOperation != "" {
			t.Fatalf("auditOperation = %q, want empty", input.auditOperation)
		}
	})

	t.Run("grok build structured compact skips hold without rewriting operation", func(t *testing.T) {
		t.Parallel()
		input := Input{
			Operation:   audit.OperationResponses,
			Streaming:   true,
			PublicModel: "grok-4.6",
			Body:        []byte(`{"input":[{"role":"user","content":"` + grokBuildCompactionStructuredPrompt + `"}]}`),
		}
		applyResponsesCompactionClassification(&input)
		if input.Operation != audit.OperationResponses {
			t.Fatalf("Operation = %q, want responses", input.Operation)
		}
		if input.auditOperation != audit.OperationCompaction {
			t.Fatalf("auditOperation = %q, want compaction", input.auditOperation)
		}
		if !input.skipQualityHold {
			t.Fatal("structured grok-build compact prompt must skip quality hold")
		}
	})
}

func TestCreateResponseCompactionTriggerQualityHoldComposition(t *testing.T) {
	t.Parallel()
	cfg := QualityRetryRuntime{Enabled: true, MaxAttempts: 2, MinOutputTokens: 32}
	route := modeldomain.Route{Provider: accountdomain.ProviderBuild, UpstreamModel: "grok-4.6", PublicID: "grok-4.6"}
	triggerBody := []byte(`{"input":[{"role":"user","content":"continue"},{"type":"compaction_trigger"}]}`)

	held := Input{Streaming: true, PublicModel: "grok-4.6", Body: triggerBody}
	if !shouldHoldQualityStream(held, nil, route, audit.OperationCompaction, cfg) {
		t.Fatal("compaction_trigger without skipQualityHold must still hold; quality guard policy is unchanged")
	}

	skipped := Input{Operation: audit.OperationResponses, Streaming: true, PublicModel: "grok-4.6", Body: triggerBody}
	applyResponsesCompactionClassification(&skipped)
	if shouldHoldQualityStream(skipped, nil, route, skipped.Operation, cfg) {
		t.Fatal("CreateResponse compaction_trigger must not hold after classification")
	}

	coding := Input{Streaming: true, PublicModel: "grok-4.6", Body: []byte(`{"input":[{"role":"user","content":"fix the nil panic in service.go"}]}`)}
	applyResponsesCompactionClassification(&coding)
	if !shouldHoldQualityStream(coding, nil, route, audit.OperationResponses, cfg) {
		t.Fatal("normal responses coding turn must still hold")
	}
}

func TestCreateResponseWiresCompactionClassification(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("service.go")
	if err != nil {
		t.Fatalf("read service.go: %v", err)
	}
	fn, ok := extractGoFunction(string(src), "func (s *Service) CreateResponse")
	if !ok {
		t.Fatal("CreateResponse not found in service.go")
	}
	if !strings.Contains(fn, "applyResponsesCompactionClassification(&input)") {
		t.Fatal("CreateResponse must call applyResponsesCompactionClassification so Codex compaction_trigger skips quality hold")
	}
	if strings.Contains(fn, "input.Operation = audit.OperationCompaction") {
		t.Fatal("CreateResponse must classify compaction via helper, not inline Operation rewrite")
	}
}

func TestApplyTUICompactionQualitySkipGrokBuildStructured(t *testing.T) {
	t.Parallel()
	input := Input{
		Operation: audit.OperationChat,
		Body:      []byte(`{"messages":[{"role":"user","content":"` + grokBuildCompactionStructuredPrompt + `"}]}`),
	}
	applyTUICompactionQualitySkip(&input)
	if input.Operation != audit.OperationChat {
		t.Fatalf("Operation = %q, want chat", input.Operation)
	}
	if input.auditOperation != audit.OperationCompaction {
		t.Fatalf("auditOperation = %q, want compaction", input.auditOperation)
	}
	if !input.skipQualityHold {
		t.Fatal("New API chat compact using grok-build structured prompt must skip hold")
	}
}

func TestGrokBuildStructuredCompactQualityHoldComposition(t *testing.T) {
	t.Parallel()
	cfg := QualityRetryRuntime{Enabled: true, MaxAttempts: 2, MinOutputTokens: 32}
	route := modeldomain.Route{Provider: accountdomain.ProviderBuild, UpstreamModel: "grok-4.6", PublicID: "grok-4.6"}
	partsBody := []byte(`{"input":[{"role":"user","content":[{"type":"input_text","text":"` + grokBuildCompactionStructuredPrompt + `"}]}]}`)

	held := Input{Streaming: true, PublicModel: "grok-4.6", Body: partsBody}
	if !shouldHoldQualityStream(held, nil, route, audit.OperationResponses, cfg) {
		t.Fatal("structured grok-build compact body without skipQualityHold must still hold")
	}

	skipped := Input{Operation: audit.OperationResponses, Streaming: true, PublicModel: "grok-4.6", Body: partsBody}
	applyResponsesCompactionClassification(&skipped)
	if skipped.auditOperation != audit.OperationCompaction {
		t.Fatalf("auditOperation = %q, want compaction", skipped.auditOperation)
	}
	if shouldHoldQualityStream(skipped, nil, route, skipped.Operation, cfg) {
		t.Fatal("structured grok-build compact on CreateResponse must not hold after classification")
	}

	coding := Input{Streaming: true, PublicModel: "grok-4.6", Body: []byte(`{"input":[{"role":"user","content":"` + grokBuildCompactionPrimaryMarkerOnly + `"}]}`)}
	applyResponsesCompactionClassification(&coding)
	if !shouldHoldQualityStream(coding, nil, route, audit.OperationResponses, cfg) {
		t.Fatal("single grok-build marker must still hold")
	}
}
