# Security policy

## Supported versions

Only the latest minor release receives security fixes. Older releases are not patched.

| Version | Supported          |
|---------|--------------------|
| `main`  | yes                |
| latest tagged release (`v0.x`) | yes |
| older tags | no              |

## Reporting a vulnerability

Please **do not open a public GitHub issue** for security problems.

The preferred channel is GitHub Security Advisories (private vulnerability reporting):

1. Go to https://github.com/JadenRazo/llm-lint/security/advisories/new
2. Describe the issue, expected impact, and reproduction steps
3. We will respond within **5 business days** with an initial acknowledgement and a triage timeline

If GitHub Security Advisories is unavailable to you, email `jadenscottrazo@gmail.com` with the same information. Please use a clear subject line such as `[llm-lint security] <one-line summary>`.

## Disclosure policy

We follow coordinated disclosure:

- Default embargo is **90 days** from initial report, or until a fix is publicly released — whichever comes first
- Embargo may be shortened if the issue is being actively exploited or already public
- We credit reporters in the release notes unless anonymity is requested

## Scope

In scope:

- The `llm-lint` CLI binary and Docker image (`ghcr.io/jadenrazo/llm-lint`)
- Released artifacts on the GitHub Releases page (tarballs, checksums, signatures)
- Supply-chain integrity of the build (cosign signatures, SBOMs, GitHub Actions workflows)

Out of scope:

- Issues in third-party Go modules used by `llm-lint` — please report those upstream. Dependabot and `govulncheck` cover known CVEs in our CI; if you find a new one, please file with the upstream project too.
- Best-practice or hardening suggestions that aren't exploitable — open a regular GitHub issue or PR for those.

## Verifying release artifacts

Each release includes:

- `checksums.txt` — SHA-256 of every archive
- `checksums.txt.sig` and `checksums.txt.pem` — cosign signature and certificate
- `*.spdx.json` — SBOM per archive

Verify with:

```sh
cosign verify-blob \
  --certificate checksums.txt.pem \
  --signature checksums.txt.sig \
  --certificate-identity-regexp 'https://github.com/JadenRazo/llm-lint/.+' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
```

Container images (`ghcr.io/jadenrazo/llm-lint:<tag>`) are signed keyless via cosign and verifiable with `cosign verify` against the same OIDC identity.
