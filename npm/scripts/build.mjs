#!/usr/bin/env node
// Generates per-platform npm packages plus a version-stamped parent
// package under `npm/build/`, suitable for `npm publish` or `npm pack`.
//
// Inputs:
//   --version <semver>   The version to stamp on every package. Required.
//   --dist <path>        Path to a directory containing raw native binaries.
//                        Default: ../dist (goreleaser output).
//                        Layout expected (one of):
//                          a) goreleaser dist/ with artifacts.json
//                          b) plain dir with files named like:
//                             llm-lint-linux-x64, llm-lint-linux-arm64,
//                             llm-lint-darwin-x64, llm-lint-darwin-arm64,
//                             llm-lint-win32-x64.exe, llm-lint-win32-arm64.exe
//   --out <path>         Output directory. Default: ./build (relative to npm/).
//   --only <plat>        Build only one platform package (for local dev). Optional.
//
// Output layout:
//   build/llm-lint/                       (parent)
//   build/llm-lint-linux-x64/             (platform)
//   build/llm-lint-linux-arm64/
//   build/llm-lint-darwin-x64/
//   build/llm-lint-darwin-arm64/
//   build/llm-lint-win32-x64/
//   build/llm-lint-win32-arm64/

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const NPM_DIR = path.resolve(__dirname, "..");
const REPO_ROOT = path.resolve(NPM_DIR, "..");

const SCOPE = "@jadenrazo";
const PARENT_NAME = `${SCOPE}/llm-lint`;

// node platform-arch  →  goreleaser  →  npm package suffix  →  binary file ext
const PLATFORMS = [
  { node: "linux-x64",    goos: "linux",   goarch: "amd64", suffix: "linux-x64",    ext: "",     os: "linux",  cpu: "x64"   },
  { node: "linux-arm64",  goos: "linux",   goarch: "arm64", suffix: "linux-arm64",  ext: "",     os: "linux",  cpu: "arm64" },
  { node: "darwin-x64",   goos: "darwin",  goarch: "amd64", suffix: "darwin-x64",   ext: "",     os: "darwin", cpu: "x64"   },
  { node: "darwin-arm64", goos: "darwin",  goarch: "arm64", suffix: "darwin-arm64", ext: "",     os: "darwin", cpu: "arm64" },
  { node: "win32-x64",    goos: "windows", goarch: "amd64", suffix: "win32-x64",    ext: ".exe", os: "win32",  cpu: "x64"   },
  { node: "win32-arm64",  goos: "windows", goarch: "arm64", suffix: "win32-arm64",  ext: ".exe", os: "win32",  cpu: "arm64" },
];

function parseArgs(argv) {
  const out = {};
  for (let i = 2; i < argv.length; i++) {
    const a = argv[i];
    if (a === "--version") out.version = argv[++i];
    else if (a === "--dist") out.dist = argv[++i];
    else if (a === "--out") out.out = argv[++i];
    else if (a === "--only") out.only = argv[++i];
    else if (a === "--help" || a === "-h") out.help = true;
    else throw new Error(`Unknown arg: ${a}`);
  }
  return out;
}

function help() {
  process.stdout.write(`Usage: build.mjs --version <semver> [--dist <path>] [--out <path>] [--only <plat>]\n`);
  process.exit(0);
}

function readArtifactsJson(distDir) {
  const p = path.join(distDir, "artifacts.json");
  if (!fs.existsSync(p)) return null;
  try {
    return JSON.parse(fs.readFileSync(p, "utf8"));
  } catch (err) {
    throw new Error(`Failed to parse ${p}: ${err.message}`);
  }
}

function findBinaryViaArtifacts(artifacts, plat) {
  // goreleaser artifact entries with type "Binary" have GOOS/GOARCH in `extra` or `goos`/`goarch` fields
  return artifacts.find((a) => {
    if (a.type !== "Binary") return false;
    const aGoos = a.goos ?? a.extra?.Goos;
    const aGoarch = a.goarch ?? a.extra?.Goarch;
    return aGoos === plat.goos && aGoarch === plat.goarch;
  });
}

function findBinaryViaConvention(distDir, plat) {
  // Fallback for local dev: look for files named after node convention
  const candidates = [
    path.join(distDir, `llm-lint-${plat.suffix}${plat.ext}`),
    path.join(distDir, `llm-lint_${plat.goos}_${plat.goarch}`, `llm-lint${plat.ext}`),
    path.join(distDir, `llm-lint_${plat.goos}_${plat.goarch}_v1`, `llm-lint${plat.ext}`),
    path.join(distDir, `llm-lint_${plat.goos}_${plat.goarch}_v8.0`, `llm-lint${plat.ext}`),
  ];
  return candidates.find((p) => fs.existsSync(p));
}

function locateBinary(distDir, artifacts, plat) {
  if (artifacts) {
    const a = findBinaryViaArtifacts(artifacts, plat);
    if (a) {
      const abs = path.isAbsolute(a.path) ? a.path : path.resolve(REPO_ROOT, a.path);
      if (fs.existsSync(abs)) return abs;
    }
  }
  return findBinaryViaConvention(distDir, plat);
}

function rmrf(p) {
  if (fs.existsSync(p)) fs.rmSync(p, { recursive: true, force: true });
}

function copyFile(src, dest, mode) {
  fs.mkdirSync(path.dirname(dest), { recursive: true });
  fs.copyFileSync(src, dest);
  if (mode !== undefined) fs.chmodSync(dest, mode);
}

function writeJSON(p, obj) {
  fs.mkdirSync(path.dirname(p), { recursive: true });
  fs.writeFileSync(p, JSON.stringify(obj, null, 2) + "\n");
}

