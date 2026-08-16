#!/usr/bin/env node
// Exercises build.mjs on every pull request, using stub binaries.
//
// build.mjs otherwise runs exactly once per release, inside the one job that
// also creates the GitHub release and pushes container images - so a break in
// it is discovered at the worst possible moment, half way through an
// irreversible release. Two seconds of fake binaries here means the packaging
// code is exercised on every commit like everything else.
//
// This checks the packaging, not the binaries: the "binaries" are four bytes.

import { spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { PLATFORMS, PARENT_NAME, packageNameFor } from "./platforms.mjs";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const NPM_DIR = path.resolve(__dirname, "..");
const VERSION = "9.9.9-smoke";

const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "llm-lint-smoke-"));
const dist = path.join(tmp, "dist");
const out = path.join(tmp, "out");
fs.mkdirSync(dist, { recursive: true });

const problems = [];

try {
  for (const plat of PLATFORMS) {
    fs.writeFileSync(path.join(dist, `llm-lint-${plat.suffix}${plat.ext}`), "stub");
  }

  const build = spawnSync(
    process.execPath,
    [path.join(__dirname, "build.mjs"), "--version", VERSION, "--dist", dist, "--out", out],
    { encoding: "utf8" },
  );
  if (build.status !== 0) {
    process.stderr.write(`smoke-build: build.mjs exited ${build.status}\n${build.stderr || build.stdout}\n`);
    process.exit(1);
  }

  const expected = [...PLATFORMS.map(packageNameFor), PARENT_NAME].sort();
  const built = new Map();
  for (const entry of fs.readdirSync(out)) {
    const pj = path.join(out, entry, "package.json");
    if (fs.existsSync(pj)) {
      const pkg = JSON.parse(fs.readFileSync(pj, "utf8"));
      built.set(pkg.name, { pkg, dir: path.join(out, entry) });
    }
  }

  const gotNames = [...built.keys()].sort();
  if (JSON.stringify(gotNames) !== JSON.stringify(expected)) {
    problems.push(
      `built packages do not match the platform matrix\n` +
      `    missing: ${expected.filter((n) => !gotNames.includes(n)).join(", ") || "(none)"}\n` +
      `    extra:   ${gotNames.filter((n) => !expected.includes(n)).join(", ") || "(none)"}`,
    );
  }

  for (const [name, { pkg, dir }] of built) {
    if (pkg.version !== VERSION) problems.push(`${name} was stamped ${pkg.version}, expected ${VERSION}`);

    const isParent = name === PARENT_NAME;
    const binName = isParent
      ? "llm-lint.js"
      : `llm-lint${PLATFORMS.find((p) => packageNameFor(p) === name).ext}`;
    const bin = path.join(dir, "bin", binName);
    if (!fs.existsSync(bin)) problems.push(`${name} has no bin/${binName}`);
    if (!fs.existsSync(path.join(dir, "LICENSE"))) problems.push(`${name} has no LICENSE`);
    if (!fs.existsSync(path.join(dir, "README.md"))) problems.push(`${name} has no README.md`);

    if (isParent) {
      for (const plat of PLATFORMS) {
        if (pkg.optionalDependencies?.[packageNameFor(plat)] !== VERSION) {
          problems.push(`parent pins ${packageNameFor(plat)} at ${pkg.optionalDependencies?.[packageNameFor(plat)]}, expected ${VERSION}`);
        }
      }
      // build.mjs strips dev scripts from the published artifact; a stray
      // lifecycle script here would execute on every user's install.
      if (pkg.scripts) problems.push(`parent still carries scripts: ${Object.keys(pkg.scripts).join(", ")}`);
    } else {
      // npm uses these to pick the right binary; without them every platform
      // package installs on every machine.
      if (!Array.isArray(pkg.os) || !Array.isArray(pkg.cpu)) {
        problems.push(`${name} is missing os/cpu, so npm cannot filter it by platform`);
      }
    }
  }

  // The source manifest must not have been mutated by the build.
  const sourceVersion = JSON.parse(fs.readFileSync(path.join(NPM_DIR, "package.json"), "utf8")).version;
  if (sourceVersion !== "0.0.0-dev") {
    problems.push(`build.mjs rewrote npm/package.json version to ${sourceVersion}; it must stay the placeholder`);
  }
} finally {
  fs.rmSync(tmp, { recursive: true, force: true });
}

if (problems.length > 0) {
  process.stderr.write(`\nsmoke-build: ${problems.length} problem(s)\n\n`);
  for (const p of problems) process.stderr.write(`  ${p}\n`);
  process.exit(1);
}

process.stdout.write(`smoke-build: ok - build.mjs produced ${PLATFORMS.length + 1} well-formed packages at ${VERSION}\n`);
