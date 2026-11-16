import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

const root = fileURLToPath(new URL("../../", import.meta.url));
const workflow = fs.readFileSync(path.join(root, ".github/workflows/release-health.yml"), "utf8");
const placeholder = '//registry.npmjs.org/:_authToken=${NODE_AUTH_TOKEN}\n';
const registry = "registry=https://registry.npmjs.org/\n";

// Read this workflow's simple step layout without adding a YAML dependency.
function step(name) {
    const block = workflow.split(/(?=^      - )/m)
        .find((part) => part.startsWith(`      - name: ${name}\n`));
    assert.ok(block, `missing workflow step: ${name}`);
    return block;
}

function runBlock(block) {
    const lines = block.split("\n");
    const index = lines.findIndex((line) => line.startsWith("        run: "));
    assert.notEqual(index, -1, "step must have a run command");
    const value = lines[index].slice("        run: ".length);
    if (value !== "|") return value;
    const body = [];
    for (const line of lines.slice(index + 1)) {
        if (line.trim() && !line.startsWith("          ")) break;
        body.push(line.slice(10));
    }
    return body.join("\n");
}

function fixture(t) {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "llm-lint-auth-test-"));
    t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
    const home = path.join(dir, "home");
    const bin = path.join(dir, "bin");
    fs.mkdirSync(home);
    fs.mkdirSync(bin);
    const config = path.join(dir, "setup-node.npmrc");
    const sentinel = path.join(home, ".npmrc");
    const sentinelContent = '//home.invalid/:_authToken=home-sentinel\n';
    fs.writeFileSync(sentinel, sentinelContent);
    const log = path.join(dir, "npm-calls.jsonl");
    fs.writeFileSync(log, "");
    fs.writeFileSync(path.join(bin, "npm"), `#!${process.execPath}
import fs from "node:fs";
const args = process.argv.slice(2);
fs.appendFileSync(process.env.FIXTURE_NPM_LOG, JSON.stringify(args) + "\\n");
if (args[0] === "whoami" && process.env.FIXTURE_WHOAMI_FAIL === "1") {
    console.error("fixture authentication failed");
    process.exit(1);
}
const replies = new Map([
    [JSON.stringify(["--version"]), process.env.FIXTURE_NPM_VERSION],
    [JSON.stringify(["whoami", "--registry", "https://registry.npmjs.org"]), "fixture-user"],
    [JSON.stringify(["profile", "get", "--json", "--registry", "https://registry.npmjs.org"]), '{"tfa":"auth-only"}'],
    [JSON.stringify(["access", "list", "packages", "@jadenrazo", "--json", "--registry", "https://registry.npmjs.org"]), '{"@jadenrazo/llm-lint":"read-write"}'],
]);
if (!replies.has(JSON.stringify(args))) process.exit(99);
console.log(replies.get(JSON.stringify(args)));
`, { mode: 0o755 });
    // The shim is extensionless, so explicitly make it an ES module on all Node versions.
    fs.writeFileSync(path.join(bin, "package.json"), '{"type":"module"}\n');
    const env = {
        HOME: home,
        PATH: `${bin}:/usr/bin:/bin`,
        TMPDIR: dir,
        NPM_CONFIG_USERCONFIG: config,
        FIXTURE_NPM_LOG: log,
        FIXTURE_NPM_VERSION: "11.5.1",
        ACTIONS_ID_TOKEN_REQUEST_URL: "https://oidc.invalid/fixture",
    };
    function execute(command) {
        const result = spawnSync("/bin/bash", ["--noprofile", "--norc", "-e", "-o", "pipefail", "-c", command], {
            cwd: root, env, encoding: "utf8", timeout: 10000,
        });
        assert.ifError(result.error);
        assert.equal(fs.readFileSync(sentinel, "utf8"), sentinelContent, "HOME config must stay untouched");
        return result;
    }
    return {
        env, config, log,
        prepare: () => execute(runBlock(step("Prepare npm auth configuration"))),
        validate: () => {
            // Run the shared, unchanged validator with the same isolated environment.
            const result = spawnSync(process.execPath, [path.join(root, "npm/scripts/check-npm-auth.mjs")], {
                cwd: root, env, encoding: "utf8", timeout: 10000,
            });
            assert.ifError(result.error);
            return result;
        },
    };
}

function succeeds(result) {
    assert.equal(result.status, 0, result.stderr || result.stdout);
}

function fails(result, message) {
    assert.equal(result.status, 1, result.stderr || result.stdout);
    assert.match(result.stderr, message);
}

test("health prepares auth after setup-node and before the shared preflight", () => {
    const setup = workflow.indexOf("uses: actions/setup-node@");
    const prepare = workflow.indexOf("- name: Prepare npm auth configuration\n");
    const preflight = workflow.indexOf("- name: npm auth configuration preflight\n");
    assert.ok(setup >= 0 && setup < prepare && prepare < preflight);
    assert.match(step("Prepare npm auth configuration"), /NODE_AUTH_TOKEN: \$\{\{ secrets\.NPM_TOKEN \}\}/);
    assert.match(step("npm auth configuration preflight"), /NODE_AUTH_TOKEN: \$\{\{ secrets\.NPM_TOKEN \}\}/);
    assert.match(runBlock(step("npm auth configuration preflight")), /node npm\/scripts\/check-npm-auth\.mjs/);
});

