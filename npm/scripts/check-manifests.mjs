#!/usr/bin/env node
// Asserts the npm distribution's manifests agree with each other, in CI, before
// anything is published.
//
// Every check here exists because the corresponding drift actually happened or
// would have shipped silently:
//
//   1. engines floor  - Renovate's "update node.js to v24" (#27) rewrote
//                       npm/package.json engines.node from ">=18.20.8" to
//                       ">=24.15.0". engines.node in a *published* package is a
//                       consumer support floor, not a build toolchain pin; that
//                       bump would have made `npx @jadenrazo/llm-lint` fail for
//                       every user on Node 18/20/22 LTS. It sat on main for
//                       months and was only caught because the publish that
//                       would have shipped it failed for an unrelated reason.
//   2. optionalDeps   - the parent resolves the native binary through
//                       optionalDependencies. A platform present in the build
//                       matrix but absent here is simply never installed.
//   3. bin dispatch   - npm/bin/llm-lint.js maps process.platform-process.arch
//                       to a package name. A platform missing there fails at
//                       runtime with "unsupported platform", after install.
//   4. goreleaser     - the release archives are the input to from-release.mjs.
//                       If goreleaser stops building a target, the npm package
//                       for it silently stops being publishable.
//
// Run: node npm/scripts/check-manifests.mjs

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { PLATFORMS, packageNameFor } from "./platforms.mjs";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const NPM_DIR = path.resolve(__dirname, "..");
const REPO_ROOT = path.resolve(NPM_DIR, "..");

// Raising this is a deliberate, breaking decision for every installed user, so
// it must be an explicit edit to this file and not a side effect of a bot bump.
const MAX_SUPPORTED_NODE_FLOOR_MAJOR = 18;

const failures = [];
const fail = (check, msg) => failures.push(`${check}: ${msg}`);
const readJSON = (p) => JSON.parse(fs.readFileSync(p, "utf8"));

const expectedPackages = PLATFORMS.map(packageNameFor).sort();

// ---------------------------------------------------------------- 1. engines
const parentPkg = readJSON(path.join(NPM_DIR, "package.json"));
const floor = parentPkg.engines?.node;

if (typeof floor !== "string") {
  fail("engines", "npm/package.json has no engines.node");
} else {
  const m = /^>=\s*(\d+)(?:\.(\d+))?(?:\.(\d+))?$/.exec(floor.trim());
  if (!m) {
    fail("engines", `engines.node must be a plain ">=X.Y.Z" floor, got ${JSON.stringify(floor)}`);
  } else if (Number(m[1]) > MAX_SUPPORTED_NODE_FLOOR_MAJOR) {
    fail(
      "engines",
      `engines.node is ${floor}, which drops support for Node ` +
        `${MAX_SUPPORTED_NODE_FLOOR_MAJOR}..${Number(m[1]) - 1}.\n` +
        `    This is almost always a dependency bot mistaking a consumer support\n` +
        `    floor for a build toolchain pin. The CLI is a Go binary behind a tiny\n` +
        `    CommonJS wrapper; it does not need a modern Node.\n` +
        `    If the drop is intentional, raise MAX_SUPPORTED_NODE_FLOOR_MAJOR in\n` +
        `    ${path.relative(REPO_ROOT, fileURLToPath(import.meta.url))} in the same commit,\n` +
        `    and cut it as a major version.`,
    );
  }
}

if (parentPkg.version !== "0.0.0-dev") {
  fail(
    "version",
    `npm/package.json version must stay the "0.0.0-dev" placeholder (build.mjs stamps ` +
      `the real one from the tag); found ${JSON.stringify(parentPkg.version)}`,
  );
}

