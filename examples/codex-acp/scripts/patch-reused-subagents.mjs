#!/usr/bin/env node
// Makes codex-acp show a subagent's work again when Codex reuses it.
//
// Codex reports a follow-up to a subagent that already finished as a
// `subAgentActivity` of kind "interacted". The adapter's subagent router
// reopens a finished child only for the older collaboration calls
// (sendInput, resumeAgent, followupTask); for "interacted" it keeps the
// child marked finished and drops all of its updates ("Ignoring update
// for terminal subagent"). An ACP client with subagent sessions then sees
// a wait and nothing of the work. Reopening the child on "interacted"
// announces it again (`subagent_spawned`, `<thread>:generation:N`) and
// routes its updates there, as the router already does for the older
// calls.
//
// Applied at build time to the installed bundle. A release that already
// reopens on "interacted" is left alone; one whose router no longer has
// the code this edits fails the build, so the patch is revisited rather
// than silently lost.
import { readFileSync, writeFileSync } from "node:fs";

const file = process.argv[2];
if (!file) {
  console.error("usage: patch-reused-subagents.mjs <codex-acp dist/index.js>");
  process.exit(2);
}

const source = readFileSync(file, "utf8");
const fixed = `if (hasNativeRepresentation && item.kind === "interacted") {`;
if (source.includes(fixed)) {
  console.log("codex-acp already reopens reused subagents");
  process.exit(0);
}

const anchor = `      if (hasNativeRepresentation && item.kind === "interrupted") {`;
const count = source.split(anchor).length - 1;
if (count !== 1) {
  console.error(`codex-acp: expected the subagent router's "interrupted" branch once, found it ${count} times`);
  process.exit(1);
}

const patch = `      ${fixed}
        await this.reopen(item.agentThreadId);
      }
`;
writeFileSync(file, source.replace(anchor, patch + anchor));
console.log("codex-acp patched: reused subagents are reopened");
