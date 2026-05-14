#!/usr/bin/env node

import { existsSync, readFileSync } from "node:fs";
import { homedir } from "node:os";
import { resolve } from "node:path";
import { spawnSync } from "node:child_process";

const rootDir = resolve(new URL("..", import.meta.url).pathname);
const args = parseArgs(process.argv.slice(2));
loadDotEnv(resolve(rootDir, ".env.local"));

const databaseUrl = args["database-url"] || process.env.DATABASE_URL || "";
const sqlitePath = expandHome(args.sqlite || process.env.MSPACE_SQLITE_PATH || "~/.mspace/mspace.db");
let workspaceID = args["workspace-id"] || process.env.MSPACE_WORKSPACE_ID || "";
let ownerUserID = args["user-id"] || process.env.MSPACE_USER_ID || "";

if (!databaseUrl) {
  fail("DATABASE_URL is required. Set it in the environment, .env.local, or pass --database-url.");
}
if (!existsSync(sqlitePath)) {
  fail(`SQLite database not found: ${sqlitePath}`);
}

const projects = sqliteJSON(sqlitePath, `
  SELECT
    id,
    name,
    repo_path,
    source_type,
    remote_url,
    git_provider,
    git_owner,
    git_repo,
    default_branch,
    deploy_command,
    validation_command,
    kube_context,
    kubeconfig_path,
    namespace,
    image_registry_prefix,
    preview_domain,
    ingress_class,
    node_host,
    default_cluster_id,
    created_at,
    updated_at
  FROM projects
  ORDER BY created_at, id
`);

const runbooks = sqliteJSON(sqlitePath, `
  SELECT project_id, content, status, source, source_session_id, content_hash, created_at, updated_at
  FROM project_runbooks
  ORDER BY updated_at, project_id
`);

const runbookRevisions = sqliteJSON(sqlitePath, `
  SELECT id, project_id, session_id, author_type, author_name, content, content_hash, status, created_at
  FROM project_runbook_revisions
  ORDER BY created_at, id
`);

const issues = sortIssues(sqliteJSON(sqlitePath, `
  SELECT
    id,
    project_id,
    parent_issue_id,
    sort_order,
    title,
    body,
    status,
    close_reason,
    triage_status,
    assignee,
    assignee_type,
    creator_name,
    creator_avatar_url,
    environment_url,
    created_at,
    updated_at
  FROM issues
  ORDER BY created_at, id
`));

const comments = sqliteJSON(sqlitePath, `
  SELECT
    id,
    issue_id,
    author_type,
    author_user_id,
    author_name,
    author_avatar_url,
    body,
    created_at,
    updated_at,
    edited_at
  FROM comments
  ORDER BY created_at, id
`);

const labels = sqliteJSON(sqlitePath, `
  SELECT
    l.id,
    l.issue_id,
    l.label_id,
    d.key AS label_key,
    l.name,
    l.color,
    l.created_at
  FROM issue_labels l
  LEFT JOIN issue_label_definitions d ON d.id = l.label_id
  ORDER BY l.created_at, l.id
`);

const reactions = sqliteJSON(sqlitePath, `
  SELECT id, issue_id, comment_id, reaction, user_id, actor_name, actor_avatar_url, created_at
  FROM comment_reactions
  ORDER BY created_at, id
`);

const inboxItems = sqliteJSON(sqlitePath, `
  SELECT id, issue_id, project_id, title, status, unread, created_at, updated_at
  FROM inbox_items
  ORDER BY created_at, id
`);

if (!workspaceID) {
  const personalWorkspaces = psqlLines(`
    SELECT id::text
    FROM workspaces
    WHERE kind = 'personal'
    ORDER BY created_at, id;
  `);
  if (personalWorkspaces.length !== 1) {
    fail(`Expected exactly one personal workspace, found ${personalWorkspaces.length}. Pass --workspace-id explicitly.`);
  }
  workspaceID = personalWorkspaces[0];
}

if (!ownerUserID) {
  const workspaceUsers = psqlLines(`
    SELECT user_id::text
    FROM workspace_members
    WHERE workspace_id = ${sqlString(workspaceID)}::uuid
    ORDER BY
      CASE role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 ELSE 2 END,
      created_at,
      user_id
    LIMIT 1;
  `);
  if (workspaceUsers.length !== 1) {
    fail(`No workspace member found for ${workspaceID}. Pass --user-id explicitly.`);
  }
  ownerUserID = workspaceUsers[0];
}

