#!/system/bin/sh
MODDIR=${VPN_MODULE_DIR:-$(cd "$(dirname "$0")/.." && pwd)}
BIN="$MODDIR/bin"; RUN="$MODDIR/run"; IPT="${IPTABLES:-iptables}"; CHAIN=KSU_HTTPVPN
mkdir -p "$RUN"
log(){ echo "[proxyctl] $*" >> "$MODDIR/logs.txt"; }
cleanup_rules(){ $IPT -t nat -D OUTPUT -j "$CHAIN" 2>/dev/null; $IPT -t nat -F "$CHAIN" 2>/dev/null; $IPT -t nat -X "$CHAIN" 2>/dev/null; }
stop_helpers(){ for f in redsocks dns2socks; do [ -f "$RUN/$f.pid" ] && kill "$(cat "$RUN/$f.pid")" 2>/dev/null; rm -f "$RUN/$f.pid"; done; }
case "$1" in
start)
 [ -x "$BIN/redsocks" ] && [ -x "$BIN/dns2socks" ] || { log 'Binary redsocks/dns2socks belum tersedia'; exit 1; }
 "$0" stop >/dev/null 2>&1
 "$BIN/redsocks" -c "$MODDIR/vpn/redsocks.conf" >> "$MODDIR/logs.txt" 2>&1 & echo $! > "$RUN/redsocks.pid"
 "$BIN/dns2socks" 127.0.0.1:1080 1.1.1.1:53 127.0.0.1:5353 >> "$MODDIR/logs.txt" 2>&1 & echo $! > "$RUN/dns2socks.pid"
 sleep 1
 cleanup_rules
 $IPT -t nat -N "$CHAIN" || exit 1
 # Exclude root-owned tunnel and helper sockets to avoid routing loops.
 $IPT -t nat -A "$CHAIN" -m owner --uid-owner 0 -j RETURN
 for net in 127.0.0.0/8 10.0.0.0/8 172.16.0.0/12 192.168.0.0/16 169.254.0.0/16 224.0.0.0/4 240.0.0.0/4; do $IPT -t nat -A "$CHAIN" -d "$net" -j RETURN; done
 $IPT -t nat -A "$CHAIN" -p udp --dport 53 -j REDIRECT --to-ports 5353
 $IPT -t nat -A "$CHAIN" -p tcp --dport 53 -j REDIRECT --to-ports 5353
 $IPT -t nat -A "$CHAIN" -p tcp -j REDIRECT --to-ports 8123
 $IPT -t nat -I OUTPUT 1 -j "$CHAIN"
 log 'Full-device IPv4 TCP + DNS routing enabled.';;
stop) cleanup_rules; stop_helpers; log 'Rules/helpers stopped.';;
status) $IPT -t nat -S "$CHAIN" 2>/dev/null;;
*) echo "Usage: $0 {start|stop|status}"; exit 2;;
esac
