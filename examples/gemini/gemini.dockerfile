# v2's sandbox.image, as content. The template carries the platform floor
# (node, jq for the MCP merge hook) and the gemini-cli install; the
# workload publishes it as-is, with the kit's version: field speaking for
# the artifact.
FROM dhi.io/sbx-templates:gemini-docker
# v2's environment.variables. GEMINI_SANDBOX=false: this container IS the
# sandbox; gemini must not try to nest its own.
ENV BROWSER=xdg-open DISPLAY=:0 SANDBOX=docker GEMINI_SANDBOX=false
# The ACP launcher acp@1 names: gemini's ACP mode ignores SIGTERM, so the
# wrapper turns it into the stdin close gemini does stop on.
USER root
COPY scripts/acp-graceful.mjs /usr/local/bin/acp-graceful
RUN chmod 0755 /usr/local/bin/acp-graceful
USER agent
WORKDIR /home/agent/workspace
# v2's sandbox.entrypoint.
ENTRYPOINT ["gemini", "--yolo"]
