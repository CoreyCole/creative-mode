#!/usr/bin/env bash
set -euo pipefail

username=$(git config user.name)
if [ -z "$username" ]; then
  echo "Error: git config user.name is not set"
  exit 1
fi

# Move any files from thoughts/shared into thoughts/$username
if [ -d "thoughts/shared" ] && [ "$(ls -A thoughts/shared 2>/dev/null)" ]; then
  mkdir -p "thoughts/$username"
  cp -r thoughts/shared/* "thoughts/$username/"
  rm -rf thoughts/shared
  echo "Moved thoughts/shared -> thoughts/$username"
fi

git add thoughts && git commit -m "Synced thoughts" && git push && echo "Synced thoughts"
