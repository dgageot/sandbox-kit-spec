# v2's sandbox.image, as content. The template carries the platform floor
# and the cursor-agent install; the workload publishes it as-is. Cursor's
# installer has no version pin, so the kit's version: field is set by hand
# to the semver spelling of the cursor-agent the template carried when it
# was published (2026.10.01-e373342 is 2026.10.1).
FROM dhi.io/sbx-templates:cursor-agent-docker
# v2's environment.variables. AGENT_CLI_CREDENTIAL_STORE=memory keeps
# cursor from validating a file-backed sentinel and re-prompting login;
# auth bootstraps through the environment when host OAuth creds exist.
ENV IS_SANDBOX=1 AGENT_CLI_CREDENTIAL_STORE=memory
USER agent
WORKDIR /home/agent/workspace
# v2's sandbox.entrypoint.
ENTRYPOINT ["cursor-agent", "--yolo"]
