# Bridge

The bridge lets the chief (Workbench) do two things on this machine, and nothing else:

1. **Reply to Dan.** Dan writes to the chief from the **Chief** tab of Eyrie's chat panel. Eyrie saves the message, sends it to the chief's wake endpoint, and the chief answers through `POST /bridge/v1/reply`. Replies are display only: Eyrie never runs them, never parses commands out of them, and never uses them to approve anything.
2. **Read allowlisted folders.** `GET` only: list, read (by line), and literal search, inside the roots Dan configures.

It is a separate `http.Server` (`internal/bridge`) on `127.0.0.1:<port>` with its own routes. It shares no mux, routes, or middleware with the management API and never proxies to it. The management API (`127.0.0.1:7200` by default) is unchanged and stays loopback only.

## Off by default

The bridge starts with `eyrie dashboard` only when `~/.eyrie/bridge.toml` exists (override the path with `EYRIE_BRIDGE_CONFIG`), contains a valid `bridge_token_sha256`, and is mode `0600`. If the file is group or world readable, or invalid, the bridge stays off and the dashboard logs why. Parse errors report only the line, column and key name, never the offending value, because it may be a secret. The dashboard itself still runs.

**To turn it off:** stop Funnel / the tunnel, then delete or move `~/.eyrie/bridge.toml` and restart `eyrie dashboard`.

## Setup

1. Copy `docs/bridge.example.toml` to `~/.eyrie/bridge.toml` and `chmod 600` it. It holds placeholders only.
2. Set `chief_wake_url` and `chief_wake_key` (Dan has these; they never go in a repo, doc, chat, or log).
3. Pick roots: `[roots]` maps an alias to an absolute folder, e.g. `picker-docs = "/abs/path"`. The chief only ever sees aliases, never paths.
4. Generate the token: `eyrie bridge token rotate`. It prints the token **once** and saves only its SHA-256. Give the token and the public bridge URL to the chief through a private secret request.
5. `eyrie bridge check` validates the file without starting anything.
6. Restart `eyrie dashboard`. The log line `bridge listening addr=127.0.0.1:7201` confirms it. If the port is taken, the bridge logs `bridge refused to start` and the Chief tab stays off (no prompts are sent with nowhere for replies to land). A bridge `port` equal to the dashboard port is refused before anything binds, so the dashboard always keeps its own port; `eyrie bridge check` reports the same conflict.

## Exposing it: Funnel or a tunnel (bridge port only)

TLS ends at Funnel or the tunnel. Expose **only** the bridge port. Never the dashboard port.

- **Tailscale Funnel (recommended):** `tailscale funnel --bg 7201`. Then set `client_ip_header = "X-Forwarded-For"` so rate limits and the access log see the real client.
- **cloudflared:** a tunnel whose ingress targets `http://127.0.0.1:7201` only. Set `client_ip_header = "Cf-Connecting-Ip"`.

Check after setup. The bridge answers 401 before routing, so the isolation check must carry the token. Keep the token out of process arguments: curl reads the header from stdin with `-H @-`.

```sh
read -rs BRIDGE_TOKEN   # paste the token; nothing is echoed
hdr() { printf 'Authorization: Bearer %s\n' "$BRIDGE_TOKEN"; }   # printf is a shell builtin: no new process
hdr | curl -s -o /dev/null -w '%{http_code}\n' -H @- https://<public-url>/api/agents          # must print 404
hdr | curl -s -o /dev/null -w '%{http_code}\n' -H @- https://<public-url>/bridge/v1/fs/roots   # 200 (or 404 if no roots)
curl -s -o /dev/null -w '%{http_code}\n' https://<public-url>/bridge/v1/fs/roots              # 401 without the token
unset BRIDGE_TOKEN
```

The dashboard port (7200) must not answer on the tailnet or tunnel address at all: `curl -m 3 http://<tailnet-ip>:7200/api/agents` should fail to connect.

## Token rotation

`eyrie bridge token rotate` replaces the stored hash (other keys in the file are kept) and prints the new token once. Restart `eyrie dashboard`, then give the new token to the chief privately. The old token stops working on restart.

## API (all routes need `Authorization: Bearer <bridge token>`)