const sql = `
BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

WITH source AS (
  SELECT value AS item
  FROM jsonb_array_elements(${jsonSQL(projects)}::jsonb)
)
INSERT INTO projects (
  id,
  workspace_id,
  name,
  repo_path,
  source_type,
  remote_url,
  git_provider,
  git_owner,
  git_repo,
  default_branch,
  deploy_command,
  validation_command,
  kube_context,
  kubeconfig_path,
  namespace,
  image_registry_prefix,
  preview_domain,
  ingress_class,
  node_host,
  default_cluster_id,
  created_by_user_id,
  created_at,
  updated_at
)
SELECT
  (item->>'id')::uuid,
  ${sqlString(workspaceID)}::uuid,
  NULLIF(item->>'name', ''),
  COALESCE(item->>'repo_path', ''),
  CASE WHEN item->>'source_type' IN ('local', 'github') THEN item->>'source_type' ELSE 'local' END,
  COALESCE(item->>'remote_url', ''),
  COALESCE(item->>'git_provider', ''),
  COALESCE(item->>'git_owner', ''),
  COALESCE(item->>'git_repo', ''),
  COALESCE(item->>'default_branch', ''),
  COALESCE(item->>'deploy_command', ''),
  COALESCE(item->>'validation_command', ''),
  COALESCE(item->>'kube_context', ''),
  COALESCE(item->>'kubeconfig_path', ''),
  COALESCE(item->>'namespace', ''),
  COALESCE(item->>'image_registry_prefix', ''),
  COALESCE(item->>'preview_domain', ''),
  COALESCE(item->>'ingress_class', ''),
  COALESCE(item->>'node_host', ''),
  COALESCE(item->>'default_cluster_id', ''),
  ${sqlString(ownerUserID)}::uuid,
  COALESCE(NULLIF(item->>'created_at', '')::timestamptz, now()),
  COALESCE(NULLIF(item->>'updated_at', '')::timestamptz, NULLIF(item->>'created_at', '')::timestamptz, now())
FROM source
ON CONFLICT (id) DO UPDATE SET
  workspace_id = EXCLUDED.workspace_id,
  name = EXCLUDED.name,
  repo_path = EXCLUDED.repo_path,
  source_type = EXCLUDED.source_type,
  remote_url = EXCLUDED.remote_url,
  git_provider = EXCLUDED.git_provider,
  git_owner = EXCLUDED.git_owner,
  git_repo = EXCLUDED.git_repo,
  default_branch = EXCLUDED.default_branch,
  deploy_command = EXCLUDED.deploy_command,
  validation_command = EXCLUDED.validation_command,
  kube_context = EXCLUDED.kube_context,
  kubeconfig_path = EXCLUDED.kubeconfig_path,
  namespace = EXCLUDED.namespace,
  image_registry_prefix = EXCLUDED.image_registry_prefix,
  preview_domain = EXCLUDED.preview_domain,
  ingress_class = EXCLUDED.ingress_class,
  node_host = EXCLUDED.node_host,
  default_cluster_id = EXCLUDED.default_cluster_id,
  created_by_user_id = COALESCE(projects.created_by_user_id, EXCLUDED.created_by_user_id),
  created_at = LEAST(projects.created_at, EXCLUDED.created_at),
  updated_at = EXCLUDED.updated_at;

WITH source AS (
  SELECT value AS item
  FROM jsonb_array_elements(${jsonSQL(runbooks)}::jsonb)
)
INSERT INTO project_runbooks (
  project_id,
  workspace_id,
  content,
  status,
  source,
  source_session_id,
  content_hash,
  created_at,
  updated_at
)
SELECT
  (item->>'project_id')::uuid,
  ${sqlString(workspaceID)}::uuid,
  COALESCE(item->>'content', ''),
  CASE WHEN item->>'status' IN ('empty', 'learned', 'stale') THEN item->>'status' ELSE 'learned' END,
  COALESCE(item->>'source', ''),
  COALESCE(item->>'source_session_id', ''),
  COALESCE(item->>'content_hash', ''),
  COALESCE(NULLIF(item->>'created_at', '')::timestamptz, now()),
  COALESCE(NULLIF(item->>'updated_at', '')::timestamptz, NULLIF(item->>'created_at', '')::timestamptz, now())
FROM source
ON CONFLICT (project_id) DO UPDATE SET
  workspace_id = EXCLUDED.workspace_id,
  content = EXCLUDED.content,
  status = EXCLUDED.status,
  source = EXCLUDED.source,
  source_session_id = EXCLUDED.source_session_id,
  content_hash = EXCLUDED.content_hash,
  created_at = LEAST(project_runbooks.created_at, EXCLUDED.created_at),
  updated_at = EXCLUDED.updated_at;

WITH source AS (
  SELECT value AS item
  FROM jsonb_array_elements(${jsonSQL(runbookRevisions)}::jsonb)
)
INSERT INTO project_runbook_revisions (
  id,
  workspace_id,
  project_id,
  session_id,
  author_type,
  author_name,
  content,
  content_hash,
  status,
  created_at
)
SELECT
  (item->>'id')::uuid,
  ${sqlString(workspaceID)}::uuid,
  (item->>'project_id')::uuid,
  COALESCE(item->>'session_id', ''),
  CASE WHEN item->>'author_type' IN ('human', 'agent', 'system') THEN item->>'author_type' ELSE 'system' END,
  COALESCE(item->>'author_name', ''),
  COALESCE(item->>'content', ''),
  COALESCE(item->>'content_hash', ''),
  COALESCE(item->>'status', 'learned'),
  COALESCE(NULLIF(item->>'created_at', '')::timestamptz, now())
FROM source
ON CONFLICT (id) DO UPDATE SET
  workspace_id = EXCLUDED.workspace_id,
  project_id = EXCLUDED.project_id,
  session_id = EXCLUDED.session_id,
  author_type = EXCLUDED.author_type,
  author_name = EXCLUDED.author_name,
  content = EXCLUDED.content,
  content_hash = EXCLUDED.content_hash,
  status = EXCLUDED.status,
  created_at = LEAST(project_runbook_revisions.created_at, EXCLUDED.created_at);

WITH source AS (
  SELECT value AS item
  FROM jsonb_array_elements(${jsonSQL(issues)}::jsonb)
)
INSERT INTO issues (
  id,
  workspace_id,
  project_id,
  parent_issue_id,
  sort_order,
  title,
  body,
  status,
  close_reason,
  triage_status,
  assignee,
  assignee_type,
  creator_user_id,
  creator_name,
  creator_avatar_url,
  environment_url,
  created_at,
  updated_at
)
SELECT
  (item->>'id')::uuid,
  ${sqlString(workspaceID)}::uuid,
  (item->>'project_id')::uuid,
  NULLIF(item->>'parent_issue_id', '')::uuid,
  COALESCE((NULLIF(item->>'sort_order', ''))::integer, 0),
  NULLIF(item->>'title', ''),
  COALESCE(item->>'body', ''),
  CASE item->>'status'
    WHEN 'needs_review' THEN 'needs_review'
    WHEN 'changes_requested' THEN 'changes_requested'
    WHEN 'ready_for_test' THEN 'ready_for_test'
    WHEN 'blocked' THEN 'blocked'
    WHEN 'cancelled' THEN 'cancelled'
    WHEN 'closed' THEN 'closed'
    WHEN 'review' THEN 'needs_review'
    WHEN 'testing' THEN 'ready_for_test'
    WHEN 'test_in_progress' THEN 'ready_for_test'
    WHEN 'completed' THEN 'closed'
    WHEN 'done' THEN 'closed'
    ELSE 'open'
  END,
  COALESCE(item->>'close_reason', ''),
  CASE WHEN item->>'triage_status' IN ('none', 'pending', 'classified', 'failed') THEN item->>'triage_status' ELSE 'none' END,
  COALESCE(item->>'assignee', ''),
  CASE WHEN item->>'assignee_type' IN ('human', 'agent') THEN item->>'assignee_type' ELSE 'human' END,
  ${sqlString(ownerUserID)}::uuid,
  COALESCE(NULLIF(item->>'creator_name', ''), 'Alex Lee'),
  COALESCE(item->>'creator_avatar_url', ''),
  COALESCE(item->>'environment_url', ''),
  COALESCE(NULLIF(item->>'created_at', '')::timestamptz, now()),
  COALESCE(NULLIF(item->>'updated_at', '')::timestamptz, NULLIF(item->>'created_at', '')::timestamptz, now())
FROM source
ON CONFLICT (id) DO UPDATE SET
  workspace_id = EXCLUDED.workspace_id,
  project_id = EXCLUDED.project_id,
  parent_issue_id = EXCLUDED.parent_issue_id,
  sort_order = EXCLUDED.sort_order,
  title = EXCLUDED.title,
  body = EXCLUDED.body,
  status = EXCLUDED.status,
  close_reason = EXCLUDED.close_reason,
  triage_status = EXCLUDED.triage_status,
  assignee = EXCLUDED.assignee,
  assignee_type = EXCLUDED.assignee_type,
  creator_user_id = COALESCE(issues.creator_user_id, EXCLUDED.creator_user_id),
  creator_name = EXCLUDED.creator_name,
  creator_avatar_url = EXCLUDED.creator_avatar_url,
  environment_url = EXCLUDED.environment_url,
  created_at = LEAST(issues.created_at, EXCLUDED.created_at),
  updated_at = EXCLUDED.updated_at;

WITH source AS (
  SELECT value AS item
  FROM jsonb_array_elements(${jsonSQL(comments)}::jsonb)
)
INSERT INTO comments (
  id,
  workspace_id,
  issue_id,
  author_type,
  author_user_id,
  author_name,
  author_avatar_url,
  body,
  created_at,
  updated_at,
  edited_at
)
SELECT
  (item->>'id')::uuid,
  ${sqlString(workspaceID)}::uuid,
  (item->>'issue_id')::uuid,
  CASE WHEN item->>'author_type' IN ('human', 'agent', 'system') THEN item->>'author_type' ELSE 'system' END,
  CASE
    WHEN item->>'author_type' = 'human' AND COALESCE(item->>'author_user_id', '') ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN (item->>'author_user_id')::uuid
    WHEN item->>'author_type' = 'human' THEN ${sqlString(ownerUserID)}::uuid
    ELSE NULL
  END,
  COALESCE(NULLIF(item->>'author_name', ''), CASE WHEN item->>'author_type' = 'human' THEN 'Alex Lee' ELSE '' END),
  COALESCE(item->>'author_avatar_url', ''),
  COALESCE(item->>'body', ''),
  COALESCE(NULLIF(item->>'created_at', '')::timestamptz, now()),
  COALESCE(NULLIF(item->>'updated_at', '')::timestamptz, NULLIF(item->>'created_at', '')::timestamptz, now()),
  NULLIF(item->>'edited_at', '')::timestamptz
FROM source
ON CONFLICT (id) DO UPDATE SET
  workspace_id = EXCLUDED.workspace_id,
  issue_id = EXCLUDED.issue_id,
  author_type = EXCLUDED.author_type,
  author_user_id = COALESCE(comments.author_user_id, EXCLUDED.author_user_id),
  author_name = EXCLUDED.author_name,
  author_avatar_url = EXCLUDED.author_avatar_url,
  body = EXCLUDED.body,
  created_at = LEAST(comments.created_at, EXCLUDED.created_at),
  updated_at = EXCLUDED.updated_at,
  edited_at = EXCLUDED.edited_at;

WITH source AS (
  SELECT value AS item
  FROM jsonb_array_elements(${jsonSQL(labels)}::jsonb)
)
INSERT INTO issue_labels (
  id,
  workspace_id,
  issue_id,
  label_id,
  name,
  color,
  created_at
)
SELECT
  (item->>'id')::uuid,
  ${sqlString(workspaceID)}::uuid,
  (item->>'issue_id')::uuid,
  def.id,
  COALESCE(NULLIF(item->>'name', ''), def.name),
  COALESCE(NULLIF(item->>'color', ''), def.color, ''),
  COALESCE(NULLIF(item->>'created_at', '')::timestamptz, now())
FROM source
LEFT JOIN issue_label_definitions def ON def.key = NULLIF(source.item->>'label_key', '')
ON CONFLICT (id) DO UPDATE SET
  workspace_id = EXCLUDED.workspace_id,
  issue_id = EXCLUDED.issue_id,
  label_id = EXCLUDED.label_id,
  name = EXCLUDED.name,
  color = EXCLUDED.color,
  created_at = LEAST(issue_labels.created_at, EXCLUDED.created_at);

WITH source AS (
  SELECT value AS item
  FROM jsonb_array_elements(${jsonSQL(reactions)}::jsonb)
)
INSERT INTO comment_reactions (
  id,
  workspace_id,
  issue_id,
  comment_id,
  reaction,
  user_id,
  actor_name,
  actor_avatar_url,
  created_at
)
SELECT
  (item->>'id')::uuid,
  ${sqlString(workspaceID)}::uuid,
  (item->>'issue_id')::uuid,
  (item->>'comment_id')::uuid,
  item->>'reaction',
  CASE
    WHEN COALESCE(item->>'user_id', '') ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN (item->>'user_id')::uuid
    ELSE ${sqlString(ownerUserID)}::uuid
  END,
  COALESCE(NULLIF(item->>'actor_name', ''), 'Alex Lee'),
  COALESCE(item->>'actor_avatar_url', ''),
  COALESCE(NULLIF(item->>'created_at', '')::timestamptz, now())
FROM source
WHERE item->>'reaction' IN ('thumbs_up', 'thumbs_down', 'laugh', 'hooray', 'confused', 'heart', 'rocket', 'eyes')
ON CONFLICT (id) DO UPDATE SET
  workspace_id = EXCLUDED.workspace_id,
  issue_id = EXCLUDED.issue_id,
  comment_id = EXCLUDED.comment_id,
  reaction = EXCLUDED.reaction,
  user_id = EXCLUDED.user_id,
  actor_name = EXCLUDED.actor_name,
  actor_avatar_url = EXCLUDED.actor_avatar_url,
  created_at = LEAST(comment_reactions.created_at, EXCLUDED.created_at);

WITH source AS (
  SELECT value AS item
  FROM jsonb_array_elements(${jsonSQL(issues)}::jsonb)
)
INSERT INTO issue_watchers (
  workspace_id,
  issue_id,
  user_id,
  reason,
  muted,
  created_at,
  updated_at
)
SELECT
  ${sqlString(workspaceID)}::uuid,
  item->>'id',
  ${sqlString(ownerUserID)}::uuid,
  'creator',
  FALSE,
  COALESCE(NULLIF(item->>'created_at', '')::timestamptz, now()),
  COALESCE(NULLIF(item->>'updated_at', '')::timestamptz, NULLIF(item->>'created_at', '')::timestamptz, now())
FROM source
ON CONFLICT (workspace_id, issue_id, user_id) DO UPDATE SET
  reason = issue_watchers.reason,
  muted = issue_watchers.muted,
  updated_at = EXCLUDED.updated_at;

WITH source AS (
  SELECT value AS item
  FROM jsonb_array_elements(${jsonSQL(inboxItems)}::jsonb)
),
inserted AS (
  INSERT INTO issue_events (
    id,
    workspace_id,
    issue_id,
    actor_user_id,
    kind,
    summary,
    payload,
    created_at
  )
  SELECT
    (item->>'id')::uuid,
    ${sqlString(workspaceID)}::uuid,
    item->>'issue_id',
    ${sqlString(ownerUserID)}::uuid,
    'legacy_inbox_item',
    COALESCE(NULLIF(item->>'title', ''), 'Imported legacy issue'),
    jsonb_build_object(
      'issueTitle', COALESCE(item->>'title', ''),
      'issueStatus', COALESCE(item->>'status', ''),
      'projectId', COALESCE(item->>'project_id', ''),
      'importedFrom', 'sqlite'
    ),
    COALESCE(NULLIF(item->>'created_at', '')::timestamptz, now())
  FROM source
  ON CONFLICT (id) DO UPDATE SET
    workspace_id = EXCLUDED.workspace_id,
    issue_id = EXCLUDED.issue_id,
    actor_user_id = EXCLUDED.actor_user_id,
    kind = EXCLUDED.kind,
    summary = EXCLUDED.summary,
    payload = EXCLUDED.payload,
    created_at = LEAST(issue_events.created_at, EXCLUDED.created_at)
  RETURNING id
)
INSERT INTO issue_event_receipts (
  event_id,
  workspace_id,
  issue_id,
  user_id,
  state,
  read_at,
  created_at,
  updated_at
)
SELECT
  (item->>'id')::uuid,
  ${sqlString(workspaceID)}::uuid,
  item->>'issue_id',
  ${sqlString(ownerUserID)}::uuid,
  CASE WHEN COALESCE((item->>'unread')::integer, 0) = 1 THEN 'unread' ELSE 'read' END,
  CASE WHEN COALESCE((item->>'unread')::integer, 0) = 1 THEN NULL ELSE COALESCE(NULLIF(item->>'updated_at', '')::timestamptz, now()) END,
  COALESCE(NULLIF(item->>'created_at', '')::timestamptz, now()),
  COALESCE(NULLIF(item->>'updated_at', '')::timestamptz, NULLIF(item->>'created_at', '')::timestamptz, now())
FROM source
ON CONFLICT (event_id, user_id) DO UPDATE SET
  workspace_id = EXCLUDED.workspace_id,
  issue_id = EXCLUDED.issue_id,
  state = EXCLUDED.state,
  read_at = EXCLUDED.read_at,
  updated_at = EXCLUDED.updated_at;

COMMIT;
`;

