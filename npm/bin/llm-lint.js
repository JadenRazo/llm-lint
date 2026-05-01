#!/usr/bin/env node
// Resolves the platform-specific @jadenrazo/llm-lint-<os>-<arch> package
// installed via optionalDependencies, then execs the native Go binary.
//
// Mirrors the model used by esbuild, swc, biome, turbo, and rollup:
// no postinstall, no network at install time, works offline once npm
// has fetched the matching optional dep.

"use strict";

const { spawnSync } = require("node:child_process");
const path = require("node:path");
const fs = require("node:fs");

const PLATFORMS = {
  "linux-x64":    "@jadenrazo/llm-lint-linux-x64",
  "linux-arm64":  "@jadenrazo/llm-lint-linux-arm64",
  "darwin-x64":   "@jadenrazo/llm-lint-darwin-x64",
  "darwin-arm64": "@jadenrazo/llm-lint-darwin-arm64",
  "win32-x64":    "@jadenrazo/llm-lint-win32-x64",
  "win32-arm64":  "@jadenrazo/llm-lint-win32-arm64",
};

function die(msg, code = 1) {
  process.stderr.write(`llm-lint: ${msg}\n`);
  process.exit(code);
}

function resolveBinary() {
  const key = `${process.platform}-${process.arch}`;
  const pkg = PLATFORMS[key];
  if (!pkg) {
    die(
      `unsupported platform ${key}.\n` +
      `  Supported: ${Object.keys(PLATFORMS).join(", ")}\n` +
      `  Download a binary directly from https://github.com/JadenRazo/llm-lint/releases`,
    );
  }

  let pkgJsonPath;
  try {
    pkgJsonPath = require.resolve(`${pkg}/package.json`, { paths: [__dirname] });
  } catch {
    die(
      `platform package ${pkg} is not installed.\n` +
      `  This usually means npm skipped optional dependencies. Reinstall with:\n` +
      `    npm install --include=optional @jadenrazo/llm-lint\n` +
      `  Or via npx: npx --include=optional @jadenrazo/llm-lint`,
    );
  }

  const ext = process.platform === "win32" ? ".exe" : "";
  const binPath = path.join(path.dirname(pkgJsonPath), "bin", `llm-lint${ext}`);
  if (!fs.existsSync(binPath)) {
    die(`binary missing inside ${pkg} (expected ${binPath}). Try reinstalling.`);
  }
  return binPath;
}

const bin = resolveBinary();
const result = spawnSync(bin, process.argv.slice(2), {
  stdio: "inherit",
  windowsHide: true,
});

if (result.error) {
  if (result.error.code === "EACCES") {
    die(`binary at ${bin} is not executable. Try reinstalling the package.`);
  }
  die(`failed to execute ${bin}: ${result.error.message}`);
}

if (result.signal) {
  // Re-raise the signal so the parent shell sees the right exit reason
  // (e.g. 130 for SIGINT). Falling back to status if signal isn't fatal.
  process.kill(process.pid, result.signal);
  return;
}

process.exit(result.status ?? 1);
