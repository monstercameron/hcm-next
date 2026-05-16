#!/usr/bin/env node

import { execFileSync } from "node:child_process";
import { existsSync, readdirSync } from "node:fs";
import path from "node:path";

const goRoot = "src/blocks/go";
const ignoredPathSegments = new Set([".git", "dist", "tmp", "vendor"]);

const normalizePath = (filePath) =>
  filePath.replaceAll("\\", "/").replace(/^\.\//u, "");

const isIgnoredPath = (filePath) =>
  normalizePath(filePath)
    .split("/")
    .some((segment) => ignoredPathSegments.has(segment));

const goFilesIn = (directory) =>
  readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const filePath = normalizePath(path.join(directory, entry.name));

    if (entry.isDirectory()) {
      return isIgnoredPath(filePath) ? [] : goFilesIn(filePath);
    }

    return entry.isFile() && filePath.endsWith(".go") ? [filePath] : [];
  });

if (!existsSync(goRoot)) {
  process.stdout.write("Go style check skipped: src/blocks/go was not found.\n");
  process.exit(0);
}

const goFiles = goFilesIn(goRoot);

if (goFiles.length === 0) {
  process.stdout.write("Go style check skipped: no Go files found.\n");
  process.exit(0);
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
      "Go style check failed: gofmt reported unformatted files.",
      "Run `gofmt -w` on the listed files.",
      "",
      ...unformattedFiles.map((filePath) => `- ${normalizePath(filePath)}`),
      "",
    ].join("\n"),
  );
  process.exit(1);
}

execFileSync("go", ["vet", "./..."], {
  cwd: goRoot,
  stdio: "inherit",
});

process.stdout.write(
  `Go style check passed: ${goFiles.length} Go file${
    goFiles.length === 1 ? "" : "s"
  } formatted; go vet passed.\n`,
);
