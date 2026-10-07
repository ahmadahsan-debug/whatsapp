#!/usr/bin/env bash
# Step 1: install Go, uv, ffmpeg and git (only what is missing).
source "$(dirname "$0")/lib.sh"

if [ "$OS" = Darwin ]; then
  command -v brew >/dev/null || { echo "Homebrew is missing. Install it from https://brew.sh then run this again."; exit 1; }
  for pair in go:go uv:uv ffmpeg:ffmpeg git:git; do
    cmd="${pair%%:*}"; pkg="${pair##*:}"
    if command -v "$cmd" >/dev/null; then echo "ok: $cmd already installed"; else echo "installing $pkg ..."; brew install "$pkg"; fi
  done
else
  missing=""
  for cmd in go uv ffmpeg git; do command -v "$cmd" >/dev/null && echo "ok: $cmd" || missing="$missing $cmd"; done
  [ -z "$missing" ] || { echo "Please install:$missing (e.g. with apt/dnf; uv: https://docs.astral.sh/uv/)"; exit 1; }
fi
echo; go version; uv --version; ffmpeg -version | head -1; git --version
