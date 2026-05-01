#!/usr/bin/env node
// Publishes the built npm packages in npm/build/ to the registry.
// Platform packages publish first; the parent publishes last so that the
// optionalDependencies it pins always already exist.
//
//   --version <semver>    Required. Must match build.mjs --version output.
//   --dry-run             Run `npm publish --dry-run` (no upload).
//   --tag <dist-tag>      npm dist-tag for the parent. Default: latest.
//                         Pre-releases auto-default to "next".
//   --provenance          Pass --provenance to npm publish (requires CI OIDC).
//   --skip-existing       Don't fail if a version is already published
//                         (useful for retrying a partial publish).

import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const NPM_DIR = path.resolve(__dirname, "..");
const REPO_ROOT = path.resolve(NPM_DIR, "..");

function parseArgs(argv) {
  const out = { dryRun: false, provenance: false, skipExisting: false };
  for (let i = 2; i < argv.length; i++) {
    const a = argv[i];
    if (a === "--version") out.version = argv[++i];
    else if (a === "--dry-run") out.dryRun = true;
    else if (a === "--tag") out.tag = argv[++i];
    else if (a === "--provenance") out.provenance = true;
    else if (a === "--skip-existing") out.skipExisting = true;
    else if (a === "--build-dir") out.buildDir = argv[++i];
    else if (a === "--help" || a === "-h") out.help = true;
    else throw new Error(`Unknown arg: ${a}`);
  }
  return out;
}

function help() {
  process.stdout.write(
    `Usage: publish.mjs --version <semver> [--tag latest|next|...] [--dry-run]\n` +
    `                   [--provenance] [--skip-existing] [--build-dir <path>]\n`,
  );
  process.exit(0);
}

function autoTag(version) {
  return /-/.test(version) ? "next" : "latest";
}

function listPackages(buildDir) {
  return fs
    .readdirSync(buildDir)
    .map((name) => path.join(buildDir, name))
    .filter((p) => fs.statSync(p).isDirectory() && fs.existsSync(path.join(p, "package.json")));
}

function readName(pkgDir) {
  return JSON.parse(fs.readFileSync(path.join(pkgDir, "package.json"), "utf8")).name;
}

function isParent(pkgDir) {
  return readName(pkgDir) === "@jadenrazo/llm-lint";
}

function alreadyPublished(name, version) {
  const r = spawnSync("npm", ["view", `${name}@${version}`, "version", "--silent"], {
    encoding: "utf8",
  });
  return r.status === 0 && r.stdout.trim() === version;
}

function publish(pkgDir, args) {
  const name = readName(pkgDir);
  const cmd = ["publish", "--access", "public"];
  if (args.dryRun) cmd.push("--dry-run");
  if (args.provenance && !args.dryRun) cmd.push("--provenance");
  if (args.tag) cmd.push("--tag", args.tag);

  if (args.skipExisting && !args.dryRun && alreadyPublished(name, args.version)) {
    process.stdout.write(`skip ${name}@${args.version} (already published)\n`);
    return;
  }

  process.stdout.write(`\n→ npm ${cmd.join(" ")}    (in ${path.relative(REPO_ROOT, pkgDir)})\n`);
  const r = spawnSync("npm", cmd, { cwd: pkgDir, stdio: "inherit" });
  if (r.status !== 0) {
    process.stderr.write(`error: failed to publish ${name}\n`);
    process.exit(r.status ?? 1);
  }
}

function main() {
  const args = parseArgs(process.argv);
  if (args.help) help();
  if (!args.version) {
    process.stderr.write("error: --version is required\n");
    process.exit(2);
  }
  args.version = args.version.replace(/^v/, "");
  if (!args.tag) args.tag = autoTag(args.version);

  const buildDir = path.resolve(args.buildDir ?? path.join(NPM_DIR, "build"));
  if (!fs.existsSync(buildDir)) {
    process.stderr.write(`error: build directory not found: ${buildDir}\n`);
    process.stderr.write(`run: node npm/scripts/build.mjs --version ${args.version}\n`);
    process.exit(2);
  }

  const all = listPackages(buildDir);
  if (all.length === 0) {
    process.stderr.write(`error: no packages found in ${buildDir}\n`);
    process.exit(2);
  }

  // Verify every package has the requested version before publishing any.
  for (const dir of all) {
    const pj = JSON.parse(fs.readFileSync(path.join(dir, "package.json"), "utf8"));
    if (pj.version !== args.version) {
      process.stderr.write(
        `error: ${pj.name} has version ${pj.version}, expected ${args.version}.\n` +
        `       rebuild with: node npm/scripts/build.mjs --version ${args.version}\n`,
      );
      process.exit(2);
    }
  }

  const platformPkgs = all.filter((p) => !isParent(p));
  const parentPkgs = all.filter((p) => isParent(p));
  if (parentPkgs.length !== 1) {
    process.stderr.write(`error: expected exactly 1 parent package, found ${parentPkgs.length}\n`);
    process.exit(2);
  }

  process.stdout.write(
    `Publishing ${platformPkgs.length} platform package(s) + 1 parent at ` +
    `version ${args.version} (tag: ${args.tag})${args.dryRun ? " [DRY RUN]" : ""}\n`,
  );

  for (const dir of platformPkgs) publish(dir, args);
  publish(parentPkgs[0], args);

  process.stdout.write(`\nDone. Try: npx @jadenrazo/llm-lint@${args.version} version\n`);
}

main();
