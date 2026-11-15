package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// summaryRegistry offers fake.messages.send, a confirmation-required tool with the given summary function.
func summaryRegistry(t *testing.T, summary capability.SummaryFunc) (*capability.Registry, string) {
	t.Helper()
	registry := capability.NewRegistry()
	if err := registry.RegisterProvider(config.ProviderMetadata{
		ID: "fake", Name: "Fake", DefaultEffects: []config.Effect{config.EffectCreate},
	}, nil); err != nil {
		t.Fatal(err)
	}
	descriptor := capability.Descriptor{
		ID: "fake.messages.send", Version: 1, Description: "Send a fake message", Provider: "fake",
		Risk: capability.Risk{
			Effect: capability.EffectCreate, Idempotency: capability.IdempotencyNonIdempotent,
			Confirmation: capability.ConfirmationRequired, OpenWorld: true, DataSensitivity: "message",
		},
		InputSchema:  json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"],"additionalProperties":false}`),
		OutputSchema: json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"],"additionalProperties":false}`),
	}
	if err := registry.Register("fake", capability.Operation{
		Descriptor: descriptor, ConfirmationSummary: summary,
		Handler: func(context.Context, *config.Resolved, *secret.Resolver, *redact.Redactor,
			json.RawMessage) (any, error) {
			t.Error("handler ran without confirmation")
			return nil, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	return registry, writeConfig(t, `version: 4
services:
  fake:
    provider: fake
    base_url: https://example.invalid
credentials:
  sender:
    type: keyring
connections:
  target:
    service: fake
    credential: sender
defaults: {}
`)
}

func textSummary(arguments json.RawMessage) string {
	var in struct{ Text string }
	_ = json.Unmarshal(arguments, &in)
	return "send " + in.Text
}

func TestConfirmationSummaryOnEverySurface(t *testing.T) {
	const canary = "canary-value-7731"
	registry, path := summaryRegistry(t, textSummary)
	redactor := &redact.Redactor{}
	redactor.Add(canary)
	text := canary + "\x1b[2J line\nbreak"
	arguments, _ := json.Marshal(map[string]string{"text": text})
	wantSummary := `send [redacted]\u001b[2J line\nbreak`

	for _, format := range []string{"default", "json"} {
		t.Run("cli "+format, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			opts := &Options{Input: strings.NewReader(string(arguments)), Redactor: redactor,
				Secrets: secret.NewWith(nil, nil, nil, redactor)}
			args := []string{"invoke", "fake.messages.send", "--connection", "target", "--config", path}
			if format == "json" {
				args = append(args, "--output", "json")
			}
			code := run(newRootCommand(opts, registry), opts, args, &stdout, &stderr)
			if code != exitUsage {
				t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
			}
			lines := strings.Split(strings.TrimSpace(stderr.String()), "\n")
			if !strings.Contains(lines[0], "confirmation-required") {
				t.Fatalf("stderr = %q", stderr.String())
			}
			var detail map[string]string
			if err := json.Unmarshal([]byte(lines[len(lines)-1]), &detail); err != nil {
				t.Fatalf("last line is not the JSON detail: %q", stderr.String())
			}
			if detail["code"] != "confirmation-required" || detail["operation"] != "fake.messages.send" ||
				detail["summary"] != wantSummary {
				t.Errorf("detail = %#v, want summary %q", detail, wantSummary)
			}
			if strings.Contains(stderr.String(), "canary") || strings.Contains(stderr.String(), "\x1b") {
				t.Errorf("stderr leaks the secret or a control character: %q", stderr.String())
			}
		})
	}

	t.Run("mcp", func(t *testing.T) {
		input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{` + mcpTestMeta +
			`,"name":"invoke","arguments":{"operation":"fake.messages.send","connection":"target","arguments":` +
			string(arguments) + `}}}` + "\n"
		responses, _ := runMCPWithOptions(t, registry, input, &Options{
			Config: path, Redactor: redactor, Secrets: secret.NewWith(nil, nil, nil, redactor),
		})
		result := toolResultFrom(t, responses["1"])
		var detail map[string]string
		if err := json.Unmarshal(result.Structured, &detail); err != nil {
			t.Fatal(err)
		}
		if !result.IsError || detail["code"] != "confirmation-required" || detail["summary"] != wantSummary ||
			detail["operation"] != "fake.messages.send" {
			t.Errorf("structuredContent = %s", result.Structured)
		}
		if got := result.Content[0].Text; !strings.HasSuffix(got, "\nsummary: "+wantSummary) ||
			!strings.HasPrefix(got, "confirmation-required: "+detail["message"]) {
			t.Errorf("text = %q", got)
		}
	})
}

// Without a summary the refusal stays what it was: message and structuredContent carry nothing more.
func TestConfirmationWithoutSummaryStaysUnchanged(t *testing.T) {
	for name, fn := range map[string]capability.SummaryFunc{
		"no function": nil,
		"empty":       func(json.RawMessage) string { return "" },
		"panic":       func(json.RawMessage) string { panic("boom") },
	} {
		t.Run(name, func(t *testing.T) {
			registry, path := summaryRegistry(t, fn)
			const message = `tool "fake.messages.send" requires confirmation in this invoke request`

			var stdout, stderr bytes.Buffer
			opts := &Options{Input: strings.NewReader(`{"text":"x"}`), Redactor: &redact.Redactor{},
				Secrets: secret.NewWith(nil, nil, nil, nil)}
			run(newRootCommand(opts, registry), opts, []string{
				"invoke", "fake.messages.send", "--connection", "target", "--config", path,
			}, &stdout, &stderr)
			if want := "qconnect: confirmation-required: " + message + "\n"; !strings.HasPrefix(stderr.String(), want) ||
				strings.Contains(stderr.String(), "summary") {
				t.Errorf("stderr = %q", stderr.String())
			}

			input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{` + mcpTestMeta +
				`,"name":"invoke","arguments":{"operation":"fake.messages.send","connection":"target","arguments":{"text":"x"}}}}` + "\n"
			responses, _ := runMCPWithOptions(t, registry, input, &Options{
				Config: path, Redactor: &redact.Redactor{}, Secrets: secret.NewWith(nil, nil, nil, nil),
			})
			result := toolResultFrom(t, responses["1"])
			if got, want := result.Content[0].Text, "confirmation-required: "+message; got != want {
				t.Errorf("text = %q, want %q", got, want)
			}
			wantJSON, _ := json.Marshal(map[string]string{"code": "confirmation-required", "message": message})
			if got := string(result.Structured); !jsonEqual([]byte(got), wantJSON) || strings.Contains(got, "summary") {
				t.Errorf("structuredContent = %s, want %s", got, wantJSON)
			}
		})
	}
}
