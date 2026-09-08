import { spawnSync } from "node:child_process";
import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";
import { ESLint } from "eslint";

const repositoryRoot = process.cwd();
const script = path.join(repositoryRoot, "scripts", "check-code-style.mjs");
mkdirSync(path.join(repositoryRoot, ".artifacts"), { recursive: true });

const run = (fixtureRoot, ...args) =>
  spawnSync(process.execPath, [script, ...args], {
    cwd: fixtureRoot,
    encoding: "utf8",
    stdio: "pipe",
  });

test("ESLint excludes repository artifacts without hiding source", async () => {
  const lint = new ESLint({ cwd: repositoryRoot });
  assert.equal(
    await lint.isPathIgnored(".artifacts/worktrees/example/src/example.ts"),
    true,
  );
  for (const file of [
    "src/example.ts",
    "src/.artifacts/example.ts",
    ".artifacts-other/example.ts",
  ]) {
    assert.equal(await lint.isPathIgnored(file), false, file);
    const results = await lint.lintText("const unusedArtifactProbe = 1;", {
      filePath: file.replace(/\.ts$/, ".mjs"),
    });
    assert.ok(
      results.some((result) =>
        result.messages.some(
          (message) => message.ruleId === "@typescript-eslint/no-unused-vars",
        ),
      ),
      `source must still be linted: ${file}`,
    );
  }
});

test("ignores only the fixture repository artifact subtree", (t) => {
  const fixtureRoot = mkdtempSync(
    path.join(repositoryRoot, ".artifacts", "check-code-style-"),
  );
  t.after(() => rmSync(fixtureRoot, { recursive: true, force: true }));

  const artifactFile = path.join(
    fixtureRoot,
    ".artifacts",
    "worktree",
    "forbidden.mjs",
  );
  mkdirSync(path.dirname(artifactFile), { recursive: true });
  writeFileSync(artifactFile, "try { work(); } catch (error) { report(error); }\n");
  const defaultRun = run(fixtureRoot);
  const explicitArtifactRun = run(fixtureRoot, artifactFile);
  if (defaultRun.status !== 0 || explicitArtifactRun.status !== 0) {
    assert.fail(
      `artifact fixture was not ignored: ${defaultRun.stderr}${explicitArtifactRun.stderr}`,
    );
  }
});

test("checks real source, nested source .artifacts, and .artifacts-other", (t) => {
  const fixtureRoot = mkdtempSync(
    path.join(repositoryRoot, ".artifacts", "check-code-style-"),
  );
  t.after(() => rmSync(fixtureRoot, { recursive: true, force: true }));

  const sourceFile = path.join(fixtureRoot, "src", "forbidden.mjs");
  const nestedArtifactFile = path.join(
    fixtureRoot,
    "src",
    ".artifacts",
    "forbidden.mjs",
  );
  const similarlyNamedFile = path.join(
    fixtureRoot,
    ".artifacts-other",
    "forbidden.mjs",
  );
  for (const file of [sourceFile, nestedArtifactFile, similarlyNamedFile]) {
    mkdirSync(path.dirname(file), { recursive: true });
    writeFileSync(file, "try { work(); } catch (error) { report(error); }\n");
  }

  const defaultRun = run(fixtureRoot);
  const nestedRun = run(fixtureRoot, nestedArtifactFile);
  const similarlyNamedRun = run(fixtureRoot, similarlyNamedFile);
  for (const result of [defaultRun, nestedRun, similarlyNamedRun]) {
    if (result.status === 0 || !result.stderr.includes("try-catch-boundary")) {
      assert.fail(
        `real source fixture was not rejected: ${result.stdout}${result.stderr}`,
      );
    }
  }
});
