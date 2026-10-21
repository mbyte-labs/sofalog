#!/bin/sh
# Opens two Terminal.app tabs: one running the Go backend, one the Vite frontend.
set -eu

ROOT="$(cd "$(dirname "$0")" && pwd)"

osascript <<EOF
tell application "Terminal"
	activate
	do script "cd '$ROOT/server' && go run ."
	delay 0.5
	tell application "System Events" to keystroke "t" using command down
	delay 0.5
	do script "cd '$ROOT/frontend' && npm run dev" in selected tab of front window
end tell
EOF
