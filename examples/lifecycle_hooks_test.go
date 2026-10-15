package examples_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker/sandbox-kit-spec/v3/spec"
	"github.com/stretchr/testify/require"
)

func TestClaudeSettingsSeed(t *testing.T) {
	for _, kit := range []string{"claude", "claude-mixin"} {
		t.Run(kit, func(t *testing.T) {
			descriptor := exampleDescriptor(t, kit)
			lifecycle, err := spec.LifecycleOf(descriptor.Capabilities)
			require.NoError(t, err)
			require.NotNil(t, lifecycle)
			require.GreaterOrEqual(t, len(lifecycle.Install), 3)
			hook := lifecycle.Install[2]
			require.Equal(t, "agent", hook.User)
			require.Equal(t, []string{"SBX_CRED_ANTHROPIC_MODE"}, hook.Env)
			require.Len(t, hook.Command, 3)
			require.Equal(t, []string{"sh", "-c"}, []string(hook.Command[:2]))

			for _, mode := range []string{"none", "apikey", "oauth"} {
				for _, model := range []string{"", "claude-test"} {
					for _, effort := range descriptor.Args["reasoningEffort"].Enum {
						t.Run(mode+"/"+model+"/"+effort, func(t *testing.T) {
							root := t.TempDir()
							path := filepath.Join(root, "settings.json")
							writeExampleFixture(t, path, `{"stale":true}`, 0o600)
							values, err := spec.ResolveArgs(descriptor.Args, map[string]string{"model": model, "reasoningEffort": effort})
							require.NoError(t, err)
							expanded, err := spec.ExpandLifecycle(lifecycle, values)
							require.NoError(t, err)
							command := append([]string(nil), expanded.Install[2].Command...)
							for i := range command {
								command[i] = strings.ReplaceAll(command[i], "/home/agent/.claude/settings.json", path)
							}
							stdout, stderr, err := runExampleCommand(t, []string{"SBX_CRED_ANTHROPIC_MODE=" + mode}, "", command...)
							require.NoError(t, err, stderr)
							require.Empty(t, stdout)
							require.Empty(t, stderr)
							got, err := os.ReadFile(path)
							require.NoError(t, err)
							want := map[string]any{
								"themeId":                           1,
								"alwaysThinkingEnabled":             true,
								"permissions":                       map[string]string{"defaultMode": "bypassPermissions"},
								"bypassPermissionsModeAccepted":     true,
								"skipDangerousModePermissionPrompt": true,
								"statusLine":                        map[string]string{"type": "command", "command": "/usr/local/bin/sbx-claude-hook claude-status"},
							}
							if mode != "none" {
								want["apiKeyHelper"] = "echo proxy-managed"
							}
							if model != "" {
								want["model"] = model
							}
							switch effort {
							case "":
							case "max", "auto":
								want["env"] = map[string]string{"CLAUDE_CODE_EFFORT_LEVEL": effort}
							default:
								want["effortLevel"] = effort
							}
							hooks := map[string]any{}
							for _, event := range []string{"Stop", "StopFailure", "UserPromptSubmit", "Notification"} {
								entry := map[string]any{"hooks": []any{map[string]any{
									"type": "command", "command": "/usr/local/bin/sbx-claude-hook claude", "timeout": 10, "async": true,
								}}}
								if event == "Notification" {
									entry["matcher"] = "permission_prompt|idle_prompt|elicitation_dialog|elicitation_url_dialog|agent_needs_input"
								}
								hooks[event] = []any{entry}
							}
							want["hooks"] = hooks
							encoded, err := json.Marshal(want)
							require.NoError(t, err)
							require.JSONEq(t, string(encoded), string(got))
							info, err := os.Stat(path)
							require.NoError(t, err)
							require.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "rewriting settings must preserve permissions")
						})
					}
				}
			}
		})
	}
}

func TestClaudeMCPRegistrationHooks(t *testing.T) {
	for _, kit := range []string{"claude", "claude-mixin"} {
		t.Run(kit, func(t *testing.T) {
			lifecycle, err := spec.LifecycleOf(exampleDescriptor(t, kit).Capabilities)
			require.NoError(t, err)
			require.NotNil(t, lifecycle)
			require.NotEmpty(t, lifecycle.Install)
			require.NotEmpty(t, lifecycle.Startup)
			install := lifecycle.Install[len(lifecycle.Install)-1]
			startup := lifecycle.Startup[len(lifecycle.Startup)-1]
			require.Equal(t, install.Command, startup.Command)
			require.Equal(t, "agent", install.User)
			require.Equal(t, install.User, startup.User)
			require.Equal(t, []string{"MCP_GATEWAY_URL", "MCP_SENTINEL_TOKEN_NAME"}, install.Env)
			require.Equal(t, install.Env, startup.Env)
			require.False(t, startup.Background)

			for _, tc := range []struct {
				name, url, status string
			}{
				{"disabled", "", "0"},
				{"registered", "http://test.invalid/mcp", "0"},
				{"already registered", "http://test.invalid/mcp", "1"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					root := t.TempDir()
					calls := filepath.Join(root, "calls")
					bin := filepath.Join(root, "bin")
					writeExampleFixture(t, filepath.Join(bin, "claude"), `#!/bin/sh
printf '%s\n' "$@" >> "$MOCK_CALLS"
exit "$MOCK_STATUS"
`, 0o755)
					for _, command := range []spec.CommandLine{install.Command, startup.Command} {
						stdout, stderr, err := runExampleCommand(t, []string{
							"HOME=" + root,
							"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"),
							"MCP_GATEWAY_URL=" + tc.url,
							"MCP_SENTINEL_TOKEN_NAME=test-token",
							"MOCK_CALLS=" + calls,
							"MOCK_STATUS=" + tc.status,
						}, "", command...)
						require.NoError(t, err, stderr)
						require.Empty(t, stdout)
						require.Empty(t, stderr)
					}
					if tc.url == "" {
						require.NoFileExists(t, calls)
						return
					}
					got, err := os.ReadFile(calls)
					require.NoError(t, err)
					want := "mcp\nadd\nmcp-gateway\n" + tc.url + "\n--transport\nhttp\n--scope\nuser\n--header\nAuthorization: Bearer test-token\n"
					require.Equal(t, want+want, string(got), "both phases must register without removing an existing entry")
				})
			}
		})
	}
}
