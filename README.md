# meshcore-kit 🧩📻

**The shared app runtime for MeshCore devices: connect, sync, inbox, keepalive — and messages that always fit.**

*[🇷🇺 Русская версия](README_RU.md)*

`meshcore-kit` is what every application on top of
[meshcore-go](https://github.com/stempit/meshcore-go) needs but nobody wants to
write twice: opening the device connection (BLE, serial or TCP over Wi-Fi/Ethernet), pulling the
messages stored while your app was down, forwarding live arrivals into a
bounded inbox with cursors, and keeping the link alive. The `fit` package
solves the other universal problem — squeezing replies into the 140-byte mesh
limit.

## Install

```sh
go get github.com/stempit/meshcore-kit
```

The kit is deliberately config-free: callers resolve their own policy into
plain structs (`ConnectOptions`, `fit.Options`), so it stays embeddable behind
any app's configuration.

## Packages

### `companion`

- `Connect(ConnectOptions)` — BLE scan by default (with optional device
  name/address selection), USB serial when asked, or TCP (`"host:port"`,
  default port 5000) for Wi-Fi/Ethernet companion firmware.
- `Service` — startup stored-message sync (bounded), a live forwarder into the
  inbox, and a 30s keepalive/reconnect loop.
- `Inbox` — a bounded (1000) FIFO of normalized DM/channel messages with
  cursor support: `Add`, `Drain`, `Since`, `LastID`. The JSON shape
  (`Message`) is what gateways hand to their clients.

```go
client, err := companion.Connect(companion.ConnectOptions{Type: "serial", SerialPort: "/dev/ttyUSB0"})
svc := companion.NewService(client, slog.Default())
svc.Start(ctx) // blocks through the startup sync, then serves in background
msgs, cursor := svc.Inbox.Drain()
```

One `Service` per physical device: a companion device serves one connection,
so don't run two apps against the same radio.

### `fit`

Fits logical replies into wire-ready mesh messages — pure, no config, no
providers:

- variants: `truncate` (rune-safe cut + ellipsis), `split` (`[i/N]` parts,
  ≤9), `skip` (drop what won't fit), `llm` (your injected compressor, with
  automatic split fallback),
- the lossless Cyrillic→Latin homoglyph squeeze (identical glyphs, half the
  bytes),
- prefix handling: the send-layer prefix lands on the first fitted message
  only and counts toward its budget.

```go
o := fit.Options{MaxBytes: 140, Ellipsis: "…", Variant: fit.Split}
parts := fit.Fit(o, "@[user] ", []string{longReply}) // each part ≤ 140 bytes
```

Applications that resolve their policy from a config file keep a thin wrapper
(see meshcore-go-bot's `internal/shrink`); the algorithms live here.

## Related projects

- [meshcore-go](https://github.com/stempit/meshcore-go) — the protocol driver
  this runtime is built on.
- [meshcore-go-bot](https://github.com/stempit/meshcore-go-bot) — the full-featured mesh chat
  bot.
- [meshcore-http-gateway](https://github.com/stempit/meshcore-http-gateway),
  [meshcore-mcp-gateway](https://github.com/stempit/meshcore-mcp-gateway) —
  gateways built on this runtime.

## License

MIT — see [LICENSE](LICENSE).
