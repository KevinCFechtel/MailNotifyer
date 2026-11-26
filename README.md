# MailNotifyer

MailNotifyer accepts emails via SMTP from a trusted internal network and sends
notifications to [Pushover](https://pushover.net/). Accepted messages are stored
durably on local storage before SMTP acceptance is acknowledged. Additional
delivery targets can be added through the small `delivery.Target` interface.

## Features

- SMTP/ESMTP listener on port `2525`
- Connection-level CIDR allowlist
- Persistent file-based queue with atomic writes
- Exponential retries for network errors and Pushover 5xx errors
- Separate `failed` directory for permanent Pushover 4xx errors
- MIME parsing with `text/plain` and an HTML-to-text fallback
- Configurable removal of unattended-upgrades logs
- Optional Pushover end-to-end encryption of the title and message
- UTF-8-safe Pushover limits:
  - Title/subject: up to 250 Unicode characters
  - Message: up to 1024 Unicode characters
- JSON logs and an external connectivity health check
- Runs as an unprivileged user in a read-only container

Attachments are ignored during MIME parsing and are not stored in the queue.
The Pushover message contains the sender and message body; the subject is used
as the Pushover title.

## Quick start

1. Create a Pushover application and have its token and user/group key ready.
2. Create the configuration:

   ```sh
   cp .env.example .env
   ```

3. Set at least `PUSHOVER_TOKEN` and `PUSHOVER_USER` in `.env`.
4. Start the service:

   ```sh
   docker compose up -d --build
   ```

5. Send a test email, for example with `swaks`:

   ```sh
   swaks \
     --server 127.0.0.1:2525 \
     --from monitoring@example.local \
     --to alerts@example.local \
     --header "Subject: Test alert" \
     --body "MailNotifyer is working."
   ```

The container image runs `/mailnotifier healthcheck` every 30 seconds.
This command checks external HTTPS connectivity to Pushover. It does not send
a notification and does not require Pushover credentials. Any received HTTP
status is considered a successful network connection.

> **Network security:** By default, Compose binds SMTP only to `127.0.0.1`.
> To allow other internal devices, set `SMTP_BIND_ADDRESS` to the specific
> private interface IP and restrict access further with a host firewall.
> The CIDR allowlist is not a replacement for a firewall. If traffic passes
> through NAT or a proxy, MailNotifyer may see that address instead; the
> allowlist must include the source address actually observed.
>
> This internal listener intentionally provides neither SMTP AUTH nor TLS.
> Emails and access to the listener must therefore be protected by an isolated,
> trusted network and the allowlist. For traffic over untrusted networks, add
> TLS/AUTH or place a hardened MTA in front of MailNotifyer.

## Configuration

| Variable | Default | Description |
| --- | --- | --- |
| `PUSHOVER_TOKEN` | – | Pushover application API token, required |
| `PUSHOVER_USER` | – | Pushover user or group key, required |
| `PUSHOVER_DEVICE` | empty | Optional single Pushover device |
| `PUSHOVER_ENCRYPTION_KEY` | empty | Optional E2EE key: exactly 64 hexadecimal characters; empty disables encryption |
| `PUSHOVER_ENDPOINT` | Pushover API | Custom API endpoint, primarily for tests |
| `PUSHOVER_TIMEOUT` | `10s` | HTTP timeout |
| `SMTP_ADDR` | `:2525` | Listener address inside the container |
| `SMTP_DOMAIN` | `mailnotifier.local` | SMTP banner domain |
| `SMTP_ALLOWED_CIDRS` | private networks + loopback | Comma-separated source network allowlist |
| `SMTP_MAX_MESSAGE_BYTES` | `10485760` | Maximum size of an incoming email |
| `UNATTENDED_UPGRADES_TRIM_FROM` | `package-installation-log` | Trim unattended-upgrades emails from `package-installation-log`, `unattended-upgrades-log`, or disable with `none` |
| `QUEUE_DIR` | `/data/queue` | Persistent queue directory |
| `HEALTHCHECK_URL` | `https://api.pushover.net/1/` | External connectivity endpoint |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error` |

The Compose-specific variables `SMTP_BIND_ADDRESS` and `SMTP_PORT` only control
SMTP port publishing on the host.

### Pushover end-to-end encryption

Generate a 256-bit key locally:

```sh
openssl rand -hex 32
```

Set the result as `PUSHOVER_ENCRYPTION_KEY` in `.env` and configure the same key
in every receiving Pushover iOS/Android app (version 5.0 or later). Restart/recreate
the service after changing it. Desktop/browser clients cannot decrypt E2EE
messages; optionally use `PUSHOVER_DEVICE` to target a compatible mobile device.
Treat the key as a secret and do not commit `.env`.

MailNotifyer follows the [Pushover E2EE specification](https://pushover.net/api#e2ee):
each title and message is independently GZIP-compressed, encrypted with
AES-256-CBC and PKCS7 padding using a fresh random IV, authenticated with
HMAC-SHA256, and Base64-encoded. Requests include `encrypted=1`; the encryption
key is never sent to Pushover. Invalid keys stop startup, and encryption failures
never fall back to plaintext.

The final encrypted fields respect the existing 250-character title and
1024-character message limits. If compression does not offset the encryption
overhead, the plaintext is shortened further with a Unicode-safe ellipsis before
encryption. Ciphertext is never truncated.

E2EE only protects title/message content on its way from MailNotifyer to the
receiving apps. API credentials and routing metadata remain visible to Pushover.
Incoming SMTP and the local on-disk queue are **not** encrypted by this feature;
protect the SMTP network and queue storage separately. Encryption happens at
delivery, including retries and items already in the queue.

### Unattended-upgrades emails

Emails whose decoded subject contains `unattended-upgrades` (case-insensitive)
are shortened before being stored in the queue. Set
`UNATTENDED_UPGRADES_TRIM_FROM` to:

- `package-installation-log` (default): remove the line `Package installation log:`
  and everything after it.
- `unattended-upgrades-log`: keep the package installation log, but remove the
  line `Unattended-upgrades log:` and everything after it.
- `none`: keep the full body (Pushover's character limit still applies).

Headings must occupy a whole line; surrounding whitespace is ignored. If the
selected heading is absent, the body is kept unchanged. Other emails are not
affected. Changes apply to newly received emails, not existing queue items.

## Queue behavior

After successful MIME parsing, only the target-independent notification is
stored at `/data/queue/pending/<id>.json`. The SMTP server acknowledges acceptance
only after a successful `fsync` and atomic rename. The Pushover target truncates
the title and message immediately before its API call so that future targets
can still receive the full text.

- Network errors and HTTP 5xx: retry after 5 seconds, then use exponential
  backoff with a maximum delay of one hour.
- HTTP 4xx, including Pushover quota errors: do not retry the unchanged request,
  as Pushover explicitly documents these responses as unsuccessful. The queue
  item is moved to `/data/queue/failed`.
- Successful delivery: the queue file is deleted.

Failed items are intentionally not deleted automatically. After resolving the
cause, they can be reviewed and manually moved back to `pending` if needed.

## Why this SMTP listener?

For the intended internal-network use case,
[`emersion/go-smtp`](https://github.com/emersion/go-smtp) is the best fit:

- Actively maintained RFC 5321 implementation
- Direct integration into the same process as the queue and targets
- Small container with a static binary and no separate MTA
- Size limits, timeouts, SMTPUTF8, and graceful shutdown

Alternatives:

- **Postfix** is the better choice if MailNotifyer is to receive mail directly
  from the internet as a public MX in the future. It provides a battle-tested
  mail queue, extensive access controls, and established MTA hardening, but adds
  complexity and container size for this internal use case.
- **Haraka** is powerful and plugin-oriented, but would require an additional
  runtime and custom queue plugin logic here.
- **Mailpit/MailHog** are primarily test mail servers and are not a suitable
  foundation for a production notification service.

A move to public MX operation should therefore involve more than opening
port 25: place Postfix in front as the MTA and hand messages off to MailNotifyer
via LMTP or a pipe.

## Development

```sh
go test ./...
go vet ./...
docker build -t mailnotifier .
```

## CI, security, and container publishing

The workflow in `.github/workflows/ci.yml` follows the CupNotifyer pattern.
It runs for pull requests, pushes to `main`, version tags, manual triggers,
and on a weekly schedule:

- Formatting checks, race tests, `go vet`, binary build, and `govulncheck`
- Dependency review for pull requests
- CodeQL analysis
- Container build and Trivy scan only for version tags
- Publishing for `linux/amd64` and `linux/arm64` with SBOM and provenance
  to `ghcr.io/<owner>/<repository>`

A semantic version tag such as `v1.2.3` publishes tags for the exact version,
major/minor version, commit SHA, and `latest`. All actions are pinned to commit
SHAs. Dependabot checks Go modules, GitHub Actions, and the Docker base image
weekly.

Key packages:

- `internal/smtpserver`: SMTP acceptance and network allowlist
- `internal/mailparser`: MIME mapping and character limits
- `internal/queue`: persistent delivery and backoff
- `internal/delivery`: extensible target interface
- `internal/target/pushover`: Pushover implementation