| Route | |
| --- | --- |
| `POST /bridge/v1/reply` | `{conversation_id, in_reply_to, reply_id, text, final}`. `in_reply_to` must be a known message in that conversation (else 404). Idempotent on `reply_id` (`{"ok":true,"duplicate":true}`). `final:false` is an interim line. Text ≤ 32 KB. |
| `GET /bridge/v1/prompts/{message_id}` | `{conversation_id, message_id, text, ts, state}`; full prompt when the wake was truncated. |
| `GET /bridge/v1/fs/roots` | Root aliases only. |
| `GET /bridge/v1/fs/list?root=&path=` | `{entries:[{name,type,size,mtime}], truncated}`: the first 1,000 visible names in sorted order. The directory is streamed in batches with a bounded result set, so memory doesn't grow with directory size. |
| `GET /bridge/v1/fs/read?root=&path=&offset=&limit=` | Lines from `offset` (1-based, default 1), `limit` default 400, max 2,000; content ≤ 256 KB *as JSON-encoded* (escaping counted). Files > 10 MB: 413. Binary: 415. |
| `GET /bridge/v1/fs/search?root=&q=&path=&max=` | Case-insensitive literal match on names and contents of text files of any size; ≤ 200 hits; 5 s / 20,000-file budget. `truncated:true` whenever the answer may be incomplete: a budget or hit cap was reached, a line over 4 MB was cut, or an entry could not be read. Directories are read in batches of 256 in directory order. |

Any other path is 404; a wrong method on a bridge route is 405.

**Wake payload** (Eyrie → chief, `Authorization: Bearer <key>` and `X-Automation-Key: <key>`; redirects are never followed, so the key can't be forwarded elsewhere, and a 3xx counts as a failed attempt): `{"source":"eyrie","type":"eyrie.prompt","conversation_id","message_id","text","ts"}`, text capped at 4,000 characters with `"truncated":true`. 10 s timeout per attempt, retries after ~2 s, 10 s, 30 s with the same `message_id`, then `failed` with a Retry button (same `message_id`). Prompts are saved before sending and survive restarts; prompts still `pending` at startup are resent.

## Safety rules

- **Paths:** relative only. Absolute paths, NUL bytes, backslashes, and any `..` component are refused (400).
- **Opening:** each root is opened once at startup as a directory descriptor (a root that is itself a symlink is refused). Every request walks from that descriptor with `openat(O_NOFOLLOW)` one component at a time, so a symlink anywhere in the path is refused (403) at open time, and swapping a file or the root's pathname after startup cannot redirect a read. The final open is non-blocking and the file type is checked with `fstat` on the opened descriptor: FIFOs, devices and sockets are refused (400) without blocking, and hidden from list and search. As the spec's second guard, every successful open is cross-checked against an `os.OpenRoot` handle opened at startup: the file `os.Root` resolves at that path must be the same file (`os.SameFile`), otherwise 403. Unix only; elsewhere every fs call 404s.
- **Deny list** (built in; `extra_deny` can only add), matched on every path component after folding the name the way APFS does (combining marks dropped, full case folding, so `ſecret`, `id_rſa`, `Key` and decomposed accents can't slip past): names starting with `.`, `*.pem`, `*.key`, `*.p12`, `*.pfx`, `*.jks`, `*.keystore`, `*.kdbx`, `id_rsa*`, `id_ed25519*`, `id_ecdsa*`, `*.env`, `*secret*`, `*credential*`. Denied paths: 403 on read, hidden from list and search. `extra_deny` patterns must be ASCII (the bridge refuses to start otherwise); use `*` for accented letters, since APFS opens composed and decomposed spellings as the same file.
- **Auth:** constant-time compare of SHA-256(token). Missing/wrong: 401, no body.
- **Limits:** 60 req/min per token, burst 20; at most 4 concurrent; > 10 failed auths/min from one address → 429. All 429s carry `Retry-After`.
- **HTTP:** bodies ≤ 64 KB, no CORS headers, `Cache-Control: no-store`.
- **Access log:** `~/.eyrie/logs/bridge-access.jsonl` (0600, append-only): time, remote, method, route label, root alias, relative path, status, bytes, duration, and an `auth_failed` flag. Never file contents, prompt or reply text, query strings, or tokens.

## Management side (loopback)

The Chief tab talks to `/api/chief/*` on the dashboard (management API), never to the bridge. Every Chief route, reads included, requires a loopback `Host` (so a DNS-rebinding page can't read the transcript or send prompts). The POSTs (`/api/chief/messages`, `.../retry`) also check each of these on its own: a `Sec-Fetch-Site` other than `same-origin`/`none` is refused; an `Origin` must equal the request's real `scheme://Host` (plain HTTP on loopback; `X-Forwarded-Proto` is ignored); and the body must be `Content-Type: application/json`, so a cross-site form or `text/plain` post can't wake the chief.

## Local state

- `~/.eyrie/bridge.toml`: secret config (0600).
- `~/.eyrie/bridge/chief.db`: prompts and replies (SQLite, 0600).
- `~/.eyrie/logs/bridge-access.jsonl`: access log.

## Tests

`go test ./internal/bridge ./internal/server` covers the path-safety table (acceptance check c: traversal, absolute, encoded traversal, symlink out of root, denied files, binary, unknown root, missing/wrong token, write methods, and that each refusal is logged) and isolation (check d: management routes 404 through the bridge handler and over a real listener; bridge binds 127.0.0.1; dashboard default host is loopback).
