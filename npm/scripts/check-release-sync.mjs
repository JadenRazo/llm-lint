#!/usr/bin/env node
// Asserts that what GitHub calls the latest release and what npm serves as
// `latest` are the same version.
//
// This is the check that would have caught v0.4.0 on day one. The release
// workflow failed loudly on 2026-08-07 - but a failed run is a notification, and
// notifications are exactly what a maintainer stops reading. Meanwhile the repo
// looked finished: the GitHub release was published and marked Latest, the
// README's npm badge kept rendering 0.3.1, and nothing anywhere said the two
// disagreed. The skew lasted until someone happened to look.
//
// Run on a schedule, this turns "a run failed once, days ago" into a standing,
// current statement about whether users can actually get the current version.
//
//   --repo <owner/name>   Default: from GITHUB_REPOSITORY, else JadenRazo/llm-lint.
//   --package <name>      Default: the parent package from platforms.mjs.
//
// Exit 0 in sync, 1 skewed, 2 could not determine.

import fs from "node:fs";

import { PARENT_NAME } from "./platforms.mjs";

const REGISTRY = "https://registry.npmjs.org";

function arg(flag, fallback) {
  const i = process.argv.indexOf(flag);
  return i !== -1 ? process.argv[i + 1] : fallback;
}

const REPO = arg("--repo", process.env.GITHUB_REPOSITORY || "JadenRazo/llm-lint");
const PKG = arg("--package", PARENT_NAME);

function emit(kv) {
  const out = process.env.GITHUB_OUTPUT;
  if (out) fs.appendFileSync(out, Object.entries(kv).map(([k, v]) => `${k}=${v}`).join("\n") + "\n");
}

async function githubLatestRelease() {
  const headers = { Accept: "application/vnd.github+json", "User-Agent": "llm-lint-release-sync" };
  if (process.env.GITHUB_TOKEN) headers.Authorization = `Bearer ${process.env.GITHUB_TOKEN}`;
  // /releases/latest excludes drafts and prereleases, which is the right
  // comparison for npm's `latest` dist-tag.
  const res = await fetch(`https://api.github.com/repos/${REPO}/releases/latest`, { headers });
  if (res.status === 404) return null;
  if (!res.ok) throw new Error(`GitHub API returned ${res.status} for ${REPO}`);
  return res.json();
}

async function npmLatest(name) {
  const res = await fetch(`${REGISTRY}/${name.replace("/", "%2F")}`, {
    headers: { Accept: "application/vnd.npm.install-v1+json" },
  });
  if (res.status === 404) return null;
  if (!res.ok) throw new Error(`registry returned ${res.status} for ${name}`);
  const doc = await res.json();
  return { latest: doc["dist-tags"]?.latest, versions: Object.keys(doc.versions ?? {}) };
}

async function main() {
  const release = await githubLatestRelease();
  if (!release) {
    process.stderr.write(`could not determine the latest GitHub release for ${REPO}\n`);
    process.exit(2);
  }
  const ghVersion = release.tag_name.replace(/^v/, "");

  const npmDoc = await npmLatest(PKG);
  if (!npmDoc) {
    process.stderr.write(`${PKG} does not exist on ${REGISTRY}\n`);
    process.exit(2);
  }

  const inSync = npmDoc.latest === ghVersion;
  const published = npmDoc.versions.includes(ghVersion);

  process.stdout.write(
    `github  ${REPO} latest release  ${release.tag_name}  (${release.published_at})\n` +
    `npm     ${PKG} dist-tag latest  ${npmDoc.latest ?? "(none)"}\n\n`,
  );

  emit({
    github_latest: release.tag_name,
    npm_latest: npmDoc.latest ?? "",
    in_sync: String(inSync),
    release_url: release.html_url,
  });

  if (inSync) {
    process.stdout.write(`in sync: users installing ${PKG} get ${release.tag_name}.\n`);
    return;
  }

  const days = Math.floor((Date.now() - Date.parse(release.published_at)) / 86400000);
  process.stderr.write(
    `SKEW: GitHub advertises ${release.tag_name} as the latest release ` +
    `(${days} day${days === 1 ? "" : "s"} ago) but npm still serves ${npmDoc.latest ?? "nothing"}.\n\n` +
    (published
      ? `  ${ghVersion} IS on the registry but is not tagged latest. Fix with:\n` +
        `    npm dist-tag add ${PKG}@${ghVersion} latest\n`
      : `  ${ghVersion} was never published to npm. Replay just the npm half with:\n` +
        `    gh workflow run publish-npm.yml -f tag=${release.tag_name}\n`) +
    `\n  Anyone running \`npm i ${PKG}\` right now gets ${npmDoc.latest ?? "nothing"}.\n`,
  );
  process.exit(1);
}

main().catch((err) => {
  process.stderr.write(`error: ${err.message}\n`);
  process.exit(2);
});