runPSQL(sql);

const counts = psqlLines(`
  SELECT 'projects=' || count(*) FROM projects WHERE workspace_id = ${sqlString(workspaceID)}::uuid;
  SELECT 'issues=' || count(*) FROM issues WHERE workspace_id = ${sqlString(workspaceID)}::uuid;
  SELECT 'comments=' || count(*) FROM comments WHERE workspace_id = ${sqlString(workspaceID)}::uuid;
  SELECT 'labels=' || count(*) FROM issue_labels WHERE workspace_id = ${sqlString(workspaceID)}::uuid;
  SELECT 'runbooks=' || count(*) FROM project_runbooks WHERE workspace_id = ${sqlString(workspaceID)}::uuid;
  SELECT 'inbox_events=' || count(*) FROM issue_events WHERE workspace_id = ${sqlString(workspaceID)}::uuid AND kind = 'legacy_inbox_item';
`);

console.log(`Imported local SQLite product data into workspace ${workspaceID}`);
console.log(`SQLite source: ${sqlitePath}`);
console.log(counts.join("\n"));

function parseArgs(argv) {
  const result = {};
  for (let index = 0; index < argv.length; index += 1) {
    const arg = argv[index];
    if (!arg.startsWith("--")) continue;
    const eqIndex = arg.indexOf("=");
    if (eqIndex >= 0) {
      result[arg.slice(2, eqIndex)] = arg.slice(eqIndex + 1);
      continue;
    }
    const key = arg.slice(2);
    const value = argv[index + 1];
    if (!value || value.startsWith("--")) {
      result[key] = "true";
      continue;
    }
    result[key] = value;
    index += 1;
  }
  return result;
}

