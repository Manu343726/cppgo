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
// fixtures), vendor and .agents (vendored skills), and files that carry the
// standard "Code generated ... DO NOT EDIT." line (their comments are emitted
// by a generator from string literals, and that text is part of the
// generator's contract, quoted as-is by the documentation). Those exclusions
// are the only ones, and each has the reason stated here.

import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative } from "node:path";

const root = process.argv[2] ?? process.cwd();

// The Go convention for a generated file: the first line says so.
const GENERATED = /^\/\/ Code generated .* DO NOT EDIT\.$/m;

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

// A generator writes its templates as raw string literals, and those literals
// contain `//` lines that look exactly like doc comments. They are string
// contents, which the Go parser never publishes as comments. Blank out every
// string, rune and block comment while keeping newlines and line comments, so
// the scanner below sees the `//` lines the Go parser sees. Byte offsets and
// line numbers stay unchanged.
function maskStrings(source) {
  const out = source.split("");
  let state = "code";
  let i = 0;
  while (i < source.length) {
    const c = source[i];
    const next = source[i + 1];
    if (state === "code") {
      if (c === "/" && next === "/") { state = "line"; i += 2; continue; }
      if (c === "/" && next === "*") { out[i] = out[i + 1] = " "; state = "block"; i += 2; continue; }
      if (c === '"') { out[i] = " "; state = "string"; i += 1; continue; }
      if (c === "'") { out[i] = " "; state = "rune"; i += 1; continue; }
      if (c === "`") { out[i] = " "; state = "raw"; i += 1; continue; }
      i += 1;
      continue;
    }
    if (state === "line") {
      if (c === "\n") state = "code";
      i += 1;
      continue;
    }
    if (state === "block") {
      if (c === "*" && next === "/") { out[i] = out[i + 1] = " "; state = "code"; i += 2; continue; }
      if (c !== "\n") out[i] = " ";
      i += 1;
      continue;
    }
    if (state === "string" || state === "rune") {
      if (c === "\\") {
        out[i] = " ";
        if (next !== undefined && next !== "\n") out[i + 1] = " ";
        i += 2;
        continue;
      }
      if (c === (state === "string" ? '"' : "'")) { out[i] = " "; state = "code"; i += 1; continue; }
      if (c !== "\n") out[i] = " ";
      i += 1;
      continue;
    }
    // raw string
    if (c === "`") { out[i] = " "; state = "code"; i += 1; continue; }
    if (c !== "\n") out[i] = " ";
    i += 1;
  }
  return out.join("");
}

// A doc comment is a run of `//` lines immediately above a declaration, with no
// blank line between. The blank line is what makes the comment a file comment
// instead, and the Go documentation tool does not publish those. `scan` decides
// where comments start, `lines` supplies the text.
function docBlocks(scan, lines) {
  const blocks = [];
  for (let i = 0; i < scan.length; i++) {
    if (!scan[i].startsWith("//")) continue;
    // The first line of a run only.
    if (i > 0 && scan[i - 1].startsWith("//")) continue;
    let j = i;
    const parts = [];
    while (j < scan.length && scan[j].startsWith("//")) {
      parts.push(lines[j].replace(/^\/\/ ?/, ""));
      j++;
    }
    if (j >= scan.length || scan[j].trim() === "") {
      i = j;
      continue;
    }
    const decl = scan[j].trim();
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
  const source = readFileSync(file, "utf8");
  if (GENERATED.test(source)) continue;
  const lines = source.split("\n");
  const scan = maskStrings(source).split("\n");
  for (const block of docBlocks(scan, lines)) {
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
