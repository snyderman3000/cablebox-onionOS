#!/bin/sh
# Cablebox — TV guide and Sonarr remote for OnionOS
appdir=/mnt/SDCARD/App/Cablebox
cd "$appdir" || exit 1
mkdir -p data
log="$appdir/data/stdout.log"
if [ -f "$log" ] && [ "$(wc -c < "$log")" -gt 262144 ]; then mv "$log" "$log.1"; fi

export SSL_CERT_FILE="$appdir/cacert.pem"
export GODEBUG=tlsmlkem=0
touch /tmp/stay_awake            # don't auto-sleep while the guide downloads

"$appdir/cablebox" >> "$log" 2>&1

rm -f /tmp/stay_awake
sync
