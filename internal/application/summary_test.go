package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// summaryCore builds a core with one confirmation-required tool whose summary function is fn, and reports how
// often its handler and a secret read ran.
func summaryCore(t *testing.T, fn capability.SummaryFunc, redactor *redact.Redactor) (*Core, *int, *int) {
	t.Helper()
	return summaryCoreLimit(t, fn, redactor, 0)
}

// summaryCoreLimit is summaryCore with a declared summary limit in runes.
func summaryCoreLimit(t *testing.T, fn capability.SummaryFunc, redactor *redact.Redactor, limit int) (*Core, *int, *int) {
	t.Helper()
	handlerCalls, secretReads := 0, 0
	descriptor := testDescriptor("fake.messages.send", capability.EffectCreate, capability.ConfirmationRequired)
	registry := capability.NewRegistry()
	if err := registry.Register("fake", capability.Operation{
		Descriptor: descriptor, ConfirmationSummary: fn, ConfirmationSummaryRunes: limit,
		Handler: func(context.Context, *config.Resolved, *secret.Resolver, *redact.Redactor,
			json.RawMessage) (any, error) {
			handlerCalls++
			return map[string]any{"ok": true}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	cfg := config.New()
	cfg.Services["service"] = config.Service{Provider: "fake", BaseURL: "https://example.invalid"}
	cfg.Credentials["sender"] = config.Credential{
		Type: config.CredentialTypeEnv, Values: map[string]string{"token": "FAKE_TOKEN"},
	}
	cfg.Connections["alerts"] = config.Connection{
		Service: "service", Credential: "sender", Effects: []config.Effect{config.EffectCreate},
	}
	cfg.Defaults.Connections["fake"] = "alerts"
	resolver := secret.NewWith(func(string) string { secretReads++; return "test-token" }, nil, nil, redactor)
	return New(registry, cfg, resolver, redactor), &handlerCalls, &secretReads
}

func refuse(t *testing.T, core *Core, arguments string) *ConfirmationRequiredError {
	t.Helper()
	_, err := core.Invoke(context.Background(), InvokeRequest{
		Operation: "fake.messages.send", Arguments: json.RawMessage(arguments),
	})
	var confirmation *ConfirmationRequiredError
	if !errors.As(err, &confirmation) {
		t.Fatalf("Invoke() = %v, want confirmation-required", err)
	}
	return confirmation
}

func TestConfirmationSummaryDerivesFromArgumentsBeforeAnySideEffect(t *testing.T) {
	var got string
	core, handlerCalls, secretReads := summaryCore(t, func(arguments json.RawMessage) string {
		got = string(arguments)
		var in struct{ ID string }
		_ = json.Unmarshal(arguments, &in)
		return "send to " + in.ID
	}, &redact.Redactor{})
	confirmation := refuse(t, core, `{"id":"abc"}`)
	if confirmation.Summary != "send to abc" || got != `{"id":"abc"}` {
		t.Errorf("summary = %q from %q", confirmation.Summary, got)
	}
	if *handlerCalls != 0 || *secretReads != 0 {
		t.Errorf("handler calls = %d, secret reads = %d, want none", *handlerCalls, *secretReads)
	}
	if want := `tool "fake.messages.send" requires confirmation in this invoke request`; confirmation.Error() != want {
		t.Errorf("message = %q, want %q", confirmation.Error(), want)
	}
}

func TestConfirmationSummaryAbsentLeavesTheRefusalUnchanged(t *testing.T) {
	for name, fn := range map[string]capability.SummaryFunc{
		"no function": nil,
		"empty text":  func(json.RawMessage) string { return "" },
		"panic":       func(json.RawMessage) string { panic("boom") },
	} {
		t.Run(name, func(t *testing.T) {
			core, handlerCalls, _ := summaryCore(t, fn, &redact.Redactor{})
			confirmation := refuse(t, core, `{"id":"abc"}`)
			if confirmation.Summary != "" || *handlerCalls != 0 {
				t.Errorf("summary = %q, handler calls = %d", confirmation.Summary, *handlerCalls)
			}
			want := `tool "fake.messages.send" requires confirmation in this invoke request`
			if confirmation.Error() != want {
				t.Errorf("message = %q, want %q", confirmation.Error(), want)
			}
		})
	}
}

func TestConfirmationSummaryCannotChangeTheArguments(t *testing.T) {
	core, _, _ := summaryCore(t, func(arguments json.RawMessage) string {
		for i := range arguments {
			arguments[i] = 'x'
		}
		return "done"
	}, &redact.Redactor{})
	arguments := json.RawMessage(`{"id":"abc"}`)
	_, err := core.Invoke(context.Background(), InvokeRequest{Operation: "fake.messages.send", Arguments: arguments})
	if err == nil || string(arguments) != `{"id":"abc"}` {
		t.Errorf("arguments = %s, err = %v", arguments, err)
	}
}

func TestConfirmationSummaryRedactsBeforeCutting(t *testing.T) {
	const canary = "canary-value-7731"
	redactor := &redact.Redactor{}
	redactor.Add(canary)
	// The secret straddles the bound: cutting first would leave a prefix the redactor no longer knows.
	padding := strings.Repeat("a", maxSummaryRunes-5)
	core, _, _ := summaryCore(t, func(json.RawMessage) string { return padding + canary + " tail" }, redactor)
	summary := refuse(t, core, `{"id":"abc"}`).Summary
	// The marker itself may be cut, but never the secret.
	if strings.Contains(summary, "canary") || !strings.Contains(summary, "[red") {
		t.Errorf("summary = %q", summary)
	}
}

func TestConfirmationSummaryIsBoundedPerRune(t *testing.T) {
	core, _, _ := summaryCore(t, func(json.RawMessage) string {
		return strings.Repeat("ä€😀", maxSummaryRunes)
	}, &redact.Redactor{})
	summary := refuse(t, core, `{"id":"abc"}`).Summary
	if !utf8.ValidString(summary) || utf8.RuneCountInString(summary) != maxSummaryRunes || !strings.HasSuffix(summary, "…") {
		t.Errorf("summary has %d runes, valid = %t", utf8.RuneCountInString(summary), utf8.ValidString(summary))
	}
}

func TestConfirmationSummaryNeutralizesControlCharacters(t *testing.T) {
	text := "a\x1b[31mred\x1b[0m\r\nb\tc\u202ertl\u2066\u200bz\u2028\x00\xff"
	core, _, _ := summaryCore(t, func(json.RawMessage) string { return text }, &redact.Redactor{})
	summary := refuse(t, core, `{"id":"abc"}`).Summary
	want := `a\u001b[31mred\u001b[0m\r\nb\tc\u202ertl\u2066\u200bz\u2028\u0000` + "\uFFFD"
	if summary != want {
		t.Errorf("summary = %q, want %q", summary, want)
	}
	for _, r := range summary {
		if r < ' ' || r == 0x7f {
			t.Errorf("summary still holds the control character %q", r)
		}
	}
}

func TestConfirmationSummaryDeclaredLimitShowsLongTextInFull(t *testing.T) {
	text := strings.Repeat("ä€😀x", 600) // 2400 runes
	core, _, _ := summaryCoreLimit(t, func(json.RawMessage) string { return text }, &redact.Redactor{}, 2400)
	if summary := refuse(t, core, `{"id":"abc"}`).Summary; summary != text {
		t.Errorf("summary has %d runes, want the full %d", utf8.RuneCountInString(summary), utf8.RuneCountInString(text))
	}
}

func TestConfirmationSummaryDeclaredLimitStillCuts(t *testing.T) {
	core, _, _ := summaryCoreLimit(t, func(json.RawMessage) string { return strings.Repeat("a", 2000) },
		&redact.Redactor{}, 1000)
	summary := refuse(t, core, `{"id":"abc"}`).Summary
	if utf8.RuneCountInString(summary) != 1000 || !strings.HasSuffix(summary, "…") {
		t.Errorf("summary has %d runes", utf8.RuneCountInString(summary))
	}
}

func TestConfirmationSummaryDeclaredLimitRedactsBeforeCutting(t *testing.T) {
	const canary = "canary-value-7731"
	redactor := &redact.Redactor{}
	redactor.Add(canary)
	text := strings.Repeat("a", 1000-5) + canary + " tail"
	core, _, _ := summaryCoreLimit(t, func(json.RawMessage) string { return text }, redactor, 1000)
	if summary := refuse(t, core, `{"id":"abc"}`).Summary; strings.Contains(summary, "canary") {
		t.Errorf("summary = %q", summary)
	}
}

func TestBoundSummaryLimits(t *testing.T) {
	long := strings.Repeat("a", 700)
	tests := []struct {
		name  string
		limit int
		runes int
		cut   bool
	}{
		{"default", 0, maxSummaryRunes, true},
		{"above default", 700, 700, false},
		{"core ceiling", capability.MaxConfirmationSummaryRunes, 700, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := boundSummary(long, tt.limit)
			if utf8.RuneCountInString(got) != tt.runes || strings.HasSuffix(got, "…") != tt.cut {
				t.Errorf("got %d runes, cut = %t", utf8.RuneCountInString(got), strings.HasSuffix(got, "…"))
			}
		})
	}
	ceiling := strings.Repeat("a", capability.MaxConfirmationSummaryRunes+1)
	got := boundSummary(ceiling, capability.MaxConfirmationSummaryRunes)
	if utf8.RuneCountInString(got) != capability.MaxConfirmationSummaryRunes || !strings.HasSuffix(got, "…") {
		t.Errorf("ceiling: got %d runes", utf8.RuneCountInString(got))
	}
}
