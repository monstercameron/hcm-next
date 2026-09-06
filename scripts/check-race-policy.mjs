#!/usr/bin/env node

// TOOL-012: runs the concurrent-package race-coverage policy
// (tools/policy/racepolicy) against the root Go module and fails the way
// check-go-style.mjs does when it finds a problem.
//
// This complements — it does not replace — the go-core CI job's own
// `go test -race -count=1 ./...` step in .github/workflows/tests.yml,
// which proves the race class for real on Linux. That step only ever
// races whatever tests already exist; this script instead proves
// *coverage*: every package that imports "sync"/"sync/atomic" or starts a
// goroutine has at least one test file that would resolve (and therefore
// actually run) under that CI job's linux/amd64 build context, so a
// concurrent package can never pass the race step by having zero tests to
// race in the first place.
//
// go.mod is not editable from this lane, so this stays a plain `go run`
// invocation rather than adding a new `tool` directive.

import { execFileSync } from "node:child_process";
import { existsSync } from "node:fs";

if (!existsSync("go.mod")) {
  process.stdout.write(
    "Race coverage policy check skipped: go.mod was not found at the repository root.\n",
  );
  process.exit(0);
}

try {
  const output = execFileSync(
    "go",
    ["run", "./tools/policy/racepolicy/cmd/racepolicy", "-root", "."],
    { encoding: "utf8" },
  );
  process.stdout.write(output);
  process.stdout.write("Race coverage policy check passed.\n");
} catch (err) {
  if (err.stdout) process.stdout.write(err.stdout);
  if (err.stderr) process.stderr.write(err.stderr);
  process.stderr.write(
    "Race coverage policy check failed: at least one concurrent package has no test file that would run under -race on Linux CI.\n" +
      "See tools/policy/racepolicy's package doc for the mechanism and how to add a suite for the named package(s).\n",
  );
  process.exit(typeof err.status === "number" && err.status !== 0 ? err.status : 1);
}
