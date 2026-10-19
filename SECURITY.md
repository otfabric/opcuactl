# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in `opcuactl`, please report it responsibly.

**Do not open a public GitHub issue for security vulnerabilities.**

Instead, please email **security@otfabric.io** with:

- A description of the vulnerability
- Steps to reproduce the issue
- The affected version(s) (`opcuactl version` output)
- Any potential impact you've identified

We will acknowledge your report within 48 hours and aim to provide a fix or mitigation within 7 days for critical issues.

## Supported Versions

| Version | Supported |
|---------|-----------|
| Latest release | Yes |
| Older releases | Best effort |

## Security Considerations

### OPC UA Transport Security

`opcuactl` supports the full range of OPC UA security configurations. For production use:

- **Always use encrypted connections** (`--security-mode SignAndEncrypt`) when communicating over untrusted networks
- **Use certificate-based authentication** (`--auth certificate`) instead of username/password where possible
- **Avoid `--security-mode None`** unless on an isolated, trusted network
- **Do not use `--insecure-skip-verify`** in production — it disables server certificate validation

### Credential Handling

- Passwords provided via `--password` or `OPCUACTL_PASSWORD` are passed in memory and never logged
- Prefer environment variables over command-line flags for credentials, as command-line arguments may appear in process listings and shell history
- Certificate private keys (`--key`) should have restrictive file permissions (`chmod 600`)

### Output Data

- `opcuactl` writes all data to stdout and all errors to stderr
- Be cautious when redirecting output to files in shared environments, as OPC UA node values may contain sensitive operational data
