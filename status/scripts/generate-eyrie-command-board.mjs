#!/usr/bin/env node

import { mkdir, readdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const statusDir = path.resolve(scriptDir, "..");
const root = path.resolve(statusDir, "..");
const itemsDir = path.join(statusDir, "items");
const outputJson = path.join(statusDir, "eyrie-command-board.json");
const outputJs = path.join(statusDir, "eyrie-command-board-data.js");

const boardUrl = "status/eyrie-command-board.html";
const manifestRef = "status/eyrie-command-board.json";
const sourceId = "eyrie";

function parseScalar(rawValue) {
  const trimmed = String(rawValue || "").trim();
  if (/^(true|false)$/i.test(trimmed)) {
    return /^true$/i.test(trimmed);
  }
  const quoted = trimmed.match(/^["'](.*)["']$/);
  return quoted ? quoted[1] : trimmed;
}

function parseFrontmatter(text, filePath) {
  const data = {};
  let listKey = null;

  text.split(/\r?\n/).forEach((line, index) => {
    if (!line.trim()) {
      return;
    }

    const listItem = line.match(/^\s{2}-\s+(.*)$/);
    if (listItem && listKey) {
      data[listKey].push(parseScalar(listItem[1]));
      return;
    }

    const field = line.match(/^([A-Za-z0-9_]+):(?:\s*(.*))?$/);
    if (!field) {
      throw new Error(`${filePath}:${index + 1}: unsupported frontmatter line: ${line}`);
    }

    const [, key, rawValue = ""] = field;
    if (rawValue === "") {
      data[key] = [];
      listKey = key;
    } else {
      data[key] = parseScalar(rawValue);
      listKey = null;
    }
  });

  return data;
}

function splitMarkdown(content, filePath) {
  if (!content.startsWith("---\n")) {
    throw new Error(`${filePath}: missing YAML frontmatter`);
  }

  const closeIndex = content.indexOf("\n---\n", 4);
  if (closeIndex === -1) {
    throw new Error(`${filePath}: missing closing frontmatter marker`);
  }

  return {
    frontmatter: content.slice(4, closeIndex),
    body: content.slice(closeIndex + 5).trim(),
  };
}

function stripMarkdown(text) {
  return String(text || "")
    .replace(/^#{1,6}\s+/gm, "")
    .replace(/\*\*([^*]+)\*\*/g, "$1")
    .replace(/`([^`]+)`/g, "$1")
    .replace(/\[([^\]]+)\]\([^)]+\)/g, "$1")
    .trim();
}

function sectionText(markdown, headingName) {
  const lines = markdown.split(/\r?\n/);
  const heading = headingName.toLowerCase();
  const start = lines.findIndex((line) => line.replace(/^#+\s+/, "").trim().toLowerCase() === heading);
  if (start === -1) {
    return "";
  }

  const collected = [];
  for (let index = start + 1; index < lines.length; index += 1) {
    if (/^#{1,6}\s+/.test(lines[index])) {
      break;
    }
    collected.push(lines[index]);
  }
  return collected.join("\n").trim();
}

function firstParagraph(markdown) {
  return stripMarkdown(
    markdown
      .split(/\n{2,}/)
      .map((paragraph) => paragraph.trim())
      .filter(Boolean)
      .filter((paragraph) => !paragraph.startsWith("#"))
      .filter((paragraph) => !paragraph.startsWith("- "))[0] || ""
  );
}

function detailText(markdown) {
  return stripMarkdown(markdown).replace(/\n{3,}/g, "\n\n");
}

function countBy(items, field) {
  return items.reduce((counts, item) => {
    const value = String(item[field] || "unknown");
    counts[value] = (counts[value] || 0) + 1;
    return counts;
  }, {});
}

function localPathFor(filePath) {
  return path.relative(root, filePath).split(path.sep).join("/");
}

function absoluteLocalPath(relativePath) {
  return path.join(root, relativePath);
}

async function readItems() {
  const entries = await readdir(itemsDir, { withFileTypes: true });
  const markdownFiles = entries
    .filter((entry) => entry.isFile())
    .filter((entry) => entry.name.endsWith(".md"))
    .sort((a, b) => a.name.localeCompare(b.name));

  const items = [];
  for (const entry of markdownFiles) {
    const filePath = path.join(itemsDir, entry.name);
    const content = await readFile(filePath, "utf8");
    const { frontmatter, body } = splitMarkdown(content, filePath);
    const data = parseFrontmatter(frontmatter, filePath);
    const id = data.id || path.basename(entry.name, ".md");
    const linkedItemRef = localPathFor(filePath);
    const absoluteLinkedItemRef = absoluteLocalPath(linkedItemRef);

    items.push({
      id,
      title: data.title || id,
      status: data.status || "active",
      priority: data.priority || "normal",
      lane: data.captain_column || data.lane || "active",
      column: data.commander_column || data.column || "capture",
      captain_column: data.captain_column || data.lane || "active",
      commander_column: data.commander_column || data.column || "capture",
      owner: data.owner || "Eyrie/Ops",
      primary_agent: data.primary_agent || "Magnus/Eyrie",
      posted_by: data.posted_by || "Magnus/Eyrie",
      source_label: data.source_label || "Eyrie",
      source: data.source || absoluteLinkedItemRef,
      summary: data.summary || firstParagraph(body),
      next_action: data.next_action || firstParagraph(sectionText(body, "Next Action")),
      task_state: data.task_state,
      task_id: data.task_id,
      task_ref: data.task_ref,
      assigned_to: data.assigned_to,
      accountable_agent: data.accountable_agent,
      origin_notice_id: data.origin_notice_id,
      notification_refs: Array.isArray(data.notification_refs) ? data.notification_refs : undefined,
      blocked_by: data.blocked_by,
      commander_visible: data.commander_visible === true,
      source_id: sourceId,
      captain: "Magnus/Eyrie",
      captain_board_profile: "eyrie",
      linked_item_ref: absoluteLinkedItemRef,
      local_board_url: boardUrl,
      local_item_url: `${boardUrl}#item=${encodeURIComponent(id)}`,
      local_manifest_ref: absoluteLocalPath(manifestRef),
      updated: data.updated || "",
      details: detailText(body),
    });
  }

  return items;
}

