#!/usr/bin/env node
// Confirms a version is genuinely installable from the public registry, after
// publishing it.
//
// A green `npm publish` is not the same claim. The failure that matters to a
// user is `npx @jadenrazo/llm-lint` not working, and that can break while every
// publish succeeds: a platform package missing from the set, an optionalDependencies
// pin that points at a version that was never uploaded, or an engines.node floor
// that excludes the user's Node. Each of those publishes perfectly happily.
//
// So this does what a user does - install from the registry and run the binary.
//
//   --version <semver>   Required.
//   --tag <dist-tag>     Also assert this dist-tag resolves to --version.
//   --timeout <seconds>  How long to wait for registry propagation. Default 180.
//   --no-install         Metadata checks only (for platforms we cannot exercise).

import { spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import { PLATFORMS, PARENT_NAME, packageNameFor } from "./platforms.mjs";

const REGISTRY = "https://registry.npmjs.org";

function parseArgs(argv) {
  const out = { timeout: 180, install: true };
  for (let i = 2; i < argv.length; i++) {
    const a = argv[i];
    if (a === "--version") out.version = argv[++i];
    else if (a === "--tag") out.tag = argv[++i];
    else if (a === "--timeout") out.timeout = Number(argv[++i]);
    else if (a === "--no-install") out.install = false;
    else if (a === "--help" || a === "-h") out.help = true;
    else throw new Error(`Unknown arg: ${a}`);
  }
  return out;
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function packument(name) {
  const res = await fetch(`${REGISTRY}/${name.replace("/", "%2F")}`, {
    headers: { Accept: "application/vnd.npm.install-v1+json" },
  });
  if (res.status === 404) return null;
  if (!res.ok) throw new Error(`${name}: registry returned ${res.status}`);
  return res.json();
}

// The registry is read through a CDN, so a just-published version can take a
// few seconds to become visible. Poll rather than assert once.
async function waitForVersion(name, version, deadline) {
  for (;;) {
    const doc = await packument(name);
    if (doc?.versions?.[version]) return doc;
    if (Date.now() > deadline) {
      throw new Error(
        `${name}@${version} is still not on the registry after the timeout.\n` +
        (doc
          ? `    Available: ${Object.keys(doc.versions ?? {}).slice(-5).join(", ")}`
          : `    The package does not exist at all.`),
      );
    }
    await sleep(5000);
  }
}

async function main() {
  const args = parseArgs(process.argv);
  if (args.help) {
    process.stdout.write("Usage: verify-published.mjs --version <semver> [--tag <t>] [--timeout <s>] [--no-install]\n");
    process.exit(0);
  }
  if (!args.version) {
    process.stderr.write("error: --version is required\n");
    process.exit(2);
  }
  args.version = args.version.replace(/^v/, "");

  const deadline = Date.now() + args.timeout * 1000;
  const names = [...PLATFORMS.map(packageNameFor), PARENT_NAME];

  process.stdout.write(`Verifying ${names.length} package(s) at ${args.version} on ${REGISTRY}\n\n`);

  for (const name of names) {
    await waitForVersion(name, args.version, deadline);
    process.stdout.write(`  ok  ${name}@${args.version}\n`);
  }

  const parent = await packument(PARENT_NAME);

  // The parent is useless if its optionalDependencies point anywhere but the
  // versions we just published.
  const optional = parent.versions[args.version].optionalDependencies ?? {};
  for (const plat of PLATFORMS) {
    const pkg = packageNameFor(plat);
    if (optional[pkg] !== args.version) {
      process.stderr.write(
        `\nerror: ${PARENT_NAME}@${args.version} pins ${pkg} at ` +
        `${optional[pkg] ?? "(absent)"}, expected ${args.version}\n`,
      );
      process.exit(1);
    }
  }
  process.stdout.write(`  ok  optionalDependencies all pin ${args.version}\n`);

  if (args.tag) {
    const resolved = parent["dist-tags"]?.[args.tag];
    if (resolved !== args.version) {
      process.stderr.write(
        `\nerror: dist-tag "${args.tag}" resolves to ${resolved ?? "(unset)"}, expected ${args.version}\n`,
      );
      process.exit(1);
    }
    process.stdout.write(`  ok  dist-tag ${args.tag} -> ${args.version}\n`);
  }

  if (!args.install) {
    process.stdout.write(`\nMetadata verified (install smoke skipped).\n`);
    return;
  }

  // Install exactly the way a user would, and run the binary.
  const key = `${process.platform}-${process.arch}`;
  if (!PLATFORMS.some((p) => p.node === key)) {
    process.stdout.write(`\nnote: ${key} is not a supported platform; skipping the install smoke\n`);
    return;
  }

  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "llm-lint-verify-"));
  try {
    process.stdout.write(`\nInstall smoke in ${dir} (${key})\n`);
    fs.writeFileSync(path.join(dir, "package.json"), JSON.stringify({ name: "verify", private: true }) + "\n");

    const install = spawnSync(
      "npm",
      ["install", "--no-audit", "--no-fund", "--include=optional", `${PARENT_NAME}@${args.version}`],
      { cwd: dir, encoding: "utf8" },
    );
    if (install.status !== 0) {
      process.stderr.write(`\nerror: install failed\n${install.stderr || install.stdout}\n`);
      process.exit(1);
    }

    const bin = path.join(dir, "node_modules", ".bin", "llm-lint");
    if (!fs.existsSync(bin)) {
      process.stderr.write(`\nerror: ${PARENT_NAME} installed but no llm-lint binary was linked\n`);
      process.exit(1);
    }

    const run = spawnSync(bin, ["version"], { encoding: "utf8" });
    if (run.status !== 0) {
      process.stderr.write(`\nerror: llm-lint version exited ${run.status}\n${run.stderr || run.stdout}\n`);
      process.exit(1);
    }
    const out = (run.stdout || "").trim();
    if (!out.includes(args.version)) {
      process.stderr.write(`\nerror: installed binary reports "${out}", expected it to contain ${args.version}\n`);
      process.exit(1);
    }
    process.stdout.write(`  ok  ${out}\n`);
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }

  process.stdout.write(`\n${args.version} is published and installable.\n`);
}

main().catch((err) => {
  process.stderr.write(`\nerror: ${err.message}\n`);
  process.exit(1);
});
