#!/system/bin/sh
MODDIR=${0%/*}
export VPN_MODULE_DIR="$MODDIR"
mkdir -p "$MODDIR/run" "$MODDIR/cfgs"
chmod 700 "$MODDIR/cfgs"
until [ "$(getprop sys.boot_completed)" = "1" ]; do sleep 2; done
if [ ! -x "$MODDIR/bin/vpn-engine" ]; then
  echo "vpn-engine ARM64 binary missing; build using GitHub Actions artifact" >> "$MODDIR/logs.txt"
  exit 0
fi
"$MODDIR/bin/vpn-engine" >> "$MODDIR/logs.txt" 2>&1 &
echo $! > "$MODDIR/run/engine.pid"