function attentionSnapshot(items) {
  const visible = items.filter((item) => item.commander_visible);
  const active = items.filter((item) => ["active", "waiting"].includes(String(item.status)));
  return {
    "Commander-visible": visible.map((item) => `${item.title}: ${item.next_action}`),
    "Local-only": items
      .filter((item) => !item.commander_visible)
      .map((item) => `${item.title}: ${item.next_action}`),
    "Active or waiting": active.map((item) => `${item.title}: ${item.summary}`),
  };
}

async function main() {
  const items = await readItems();
  const generatedAt = new Date().toISOString();
  const manifest = {
    schema_version: 1,
    kind: "captain-command-board",
    generated_at: generatedAt,
    captain: "Magnus/Eyrie",
    domain: "Eyrie",
    board_profile: "eyrie",
    captain_meta: {
      address: "magnus.eyrie",
      project_id: "eyrie",
      canonical_commander_inbox: "Eyrie/Ops",
    },
    commander_interop: {
      source_id: sourceId,
      summary_items: true,
      deep_links: true,
      global_card_id: "eyrie-captain-board",
      recommended_global_label: "Magnus/Eyrie Captain Board",
      local_board_url: boardUrl,
      local_manifest_ref: manifestRef,
      canonical_item_source: "status/items/*.md",
    },
    sources: {
      items: "status/items/*.md",
      local_mesh: "docs/agent-mesh/manifest.yaml",
      runtime_registry: "docs/runtime-registry",
      commander_inbox: "/Users/dan/Documents/Personal/Commander/Shared/notices/eyrie-inbox.yaml",
    },
    attention_snapshot: attentionSnapshot(items),
    agents: [
      {
        name: "Magnus/Eyrie",
        address: "magnus.eyrie",
        role: "Captain and coordinator prototype",
        status: "active",
      },
      {
        name: "Danya/Eyrie",
        address: "danya.eyrie",
        role: "Companion engineer under Magnus",
        status: "active",
      },
      {
        name: "Hermes/Eyrie",
        address: "hermes.eyrie",
        role: "Runtime-control agent under Magnus",
        status: "available",
      },
      {
        name: "Eyrie Docs",
        address: "docs.eyrie",
        role: "Documentation and sync lane under Magnus",
        status: "available",
      },
    ],
    items,
    counts: {
      total: items.length,
      commander_visible: items.filter((item) => item.commander_visible).length,
      local_only: items.filter((item) => !item.commander_visible).length,
      by_status: countBy(items, "status"),
      by_priority: countBy(items, "priority"),
      by_lane: countBy(items, "lane"),
    },
  };

  await mkdir(statusDir, { recursive: true });
  await writeFile(outputJson, `${JSON.stringify(manifest, null, 2)}\n`);
  await writeFile(outputJs, `window.eyrieCommandBoard = ${JSON.stringify(manifest, null, 2)};\n`);
  console.log(`Wrote ${outputJson}`);
  console.log(`Wrote ${outputJs}`);
}

main().catch((error) => {
  console.error(error);
  process.exit(1);
});
