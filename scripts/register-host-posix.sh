#!/usr/bin/env sh
set -e

# Register SheepGet Native Messaging Host for macOS and Linux
EXTENSION_ID="oediboaeofmnlkgcjhnpfnngphkjooam"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

HOST_BIN="$1"
if [ -z "$HOST_BIN" ]; then
  if [ -f "$SCRIPT_DIR/sheepget-host" ]; then
    HOST_BIN="$SCRIPT_DIR/sheepget-host"
  elif [ -f "$ROOT_DIR/build/bin/sheepget-host" ]; then
    HOST_BIN="$ROOT_DIR/build/bin/sheepget-host"
  fi
fi

if [ -z "$HOST_BIN" ] || [ ! -f "$HOST_BIN" ]; then
  echo "Error: sheepget-host not found. Build it first." >&2
  exit 1
fi

HOST_BIN_ABS="$(cd "$(dirname "$HOST_BIN")" && pwd)/$(basename "$HOST_BIN")"
MANIFEST_DIR="$(dirname "$HOST_BIN_ABS")"
MANIFEST_FILE="$MANIFEST_DIR/com.sheepget.host.json"

cat <<EOF > "$MANIFEST_FILE"
{
  "name": "com.sheepget.host",
  "description": "SheepGet Native Messaging Host",
  "path": "$HOST_BIN_ABS",
  "type": "stdio",
  "allowed_origins": [
    "chrome-extension://$EXTENSION_ID/"
  ]
}
EOF

echo "Created manifest at $MANIFEST_FILE"

OS="$(uname -s)"
TARGET_DIRS=""

if [ "$OS" = "Darwin" ]; then
  TARGET_DIRS="
    $HOME/Library/Application Support/Google/Chrome/NativeMessagingHosts
    $HOME/Library/Application Support/Microsoft Edge/NativeMessagingHosts
  "
else
  TARGET_DIRS="
    $HOME/.config/google-chrome/NativeMessagingHosts
    $HOME/.config/microsoft-edge/NativeMessagingHosts
  "
fi

for dir in $TARGET_DIRS; do
  mkdir -p "$dir"
  cp -f "$MANIFEST_FILE" "$dir/com.sheepget.host.json"
  echo "Linked manifest into $dir/com.sheepget.host.json"
done

echo "Native Messaging Host registration complete."