function loadDotEnv(path) {
  if (!existsSync(path)) return;
  const text = readFileSync(path, "utf8");
  for (const line of text.split(/\r?\n/)) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith("#")) continue;
    const match = /^([A-Za-z_][A-Za-z0-9_]*)=(.*)$/.exec(trimmed);
    if (!match) continue;
    const [, key, rawValue] = match;
    if (process.env[key]) continue;
    process.env[key] = rawValue.replace(/^['"]|['"]$/g, "");
  }
}

function expandHome(path) {
  if (path === "~") return homedir();
  if (path.startsWith("~/")) return resolve(homedir(), path.slice(2));
  return resolve(path);
}

function sqliteJSON(dbPath, query) {
  const result = spawnSync("sqlite3", ["-json", dbPath, query], { encoding: "utf8" });
  if (result.status !== 0) {
    fail(`sqlite3 failed:\n${result.stderr || result.stdout}`);
  }
  const output = result.stdout.trim();
  if (!output) return [];
  try {
    return JSON.parse(output);
  } catch (error) {
    fail(`Failed to parse sqlite3 JSON output: ${error.message}`);
  }
}

function psqlLines(query) {
  const result = spawnSync("psql", [databaseUrl, "-X", "-A", "-t", "-v", "ON_ERROR_STOP=1", "-c", query], {
    encoding: "utf8",
  });
  if (result.status !== 0) {
    fail(`psql failed:\n${result.stderr || result.stdout}`);
  }
  return result.stdout
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean);
}

function runPSQL(query) {
  const result = spawnSync("psql", [databaseUrl, "-X", "-q", "-v", "ON_ERROR_STOP=1"], {
    input: query,
    encoding: "utf8",
  });
  if (result.status !== 0) {
    fail(`psql import failed:\n${result.stderr || result.stdout}`);
  }
}

function jsonSQL(value) {
  const text = JSON.stringify(value);
  let tag = "mspace_json";
  while (text.includes(`$${tag}$`)) tag += "_x";
  return `$${tag}$${text}$${tag}$`;
}

function sqlString(value) {
  return `'${String(value).replaceAll("'", "''")}'`;
}

function sortIssues(rows) {
  const pending = new Map(rows.map((row) => [row.id, row]));
  const sorted = [];
  let moved = true;
  while (pending.size > 0 && moved) {
    moved = false;
    for (const [id, row] of [...pending.entries()]) {
      const parentID = row.parent_issue_id || "";
      if (!parentID || !pending.has(parentID)) {
        sorted.push(row);
        pending.delete(id);
        moved = true;
      }
    }
  }
  return sorted.concat([...pending.values()]);
}

function fail(message) {
  console.error(message);
  process.exit(1);
}