// -------------------------------------------------------- 2. optionalDependencies
const optional = Object.keys(parentPkg.optionalDependencies ?? {}).sort();
if (JSON.stringify(optional) !== JSON.stringify(expectedPackages)) {
  fail(
    "optionalDependencies",
    `npm/package.json optionalDependencies do not match the platform matrix.\n` +
      `    missing: ${expectedPackages.filter((p) => !optional.includes(p)).join(", ") || "(none)"}\n` +
      `    extra:   ${optional.filter((p) => !expectedPackages.includes(p)).join(", ") || "(none)"}`,
  );
}

// ------------------------------------------------------------- 3. bin dispatch
const wrapperPath = path.join(NPM_DIR, "bin", "llm-lint.js");
const wrapperSrc = fs.readFileSync(wrapperPath, "utf8");
const wrapperBlock = /const PLATFORMS = \{([\s\S]*?)\};/.exec(wrapperSrc);

if (!wrapperBlock) {
  fail("bin", `could not find the PLATFORMS map in ${path.relative(REPO_ROOT, wrapperPath)}`);
} else {
  const wrapperMap = new Map();
  for (const line of wrapperBlock[1].split("\n")) {
    const m = /^\s*"([^"]+)":\s*"([^"]+)"/.exec(line);
    if (m) wrapperMap.set(m[1], m[2]);
  }
  for (const plat of PLATFORMS) {
    const got = wrapperMap.get(plat.node);
    if (!got) {
      fail("bin", `${path.relative(REPO_ROOT, wrapperPath)} has no entry for "${plat.node}"`);
    } else if (got !== packageNameFor(plat)) {
      fail("bin", `"${plat.node}" maps to ${got}, expected ${packageNameFor(plat)}`);
    }
  }
  for (const key of wrapperMap.keys()) {
    if (!PLATFORMS.some((p) => p.node === key)) {
      fail("bin", `${path.relative(REPO_ROOT, wrapperPath)} has "${key}", which is not in the platform matrix`);
    }
  }
}

// -------------------------------------------------------------- 4. goreleaser
const goreleaserPath = [".goreleaser.yaml", ".goreleaser.yml"]
  .map((f) => path.join(REPO_ROOT, f))
  .find((p) => fs.existsSync(p));

if (!goreleaserPath) {
  fail("goreleaser", "no .goreleaser.yaml / .goreleaser.yml at the repo root");
} else {
  const src = fs.readFileSync(goreleaserPath, "utf8");
  const list = (key) => {
    const m = new RegExp(`^\\s*${key}:\\s*\\[([^\\]]*)\\]`, "m").exec(src);
    return m ? m[1].split(",").map((s) => s.trim()).filter(Boolean) : null;
  };
  const goos = list("goos");
  const goarch = list("goarch");

  if (!goos || !goarch) {
    fail(
      "goreleaser",
      `could not read the inline goos/goarch lists from ${path.basename(goreleaserPath)}. ` +
        `If the build matrix moved to block style, update this check - do not delete it.`,
    );
  } else {
    for (const plat of PLATFORMS) {
      if (!goos.includes(plat.goos) || !goarch.includes(plat.goarch)) {
        fail(
          "goreleaser",
          `${plat.goos}/${plat.goarch} is in the npm platform matrix but goreleaser ` +
            `does not build it, so ${plat.archive} will never exist`,
        );
      }
    }
    const built = goos.length * goarch.length;
    if (built !== PLATFORMS.length) {
      fail(
        "goreleaser",
        `goreleaser builds ${built} target(s) (${goos.join("/")} x ${goarch.join("/")}) but the npm ` +
          `platform matrix has ${PLATFORMS.length}. Every built target should be published, or the ` +
          `release ships binaries npm users cannot get.`,
      );
    }
  }
}

// -------------------------------------------------------------------- report
if (failures.length > 0) {
  process.stderr.write(`\ncheck-manifests: ${failures.length} problem(s)\n\n`);
  for (const f of failures) process.stderr.write(`  ${f}\n\n`);
  process.exit(1);
}

process.stdout.write(
  `check-manifests: ok - ${PLATFORMS.length} platforms, engines.node ${floor}, ` +
    `optionalDependencies and bin dispatch agree with the matrix\n`,
);
