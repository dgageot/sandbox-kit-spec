# OpenCode

This sandbox runs OpenCode, a multi-provider coding agent. Provider keys
are proxy-managed sentinels; ChatGPT-subscription OAuth renders into
~/.local/share/opencode/auth.json for OpenCode's codex auth plugin.
Claude-subscription OAuth renders an `anthropic` entry into the same
file when ChatGPT OAuth is not bound; OpenCode reads it only through an
Anthropic auth plugin.
