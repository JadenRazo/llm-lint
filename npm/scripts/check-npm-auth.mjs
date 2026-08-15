#!/usr/bin/env node
// Proves - before anything irreversible happens - that this job can actually
// publish to npm.
//
// Why: release.yml publishes to npm last, after goreleaser has created the
// GitHub release, pushed ghcr.io images and written to the sigstore transparency
// log. On v0.4.0 the NPM_TOKEN had silently expired, so all of that succeeded
// and only the npm publish failed. GitHub said v0.4.0 was the latest release
// while `npm i @jadenrazo/llm-lint` kept resolving to 0.3.1, and the README's
// npm badge advertised the skew. There is no way to un-create a GitHub release
// or a transparency-log entry, so the credential has to be checked at the top
// of the job, not at the bottom.
//
// A dead token fails `npm whoami` in about a second. The registry answers a
// publish attempt with a bare 404 - not a 401 - so the failure at publish time
// reads as "package not found" and sends you looking in the wrong place.
//
// Two supported auth modes:
//   token  - NODE_AUTH_TOKEN is set. Validated here.
//   OIDC   - no token; npm trusted publishing exchanges a GitHub OIDC token at
//            publish time. Nothing to expire. Requires npm >= 11.5.1 and
//            `permissions: id-token: write`, both asserted here.
//
// Run: node npm/scripts/check-npm-auth.mjs [--scope @jadenrazo]

import { spawnSync } from "node:child_process";

const REGISTRY = "https://registry.npmjs.org";
const MIN_NPM_FOR_OIDC = [11, 5, 1];

const scopeArg = process.argv.indexOf("--scope");
const SCOPE = scopeArg !== -1 ? process.argv[scopeArg + 1] : "@jadenrazo";

function die(msg) {
  process.stderr.write(`\nnpm auth preflight FAILED\n\n${msg}\n`);
  process.exit(1);
}

function npm(args) {
  return spawnSync("npm", args, { encoding: "utf8" });
}

function cmp(a, b) {
  for (let i = 0; i < 3; i++) {
    if ((a[i] ?? 0) !== (b[i] ?? 0)) return (a[i] ?? 0) - (b[i] ?? 0);
  }
  return 0;
}

const npmVersionRaw = (npm(["--version"]).stdout || "").trim();
const npmVersion = npmVersionRaw.split(".").map(Number);
process.stdout.write(`npm ${npmVersionRaw} on node ${process.version}\n`);

const token = (process.env.NODE_AUTH_TOKEN || "").trim();
const hasOidc = Boolean(process.env.ACTIONS_ID_TOKEN_REQUEST_URL);

if (!token) {
  // ---------------------------------------------------------- OIDC mode
  process.stdout.write(`auth mode: trusted publishing (OIDC) - no NODE_AUTH_TOKEN set\n`);

  if (!hasOidc) {
    die(
      `No NODE_AUTH_TOKEN and no OIDC token available.\n\n` +
      `  The job cannot authenticate to the registry at all. Fix one of:\n\n` +
      `  a) Trusted publishing (preferred - nothing expires):\n` +
      `       - add \`permissions: { id-token: write }\` to this job\n` +
      `       - on npmjs.com, for each package under ${SCOPE}, set\n` +
      `         Settings -> Trusted Publisher -> GitHub Actions\n` +
      `         (repository JadenRazo/llm-lint, plus this workflow's filename)\n\n` +
      `  b) Token: set the NPM_TOKEN repository secret and pass it as\n` +
      `     NODE_AUTH_TOKEN on the publish step.`,
    );
  }

  if (cmp(npmVersion, MIN_NPM_FOR_OIDC) < 0) {
    die(
      `npm ${npmVersionRaw} is too old for trusted publishing ` +
      `(needs >= ${MIN_NPM_FOR_OIDC.join(".")}).\n\n` +
      `  Either raise the node-version on the setup-node step so it ships a newer\n` +
      `  npm, or add a \`npm install -g npm@^${MIN_NPM_FOR_OIDC.join(".")}\` step before this one.`,
    );
  }

  process.stdout.write(
    `ok: OIDC token endpoint present and npm ${npmVersionRaw} supports trusted publishing.\n` +
    `note: this cannot verify that a trusted publisher is configured for each package\n` +
    `      on npmjs.com - only the publish itself can. If publish fails with a 404,\n` +
    `      that configuration is what is missing.\n`,
  );
  process.exit(0);
}

// ---------------------------------------------------------------- token mode
process.stdout.write(`auth mode: NODE_AUTH_TOKEN (${token.length} chars)\n`);

const who = npm(["whoami", "--registry", REGISTRY]);
if (who.status !== 0) {
  die(
    `NODE_AUTH_TOKEN is not a valid credential for ${REGISTRY}.\n\n` +
    `  npm whoami said:\n${(who.stderr || who.stdout || "(no output)").trim().split("\n").map((l) => `    ${l}`).join("\n")}\n\n` +
    `  Almost always this means the token expired. npm granular access tokens\n` +
    `  have a maximum lifetime, so a token that worked at the last release can be\n` +
    `  dead at the next one with no warning and no repo change.\n\n` +
    `  Fix, in order of preference:\n` +
    `    1. Move to trusted publishing (OIDC) and delete the NPM_TOKEN secret\n` +
    `       entirely - there is then nothing left to expire.\n` +
    `       npmjs.com -> each ${SCOPE}/* package -> Settings -> Trusted Publisher\n` +
    `    2. Mint a fresh granular token with read+write on ${SCOPE} and update\n` +
    `       the NPM_TOKEN repository secret.`,
  );
}

const user = who.stdout.trim();
process.stdout.write(`ok: authenticated to ${REGISTRY} as "${user}"\n`);

// Best-effort scope check. `npm access` output shape has changed across npm
// majors, so a parse failure here is reported and tolerated - whoami already
// proved the credential is live, which is the failure this preflight exists for.
const access = npm(["access", "list", "packages", SCOPE, "--json", "--registry", REGISTRY]);
if (access.status === 0) {
  try {
    const pkgs = JSON.parse(access.stdout);
    const names = Object.keys(pkgs);
    const writable = names.filter((n) => pkgs[n] === "read-write");
    process.stdout.write(
      `ok: token sees ${names.length} package(s) under ${SCOPE}, ${writable.length} writable\n`,
    );
    if (names.length > 0 && writable.length === 0) {
      die(
        `The token can read ${SCOPE} but has write access to nothing.\n` +
        `  A read-only token fails publish with a 404, not a permission error.\n` +
        `  Re-mint it with "Read and write" on the ${SCOPE} scope.`,
      );
    }
  } catch {
    process.stdout.write(`note: could not parse \`npm access\` output; skipping the scope check\n`);
  }
} else {
  process.stdout.write(`note: \`npm access list packages\` unavailable here; skipping the scope check\n`);
}
