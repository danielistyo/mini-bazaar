#!/bin/sh
# Deploy one commit to the bazaar bot, with the e2e suite as a gate.
# A failing e2e stops here: the running bot keeps the old binary.
set -e

cd "$(dirname "$0")/.."
ROOT="$(pwd)"

echo "== commit"
git pull --ff-only
git rev-parse --short HEAD

echo "== build"
make build

echo "== e2e (required)"
if ! make e2e; then
    echo "FAIL e2e: not restarting. The previous process is still running." >&2
    exit 1
fi

echo "== backup"
mkdir -p backups
sqlite3 data/orders.db ".backup 'backups/orders-$(date +%F-%H%M).db'"

echo "== restart"
for p in $(pgrep -x zeroclaw || true); do kill -INT "$p"; done
sleep 8
tmux kill-session -t bazaar 2>/dev/null || true
tmux new-session -d -s bazaar "cd $ROOT; ./start-bazaar.sh 2>&1 | tee -a logs/zeroclaw.log"
sleep 25

if ! pgrep -x zeroclaw >/dev/null; then
    echo "FAIL daemon is not running after restart" >&2
    exit 1
fi

echo "== running"
pgrep -x zeroclaw | head -1
tail -3 logs/zeroclaw.log
echo "== now send one message in a bound group; a preview reply means the identity path works"