test("the existing validator rejects setup-node's original empty-token placeholder", (t) => {
    const f = fixture(t);
    fs.writeFileSync(f.config, registry + placeholder);
    fails(f.validate(), /still contains an _authToken line/);
    assert.equal(fs.readFileSync(f.config, "utf8"), registry + placeholder);
});

test("actual cleanup preserves registry and comments, enables OIDC, and is idempotent", (t) => {
    const f = fixture(t);
    const retained = registry + "# keep this comment\nalways-auth=true\n";
    fs.writeFileSync(f.config, retained + placeholder);
    succeeds(f.prepare());
    assert.equal(fs.readFileSync(f.config, "utf8"), retained);
    succeeds(f.validate());
    const once = fs.readFileSync(f.config);
    succeeds(f.prepare());
    assert.deepEqual(fs.readFileSync(f.config), once);
    succeeds(f.validate());
    assert.deepEqual(fs.readFileSync(f.log, "utf8").trim().split("\n").map(JSON.parse), [["--version"], ["--version"]]);
});

test("a present token skips cleanup byte-for-byte and exercises the existing token checks", (t) => {
    const f = fixture(t);
    f.env.NODE_AUTH_TOKEN = "fixture-token";
    delete f.env.ACTIONS_ID_TOKEN_REQUEST_URL;
    const original = Buffer.from(registry + placeholder + "# keep trailing whitespace  \n");
    fs.writeFileSync(f.config, original);
    succeeds(f.prepare());
    assert.deepEqual(fs.readFileSync(f.config), original);
    succeeds(f.validate());
    const calls = fs.readFileSync(f.log, "utf8").trim().split("\n").map(JSON.parse);
    assert.deepEqual(calls.map((args) => args[0]), ["--version", "whoami", "profile", "access"]);
});

test("the unchanged token preflight rejects failed whoami without exposing the dummy token", (t) => {
    const f = fixture(t);
    f.env.NODE_AUTH_TOKEN = "fixture-expired-token";
    f.env.FIXTURE_WHOAMI_FAIL = "1";
    fs.writeFileSync(f.config, registry + placeholder);
    succeeds(f.prepare());
    assert.equal(fs.readFileSync(f.config, "utf8"), registry + placeholder);
    const result = f.validate();
    fails(result, /NODE_AUTH_TOKEN is not a valid credential/);
    assert.equal((result.stdout + result.stderr).includes(f.env.NODE_AUTH_TOKEN), false);
    const calls = fs.readFileSync(f.log, "utf8").trim().split("\n").map(JSON.parse);
    assert.deepEqual(calls.map((args) => args[0]), ["--version", "whoami"]);
});

for (const entry of [
    '//other.invalid/:_authToken=${NODE_AUTH_TOKEN}\n',
    '//registry.npmjs.org/:_authToken=fixture-literal\n',
    ' //registry.npmjs.org/:_authToken=${NODE_AUTH_TOKEN}\n',
    '//registry.npmjs.org/:_authToken=${NODE_AUTH_TOKEN} # keep\n',
]) {
    test(`cleanup retains and the validator rejects nonmatching auth: ${entry.trim()}`, (t) => {
        const f = fixture(t);
        fs.writeFileSync(f.config, registry + placeholder + entry);
        succeeds(f.prepare());
        assert.equal(fs.readFileSync(f.config, "utf8"), registry + entry);
        fails(f.validate(), /still contains an _authToken line/);
    });
}

test("missing explicit config is harmless and never falls back to HOME", (t) => {
    const f = fixture(t);
    succeeds(f.prepare());
    assert.equal(fs.existsSync(f.config), false);
    succeeds(f.validate());
    delete f.env.NPM_CONFIG_USERCONFIG;
    succeeds(f.prepare());
    // The unchanged validator does use HOME; its rejection proves the sentinel survived.
    fails(f.validate(), /still contains an _authToken line/);
    f.env.NPM_CONFIG_USERCONFIG = "";
    succeeds(f.prepare());
    fails(f.validate(), /still contains an _authToken line/);
});

test("cleanup cannot conceal a missing OIDC endpoint", (t) => {
    const f = fixture(t);
    fs.writeFileSync(f.config, registry + placeholder);
    delete f.env.ACTIONS_ID_TOKEN_REQUEST_URL;
    succeeds(f.prepare());
    fails(f.validate(), /No NODE_AUTH_TOKEN and no OIDC token available/);
});

test("cleanup cannot conceal an npm version below the OIDC floor", (t) => {
    const f = fixture(t);
    fs.writeFileSync(f.config, registry + placeholder);
    f.env.FIXTURE_NPM_VERSION = "11.5.0";
    succeeds(f.prepare());
    fails(f.validate(), /npm 11\.5\.0 is too old for trusted publishing/);
});
