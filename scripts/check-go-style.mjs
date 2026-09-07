#!/usr/bin/env node

import { execFileSync } from "node:child_process";
import { existsSync, readdirSync } from "node:fs";
import path from "node:path";

const normalizePath = (filePath) =>
  filePath.replaceAll("\\", "/").replace(/^\.\//u, "");

const goFilesIn = (directory, ignoredPathSegments) =>
  readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const filePath = normalizePath(path.join(directory, entry.name));

    if (entry.isDirectory()) {
      // Dot-prefixed directories (.git, .artifacts, lane and cache scratch)
      // are never source; they may hold generated _testmain.go files.
      const isIgnored = normalizePath(filePath)
        .split("/")
        .some((segment) => ignoredPathSegments.has(segment) || segment.startsWith("."));
      return isIgnored ? [] : goFilesIn(filePath, ignoredPathSegments);
    }

    return entry.isFile() && filePath.endsWith(".go") ? [filePath] : [];
  });

const runGofmtCheck = (label, goFiles) => {
  if (goFiles.length === 0) {
    process.stdout.write(`${label}: no Go files found, skipping gofmt.\n`);
    return;
  }

  // Windows caps a command line at 32 KB and the repository holds more Go
  // files than fit in one argv, so gofmt runs over bounded chunks.
  const chunkSize = 200;
  const unformattedFiles = [];
  for (let start = 0; start < goFiles.length; start += chunkSize) {
    const chunk = goFiles.slice(start, start + chunkSize);
    const gofmtOutput = execFileSync("gofmt", ["-l", ...chunk], {
      encoding: "utf8",
    });
    for (const line of gofmtOutput.split(/\r?\n/u)) {
      const trimmed = line.trim();
      if (trimmed) {
        unformattedFiles.push(trimmed);
      }
    }
  }

  if (unformattedFiles.length > 0) {
    process.stderr.write(
      [
        `${label}: gofmt reported unformatted files.`,
        "Run `gofmt -w` on the listed files.",
        "",
        ...unformattedFiles.map((filePath) => `- ${normalizePath(filePath)}`),
        "",
      ].join("\n"),
    );
    process.exit(1);
  }

  process.stdout.write(
    `${label}: ${goFiles.length} Go file${goFiles.length === 1 ? "" : "s"} formatted.\n`,
  );
};

// --- Legacy module: src/blocks/go (its own go.mod, hcm-next-executor) ---

const legacyGoRoot = "src/blocks/go";
const legacyIgnoredPathSegments = new Set([".git", "dist", "tmp", "vendor"]);

if (!existsSync(legacyGoRoot)) {
  process.stdout.write("Legacy Go style check skipped: src/blocks/go was not found.\n");
} else {
  const legacyGoFiles = goFilesIn(legacyGoRoot, legacyIgnoredPathSegments);
  runGofmtCheck("Legacy Go style check (src/blocks/go)", legacyGoFiles);

  if (legacyGoFiles.length > 0) {
    execFileSync("go", ["vet", "./..."], {
      cwd: legacyGoRoot,
      stdio: "inherit",
    });
    process.stdout.write("Legacy Go style check (src/blocks/go): go vet passed.\n");
  }
}

// --- Root module: github.com/monstercameron/hcm-next (repository root go.mod) ---
//
// Walks every root-module Go file except the legacy module (src/), vendored
// JS (node_modules/) and Go's own package-discovery exclusions (testdata/,
// which intentionally holds tools/quality's malformed fixtures and must
// never be gofmt-clean). gen/ is included: generated Protobuf/Go output is
// expected to already be gofmt-clean.
const rootIgnoredPathSegments = new Set([
  ".artifacts",
  ".git",
  "dist",
  "tmp",
  "vendor",
  "node_modules",
  "src",
  "testdata",
]);

if (!existsSync("go.mod")) {
  process.stdout.write(
    "Root Go style check skipped: go.mod was not found at the repository root.\n",
  );
} else {
  const rootGoFiles = goFilesIn(".", rootIgnoredPathSegments);
  runGofmtCheck("Root Go style check", rootGoFiles);

  if (rootGoFiles.length > 0) {
    execFileSync("go", ["vet", "./..."], {
      stdio: "inherit",
    });
    process.stdout.write("Root Go style check: go vet ./... passed.\n");
  }
}

process.stdout.write("Go style check passed.\n");
