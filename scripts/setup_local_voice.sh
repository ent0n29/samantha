#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

TOOLS_DIR="$ROOT/.tools"
WHISPER_VER="${WHISPER_CPP_VERSION:-v1.8.3}"
WHISPER_URL="https://github.com/ggerganov/whisper.cpp/archive/refs/tags/${WHISPER_VER}.tar.gz"
WHISPER_TGZ="$TOOLS_DIR/cache/whisper.cpp-${WHISPER_VER}.tar.gz"
WHISPER_SRC_DIR="$TOOLS_DIR/src/whisper.cpp-${WHISPER_VER}"
WHISPER_BUILD_DIR="$TOOLS_DIR/build/whisper.cpp-${WHISPER_VER}"
WHISPER_BIN_DIR="$TOOLS_DIR/whispercpp/bin"
WHISPER_LIB_DIR="$TOOLS_DIR/whispercpp/lib"

MODEL_PATH="${LOCAL_WHISPER_MODEL_PATH:-.models/whisper/ggml-base.en.bin}"
if [[ ! "$MODEL_PATH" = /* ]]; then
  MODEL_PATH="$ROOT/$MODEL_PATH"
fi

UV_BIN="$HOME/.local/bin/uv"

log() { echo "[local-voice] $*" >&2; }

mkdir -p "$TOOLS_DIR/cache" "$TOOLS_DIR/src" "$TOOLS_DIR/build" "$WHISPER_BIN_DIR" "$WHISPER_LIB_DIR"

list_macho_rpaths() {
  local target="$1"
  if ! command -v otool >/dev/null 2>&1; then
    return 0
  fi
  otool -l "$target" 2>/dev/null | awk '
    $1 == "cmd" && $2 == "LC_RPATH" {
      getline
      getline
      if ($1 == "path") print $2
    }'
}

list_macho_deps() {
  local target="$1"
  if ! command -v otool >/dev/null 2>&1; then
    return 0
  fi
  otool -L "$target" 2>/dev/null | tail -n +2 | awk '{print $1}'
}

has_stale_whisper_rpath() {
  local target="$1"
  local rp
  while IFS= read -r rp; do
    if [[ "$rp" == "$TOOLS_DIR/build/"* || "$rp" == *"/.tools/build/"* ]]; then
      return 0
    fi
  done < <(list_macho_rpaths "$target")
  return 1
}

has_loader_error() {
  local tool_path="$1"
  local out
  out="$("$tool_path" --help 2>&1 >/dev/null || true)"
  if echo "$out" | grep -Eiq 'dyld|library not loaded|image not found'; then
    return 0
  fi
  return 1
}

whisper_tool_healthy() {
  local tool_path="$1"
  if [[ ! -x "$tool_path" ]]; then
    return 1
  fi
  if command -v file >/dev/null 2>&1 && ! file "$tool_path" 2>/dev/null | grep -q 'arm64'; then
    return 1
  fi
  if has_stale_whisper_rpath "$tool_path"; then
    return 1
  fi
  if has_loader_error "$tool_path"; then
    return 1
  fi
  return 0
}

repair_macos_whisper_runtime_links() {
  local target="$1"
  if ! command -v install_name_tool >/dev/null 2>&1; then
    return 0
  fi

  local rp
  while IFS= read -r rp; do
    if [[ "$rp" == "$TOOLS_DIR/build/"* || "$rp" == *"/.tools/build/"* ]]; then
      install_name_tool -delete_rpath "$rp" "$target" >/dev/null 2>&1 || true
    fi
  done < <(list_macho_rpaths "$target")

  local dep
  while IFS= read -r dep; do
    if [[ "$dep" == "$TOOLS_DIR/build/"* || "$dep" == *"/.tools/build/"* ]]; then
      install_name_tool -change "$dep" "@rpath/$(basename "$dep")" "$target" >/dev/null 2>&1 || true
    fi
  done < <(list_macho_deps "$target")
}

# Ensure we have uv (prefer a native arm64 install under ~/.local/bin).
if [[ ! -x "$UV_BIN" ]]; then
  log "uv not found at $UV_BIN; installing (arm64)"
  mkdir -p "$HOME/.local/bin"
  # Run the installer under arm64 so it downloads the arm64 binary.
  arch -arm64 /bin/bash -lc "UV_UNMANAGED_INSTALL=1 UV_NO_MODIFY_PATH=1 sh -c \"\$(curl -LsSf https://astral.sh/uv/install.sh)\"" >/dev/null
fi

if [[ ! -x "$UV_BIN" ]]; then
  log "uv install failed (still missing at $UV_BIN)."
  exit 1
fi

# Pick a python3 to build the venv.
# Prefer uv-managed python (arm64-only) if present to avoid universal-binary arch confusion.
PYTHON3_BIN=""
if [[ -x "$HOME/.local/bin/python3" ]]; then
  PYTHON3_BIN="$HOME/.local/bin/python3"
else
  PYTHON3_BIN="$(command -v python3 || true)"
fi

if [[ -z "$PYTHON3_BIN" ]]; then
  log "python3 not found"
  exit 1
fi

# Resolve a *real* python path. Symlinks can produce a broken venv on macOS.
PYTHON3_REAL="$("$PYTHON3_BIN" -c 'import os,sys; print(os.path.realpath(sys.executable))' 2>/dev/null || true)"
if [[ -z "$PYTHON3_REAL" ]]; then
  PYTHON3_REAL="$PYTHON3_BIN"
fi

# Ensure an arm64-capable venv at .venv.
if [[ -d "$ROOT/.venv" ]]; then
  # Validate the env by importing numpy under arm64.
  if ! arch -arm64 "$ROOT/.venv/bin/python" -c 'import platform, numpy; print(platform.machine())' 2>/dev/null | grep -q '^arm64'; then
    log "Existing .venv is not a working arm64 env; recreating"
    rm -rf "$ROOT/.venv"
  fi
fi

if [[ ! -d "$ROOT/.venv" ]]; then
  log "Creating .venv (arm64 python)"
  arch -arm64 "$PYTHON3_REAL" -m venv "$ROOT/.venv"
fi

PY_BIN="$ROOT/.venv/bin/python"

# Install python deps with uv (fast, reproducible).
log "Installing python deps (kokoro + numpy)"
"$UV_BIN" pip install -q -p "$PY_BIN" -U pip setuptools wheel
"$UV_BIN" pip install -q -p "$PY_BIN" -U "kokoro>=0.9.2" numpy

# Sanity check: ensure deps import under arm64.
if ! arch -arm64 "$PY_BIN" -c 'import numpy, kokoro; import platform; print(platform.machine())' 2>/dev/null | grep -q '^arm64'; then
  log "Python deps did not import under arm64. Removing .venv so a rerun can repair it."
  rm -rf "$ROOT/.venv"
  exit 1
fi

# Download whisper model (ggml) if missing.
MODEL_DIR="$(dirname "$MODEL_PATH")"
mkdir -p "$MODEL_DIR"

if [[ ! -f "$MODEL_PATH" ]]; then
  log "Downloading whisper model -> $MODEL_PATH"
  file_name="$(basename "$MODEL_PATH")"
  url="https://huggingface.co/ggerganov/whisper.cpp/resolve/main/${file_name}"
  if ! curl -L --fail --retry 3 --retry-delay 2 -o "$MODEL_PATH" "$url"; then
    log "Failed to download model: $url"
    log "Set LOCAL_WHISPER_MODEL_PATH to a valid whisper.cpp ggml model filename (e.g. .models/whisper/ggml-small.bin) and re-run."
    exit 1
  fi
else
  log "whisper model present: $MODEL_PATH"
fi

# Build whisper.cpp tools locally if missing or unhealthy.
need_build=0
if ! whisper_tool_healthy "$WHISPER_BIN_DIR/whisper-cli"; then
  need_build=1
fi
if ! whisper_tool_healthy "$WHISPER_BIN_DIR/whisper-server"; then
  need_build=1
fi

if [[ "$need_build" == "1" ]]; then
  log "Building whisper.cpp (${WHISPER_VER}) with Metal (this may take a few minutes)"

  if [[ ! -f "$WHISPER_TGZ" ]]; then
    log "Downloading whisper.cpp source -> $WHISPER_TGZ"
    curl -L --fail --retry 3 --retry-delay 2 -o "$WHISPER_TGZ" "$WHISPER_URL"
  fi

  if [[ ! -d "$WHISPER_SRC_DIR" ]]; then
    tmp_extract="$TOOLS_DIR/src/.extract-$$"
    rm -rf "$tmp_extract"
    mkdir -p "$tmp_extract"
    tar -xzf "$WHISPER_TGZ" -C "$tmp_extract"

    extracted="$(find "$tmp_extract" -maxdepth 1 -type d -name 'whisper.cpp-*' | head -n 1 || true)"
    if [[ -z "$extracted" ]]; then
      log "Failed to locate extracted whisper.cpp source directory"
      exit 1
    fi

    rm -rf "$WHISPER_SRC_DIR"
    mv "$extracted" "$WHISPER_SRC_DIR"
    rm -rf "$tmp_extract"
  fi

  # Prefer system cmake, otherwise install a local one via pip (no Homebrew needed).
  CMAKE_BIN="$(command -v cmake || true)"
  if [[ -z "$CMAKE_BIN" ]]; then
    log "cmake not found; installing cmake into .venv"
    "$UV_BIN" pip install -q -p "$PY_BIN" -U cmake
    CMAKE_BIN="$ROOT/.venv/bin/cmake"
  fi

  rm -rf "$WHISPER_BUILD_DIR"
  mkdir -p "$WHISPER_BUILD_DIR"

  "$CMAKE_BIN" -S "$WHISPER_SRC_DIR" -B "$WHISPER_BUILD_DIR" \
    -DCMAKE_BUILD_TYPE=Release \
    -DCMAKE_OSX_ARCHITECTURES=arm64 \
    -DBUILD_SHARED_LIBS=OFF \
    -DWHISPER_BUILD_EXAMPLES=ON \
    -DGGML_METAL=ON \
    -DGGML_NATIVE=OFF

  ncpu="$(sysctl -n hw.ncpu 2>/dev/null || echo 8)"
  "$CMAKE_BIN" --build "$WHISPER_BUILD_DIR" -j "$ncpu"

  cli_path=""
  server_path=""
  if [[ -x "$WHISPER_BUILD_DIR/bin/whisper-cli" ]]; then
    cli_path="$WHISPER_BUILD_DIR/bin/whisper-cli"
  else
    cli_path="$(find "$WHISPER_BUILD_DIR" -type f -name whisper-cli -perm -111 2>/dev/null | head -n 1 || true)"
  fi

  if [[ -x "$WHISPER_BUILD_DIR/bin/whisper-server" ]]; then
    server_path="$WHISPER_BUILD_DIR/bin/whisper-server"
  else
    server_path="$(find "$WHISPER_BUILD_DIR" -type f -name whisper-server -perm -111 2>/dev/null | head -n 1 || true)"
  fi

  if [[ -z "$cli_path" || -z "$server_path" ]]; then
    log "Build succeeded but whisper-cli/whisper-server were not found."
    exit 1
  fi

  cp -f "$cli_path" "$WHISPER_BIN_DIR/whisper-cli"
  cp -f "$server_path" "$WHISPER_BIN_DIR/whisper-server"
  chmod +x "$WHISPER_BIN_DIR/whisper-cli" "$WHISPER_BIN_DIR/whisper-server"

  # If a shared build was produced, vendor dylibs beside the binaries.
  # This keeps runtime independent from ephemeral .tools/build paths.
  found_dylib=0
  while IFS= read -r dylib; do
    found_dylib=1
    cp -f "$dylib" "$WHISPER_LIB_DIR/$(basename "$dylib")"
  done < <(find "$WHISPER_BUILD_DIR" -type f -name '*.dylib' 2>/dev/null | sort -u)

  if [[ "$found_dylib" == "1" ]] && command -v install_name_tool >/dev/null 2>&1; then
    for dylib in "$WHISPER_LIB_DIR"/*.dylib; do
      [[ -e "$dylib" ]] || continue
      install_name_tool -id "@rpath/$(basename "$dylib")" "$dylib" >/dev/null 2>&1 || true
      repair_macos_whisper_runtime_links "$dylib"
      install_name_tool -add_rpath "@loader_path" "$dylib" >/dev/null 2>&1 || true
    done
    for tool in "$WHISPER_BIN_DIR/whisper-cli" "$WHISPER_BIN_DIR/whisper-server"; do
      [[ -x "$tool" ]] || continue
      repair_macos_whisper_runtime_links "$tool"
      install_name_tool -add_rpath "@executable_path/../lib" "$tool" >/dev/null 2>&1 || true
    done
  fi

  log "Installed whisper-cli -> $WHISPER_BIN_DIR/whisper-cli"
  log "Installed whisper-server -> $WHISPER_BIN_DIR/whisper-server"
else
  log "whisper.cpp tools present: $WHISPER_BIN_DIR"
fi

if ! whisper_tool_healthy "$WHISPER_BIN_DIR/whisper-cli"; then
  log "whisper-cli runtime check failed after setup."
  exit 1
fi
if ! whisper_tool_healthy "$WHISPER_BIN_DIR/whisper-server"; then
  log "whisper-server runtime check failed after setup."
  exit 1
fi

log "Done."
log "Next: make dev"
