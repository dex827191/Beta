#!/system/bin/sh
SKIPUNZIP=0
ui_print "- HTTP Custom Headers VPN Go"
ui_print "- ARM64 module with local web dashboard"
set_perm_recursive "$MODPATH" 0 0 0755 0644
set_perm "$MODPATH/service.sh" 0 0 0755
set_perm "$MODPATH/action.sh" 0 0 0755
set_perm "$MODPATH/uninstall.sh" 0 0 0755
set_perm "$MODPATH/bin/vpn-engine" 0 0 0755
ui_print "- Installed. Open http://127.0.0.1:8787"

set_perm "$MODPATH/scripts/proxyctl.sh" 0 0 0755
set_perm_recursive "$MODPATH/libs" 0 0 0755 0644
