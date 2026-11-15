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
- **`--insecure-no-security`** is the only way to run without message security over `opc.tcp`, as a client and with `serve`. Use it only on an isolated, trusted network
- **List the servers you trust** with `--trusted-cert`: the system CA pool is not used, and a secure channel needs this list
- **`--security-policy auto` and `--security-mode auto` select from the GetEndpoints answer**, which the server sends without security, as an explicit `--security-policy` without `--server-cert` does. Over the secured channel opcuactl compares that answer with the endpoints of CreateSession and refuses a server whose lists differ before it activates a session (exit 4, `BadSecurityChecksFailed`). `--security-policy`, `--security-mode` and `--server-cert` together do not use GetEndpoints
- **Do not use `--insecure-skip-verify`** in production — it disables server certificate validation
- **Size the limits of `serve`** to the clients you expect with `--max-sessions`, `--max-subscriptions` and `--max-subscriptions-per-session`: a client can open sessions before it logs in, so the limits bound what an unauthenticated client can tie up

### Credential Handling

- Read secrets from a file or the environment: `--password-file`, `--token-file`, `--pfx-password-file` and `--key-password-file` (`-` reads stdin), or `OPCUACTL_PASSWORD`, `OPCUACTL_TOKEN`, `OPCUACTL_PFX_PASSWORD`, `OPCUACTL_KEY_PASSWORD` and `OPCUACTL_BROKER_PASSWORD` (`OPCUACTL_PASSWORD_FILE`, `OPCUACTL_PFX_PASSWORD_FILE` and `OPCUACTL_KEY_PASSWORD_FILE` name a file). `--password`, `--token`, `--pfx-password`, `--key-password` and `--broker-password` take the secret itself, which the process list and the shell history show to other users of the machine
- `opcuactl` does not print a secret: a message about one names the flag, variable or file it came from, and an invalid `serve --user` entry is named by its position
- Certificate private keys (`--key`) should have restrictive file permissions (`chmod 600`)

### PubSub Keys

- `pubsub sks get-keys` prints key material only with `--show-keys`; the keys it prints (and the `--keys-file` it is saved to) decrypt and forge the messages of the SecurityGroup until the keys rotate — treat them like a password (`chmod 600`, do not commit them)
- Prefer `--security-group`, which pulls keys from the Security Key Service when needed and keeps them in memory only
- Broker passwords (MQTT, AMQP): prefer `OPCUACTL_BROKER_PASSWORD` over `--broker-password`; `--mqtt-password` and `OPCUACTL_MQTT_PASSWORD` are the deprecated names of v0.4

### PubSub Input

- `pubsub subscribe` prints what arrives at `--url`. Without message security (JSON messages, UADP without keys) anyone who can send to that address or topic decides what is printed; on a shared network, use secured UADP or a broker that authenticates its clients
- With `--security-group` or `--keys-file`, `pubsub subscribe` requires signed messages unless `--message-security` says otherwise: an unsigned UADP message is reported on stderr and not printed. `--message-security none` accepts it, `sign-and-encrypt` requires encryption too
- What a subscriber learns from the network is bounded: DataSetMetaData of `--max-learned-metadata` writers (default 1024, the one used least recently makes room; one with more than 4096 fields is not kept), and the probes of `--probe`, for at most 1024 writers at a time, each until its DataSetMetaData arrives or a minute after its last message

### Test Server (`serve`)

`opcuactl serve` is a test server. Its defaults are open:

- it listens on `opc.tcp://0.0.0.0:4840`, every interface: `--listen 127.0.0.1:4840` keeps it on the machine
- anonymous sessions are allowed: `--allow-anonymous=false` with `--user` requires a user
- every client certificate is accepted: `--trusted-cert` or `--trust-dir` lists the clients it accepts
- anonymous and authenticated users may read, write (`Demo.Level`) and call methods (`Demo.Add`): `--no-demo` leaves out the demo namespace
- the passwords of `--user` are on its command line: `OPCUACTL_SERVE_USERS` keeps them in the environment
- `--insecure-no-security` adds endpoints without message security; leave it off outside an isolated network

### Output Data

- `opcuactl` writes all data to stdout and all errors to stderr
- Text and table output show the control characters of a string from a server or the network as escapes (`\x1b`, `\r`, `\n`, `\u202e`), on stdout and in messages on stderr, so a terminal shows the characters the string holds. JSON, JSONL, YAML and CSV output keep the value as it is: escape it before you print it to a terminal
- Recursive walks (`browse tree`, `find`, `get vars --recursive`, `export`) visit each node once stop after `--max-nodes` nodes (default 100000) with the output so far and exit 7, and end after `--walk-timeout` (default 10m) with exit 5
- Be cautious when redirecting output to files in shared environments, as OPC UA node values may contain sensitive operational data
