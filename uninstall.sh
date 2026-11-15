#!/system/bin/sh
MODDIR=${0%/*}
export VPN_MODULE_DIR="$MODDIR"
/system/bin/sh "$MODDIR/scripts/proxyctl.sh" stop >/dev/null 2>&1
if [ -f "$MODDIR/run/engine.pid" ]; then kill "$(cat "$MODDIR/run/engine.pid")" 2>/dev/null; fi
