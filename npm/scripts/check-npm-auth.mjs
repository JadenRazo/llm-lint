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
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

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
    process.stdout.write(
        `auth mode: trusted publishing (OIDC) - no NODE_AUTH_TOKEN set\n`,
    );

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

    // The single most common reason trusted publishing silently does not engage.
    //
    // actions/setup-node with `registry-url` always writes
    //   //registry.npmjs.org/:_authToken=${NODE_AUTH_TOKEN}
    // into the .npmrc. With no token that resolves to an empty value, and npm
    // reads it as "auth is configured for this registry" - so it never starts the
    // OIDC exchange and publishes with empty credentials instead, failing with
    // ENEEDAUTH. The error names the account; the cause is this line.
    //
    // v0.4.1 failed this way twice with trusted publishers correctly configured.
    // See actions/setup-node#1551, npm/documentation#1960, npm/cli#9088.
    const npmrcPath =
        process.env.NPM_CONFIG_USERCONFIG || path.join(os.homedir(), ".npmrc");
    if (fs.existsSync(npmrcPath)) {
        const npmrc = fs.readFileSync(npmrcPath, "utf8");
        const offending = npmrc
            .split("\n")
            .filter(
                (l) => l.includes("_authToken") && !l.trim().startsWith("#"),
            );

        if (offending.length > 0) {
            die(
                `${npmrcPath} still contains an _authToken line, which stops npm from using\n` +
                    `trusted publishing:\n\n` +
                    offending
                        .map((l) => `    ${l.replace(/=.*$/, "=<redacted>")}`)
                        .join("\n") +
                    `\n\n` +
                    `  npm treats the registry as already having credentials and never starts the\n` +
                    `  OIDC exchange, then fails the publish with ENEEDAUTH - an error that points\n` +
                    `  at the npm account rather than at this file.\n\n` +
                    `  actions/setup-node writes this line whenever \`registry-url\` is set, even with\n` +
                    `  no token. Remove it before publishing:\n\n` +
                    `    sed -i '/_authToken/d' "$NPM_CONFIG_USERCONFIG"\n\n` +
                    `  (actions/setup-node#1551, npm/documentation#1960)`,
            );
        }
        process.stdout.write(
            `ok: ${npmrcPath} has no _authToken line blocking the OIDC exchange\n`,
        );
    }

    process.stdout.write(
        `ok: OIDC token endpoint present and npm ${npmVersionRaw} supports trusted publishing.\n` +
            `note: this cannot verify that a trusted publisher is configured for each package\n` +
            `      on npmjs.com - only the publish itself can. If publish fails with ENEEDAUTH\n` +
            `      or 404 despite the checks above, that configuration is what is missing.\n`,
    );
    process.exit(0);
}

// ---------------------------------------------------------------- token mode
process.stdout.write(`auth mode: NODE_AUTH_TOKEN (${token.length} chars)\n`);

const who = npm(["whoami", "--registry", REGISTRY]);
if (who.status !== 0) {
    die(
        `NODE_AUTH_TOKEN is not a valid credential for ${REGISTRY}.\n\n` +
            `  npm whoami said:\n${(who.stderr || who.stdout || "(no output)")
                .trim()
                .split("\n")
                .map((l) => `    ${l}`)
                .join("\n")}\n\n` +
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

// A valid token is not the same as a token that can publish.
//
// v0.4.1 got all the way to `npm publish` with a freshly minted, entirely valid
// token and failed with:
//
//   npm error code EOTP
//   npm error This operation requires a one-time password.
//
// The account has two-factor authentication set to cover writes, and the token
// did not bypass it. `npm whoami` is a read, so it succeeded; the provenance
// statement was even signed and written to the transparency log. Only the PUT
// demanded an OTP - which no CI job can supply. By then goreleaser had already
// cut the GitHub release and pushed the container image.
//
// So checking the credential is live is necessary but not sufficient: the
// account's 2FA mode has to be checked too.
if (process.env.NPM_SKIP_2FA_CHECK !== "1") {
    const profile = npm(["profile", "get", "--json", "--registry", REGISTRY]);
    if (profile.status === 0) {
        let tfa;
        try {
            ({ tfa } = JSON.parse(profile.stdout));
        } catch {
            tfa = undefined;
        }
        // npm reports this as `auth-and-writes` (OTP on publish) or `auth-only`
        // (OTP on login only), and as false/null when 2FA is off.
        const mode = typeof tfa === "string" ? tfa : (tfa?.mode ?? null);

        if (mode === "auth-and-writes") {
            die(
                `The credential is valid, but this npm account requires a one-time password\n` +
                    `for writes (two-factor mode: "auth-and-writes"), and this token does not\n` +
                    `bypass it. The publish would fail with EOTP after the GitHub release and\n` +
                    `the container image had already been created.\n\n` +
                    `  Fix, in order of preference:\n\n` +
                    `  1. Trusted publishing (OIDC). Not a token at all, so 2FA does not apply,\n` +
                    `     nothing expires, and provenance is automatic. Configure it per package\n` +
                    `     at npmjs.com -> ${SCOPE}/<pkg> -> Settings -> Trusted Publisher\n` +
                    `     (GitHub Actions, repo JadenRazo/llm-lint, workflows release.yml and\n` +
                    `     publish-npm.yml), then DELETE the NPM_TOKEN secret. An absent secret\n` +
                    `     selects OIDC here with no workflow change.\n\n` +
                    `  2. A classic npm *automation* token, which is the one token type that\n` +
                    `     bypasses 2FA for publishing. Note npm is restricting exactly that\n` +
                    `     behaviour - see https://gh.io/npm-gat-bypass2fa-deprecation - so this\n` +
                    `     buys time rather than solving it.\n\n` +
                    `  Set NPM_SKIP_2FA_CHECK=1 to bypass this check if you know the token can\n` +
                    `  publish regardless.`,
            );
        }

        process.stdout.write(
            `ok: account 2FA mode is ${mode ?? "off"}; token publishes will not need an OTP\n`,
        );
    } else {
        // Never fail the release over an unreadable profile - report and continue.
        process.stdout.write(
            `note: could not read the account profile, so the 2FA mode is unknown.\n` +
                `      If the publish fails with EOTP, that is the reason.\n`,
        );
    }
}

// Best-effort scope check. `npm access` output shape has changed across npm
// majors, so a parse failure here is reported and tolerated - whoami already
// proved the credential is live, which is the failure this preflight exists for.
const access = npm([
    "access",
    "list",
    "packages",
    SCOPE,
    "--json",
    "--registry",
    REGISTRY,
]);
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
        process.stdout.write(
            `note: could not parse \`npm access\` output; skipping the scope check\n`,
        );
    }
} else {
    process.stdout.write(
        `note: \`npm access list packages\` unavailable here; skipping the scope check\n`,
    );
}