function platformReadme(plat, version) {
  return `# ${SCOPE}/llm-lint-${plat.suffix}\n\n` +
    `Native \`llm-lint\` binary for **${plat.os} ${plat.cpu}**, version ${version}.\n\n` +
    `Do not install this package directly — install [\`${PARENT_NAME}\`](https://www.npmjs.com/package/${PARENT_NAME.replace("@", "")}) instead.\n` +
    `npm will pick the right platform package automatically via \`optionalDependencies\`.\n\n` +
    `Source: https://github.com/JadenRazo/llm-lint\n`;
}

function buildPlatformPackage({ plat, version, binarySrc, outDir }) {
  const pkgDir = path.join(outDir, `llm-lint-${plat.suffix}`);
  rmrf(pkgDir);

  const pkg = {
    name: `${SCOPE}/llm-lint-${plat.suffix}`,
    version,
    description: `llm-lint binary for ${plat.os} ${plat.cpu}.`,
    homepage: "https://github.com/JadenRazo/llm-lint#readme",
    bugs: "https://github.com/JadenRazo/llm-lint/issues",
    repository: {
      type: "git",
      url: "git+https://github.com/JadenRazo/llm-lint.git",
    },
    license: "Apache-2.0",
    author: "Jaden Razo",
    os: [plat.os],
    cpu: [plat.cpu],
    files: ["bin/", "README.md", "LICENSE"],
    engines: { node: ">=18" },
  };
  writeJSON(path.join(pkgDir, "package.json"), pkg);

  // 0o755 = rwxr-xr-x. Required for POSIX execution.
  // Windows ignores file modes; .exe extension is the executability marker.
  const mode = plat.ext === ".exe" ? undefined : 0o755;
  copyFile(binarySrc, path.join(pkgDir, "bin", `llm-lint${plat.ext}`), mode);

  fs.writeFileSync(path.join(pkgDir, "README.md"), platformReadme(plat, version));
  fs.copyFileSync(path.join(REPO_ROOT, "LICENSE"), path.join(pkgDir, "LICENSE"));
  return pkgDir;
}

function buildParentPackage({ version, outDir, includeOnly }) {
  const pkgDir = path.join(outDir, "llm-lint");
  rmrf(pkgDir);

  const sourcePkg = JSON.parse(fs.readFileSync(path.join(NPM_DIR, "package.json"), "utf8"));
  const optionalDeps = {};
  for (const plat of PLATFORMS) {
    if (includeOnly && plat.node !== includeOnly) continue;
    optionalDeps[`${SCOPE}/llm-lint-${plat.suffix}`] = version;
  }

  const pkg = {
    ...sourcePkg,
    version,
    optionalDependencies: optionalDeps,
  };
  delete pkg.scripts; // strip dev scripts from published artifact
  writeJSON(path.join(pkgDir, "package.json"), pkg);

  copyFile(path.join(NPM_DIR, "bin", "llm-lint.js"), path.join(pkgDir, "bin", "llm-lint.js"), 0o755);
  fs.copyFileSync(path.join(NPM_DIR, "README.md"), path.join(pkgDir, "README.md"));
  fs.copyFileSync(path.join(REPO_ROOT, "LICENSE"), path.join(pkgDir, "LICENSE"));
  return pkgDir;
}

function main() {
  const args = parseArgs(process.argv);
  if (args.help) help();
  if (!args.version) {
    process.stderr.write("error: --version is required\n");
    process.exit(2);
  }
  if (/^v/.test(args.version)) {
    args.version = args.version.replace(/^v/, "");
  }
  if (!/^\d+\.\d+\.\d+(-[\w.]+)?(\+[\w.]+)?$/.test(args.version)) {
    process.stderr.write(`error: --version "${args.version}" is not a valid semver\n`);
    process.exit(2);
  }

  const distDir = path.resolve(args.dist ?? path.join(REPO_ROOT, "dist"));
  const outDir = path.resolve(args.out ?? path.join(NPM_DIR, "build"));
  if (!fs.existsSync(distDir)) {
    process.stderr.write(`error: dist directory not found: ${distDir}\n`);
    process.exit(2);
  }

  rmrf(outDir);
  fs.mkdirSync(outDir, { recursive: true });

  const artifacts = readArtifactsJson(distDir);
  const targets = args.only
    ? PLATFORMS.filter((p) => p.node === args.only)
    : PLATFORMS;
  if (targets.length === 0) {
    process.stderr.write(`error: --only "${args.only}" matched no platforms\n`);
    process.exit(2);
  }

  const built = [];
  const missing = [];
  for (const plat of targets) {
    const bin = locateBinary(distDir, artifacts, plat);
    if (!bin) {
      missing.push(plat.node);
      continue;
    }
    const dir = buildPlatformPackage({ plat, version: args.version, binarySrc: bin, outDir });
    built.push({ plat, dir });
    process.stdout.write(`built ${path.relative(REPO_ROOT, dir)} (from ${path.relative(REPO_ROOT, bin)})\n`);
  }

  if (missing.length === targets.length) {
    process.stderr.write(`error: no binaries found in ${distDir} for any requested platform\n`);
    process.stderr.write(`  expected goreleaser artifacts.json or convention-named files\n`);
    process.exit(2);
  }
  if (missing.length > 0 && !args.only) {
    process.stderr.write(`warning: skipping platforms with no binary: ${missing.join(", ")}\n`);
  }

  const parentDir = buildParentPackage({
    version: args.version,
    outDir,
    includeOnly: args.only,
  });
  process.stdout.write(`built ${path.relative(REPO_ROOT, parentDir)} (parent)\n`);
  process.stdout.write(`\nNext: node npm/scripts/publish.mjs --version ${args.version}\n`);
}

main();
