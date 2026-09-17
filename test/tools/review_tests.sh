#!/bin/sh
set -eu
cd /var/tmp/smail-review-20260917
for name in outbox utils-send utils-smtp controllers-email db-migration res_init services-dbmigration; do
    if runuser -u smail -- "./$name.test" -test.v -test.timeout=180s > "$name.result.log" 2>&1; then
        printf '%s PASS\n' "$name"
        tail -n 3 "$name.result.log"
    else
        cat "$name.result.log"
        exit 1
    fi
done
