import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

const root = fileURLToPath(new URL("../../", import.meta.url));
const workflow = fs.readFileSync(path.join(root, ".github/workflows/publish-npm.yml"), "utf8");
const steps = workflow.split(/(?=^      - )/m);

function step(name) {
    const block = steps.find((part) => part.startsWith(`      - name: ${name}\n`));
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

const preserveName = "Preserve release verification tools";
const verifyName = "Verify the release is installable from npm";
const cleanupName = "Clean up release verification tools";

test("recovery checks workflow policy before switching to tag package sources", () => {
    const checkout = steps.filter((part) => /^      - (?:name: .*\n        )?uses: actions\/checkout@/m.test(part));
    assert.equal(checkout.length, 2);
    assert.match(checkout[0], /ref: \$\{\{ github\.workflow_sha \}\}/);
    assert.match(checkout[1], /ref: \$\{\{ inputs\.tag \}\}/);
    const policy = step("Verify immutable workflow dependencies");
    assert.equal(runBlock(policy), "scripts/check-workflow-pins.sh");
    const ordered = [checkout[0], policy, step(preserveName), checkout[1], step(verifyName), step(cleanupName)];
    for (let i = 1; i < ordered.length; i++) {
        assert.ok(steps.indexOf(ordered[i - 1]) < steps.indexOf(ordered[i]), "workflow steps must retain source ordering");
    }
    assert.match(runBlock(step(verifyName)), /node "\$LLM_LINT_RELEASE_TOOLS\/verify-published\.mjs" --version "\$\{TAG#v\}"/);
    assert.match(step(cleanupName), /if: \$\{\{\s*always\(\)\s*\}\}/);
});

function fixture(t) {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "llm-lint-recovery-test-"));
    t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
    const workspace = path.join(dir, "workspace");
    const runnerTemp = path.join(dir, "runner-temp");
    const home = path.join(dir, "home");
    const bin = path.join(dir, "bin");
    for (const directory of [workspace, runnerTemp, home, bin]) fs.mkdirSync(directory);
    fs.symlinkSync(process.execPath, path.join(bin, "node"));
    const githubEnv = path.join(dir, "github-env");
    fs.writeFileSync(githubEnv, "");
    // An allowlist keeps real runner tokens, npm settings, and Node options out.
    const env = {
        HOME: home, PATH: `${bin}:/usr/bin:/bin`, TMPDIR: runnerTemp,
        RUNNER_TEMP: runnerTemp, GITHUB_ENV: githubEnv, TAG: "v0.4.1",
    };
    function execute(name) {
        const result = spawnSync("/bin/bash", ["--noprofile", "--norc", "-e", "-o", "pipefail", "-c", runBlock(step(name))], {
            cwd: workspace, env, encoding: "utf8", timeout: 10000,
        });
        assert.ifError(result.error);
        return result;
    }
    return { dir, workspace, runnerTemp, githubEnv, env, execute };
}

