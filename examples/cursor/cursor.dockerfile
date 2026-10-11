# v2's sandbox.image, as content. The template carries the platform floor
# and the cursor-agent install; the workload publishes it as-is. Cursor's
# installer has no version pin, so the kit's version: field is the semver
# spelling of the cursor-agent this template build carries
# (2026.10.01-e373342 is 2026.10.1). The template tag moves on every
# refresh, so the base is pinned by digest and the build asserts the
# agent it carries: otherwise a refresh would publish a different binary
# under an unchanged kit tag. Move the digest, CURSOR_AGENT_BUILD, and
# version: together.
FROM dhi.io/sbx-templates:cursor-agent-docker@sha256:a75af8edd517e5a4f3cc0986b1d451135fb090b21ac750548653c0d1227978d8
ARG CURSOR_AGENT_BUILD=2026.10.01-e373342
# A scratch HOME, so asking for the version leaves no state in the image.
RUN scratch=$(mktemp -d) \
 && got=$(HOME="$scratch" cursor-agent --version) \
 && rm -rf "$scratch" \
 && { [ "$got" = "${CURSOR_AGENT_BUILD}" ] || { echo "cursor-agent is $got, expected ${CURSOR_AGENT_BUILD}" >&2; exit 1; }; }
# v2's environment.variables. AGENT_CLI_CREDENTIAL_STORE=memory keeps
# cursor from validating a file-backed sentinel and re-prompting login;
# auth bootstraps through the environment when host OAuth creds exist.
ENV IS_SANDBOX=1 AGENT_CLI_CREDENTIAL_STORE=memory
# The ACP launcher acp@1 names: cursor-agent's ACP server keeps running
# after stdin closes, so the wrapper follows the close with SIGTERM, which
# it does stop on.
USER root
COPY scripts/acp-graceful.mjs /usr/local/bin/acp-graceful
RUN chmod 0755 /usr/local/bin/acp-graceful
USER agent
WORKDIR /home/agent/workspace
# v2's sandbox.entrypoint.
ENTRYPOINT ["cursor-agent", "--yolo"]
