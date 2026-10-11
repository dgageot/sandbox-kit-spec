# syntax=docker/dockerfile:1
# The agent is a Node app and the shell base ships no Node, so the
# overlay carries the node binary plus the globally installed package.
# dhi.io/node:24-debian13-dev is the hardened Node image: debian13 is
# trixie, matching the glibc floor of the bases these examples compose
# onto, and the -dev variant is the one carrying npm and a shell.
#
# --omit=optional drops the SDK's vendored native claude binary (shipped
# as platform-specific optional deps of @anthropic-ai/claude-agent-sdk).
# It is dead weight here — the wrapper points the adapter at the claude
# the composition installs — and omitting it keeps one claude in the
# sandbox: without the binary present the adapter cannot silently fall
# back to a second, SDK-pinned version.
FROM dhi.io/node:24-debian13-dev AS build
ARG ACP_VERSION
RUN npm install -g --omit=optional --prefix /opt/claude-agent-acp \
      "@agentclientprotocol/claude-agent-acp@${ACP_VERSION}"

FROM dhi.io/debian-base:trixie-dev
COPY --from=build /usr/bin/node /usr/local/bin/node
COPY --from=build /opt/claude-agent-acp /opt/claude-agent-acp
# The adapter briefly reports "Not logged in" while its asynchronous
# account probe is still resolving. Some ACP clients treat that interim
# notification as terminal and close an otherwise healthy connection.
# The wrapper drops only those false negatives for which a synchronous
# `claude auth status` confirms a login; genuine logged-out states pass.
# It also maps legacy `session/set_model` requests to the current
# `session/set_config_option` form for clients that still send the old
# method.
#
# And it binds the adapter to the composed claude: the adapter drives
# the CLI named by CLAUDE_CODE_EXECUTABLE, which the wrapper sets per
# launch to the claude an interactive shell would run (PATH order), so
# the ACP path and `claude` are the same agent at the version this
# composition states. Not a fixed image ENV: claude-mixin installs
# /usr/local/bin/claude but the claude workload kit $HOME/.local/bin,
# and either one answers this kit's requires: ["claude"]. An explicit
# CLAUDE_CODE_EXECUTABLE still wins when it names a real binary.
COPY scripts/claude-agent-acp-wrapper.mjs /usr/local/libexec/claude-agent-acp-wrapper.mjs
RUN chmod 0755 /usr/local/libexec/claude-agent-acp-wrapper.mjs \
 && ln -s /usr/local/libexec/claude-agent-acp-wrapper.mjs /usr/local/bin/claude-agent-acp