for (const status of [0, 73]) {
    test(`saved workflow tools verify old tag sources and clean up after exit ${status}`, (t) => {
        const f = fixture(t);
        const scripts = path.join(f.workspace, "npm/scripts");
        fs.mkdirSync(scripts, { recursive: true });
        fs.mkdirSync(path.join(f.workspace, "scripts"));
        fs.writeFileSync(path.join(f.workspace, "scripts/check-workflow-pins.sh"), "# workflow-only policy\n");
        const maintained = `import fs from "node:fs";
import { marker } from "./platforms.mjs";
fs.writeFileSync("observed.json", JSON.stringify({
    tool: "workflow-verifier", marker, args: process.argv.slice(2),
    source: fs.readFileSync("tag-source.txt", "utf8"),
}));
process.exit(Number(process.env.FIXTURE_VERIFY_STATUS));
`;
        const platforms = 'export const marker = "workflow-platforms";\n';
        fs.writeFileSync(path.join(scripts, "verify-published.mjs"), maintained);
        fs.writeFileSync(path.join(scripts, "platforms.mjs"), platforms);
        const preserved = f.execute(preserveName);
        assert.equal(preserved.status, 0, preserved.stderr);
        const assignments = fs.readFileSync(f.githubEnv, "utf8").trim().split("\n");
        assert.equal(assignments.length, 1);
        assert.ok(assignments[0].startsWith("LLM_LINT_RELEASE_TOOLS="));
        const saved = assignments[0].slice("LLM_LINT_RELEASE_TOOLS=".length);
        f.env.LLM_LINT_RELEASE_TOOLS = saved;
        assert.equal(path.dirname(saved), f.runnerTemp);
        assert.match(path.basename(saved), /^llm-lint-release-tools\./);
        assert.equal(fs.readFileSync(path.join(saved, "verify-published.mjs"), "utf8"), maintained);
        assert.equal(fs.readFileSync(path.join(saved, "platforms.mjs"), "utf8"), platforms);

        // Simulate checkout's replacement with an old tag, without any local git tag.
        fs.rmSync(f.workspace, { recursive: true });
        fs.mkdirSync(scripts, { recursive: true });
        const stale = 'throw new Error("stale tag verifier must not execute");\n';
        fs.writeFileSync(path.join(scripts, "verify-published.mjs"), stale);
        fs.writeFileSync(path.join(scripts, "platforms.mjs"), 'export const marker = "stale-tag-platforms";\n');
        fs.writeFileSync(path.join(f.workspace, "tag-source.txt"), "old-tag-package-source");
        assert.equal(fs.existsSync(path.join(f.workspace, "scripts/check-workflow-pins.sh")), false);
        f.env.FIXTURE_VERIFY_STATUS = String(status);
        const verified = f.execute(verifyName);
        assert.equal(verified.status, status, verified.stderr);
        assert.deepEqual(JSON.parse(fs.readFileSync(path.join(f.workspace, "observed.json"), "utf8")), {
            tool: "workflow-verifier", marker: "workflow-platforms", args: ["--version", "0.4.1"],
            source: "old-tag-package-source",
        });
        assert.equal(fs.readFileSync(path.join(scripts, "verify-published.mjs"), "utf8"), stale);
        const cleaned = f.execute(cleanupName);
        assert.equal(cleaned.status, 0, cleaned.stderr);
        assert.equal(fs.existsSync(saved), false);
        assert.equal(fs.readFileSync(path.join(f.workspace, "tag-source.txt"), "utf8"), "old-tag-package-source");
        assert.equal(f.execute(cleanupName).status, 0, "cleanup is idempotent");
    });
}

test("a failed preservation still exports its temporary directory for always cleanup", (t) => {
    const f = fixture(t);
    const scripts = path.join(f.workspace, "npm/scripts");
    fs.mkdirSync(scripts, { recursive: true });
    fs.writeFileSync(path.join(scripts, "verify-published.mjs"), "// fixture verifier\n");
    // Missing platforms.mjs makes the real cp command fail after mktemp succeeds.
    const preserved = f.execute(preserveName);
    assert.notEqual(preserved.status, 0);
    const assignment = fs.readFileSync(f.githubEnv, "utf8").trim();
    assert.ok(assignment.startsWith("LLM_LINT_RELEASE_TOOLS="));
    const saved = assignment.slice("LLM_LINT_RELEASE_TOOLS=".length);
    assert.equal(path.dirname(saved), f.runnerTemp);
    assert.equal(fs.existsSync(saved), true);
    f.env.LLM_LINT_RELEASE_TOOLS = saved;
    const cleaned = f.execute(cleanupName);
    assert.equal(cleaned.status, 0, cleaned.stderr);
    assert.equal(fs.existsSync(saved), false);
});

test("release-tool cleanup tolerates no saved tools and preserves unrelated directories", (t) => {
    const f = fixture(t);
    assert.equal(f.execute(cleanupName).status, 0);
    for (const unrelated of [
        path.join(f.runnerTemp, "unrelated-tools"),
        path.join(f.dir, "llm-lint-release-tools.outside-runner-temp"),
    ]) {
        fs.mkdirSync(unrelated);
        fs.writeFileSync(path.join(unrelated, "sentinel"), "keep");
        f.env.LLM_LINT_RELEASE_TOOLS = unrelated;
        assert.equal(f.execute(cleanupName).status, 0);
        assert.equal(fs.readFileSync(path.join(unrelated, "sentinel"), "utf8"), "keep");
    }
});
