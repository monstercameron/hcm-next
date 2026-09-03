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
      const isIgnored = normalizePath(filePath)
        .split("/")
        .some((segment) => ignoredPathSegments.has(segment));
      return isIgnored ? [] : goFilesIn(filePath, ignoredPathSegments);
    }

    return entry.isFile() && filePath.endsWith(".go") ? [filePath] : [];
  });

const runGofmtCheck = (label, goFiles) => {
  if (goFiles.length === 0) {
    process.stdout.write(`${label}: no Go files found, skipping gofmt.\n`);
    return;
  }

  const gofmtOutput = execFileSync("gofmt", ["-l", ...goFiles], {
    encoding: "utf8",
  });
  const unformattedFiles = gofmtOutput
    .split(/\r?\n/u)
    .map((line) => line.trim())
    .filter(Boolean);

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
