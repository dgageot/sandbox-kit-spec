#!/usr/bin/env node
import { getSessionMessages, listSessions } from "@anthropic-ai/claude-agent-sdk";

for (const { sessionId } of await listSessions()) {
  // A title alone can make an abandoned transcript appear in the listing.
  if ((await getSessionMessages(sessionId, { limit: 1 })).length > 0) {
    process.stdout.write(`${sessionId}\n`);
  }
}
