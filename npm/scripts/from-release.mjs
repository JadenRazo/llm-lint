#!/usr/bin/env node
// Rebuilds the `--dist` input for build.mjs out of the archives attached to a
// published GitHub release, instead of out of a fresh goreleaser run.
//
// Why this exists: the npm publish is the last step of release.yml, after
// goreleaser has already created the GitHub release, pushed the ghcr images and
// written to the sigstore transparency log. Every one of those is irreversible.
// When the npm step alone failed on v0.4.0 (a dead NPM_TOKEN) there was no way
// to finish the release without re-running goreleaser against an existing tag or
// moving the tag - so the release simply stayed half-published for days.
//
// With this script the npm half can be replayed on its own, from the exact
// bytes users already download, at any time. See .github/workflows/publish-npm.yml.
//
//   --assets <dir>    Directory holding the release archives + checksums.txt. Required.
//   --out <dir>       Directory to write the extracted binaries into. Required.
//   --version <sem>   Expected version. Optional; only used for the summary line.
//   --skip-checksums  Escape hatch for a release cut before checksums.txt existed.
//
// The binaries are written under the names build.mjs's findBinaryViaConvention()
// looks for, so the caller then runs:
//
//   node npm/scripts/build.mjs --version <v> --dist <out>

import { spawnSync } from "node:child_process";
import crypto from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import { PLATFORMS } from "./platforms.mjs";

// build.mjs's findBinaryViaConvention() looks for exactly this filename inside
// the --dist directory.
const outNameFor = (plat) => `llm-lint-${plat.suffix}${plat.ext}`;
// The binary inside a goreleaser archive is always the bare project name.
const memberFor = (plat) => `llm-lint${plat.ext}`;

function parseArgs(argv) {
  const out = { skipChecksums: false };
  for (let i = 2; i < argv.length; i++) {
    const a = argv[i];
    if (a === "--assets") out.assets = argv[++i];
    else if (a === "--out") out.out = argv[++i];
    else if (a === "--version") out.version = argv[++i];
    else if (a === "--skip-checksums") out.skipChecksums = true;
    else if (a === "--help" || a === "-h") out.help = true;
    else throw new Error(`Unknown arg: ${a}`);
  }
  return out;
}

function die(msg, code = 1) {
  process.stderr.write(`from-release: ${msg}\n`);
  process.exit(code);
}

function sha256(file) {
  return crypto.createHash("sha256").update(fs.readFileSync(file)).digest("hex");
}

// checksums.txt is `<sha256>  <filename>` per line, as emitted by goreleaser.
function readChecksums(assetsDir) {
  const p = path.join(assetsDir, "checksums.txt");
  if (!fs.existsSync(p)) return null;
  const map = new Map();
  for (const line of fs.readFileSync(p, "utf8").split("\n")) {
    const m = /^([0-9a-f]{64})\s+\*?(.+?)\s*$/.exec(line);
    if (m) map.set(m[2], m[1]);
  }
  return map;
}

function run(cmd, args, cwd) {
  const r = spawnSync(cmd, args, { cwd, stdio: ["ignore", "pipe", "pipe"], encoding: "utf8" });
  if (r.error && r.error.code === "ENOENT") die(`required tool not found on PATH: ${cmd}`);
  if (r.status !== 0) die(`${cmd} ${args.join(" ")} failed (exit ${r.status}):\n${r.stderr || r.stdout}`);
  return r.stdout;
}

function extract(archive, member, destDir) {
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "llm-lint-rel-"));
  try {
    if (archive.endsWith(".zip")) run("unzip", ["-qo", archive, member, "-d", tmp]);
    else run("tar", ["-xzf", archive, "-C", tmp, member]);
    const extracted = path.join(tmp, member);
    if (!fs.existsSync(extracted)) {
      die(`archive ${path.basename(archive)} did not contain ${member}`);
    }
    fs.mkdirSync(path.dirname(destDir), { recursive: true });
    fs.copyFileSync(extracted, destDir);
    // 0o755 = rwxr-xr-x. The archive preserves the bit but copyFileSync does not.
    fs.chmodSync(destDir, 0o755);
    return fs.statSync(destDir).size;
  } finally {
    fs.rmSync(tmp, { recursive: true, force: true });
  }
}

function main() {
  const args = parseArgs(process.argv);
  if (args.help) {
    process.stdout.write("Usage: from-release.mjs --assets <dir> --out <dir> [--version <semver>] [--skip-checksums]\n");
    process.exit(0);
  }
  if (!args.assets) die("--assets is required", 2);
  if (!args.out) die("--out is required", 2);
  if (!fs.existsSync(args.assets)) die(`assets directory not found: ${args.assets}`, 2);

  const missing = PLATFORMS.filter((p) => !fs.existsSync(path.join(args.assets, p.archive)));
  if (missing.length > 0) {
    die(
      `release is missing ${missing.length} archive(s):\n` +
      missing.map((p) => `  ${p.archive}`).join("\n") +
      `\nEvery supported platform must be present; publishing a partial set would\n` +
      `leave the parent package pinning optionalDependencies that do not exist.`,
    );
  }

  const checksums = readChecksums(args.assets);
  if (!checksums && !args.skipChecksums) {
    die("checksums.txt not found in --assets (pass --skip-checksums to override)");
  }

  fs.mkdirSync(args.out, { recursive: true });

  for (const plat of PLATFORMS) {
    const archive = path.join(args.assets, plat.archive);

    if (checksums) {
      const want = checksums.get(plat.archive);
      if (!want) die(`checksums.txt has no entry for ${plat.archive}`);
      const got = sha256(archive);
      if (got !== want) {
        die(`checksum mismatch for ${plat.archive}\n  expected ${want}\n  got      ${got}`);
      }
    }

    const dest = path.join(args.out, outNameFor(plat));
    const size = extract(archive, memberFor(plat), dest);
    process.stdout.write(
      `${checksums ? "verified" : "extracted"}  ${plat.archive.padEnd(30)} -> ${outNameFor(plat)} (${size} bytes)\n`,
    );
  }

  process.stdout.write(
    `\n${PLATFORMS.length} binaries ready in ${args.out}` +
    `${args.version ? ` for version ${args.version}` : ""}\n` +
    `Next: node npm/scripts/build.mjs --version <v> --dist ${args.out}\n`,
  );
}

main();
