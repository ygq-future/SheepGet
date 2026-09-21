#!/usr/bin/env sh
set -e

# Unregister SheepGet Native Messaging Host for macOS and Linux

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
  if [ -f "$dir/com.sheepget.host.json" ]; then
    rm -f "$dir/com.sheepget.host.json"
    echo "Removed manifest from $dir/com.sheepget.host.json"
  fi
done

echo "Native Messaging Host unregistration complete."
