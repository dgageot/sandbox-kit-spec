package examples_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func desktopBuilders(t *testing.T, kit string) string {
	t.Helper()
	script, err := os.ReadFile(filepath.Join(kit, "scripts", "sbx-agent-hook.sh"))
	require.NoError(t, err)
	builders, _, found := strings.Cut(string(script), "\n{\n  [ -n \"$url\" ]")
	require.True(t, found, "load the functions without running the gateway dispatcher")
	return builders
}

func runCodexPayload(t *testing.T, kit, records string) (string, string, error) {
	t.Helper()
	root := t.TempDir()
	writeExampleFixture(t, filepath.Join(root, "sessions", "2026", "10", "15", "rollout-2026-test.jsonl"), records, 0o644)
	return runExampleCommand(t, []string{"CODEX_HOME=" + root}, "", "sh", "-c",
		desktopBuilders(t, kit)+"\ncodex_session_args test\n", "payload-test", "codex", "{}")
}

func TestCodexRolloutLastRecords(t *testing.T) {
	requireExampleTool(t, "jq")
	for _, kit := range []string{"claude", "claude-mixin", "codex", "codex-mixin"} {
		for _, tc := range []struct{ name, records, expected string }{
			{"empty", "", `{"session_id":null,"model_id":null,"context_used_tokens":null}`},
			{"last matching records", `{"type":"session_meta","payload":{"id":"old"}}
{"type":"turn_context","payload":{"model":"old"}}
{"type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"total_tokens":1}}}}
{"type":"session_meta","payload":{"id":"new"}}
{"type":"turn_context","payload":{"model":"new"}}
{"type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"total_tokens":2}}}}`,
				`{"session_id":"new","model_id":"new","context_used_tokens":2}`},
			{"last missing payload", `{"type":"session_meta","payload":{"id":"old"}}
{"type":"session_meta"}`, `{"session_id":null,"model_id":null,"context_used_tokens":null}`},
		} {
			t.Run(kit+"/"+tc.name, func(t *testing.T) {
				stdout, stderr, err := runCodexPayload(t, kit, tc.records)
				require.NoError(t, err, stderr)
				require.Empty(t, stderr)
				var payload, expected map[string]json.RawMessage
				require.NoError(t, json.Unmarshal([]byte(stdout), &payload))
				require.NoError(t, json.Unmarshal([]byte(tc.expected), &expected))
				for key, value := range expected {
					require.JSONEq(t, string(value), string(payload[key]))
				}
			})
		}
	}
}

func TestCodexRolloutTitleUsesFirstTrimmedLine(t *testing.T) {
	requireExampleTool(t, "jq")
	for _, kit := range []string{"claude", "claude-mixin", "codex", "codex-mixin"} {
		for _, tc := range []struct{ name, prompt, title string }{
			{"blank", " \n\t\n ", ""},
			{"leading blank lines", " \n\t First line \t\n Second line", "First line"},
			{"unicode", "\u2003\n\u2003世界\u2003\nOther line", "世界"},
			{"long", strings.Repeat("界", 201) + "\nOther line", strings.Repeat("界", 200)},
		} {
			t.Run(kit+"/"+tc.name, func(t *testing.T) {
				prompt, err := json.Marshal(tc.prompt)
				require.NoError(t, err)
				record := `{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":` + string(prompt) + `}]}}`
				stdout, stderr, err := runCodexPayload(t, kit, record)
				require.NoError(t, err, stderr)
				require.Empty(t, stderr)
				var payload struct{ Title *string }
				require.NoError(t, json.Unmarshal([]byte(stdout), &payload))
				if tc.title == "" {
					require.Nil(t, payload.Title)
				} else {
					require.NotNil(t, payload.Title)
					require.Equal(t, tc.title, *payload.Title)
				}
			})
		}
	}
}
