// The supported platform matrix, declared once.
//
// Four things have to agree about this list or `npx @jadenrazo/llm-lint` breaks
// on some platform without anyone noticing: build.mjs (which packages), the
// parent's optionalDependencies (which npm resolves), npm/bin/llm-lint.js
// (which dispatches at runtime), and from-release.mjs (which unpacks the
// goreleaser archives). They used to be four hand-maintained copies.
// npm/scripts/check-manifests.mjs asserts they still agree, in CI.
//
//   node    - `${process.platform}-${process.arch}`, the runtime lookup key
//   goos/goarch - goreleaser's build matrix
//   suffix  - npm package name suffix: @jadenrazo/llm-lint-<suffix>
//   ext     - binary file extension
//   os/cpu  - npm's own `os`/`cpu` package.json fields
//   archive - goreleaser release asset, per .goreleaser.yaml archives[].name_template

export const SCOPE = "@jadenrazo";
export const PARENT_NAME = `${SCOPE}/llm-lint`;

export const PLATFORMS = [
  { node: "linux-x64",    goos: "linux",   goarch: "amd64", suffix: "linux-x64",    ext: "",     os: "linux",  cpu: "x64",   archive: "llm-lint_Linux_x86_64.tar.gz" },
  { node: "linux-arm64",  goos: "linux",   goarch: "arm64", suffix: "linux-arm64",  ext: "",     os: "linux",  cpu: "arm64", archive: "llm-lint_Linux_arm64.tar.gz" },
  { node: "darwin-x64",   goos: "darwin",  goarch: "amd64", suffix: "darwin-x64",   ext: "",     os: "darwin", cpu: "x64",   archive: "llm-lint_Darwin_x86_64.tar.gz" },
  { node: "darwin-arm64", goos: "darwin",  goarch: "arm64", suffix: "darwin-arm64", ext: "",     os: "darwin", cpu: "arm64", archive: "llm-lint_Darwin_arm64.tar.gz" },
  { node: "win32-x64",    goos: "windows", goarch: "amd64", suffix: "win32-x64",    ext: ".exe", os: "win32",  cpu: "x64",   archive: "llm-lint_Windows_x86_64.zip" },
  { node: "win32-arm64",  goos: "windows", goarch: "arm64", suffix: "win32-arm64",  ext: ".exe", os: "win32",  cpu: "arm64", archive: "llm-lint_Windows_arm64.zip" },
];

export function packageNameFor(plat) {
  return `${SCOPE}/llm-lint-${plat.suffix}`;
}
