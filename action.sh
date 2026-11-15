#!/system/bin/sh
MODDIR=${0%/*}
PIDFILE="$MODDIR/run/engine.pid"
export VPN_MODULE_DIR="$MODDIR"
case "$1" in
 start)
  if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then echo "Panel already running"; exit 0; fi
  "$MODDIR/bin/vpn-engine" >> "$MODDIR/logs.txt" 2>&1 & echo $! > "$PIDFILE"
  echo "Panel started at http://127.0.0.1:8787";;
 stop)
  /system/bin/sh "$MODDIR/scripts/proxyctl.sh" stop >/dev/null 2>&1
  if [ -f "$PIDFILE" ]; then kill "$(cat "$PIDFILE")" 2>/dev/null; rm -f "$PIDFILE"; fi
  echo "Panel stopped";;
 restart)
  "$MODDIR/action.sh" stop; sleep 1; "$MODDIR/action.sh" start;;
 status)
  if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then echo "Panel running"; else echo "Panel stopped"; fi
  /system/bin/sh "$MODDIR/scripts/proxyctl.sh" status;;
 *) echo "Usage: action.sh {start|stop|restart|status}"; exit 1;;
esac
