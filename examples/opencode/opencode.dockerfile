# v2's sandbox.image, as content. The template carries the platform floor
# and the opencode install; the build re-pins opencode to the release this
# kit publishes, so the provide cannot claim a version the image does not
# ship.
FROM dhi.io/sbx-templates:opencode-docker
ARG OPENCODE_VERSION
# Root, and fatal: a failed re-pin must fail the build rather than let
# the kit publish a version the template's older binary does not ship.
# The template sets HOME=/home/agent, and both npm and opencode write
# state under $HOME (~/.npm, ~/.config/opencode, ~/.local/share/opencode,
# ~/.cache/opencode), so root's steps get root's own home; otherwise the
# agent's hooks cannot write into directories root created. The last line
# holds the property rather than asserting it.
USER root
RUN export HOME=/root \
 && npm install -g "opencode-ai@${OPENCODE_VERSION}" \
 && opencode --version \
 && rm -rf /root/.npm /root/.cache /root/.config /root/.local \
 && ! find /home/agent -uid 0 | grep -q .
USER agent
WORKDIR /home/agent/workspace
# v2's sandbox.entrypoint.
ENTRYPOINT ["opencode"]
