#!/usr/bin/env node
// Extract Go doc comments into markdown for the prose checker.
//
// godoc is published prose, so it is held to the same style as the pages. The
// extractor emits one block per package comment and per exported top-level
// declaration, each preceded by a `<!-- path:line -->` marker and a link whose
// text is `path:line`, so a reported sentence maps back to the Go source.
//
//   node scripts/prose-check-godoc.mjs [root]
//
// Skips *_test.go (godoc does not publish tests), testdata (generated
// fixtures), vendor and .agents (vendored skills). Those exclusions are the
// only ones, and each has the reason stated here.

import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative } from "node:path";

const root = process.argv[2] ?? process.cwd();

function walk(dir, found = []) {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (entry.name.startsWith(".")) continue;
    const path = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (entry.name === "testdata" || entry.name === "vendor" || entry.name === "node_modules") continue;
      walk(path, found);
    } else if (entry.name.endsWith(".go") && !entry.name.endsWith("_test.go")) {
      found.push(path);
    }
  }
  return found;
}

// A doc comment is a run of `//` lines immediately above a declaration, with no
// blank line between. The blank line is what makes the comment a file comment
// instead, and the Go documentation tool does not publish those.
function docBlocks(lines) {
  const blocks = [];
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    if (!line.startsWith("//")) continue;
    // The first line of a run only.
    if (i > 0 && lines[i - 1].startsWith("//")) continue;
    let j = i;
    const parts = [];
    while (j < lines.length && lines[j].startsWith("//")) {
      parts.push(lines[j].replace(/^\/\/ ?/, ""));
      j++;
    }
    if (j >= lines.length || lines[j].trim() === "") {
      i = j;
      continue;
    }
    const decl = lines[j].trim();
    const exported =
      /^package\s+\w+$/.test(decl) ||
      /^func\s+[A-Z]/.test(decl) ||
      /^func\s+\([^)]*\)\s+[A-Z]/.test(decl) ||
      /^type\s+[A-Z]/.test(decl) ||
      /^(const|var)\s+[A-Z]/.test(decl) ||
      /^(const|var)\s*\($/.test(decl);
    if (exported) blocks.push({ line: i + 1, text: parts.join("\n").trim() });
    i = j;
  }
  return blocks;
}

const sections = [];
for (const file of walk(root).sort()) {
  const rel = relative(root, file);
  const lines = readFileSync(file, "utf8").split("\n");
  for (const block of docBlocks(lines)) {
    if (!block.text) continue;
    const where = `${rel}:${block.line}`;
    sections.push(
      [
        `<!-- ${where} -->`,
        `[${where}](https://github.com/Manu343726/cppgo/blob/main/${rel}#L${block.line}) ${block.text}`,
        "",
      ].join("\n"),
    );
  }
}

process.stdout.write(sections.join("\n"));
