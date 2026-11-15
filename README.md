# HTTP Custom Headers VPN KSU — Go edition

Go rewrite scaffold with an embedded-local web control interface served from the
module's `web/` directory. Target output: Android/Linux ARM64 ELF, suitable for
rooted Android kernels. GitHub Actions compiles a static ARM64 executable
(`GOOS=linux GOARCH=arm64 CGO_ENABLED=0`); it does not require Python/Termux.

## Web root / dashboard
After installation and boot, open `http://127.0.0.1:8787` on the device.
The dashboard edits `cfgs/config.json`, displays logs/status, and has Start,
Stop, Restart controls. It is deliberately bound to localhost only.

## SSH / proxy
Engine supports SSH password or private-key authentication, SSH direct,
HTTP payload, TLS/SNI and payload+TLS modes, plus a local SOCKS5 listener
(default 127.0.0.1:1080). Set SSH endpoint and credentials in the dashboard.
Payload mode expects a proxy response ending in CRLF CRLF before SSH begins.
TLS certificate verification is enabled by Go defaults.

## Install/build
1. Upload this repository to GitHub.
2. GitHub Actions → Build KSU ARM64 → Run workflow.
3. Download artifact `HTTP-CUSTOM-HEADERS-VPN-KSU-arm64.zip`.
4. Install ZIP in KernelSU and reboot.
5. Open `http://127.0.0.1:8787`.

## Full-device routing (Option A)
This package also carries the original `libs/redsocks.zip` and
`libs/dns2socks.zip` source archives. GitHub Actions cross-compiles both for
Android ARM64 (redsocks linked against Android-built libevent). When the Go
SSH tunnel starts, `scripts/proxyctl.sh` launches:

- redsocks: TCP redirect listener `127.0.0.1:8123` → SOCKS5 `127.0.0.1:1080`;
- dns2socks: DNS listener `127.0.0.1:5353` → SOCKS5 `127.0.0.1:1080` → DNS-over-TCP `1.1.1.1:53`;
- reversible IPv4 `iptables` OUTPUT chain `KSU_HTTPVPN` redirects app TCP and
  DNS traffic. Loopback, private/reserved destinations and UID 0 are bypassed
  so the root-owned SSH and helper processes do not loop back into redsocks.

Stop in the web panel removes the chain and terminates both helper processes.
The rules affect locally generated IPv4 traffic; IPv6 and forwarded hotspot
traffic are not covered. Android firewall implementations differ, so verify
`iptables` and owner-match support on the target ROM. Keep KernelSU recovery
available before enabling full-device routing. If the SSH tunnel drops, the
iptables chain remains until Stop or module cleanup is run; use the panel Stop
button immediately. This is an initial integration and must be tested on the
specific device/ROM before daily use.

Never expose the dashboard on 0.0.0.0 on an untrusted network.
