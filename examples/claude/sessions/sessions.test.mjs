import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtemp, mkdir, readFile, rm, utimes, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { test } from "node:test";

const helper = fileURLToPath(new URL("claude-sessions.mjs", import.meta.url));
const older = "11111111-1111-4111-8111-111111111111";
const newer = "22222222-2222-4222-8222-222222222222";
const excluded = "33333333-3333-4333-8333-333333333333";

async function fixture(t) {
  const home = await mkdtemp(join(tmpdir(), "claude-sessions-"));
  t.after(() => rm(home, { recursive: true, force: true }));
  return { home, config: join(home, ".claude") };
}

function transcript(sessionId, entrypoint = "cli", extra = {}) {
  return JSON.stringify({
    parentUuid: null,
    isSidechain: false,
    type: "user",
    message: { role: "user", content: "An ordinary conversation" },
    uuid: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
    timestamp: "2026-09-24T12:00:00.000Z",
    entrypoint,
    cwd: "/workspace",
    sessionId,
    version: "2.1.281",
    ...extra,
  }) + "\n";
}

async function store(config, path, content, mtime = 100) {
  const file = join(config, "projects", path);
  await mkdir(dirname(file), { recursive: true });
  await writeFile(file, content);
  await utimes(file, mtime, mtime);
}

function list(home, config) {
  const result = spawnSync(process.execPath, [helper], {
    cwd: home,
    env: { PATH: process.env.PATH, HOME: home, ...(config && { CLAUDE_CONFIG_DIR: config }) },
    encoding: "utf8",
    timeout: 10_000,
  });
  assert.ifError(result.error);
  assert.equal(result.status, 0, result.stderr);
  assert.equal(result.stderr, "");
  return result.stdout;
}

test("missing and empty history produce no output, not a blank ID", async (t) => {
  const { home, config } = await fixture(t);
  assert.equal(list(home), "");
  await mkdir(join(config, "projects"), { recursive: true });
  assert.equal(list(home), "");
});

test("lists ordinary interactive and headless sessions across projects, newest first", async (t) => {
  const { home, config } = await fixture(t);
  await store(config, `project-a/${older}.jsonl`, transcript(older), 100);
  await store(config, `project-b/${newer}.jsonl`, transcript(newer, "sdk-cli"), 200);
  assert.equal(list(home), `${newer}\n${older}\n`);
});

test("honors CLAUDE_CONFIG_DIR instead of the default history", async (t) => {
  const { home, config } = await fixture(t);
  const custom = join(home, "custom config");
  await store(config, `project/${older}.jsonl`, transcript(older));
  await store(custom, `project/${newer}.jsonl`, transcript(newer));
  assert.equal(list(home, custom), `${newer}\n`);
});

test("excludes files without resumable main-conversation messages", async (t) => {
  const { home, config } = await fixture(t);
  const cases = [
    ["empty", `${excluded}.jsonl`, ""],
    ["metadata", `${excluded}.jsonl`, '{"type":"file-history-snapshot"}\n'],
    ["title-only", `${excluded}.jsonl`, JSON.stringify({ type: "custom-title", sessionId: excluded, customTitle: "Abandoned" }) + "\n"],
    ["malformed", `${excluded}.jsonl`, "not JSON\n"],
    ["sidechain", `${excluded}.jsonl`, transcript(excluded, "cli", { isSidechain: true })],
    ["nested-subagent", `${older}/subagents/${excluded}.jsonl`, transcript(excluded)],
    ["legacy-subagent", "agent-a123.jsonl", transcript(excluded)],
    ["orphaned", `${excluded}.orphaned-123-abc.jsonl`, transcript(excluded)],
    ["superseded", `${excluded}.jsonl.superseded-123`, transcript(excluded)],
  ];
  for (const [project, path, content] of cases) {
    await store(config, `${project}/${path}`, content);
  }
  await store(config, `ordinary/${older}.jsonl`, transcript(older));
  assert.equal(list(home), `${older}\n`);
});

test("does not require a text first prompt", async (t) => {
  const { home, config } = await fixture(t);
  await store(config, `project/${older}.jsonl`, transcript(older, "cli", {
    message: { role: "user", content: [{ type: "image", source: { type: "base64", media_type: "image/png", data: "iVBORw0KGgo=" } }] },
  }));
  assert.equal(list(home), `${older}\n`);
});

test("deduplicates session IDs across projects", async (t) => {
  const { home, config } = await fixture(t);
  await store(config, `project-a/${older}.jsonl`, transcript(older), 100);
  await store(config, `project-b/${older}.jsonl`, transcript(older), 300);
  await store(config, `project-b/${newer}.jsonl`, transcript(newer), 200);
  assert.equal(list(home), `${older}\n${newer}\n`);
});

test("the descriptor and image install the tested command", async () => {
  const descriptor = await readFile(new URL("../claude.yaml", import.meta.url), "utf8");
  const dockerfile = await readFile(new URL("../claude.dockerfile", import.meta.url), "utf8");
  const command = JSON.parse(descriptor.match(/^      list: (\[.*\])$/m)[1]);
  assert.deepEqual(command, ["node", "/opt/claude-sessions/claude-sessions.mjs"]);
  assert.match(dockerfile, /COPY sessions\/claude-sessions\.mjs \/opt\/claude-sessions\//);
  const sdk = JSON.parse(await readFile(new URL("node_modules/@anthropic-ai/claude-agent-sdk/package.json", import.meta.url)));
  assert.equal(sdk.claudeCodeVersion, descriptor.match(/^    default: "([^"]+)"$/m)[1]);
});
