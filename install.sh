#!/usr/bin/env bash
# Copyright 2026 LHU CSIE DCLAB (yuanyi) Authors.
# SPDX-License-Identifier: Apache-2.0
#
# Yuanyi Client Agent -- interactive installer and manager for Linux.
#
# Handles install, uninstall, and model management (download / switch / delete)
# from a single menu. Run with no arguments for the menu, or --help for flags.
#
#   curl -fsSL https://raw.githubusercontent.com/lhu-csie-dclab/yuanyi/main/install.sh -o install.sh
#   bash install.sh
#
# Add --example (or -y / --yes) anywhere in the arguments to skip every interactive prompt
# and accept the default answer at each one -- e.g. `bash install.sh install --example`
# installs and starts a node fully unattended, in one shot. Confirmations that would be
# destructive or surprising to run unattended (uninstalling, deleting a model, turning this
# node into a network hub) intentionally still default to "no" even in this mode -- see each
# confirm_default_yes call site for what actually auto-proceeds.
#
# To join an EXISTING swarm fully unattended (rather than always generating a brand-new,
# isolated swarm.key), pass --swarm-key <path> pointing at that swarm's key file, e.g.:
#   bash install.sh install --example --swarm-key /path/to/swarm.key
# This also works without --example, as a shortcut that skips just that one prompt.

set -euo pipefail

NON_INTERACTIVE=0
SWARM_KEY_PATH=""
_args=("$@")
for _i in "${!_args[@]}"; do
  case "${_args[$_i]}" in
    --example|-y|--yes) NON_INTERACTIVE=1 ;;
    --swarm-key)
      SWARM_KEY_PATH="${_args[$((_i+1))]:-}"
      ;;
    --swarm-key=*)
      SWARM_KEY_PATH="${_args[$_i]#--swarm-key=}"
      ;;
  esac
done

REPO_URL="https://github.com/lhu-csie-dclab/yuanyi.git"

# Defaults. Every one of these can be overridden interactively during install.
DEFAULT_MODEL="Qwen/Qwen3-4B-AWQ"
DEFAULT_BOOTSTRAP="/dns4/host1.niveec.com/tcp/50004/p2p/12D3KooWBaeTNHHUc1RAePLbYJWvxy9xJXBVyYyW5aEY5hNWfzAh"
DEFAULT_WEB_PORT=50007
DEFAULT_PROXY_PORT=50006
DEFAULT_VLLM_PORT=8100
DEFAULT_MOONCAKE_PORT=8998
DEFAULT_HUB_P2P_PORT=50004
DEFAULT_HUB_PROXY_PORT=50008

# Install under /opt when run as root, otherwise under the user's home, so the
# script never needs sudo just to pick a location.
if [ "$(id -u)" -eq 0 ]; then
  DEFAULT_INSTALL_DIR="/opt/yuanyi-client"
  DEFAULT_MODEL_DIR="/opt/yuanyi-models"
else
  DEFAULT_INSTALL_DIR="$HOME/yuanyi-client"
  DEFAULT_MODEL_DIR="$HOME/yuanyi-models"
fi

# State file lets uninstall and model management find a previous install without
# asking the user to retype the path every time.
STATE_FILE="${XDG_CONFIG_HOME:-$HOME/.config}/yuanyi-client/install.conf"

C_RESET=$'\033[0m'; C_BOLD=$'\033[1m'; C_DIM=$'\033[2m'
C_RED=$'\033[31m'; C_GREEN=$'\033[32m'; C_YELLOW=$'\033[33m'; C_CYAN=$'\033[36m'

info()  { printf '%s[*]%s %s\n' "$C_CYAN"  "$C_RESET" "$*"; }
ok()    { printf '%s[+]%s %s\n' "$C_GREEN" "$C_RESET" "$*"; }
warn()  { printf '%s[!]%s %s\n' "$C_YELLOW" "$C_RESET" "$*"; }
err()   { printf '%s[x]%s %s\n' "$C_RED"   "$C_RESET" "$*" >&2; }
die()   { err "$*"; exit 1; }

heading() {
  printf '\n%s%s%s\n' "$C_BOLD" "$*" "$C_RESET"
  printf '%s%s%s\n' "$C_DIM" "$(printf '%.0s-' $(seq 1 ${#1}))" "$C_RESET"
}

# ----------------------------------------------------------------------------
# prompts
# ----------------------------------------------------------------------------

# ask <prompt> <default> -- echoes the answer, or the default when input is empty.
ask() {
  local prompt="$1" default="${2:-}" reply
  if [ "$NON_INTERACTIVE" = "1" ]; then
    printf '%s' "$default"
    return
  fi
  if [ -n "$default" ]; then
    read -r -p "$prompt [$default]: " reply </dev/tty || reply=""
    printf '%s' "${reply:-$default}"
  else
    read -r -p "$prompt: " reply </dev/tty || reply=""
    printf '%s' "$reply"
  fi
}

confirm() {
  local prompt="$1" reply
  if [ "$NON_INTERACTIVE" = "1" ]; then return 1; fi
  read -r -p "$prompt [y/N]: " reply </dev/tty || reply=""
  [[ "$reply" =~ ^[Yy]$ ]]
}

confirm_default_yes() {
  local prompt="$1" reply
  if [ "$NON_INTERACTIVE" = "1" ]; then return 0; fi
  read -r -p "$prompt [Y/n]: " reply </dev/tty || reply=""
  [[ -z "$reply" || "$reply" =~ ^[Yy]$ ]]
}

ask_port() {
  local prompt="$1" default="$2" value
  while true; do
    value="$(ask "$prompt" "$default")"
    if [[ "$value" =~ ^[0-9]+$ ]] && [ "$value" -ge 1 ] && [ "$value" -le 65535 ]; then
      printf '%s' "$value"; return 0
    fi
    warn "Not a valid port (1-65535): $value"
  done
}

# ----------------------------------------------------------------------------
# state
# ----------------------------------------------------------------------------

save_state() {
  mkdir -p "$(dirname "$STATE_FILE")"
  cat > "$STATE_FILE" <<EOF
INSTALL_DIR="$INSTALL_DIR"
MODEL_DIR="$MODEL_DIR"
EOF
}

load_state() {
  INSTALL_DIR="${INSTALL_DIR:-}"
  MODEL_DIR="${MODEL_DIR:-}"
  # shellcheck source=/dev/null
  [ -f "$STATE_FILE" ] && . "$STATE_FILE" || true
}

# Resolve an existing install, prompting only if the recorded one is gone.
require_install() {
  load_state
  if [ -n "${INSTALL_DIR:-}" ] && [ -f "$INSTALL_DIR/docker-compose.yml" ]; then
    return 0
  fi
  warn "No installation recorded at ${INSTALL_DIR:-<unset>}."
  INSTALL_DIR="$(ask "Path to the existing installation" "$DEFAULT_INSTALL_DIR")"
  [ -f "$INSTALL_DIR/docker-compose.yml" ] || die "Not an installation directory: $INSTALL_DIR"
  MODEL_DIR="${MODEL_DIR:-$DEFAULT_MODEL_DIR}"
}

# ----------------------------------------------------------------------------
# prerequisites
# ----------------------------------------------------------------------------

have() { command -v "$1" >/dev/null 2>&1; }

compose() {
  if docker compose version >/dev/null 2>&1; then
    docker compose "$@"
  elif have docker-compose; then
    docker-compose "$@"
  else
    die "Neither 'docker compose' nor 'docker-compose' is available."
  fi
}

# Detects the distribution's package manager. Only families this script actually knows how
# to drive are reported; anything else falls back to printing manual instructions rather
# than guessing at a package name and failing halfway through an install.
detect_pkg_mgr() {
  if have apt-get; then echo "apt"
  elif have dnf; then echo "dnf"
  elif have yum; then echo "yum"
  elif have pacman; then echo "pacman"
  elif have zypper; then echo "zypper"
  else echo ""
  fi
}

# Installs git and/or docker via the distribution's package manager. Docker is taken from
# the distro repos (docker.io / docker / moby-engine) rather than Docker's own convenience
# script: piping a remote script into a root shell is a much bigger thing to do to someone's
# machine than installing a signed distro package, and the distro build is sufficient here.
install_prereqs() {
  local pkgs=("$@")
  local mgr; mgr="$(detect_pkg_mgr)"
  local sudo_cmd=""
  [ "$(id -u)" -ne 0 ] && sudo_cmd="sudo"
  if [ -n "$sudo_cmd" ] && ! have sudo; then
    err "Need root to install packages, and sudo is not available. Re-run as root."
    return 1
  fi

  local resolved=()
  local p
  for p in "${pkgs[@]}"; do
    case "$p:$mgr" in
      docker:apt)            resolved+=("docker.io") ;;
      docker:dnf|docker:yum) resolved+=("docker") ;;
      docker:pacman)         resolved+=("docker") ;;
      docker:zypper)         resolved+=("docker") ;;
      *)                     resolved+=("$p") ;;
    esac
  done

  info "Installing: ${resolved[*]}"
  case "$mgr" in
    apt)    $sudo_cmd apt-get update -qq && $sudo_cmd apt-get install -y "${resolved[@]}" ;;
    dnf)    $sudo_cmd dnf install -y "${resolved[@]}" ;;
    yum)    $sudo_cmd yum install -y "${resolved[@]}" ;;
    pacman) $sudo_cmd pacman -Sy --noconfirm "${resolved[@]}" ;;
    zypper) $sudo_cmd zypper install -y "${resolved[@]}" ;;
    *)      return 1 ;;
  esac || return 1

  # Docker installs but does not start on most distros, and its socket is root-owned, so a
  # freshly installed docker is unusable without these two steps.
  if printf '%s\n' "${pkgs[@]}" | grep -qx docker; then
    $sudo_cmd systemctl enable --now docker >/dev/null 2>&1 || true
    if [ -n "$sudo_cmd" ]; then
      $sudo_cmd usermod -aG docker "$USER" >/dev/null 2>&1 || true
      warn "Added $USER to the 'docker' group. Group changes only apply to new logins:"
      warn "  run 'newgrp docker' (or log out and back in), then re-run this script."
    fi
  fi
  return 0
}

check_prereqs() {
  local missing=()
  have git || missing+=("git")
  have docker || missing+=("docker")
  if [ ${#missing[@]} -gt 0 ]; then
    warn "Missing required commands: ${missing[*]}"
    local mgr; mgr="$(detect_pkg_mgr)"
    if [ -z "$mgr" ]; then
      err "No supported package manager found (apt/dnf/yum/pacman/zypper)."
      echo "  Install them manually first. See docs/install/ubuntu/README.md"
      return 1
    fi
    echo "  Recommended: install them now with $mgr (git, and Docker from your distro repo)."
    if confirm_default_yes "Install the missing prerequisites automatically?"; then
      install_prereqs "${missing[@]}" || {
        err "Automatic install failed. Install them manually, then re-run."
        echo "  See docs/install/ubuntu/README.md"
        return 1
      }
      # Re-check rather than assume: the package may have installed while the daemon or
      # group membership still is not usable in this shell.
      local still=()
      have git || still+=("git")
      have docker || still+=("docker")
      if [ ${#still[@]} -gt 0 ]; then
        err "Still missing after install: ${still[*]}"
        return 1
      fi
      ok "Prerequisites installed."
    else
      echo "  Install them first. See docs/install/ubuntu/README.md"
      return 1
    fi
  fi
  docker compose version >/dev/null 2>&1 || have docker-compose \
    || die "Docker Compose plugin not found. See docs/install/ubuntu/README.md"

  if ! docker info >/dev/null 2>&1; then
    err "Cannot talk to the Docker daemon."
    echo "  Either start it, or add yourself to the docker group:"
    echo "    sudo usermod -aG docker \$USER && newgrp docker"
    return 1
  fi

  if ! have nvidia-smi; then
    warn "nvidia-smi not found. Without a GPU this node can only run in relay-only mode."
  fi
  return 0
}

# ----------------------------------------------------------------------------
# swarm key
# ----------------------------------------------------------------------------

# libp2p PSK: 7-byte header lines plus 32 random bytes as hex, LF endings, 96 bytes total.
generate_swarm_key() {
  local dest="$1" hex
  if have openssl; then
    hex="$(openssl rand -hex 32)"
  else
    hex="$(head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n')"
  fi
  printf '/key/swarm/psk/1.0.0/\n/base16/\n%s\n' "$hex" > "$dest"
  # 644, not 600: this file is bind-mounted into the container, which runs as uid 1000
  # (dynamo) rather than root. At 600 the agent cannot read it and exits with
  # "failed to open swarm.key: permission denied" in a restart loop.
  chmod 644 "$dest"
}

validate_swarm_key() {
  local f="$1"
  [ -s "$f" ] || return 1
  head -1 "$f" | grep -q '^/key/swarm/psk/1.0.0/' || return 1
  [ "$(sed -n '3p' "$f" | tr -d '\r\n' | wc -c)" -eq 64 ] || return 1
  return 0
}

setup_swarm_key() {
  local dest="$INSTALL_DIR/swarm.key"

  if [ -f "$dest" ] && validate_swarm_key "$dest"; then
    ok "Existing swarm.key kept (sha256: $(sha256sum "$dest" | cut -c1-16)...)"
    return 0
  fi

  echo
  echo "The swarm key (PSK) decides which private network this node joins."
  echo "  - Joining an existing swarm: paste that swarm's key, it must match exactly."
  echo "  - Starting a new swarm:      leave blank and one will be generated."
  echo
  local path
  if [ -n "$SWARM_KEY_PATH" ]; then
    echo "Using --swarm-key: $SWARM_KEY_PATH"
  fi
  path="$(ask "Path to an existing swarm.key (blank = generate new)" "$SWARM_KEY_PATH")"

  if [ -n "$path" ]; then
    path="${path/#\~/$HOME}"
    [ -f "$path" ] || die "No such file: $path"
    validate_swarm_key "$path" || die "Not a valid libp2p swarm.key: $path"
    install -m 644 "$path" "$dest"   # see generate_swarm_key note: container runs as uid 1000
    ok "swarm.key installed from $path"
  else
    generate_swarm_key "$dest"
    ok "New swarm.key generated."
    warn "Every node in this swarm needs this exact file. Back it up; it is not recoverable."
  fi
  echo "  sha256: $(sha256sum "$dest" | cut -d' ' -f1)"
}

# ----------------------------------------------------------------------------
# models
# ----------------------------------------------------------------------------

# Local directory name for a Hugging Face repo id, e.g. Qwen/Qwen3-4B-AWQ -> Qwen3-4B-AWQ
model_dirname() { printf '%s' "${1##*/}"; }

# fetch_model_weights <repo> <dest> -- the one place that actually pulls weights, used by
# both do_install() and the models-menu download_model() so they can't drift out of sync
# (they used to have separate copies; only one of them warned about missing git-lfs).
#
# Without huggingface-cli/hf, this falls back to a plain git clone. If git-lfs isn't
# present, that clone silently succeeds with LFS *pointer* files instead of the actual
# weights -- a few hundred bytes each instead of gigabytes -- and every prior version of
# this script reported that as "Model ready". The container then builds and starts fine,
# and vLLM fails to load the model with a confusing error minutes later, far from this
# step. Auto-install git-lfs (best effort, matches how this script already handles other
# missing prerequisites) and verify the result actually contains weight-sized files
# before calling it done.
fetch_model_weights() {
  local repo="$1" dest="$2"
  mkdir -p "$dest"

  if have huggingface-cli; then
    huggingface-cli download "$repo" --local-dir "$dest" || { rm -rf "$dest"; die "Download failed."; }
  elif have hf; then
    hf download "$repo" --local-dir "$dest" || { rm -rf "$dest"; die "Download failed."; }
  else
    if ! have git-lfs; then
      info "git-lfs not found; installing it so weights download as real files, not pointers"
      if have apt-get; then
        (apt-get update -qq && apt-get install -y -qq git-lfs) >/dev/null 2>&1 || true
      elif have dnf; then
        dnf install -y -q git-lfs >/dev/null 2>&1 || true
      elif have yum; then
        yum install -y -q git-lfs >/dev/null 2>&1 || true
      fi
    fi
    if ! have git-lfs; then
      rm -rf "$dest"
      die "Cannot download model weights: no huggingface-cli/hf, and git-lfs could not be installed automatically. Install git-lfs (or huggingface-cli) yourself and re-run."
    fi
    git lfs install --skip-repo >/dev/null 2>&1 || true
    git clone "https://huggingface.co/$repo" "$dest" || { rm -rf "$dest"; die "Clone failed."; }
    warn "Downloaded via git. Install 'huggingface-cli' to avoid the extra .git copy."
  fi

  # A real model directory (even a small/quantized one) is at minimum tens of MB. Anything
  # under that is almost certainly LFS pointer files, not weights -- catch it here instead
  # of leaving the user to debug a vLLM crash with no clue why the model won't load.
  local size_kb; size_kb="$(du -sk "$dest" 2>/dev/null | cut -f1)"
  if [ -z "$size_kb" ] || [ "$size_kb" -lt 51200 ]; then
    rm -rf "$dest"
    die "Downloaded model is only $((size_kb / 1024))MB -- that's LFS pointer files, not real weights (git-lfs was likely unavailable during clone). Install huggingface-cli (pip install huggingface_hub) or git-lfs and re-run."
  fi
}

list_models() {
  load_state
  MODEL_DIR="${MODEL_DIR:-$DEFAULT_MODEL_DIR}"
  if [ ! -d "$MODEL_DIR" ] || [ -z "$(ls -A "$MODEL_DIR" 2>/dev/null)" ]; then
    echo "  (no models in $MODEL_DIR)"
    return 1
  fi
  local current=""
  [ -n "${INSTALL_DIR:-}" ] && [ -f "$INSTALL_DIR/.env" ] &&
    current="$(grep -E '^ABS_MODEL_PATH=' "$INSTALL_DIR/.env" 2>/dev/null | cut -d= -f2- || true)"

  local i=1 d
  for d in "$MODEL_DIR"/*/; do
    [ -d "$d" ] || continue
    d="${d%/}"
    local mark="  "
    [ "$d" = "$current" ] && mark="${C_GREEN}->${C_RESET}"
    printf '  %s %2d) %-40s %s\n' "$mark" "$i" "$(basename "$d")" "$(du -sh "$d" 2>/dev/null | cut -f1)"
    i=$((i+1))
  done
  return 0
}

# Echoes the path of the model directory chosen by number, or empty.
pick_model() {
  local dirs=() d
  for d in "$MODEL_DIR"/*/; do [ -d "$d" ] && dirs+=("${d%/}"); done
  [ ${#dirs[@]} -gt 0 ] || return 1
  local n
  n="$(ask "Number" "")"
  [[ "$n" =~ ^[0-9]+$ ]] || return 1
  [ "$n" -ge 1 ] && [ "$n" -le ${#dirs[@]} ] || return 1
  printf '%s' "${dirs[$((n-1))]}"
}

download_model() {
  load_state
  MODEL_DIR="${MODEL_DIR:-$DEFAULT_MODEL_DIR}"
  mkdir -p "$MODEL_DIR"

  echo
  echo "Enter any Hugging Face repo id, for example:"
  echo "    Qwen/Qwen3-4B-AWQ            Qwen/Qwen2.5-7B-Instruct-AWQ"
  echo "    meta-llama/Llama-3.1-8B      mistralai/Mistral-7B-Instruct-v0.3"
  echo
  local repo dest
  repo="$(ask "Model repo id" "$DEFAULT_MODEL")"
  [ -n "$repo" ] || { warn "Cancelled."; return 1; }
  if [[ ! "$repo" =~ ^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$ ]]; then
    err "That does not look like a repo id (expected 'owner/name'): $repo"
    return 1
  fi

  dest="$MODEL_DIR/$(model_dirname "$repo")"
  if [ -d "$dest" ] && [ -n "$(ls -A "$dest" 2>/dev/null)" ]; then
    warn "Already present: $dest"
    confirm "Re-download (existing directory will be replaced)?" || return 0
    rm -rf "$dest"
  fi

  info "Downloading $repo -> $dest"
  fetch_model_weights "$repo" "$dest"

  ok "Model ready: $dest ($(du -sh "$dest" 2>/dev/null | cut -f1))"
  if [ -n "${INSTALL_DIR:-}" ] && [ -f "$INSTALL_DIR/.env" ]; then
    confirm "Make this the active model now?" && apply_model "$dest" "$repo"
  fi
}

# apply_model <dir> <repo-id-or-name> -- points .env and config.json at a model.
apply_model() {
  local dir="$1" name="${2:-}"
  [ -n "$name" ] || name="$(basename "$dir")"
  local served="$(model_dirname "$name")"

  sed -i "s|^ABS_MODEL_PATH=.*|ABS_MODEL_PATH=$dir|" "$INSTALL_DIR/.env"
  if [ -f "$INSTALL_DIR/config.json" ]; then
    sed -i "s|\"model_name\"[[:space:]]*:[[:space:]]*\"[^\"]*\"|\"model_name\": \"$served\"|" "$INSTALL_DIR/config.json"
  fi
  ok "Active model set to $served ($dir)"

  if container_running; then
    confirm "Restart the node so the change takes effect?" && restart_stack
  fi
}

switch_model() {
  require_install
  heading "Available models"
  list_models || { warn "Download one first."; return 1; }
  local d
  d="$(pick_model)" || { warn "Cancelled."; return 1; }
  apply_model "$d"
}

delete_model() {
  require_install
  heading "Available models"
  list_models || return 1
  local d
  d="$(pick_model)" || { warn "Cancelled."; return 1; }

  local current=""
  [ -f "$INSTALL_DIR/.env" ] &&
    current="$(grep -E '^ABS_MODEL_PATH=' "$INSTALL_DIR/.env" | cut -d= -f2- || true)"
  if [ "$d" = "$current" ]; then
    warn "That is the model currently in use. The node will not start until another is selected."
  fi

  echo "About to delete: $d ($(du -sh "$d" 2>/dev/null | cut -f1))"
  confirm "Delete permanently?" || { info "Cancelled."; return 0; }
  rm -rf "$d"
  ok "Deleted $d"
}

# ----------------------------------------------------------------------------
# install / uninstall
# ----------------------------------------------------------------------------

container_running() {
  [ -n "${INSTALL_DIR:-}" ] && [ -f "$INSTALL_DIR/docker-compose.yml" ] || return 1
  ( cd "$INSTALL_DIR" && compose ps --status running 2>/dev/null | grep -q yuanyi )
}

restart_stack() {
  ( cd "$INSTALL_DIR" && compose down >/dev/null 2>&1 || true; compose up -d )
  ok "Node restarted."
}

# docker-compose.yml unconditionally reserves an NVIDIA GPU. A relay-only node runs on
# machines that have none, where the container refuses to start with:
#   could not select device driver "nvidia" with capabilities: [[gpu]]
# Compose merges list values rather than replacing them, so an empty devices list does
# not clear the reservation -- the `!reset` tag (Compose 2.24+) is what actually drops it.
# The result is verified rather than assumed, because on older Compose `!reset` is a YAML
# error instead of a no-op.
write_relay_override() {
  local f="$INSTALL_DIR/docker-compose.override.yml"
  cat > "$f" <<'EOF'
# Written by install.sh for relay-only nodes: this node contributes network relaying and
# has no GPU, so the base file's GPU reservation must be dropped or the container will not
# start. Delete this file if you later add a GPU and switch to an inference node.
services:
  yuanyi-client:
    deploy: !reset null
EOF
  if ( cd "$INSTALL_DIR" && compose config 2>/dev/null | grep -q "driver: nvidia" ); then
    rm -f "$f"
    warn "This Docker Compose version does not support '!reset', so the GPU reservation could not be removed."
    warn "Relay-only nodes need Compose 2.24+, or you must delete the 'deploy:' block from docker-compose.yml by hand."
  else
    ok "Relay mode: GPU reservation removed via docker-compose.override.yml"
  fi
}

write_env() {
  cat > "$INSTALL_DIR/.env" <<EOF
# Generated by install.sh on $(date -Iseconds)
ABS_MODEL_PATH=$MODEL_PATH
SERVER_ADDRESS=$BOOTSTRAP
IFACE=$IFACE
CLIENT_WEB_PORT=$WEB_PORT
EOF
}

# Emitted without the "//" comments the shipped template carries; the loader strips
# those anyway, and comment-free output is far safer to rewrite with sed later.
write_config() {
  cat > "$INSTALL_DIR/config.json" <<EOF
{
  "version": "1.0",
  "web_port": $WEB_PORT,
  "proxy_port": $PROXY_PORT,
  "p2p": {
    "server_address": "$BOOTSTRAP",
    "server_addresses": [],
    "hub_api_port": 50008
  },
  "docker": {
    "container_name": "vllm_node",
    "image": "vllm-runtime-mooncake:latest",
    "network": "host",
    "shm_size": "16gb",
    "iface": "$IFACE"
  },
  "paths": {
    "config_path": "/app/config.json",
    "model_path": "/data/model",
    "mooncake_path": "/data/mooncake.json"
  },
  "vllm": {
    "model_name": "$MODEL_NAME",
    "max_model_len": 16384,
    "max_num_seqs": 32,
    "gpu_memory_utilization": $GPU_UTIL,
    "port": $VLLM_PORT,
    "tensor_parallel_size": 1,
    "dtype": "float16",
    "kv_role": "kv_both",
    "mooncake_bootstrap_port": $MOONCAKE_PORT,
    "mooncake_abort_request_timeout": 15,
    "attention_backend": "FLASH_ATTN",
    "placement_group_bundle_strategy": "SPREAD"
  },
  "server_mode": {
    "enabled": $HUB_ENABLED,
    "relay_only": $RELAY_ONLY,
    "p2p_port": $HUB_P2P_PORT,
    "proxy_port": $HUB_PROXY_PORT,
    "database_path": "./peers.db",
    "max_fail_count": 3,
    "check_interval_sec": 30,
    "cluster": {
      "prefill_nodes": 0,
      "decode_nodes": 0
    }
  }
}
EOF
}

# docker-compose.yml pins container_name, so only one node can run per host. If a container
# with that name already exists from a *different* install directory, `compose up` fails with
# Docker's raw "Conflict. The container name ... is already in use by container <hex id>",
# which names neither the other installation nor a way out -- and it surfaces only after the
# full image build, so the user waits several minutes to reach a dead end.
#
# Catch it before building and say where the other node lives and how to proceed.
preflight_container_name() {
  have docker || return 0

  local name
  name="$(sed -n 's/^[[:space:]]*container_name:[[:space:]]*//p' "$INSTALL_DIR/docker-compose.yml" 2>/dev/null | head -1)"
  [ -n "$name" ] || return 0

  local existing
  existing="$(docker ps -a --filter "name=^/${name}$" --format '{{.ID}}' 2>/dev/null | head -1)"
  [ -n "$existing" ] || return 0

  # Compose labels each container with the directory it was brought up from. If that is this
  # install, compose will simply recreate it -- expected on re-install, nothing to report.
  local owner
  owner="$(docker inspect "$existing" \
    --format '{{index .Config.Labels "com.docker.compose.project.working_dir"}}' 2>/dev/null)"
  [ "$owner" = "$INSTALL_DIR" ] && return 0

  err "A container named '$name' already exists on this host."
  if [ -n "$owner" ]; then
    echo "    It belongs to another installation at: $owner" >&2
  else
    echo "    It was not created by this installer (no compose project label)." >&2
  fi
  echo "    docker-compose.yml pins this name, so only one node can run per host." >&2
  echo >&2
  echo "    Either uninstall that node:   bash install.sh uninstall" >&2
  echo "    or remove the container:      docker rm -f $name" >&2
  die "Stopping before the build, which would fail on the name conflict."
}

do_install() {
  heading "Install Yuanyi Client Agent"
  check_prereqs || return 1

  load_state
  INSTALL_DIR="$(ask "Install directory" "${INSTALL_DIR:-$DEFAULT_INSTALL_DIR}")"
  INSTALL_DIR="${INSTALL_DIR/#\~/$HOME}"

  if [ -e "$INSTALL_DIR" ] && [ -n "$(ls -A "$INSTALL_DIR" 2>/dev/null)" ]; then
    if [ -f "$INSTALL_DIR/docker-compose.yml" ]; then
      warn "An installation already exists at $INSTALL_DIR"
      # swarm.key is genuinely preserved (setup_swarm_key checks for an existing valid key
      # and leaves it alone) -- config.json is NOT: write_config below always rewrites it.
      # Whether to actually overwrite is asked explicitly, later, right before that happens.
      confirm_default_yes "Update it in place (swarm.key is preserved; you'll be asked separately about config.json)?" || return 0
      ( cd "$INSTALL_DIR" && git pull --ff-only ) || warn "git pull failed; continuing with the existing checkout."
    else
      die "$INSTALL_DIR exists and is not empty, and is not an installation. Choose another path."
    fi
  else
    info "Cloning $REPO_URL"
    git clone --depth 1 "$REPO_URL" "$INSTALL_DIR"
  fi

  # Check for a name clash with another node as soon as docker-compose.yml exists, i.e.
  # before the model download. Checking any later means the user waits through a multi-GB
  # download only to be told the node cannot start.
  preflight_container_name

  # --- node role -----------------------------------------------------------
  echo
  echo "How should this node contribute?"
  echo "  1) Inference node  - runs a local GPU engine and serves requests (needs an NVIDIA GPU)"
  echo "  2) Relay only      - contributes network relaying, no GPU required"
  local role
  role="$(ask "Choice" "1")"
  if [ "$role" = "2" ]; then
    RELAY_ONLY=true; HUB_ENABLED=true
    info "Relay-only: no model is needed and no local engine will start."
  else
    RELAY_ONLY=false
    HUB_ENABLED=false
    confirm "Also run hub services (peer database, scoring, topology API)?" && HUB_ENABLED=true
  fi

  # --- swarm key -----------------------------------------------------------
  setup_swarm_key

  # --- network -------------------------------------------------------------
  echo
  BOOTSTRAP="$(ask "Bootstrap peer multiaddress (blank = start a new swarm)" "$DEFAULT_BOOTSTRAP")"
  local detected; detected="$(ip -o -4 route show to default 2>/dev/null | awk '{print $5}' | head -1)"
  IFACE="$(ask "Network interface" "${detected:-eth0}")"

  # --- ports ---------------------------------------------------------------
  echo
  if confirm_default_yes "Use default ports (web $DEFAULT_WEB_PORT, gateway $DEFAULT_PROXY_PORT, vLLM $DEFAULT_VLLM_PORT)?"; then
    WEB_PORT=$DEFAULT_WEB_PORT; PROXY_PORT=$DEFAULT_PROXY_PORT
    VLLM_PORT=$DEFAULT_VLLM_PORT; MOONCAKE_PORT=$DEFAULT_MOONCAKE_PORT
    HUB_P2P_PORT=$DEFAULT_HUB_P2P_PORT; HUB_PROXY_PORT=$DEFAULT_HUB_PROXY_PORT
  else
    WEB_PORT="$(ask_port "Web dashboard port" "$DEFAULT_WEB_PORT")"
    PROXY_PORT="$(ask_port "OpenAI gateway port" "$DEFAULT_PROXY_PORT")"
    VLLM_PORT="$(ask_port "vLLM engine port" "$DEFAULT_VLLM_PORT")"
    MOONCAKE_PORT="$(ask_port "Mooncake KV bootstrap port" "$DEFAULT_MOONCAKE_PORT")"
    HUB_P2P_PORT="$(ask_port "libp2p listen port" "$DEFAULT_HUB_P2P_PORT")"
    HUB_PROXY_PORT="$(ask_port "Hub dispatcher port" "$DEFAULT_HUB_PROXY_PORT")"
  fi

  # --- model ---------------------------------------------------------------
  MODEL_DIR="${MODEL_DIR:-$DEFAULT_MODEL_DIR}"
  MODEL_NAME="$DEFAULT_MODEL"
  MODEL_PATH="$MODEL_DIR/$(model_dirname "$DEFAULT_MODEL")"
  GPU_UTIL="0.9"

  if [ "$RELAY_ONLY" = "true" ]; then
    mkdir -p "$MODEL_PATH"   # keeps the compose bind mount valid without weights
    write_relay_override
  else
    echo
    MODEL_DIR="$(ask "Model storage directory" "$MODEL_DIR")"
    MODEL_DIR="${MODEL_DIR/#\~/$HOME}"
    mkdir -p "$MODEL_DIR"

    local repo
    repo="$(ask "Hugging Face model to use" "$DEFAULT_MODEL")"
    MODEL_NAME="$(model_dirname "$repo")"
    MODEL_PATH="$MODEL_DIR/$MODEL_NAME"
    GPU_UTIL="$(ask "GPU memory utilization (fraction of TOTAL VRAM)" "0.9")"

    if [ -d "$MODEL_PATH" ] && [ -n "$(ls -A "$MODEL_PATH" 2>/dev/null)" ]; then
      ok "Model already present: $MODEL_PATH"
    else
      info "Downloading $repo (this can take a while)"
      fetch_model_weights "$repo" "$MODEL_PATH"
      ok "Model ready ($(du -sh "$MODEL_PATH" 2>/dev/null | cut -f1))"
    fi
  fi

  # A fresh install has nothing to overwrite, so just write. An existing config.json/.env
  # pair only gets replaced if explicitly confirmed -- defaults to yes (matches this
  # script's previous behavior of always rewriting), but now it's an informed choice
  # instead of a silent side effect of "update in place" above.
  local write_cfg=1
  if [ -f "$INSTALL_DIR/config.json" ]; then
    if confirm_default_yes "Overwrite existing config.json and .env with the values just entered?"; then
      write_cfg=1
    else
      write_cfg=0
    fi
  fi
  if [ "$write_cfg" = "1" ]; then
    write_env
    write_config
  else
    ok "Kept existing config.json and .env."
  fi
  save_state

  # docker-compose bind-mounts identity.key/stats.json/peers.db as files that the app
  # creates itself on first run (loadOrGenerateIdentity, etc.) -- but if the host path
  # doesn't exist yet when the container is first created, Docker silently creates a
  # DIRECTORY there instead of a file. The app can then never write a real file at that
  # path, so identity.key in particular never persists: every container restart gets a
  # fresh random PeerID, breaking bootstrap/relay reservations that other peers hold for
  # the old identity. Pre-touch them as empty regular files so the bind mount attaches
  # to something the app can actually open for writing.
  touch "$INSTALL_DIR/identity.key" "$INSTALL_DIR/stats.json" "$INSTALL_DIR/peers.db"
  # The container runs as a non-root uid (dynamo, uid 1000) that does not own these
  # host-created files, so without group/other write bits the app's own write attempt
  # fails with a permission error -- silently, since loadOrGenerateIdentity() and the
  # stats/db writers don't check os.WriteFile's return value. World-writable is fine
  # here: these are per-install runtime state, not secrets shared across installs
  # (unlike swarm.key, which stays :ro and is never written by the app).
  chmod 666 "$INSTALL_DIR/identity.key" "$INSTALL_DIR/stats.json" "$INSTALL_DIR/peers.db"

  echo
  info "Building and starting (first build pulls several GB)"
  ( cd "$INSTALL_DIR" && compose up -d --build ) || die "Build/start failed. Check 'docker compose logs' in $INSTALL_DIR"

  heading "Installed"
  echo "  Directory : $INSTALL_DIR"
  echo "  Models    : $MODEL_DIR"
  echo "  Dashboard : http://localhost:$WEB_PORT"
  echo "  Gateway   : http://localhost:$PROXY_PORT/v1/chat/completions"
  [ "$RELAY_ONLY" = "true" ] && echo "  Role      : relay-only (no local inference)"
  echo
  echo "  Logs   : cd $INSTALL_DIR && docker compose logs -f"
  echo "  Manage : bash $0"
  echo
  warn "Model load takes ~1-2 minutes before the gateway answers."
}

do_uninstall() {
  heading "Uninstall"
  require_install

  echo "This will remove the installation at:"
  echo "    $INSTALL_DIR"
  echo
  confirm "Continue?" || { info "Cancelled."; return 0; }

  if [ -f "$INSTALL_DIR/docker-compose.yml" ]; then
    info "Stopping containers"
    ( cd "$INSTALL_DIR" && compose down -v ) || warn "Could not stop cleanly; continuing."
  fi

  # swarm.key is unrecoverable and shared by the whole swarm, so back it up unless
  # the operator explicitly says otherwise.
  if [ -f "$INSTALL_DIR/swarm.key" ]; then
    if confirm "Back up swarm.key before deleting?"; then
      local backup="$HOME/swarm.key.backup-$(date +%Y%m%d-%H%M%S)"
      install -m 600 "$INSTALL_DIR/swarm.key" "$backup"
      ok "Saved to $backup"
    else
      warn "swarm.key will be destroyed. Nodes sharing it cannot be rejoined without it."
    fi
  fi

  rm -rf "$INSTALL_DIR"
  ok "Removed $INSTALL_DIR"

  if [ -d "${MODEL_DIR:-}" ] && [ -n "$(ls -A "$MODEL_DIR" 2>/dev/null)" ]; then
    echo
    echo "Models are stored separately at $MODEL_DIR ($(du -sh "$MODEL_DIR" 2>/dev/null | cut -f1))"
    if confirm "Delete downloaded models too?"; then
      rm -rf "$MODEL_DIR"; ok "Removed $MODEL_DIR"
    else
      info "Kept $MODEL_DIR"
    fi
  fi

  rm -f "$STATE_FILE"
  echo
  ok "Uninstalled."
  echo "  Docker images were left in place. To reclaim that space: docker image prune -a"
}

do_status() {
  heading "Status"
  load_state
  if [ -z "${INSTALL_DIR:-}" ] || [ ! -f "$INSTALL_DIR/docker-compose.yml" ]; then
    echo "  Not installed."
    return 0
  fi
  echo "  Directory : $INSTALL_DIR"
  [ -f "$INSTALL_DIR/.env" ] && {
    echo "  Model     : $(grep -E '^ABS_MODEL_PATH=' "$INSTALL_DIR/.env" | cut -d= -f2-)"
    echo "  Web port  : $(grep -E '^CLIENT_WEB_PORT=' "$INSTALL_DIR/.env" | cut -d= -f2-)"
  }
  [ -f "$INSTALL_DIR/swarm.key" ] &&
    echo "  Key       : sha256 $(sha256sum "$INSTALL_DIR/swarm.key" | cut -c1-16)..."
  echo -n "  Container : "
  if container_running; then printf '%srunning%s\n' "$C_GREEN" "$C_RESET"; else printf '%sstopped%s\n' "$C_YELLOW" "$C_RESET"; fi

  local wp; wp="$(grep -E '^CLIENT_WEB_PORT=' "$INSTALL_DIR/.env" 2>/dev/null | cut -d= -f2- || echo "$DEFAULT_WEB_PORT")"
  if have curl && curl -fsS -m 3 "http://127.0.0.1:$wp/api/node_info" >/dev/null 2>&1; then
    echo "  Dashboard : responding on http://localhost:$wp"
  fi
}

# Lifecycle controls. Previously the only ways to affect a running node were install
# (which also starts it) and uninstall (which stops it by deleting everything), so there was
# no way to simply stop or restart one -- see the equivalent commands in install.ps1.
require_install() {
  load_state
  if [ -z "${INSTALL_DIR:-}" ] || [ ! -f "$INSTALL_DIR/docker-compose.yml" ]; then
    err "No installation found. Run 'bash $0 install' first."
    return 1
  fi
  return 0
}

do_start() {
  heading "Start"
  require_install || return 1
  if container_running; then
    warn "Already running."
    return 0
  fi
  ( cd "$INSTALL_DIR" && compose up -d ) || { err "Failed to start."; return 1; }
  ok "Started."
}

do_stop() {
  heading "Stop"
  require_install || return 1
  if ! container_running; then
    info "Not running."
    return 0
  fi
  ( cd "$INSTALL_DIR" && compose down ) || { err "Failed to stop."; return 1; }
  ok "Stopped."
}

do_restart() {
  heading "Restart"
  require_install || return 1
  do_stop || true
  do_start
}

models_menu() {
  while true; do
    heading "Models"
    load_state; MODEL_DIR="${MODEL_DIR:-$DEFAULT_MODEL_DIR}"
    list_models || true
    echo
    echo "  1) Download a model from Hugging Face"
    echo "  2) Switch active model"
    echo "  3) Delete a model"
    echo "  4) Back"
    case "$(ask "Choice" "4")" in
      1) download_model || true ;;
      2) switch_model   || true ;;
      3) delete_model   || true ;;
      *) return 0 ;;
    esac
  done
}

usage() {
  cat <<EOF
Yuanyi Client Agent -- installer and manager

  bash $0             interactive menu
  bash $0 install     install / update
  bash $0 uninstall   remove this installation
  bash $0 models      manage models
  bash $0 status      show current state
  bash $0 start       start the node
  bash $0 stop        stop the node
  bash $0 restart     stop, then start
  bash $0 --help      this message

Defaults: install ${DEFAULT_INSTALL_DIR}, models ${DEFAULT_MODEL_DIR},
dashboard ${DEFAULT_WEB_PORT}, gateway ${DEFAULT_PROXY_PORT}. All are prompted with
these as defaults, so pressing Enter accepts them.
EOF
}

main_menu() {
  while true; do
    heading "Yuanyi Client Agent"
    load_state
    if [ -n "${INSTALL_DIR:-}" ] && [ -f "$INSTALL_DIR/docker-compose.yml" ]; then
      echo "  Installed at $INSTALL_DIR"
    else
      echo "  Not installed."
    fi
    echo
    echo "  1) Install / update"
    echo "  2) Manage models (download / switch / delete)"
    echo "  3) Status"
    echo "  4) Start"
    echo "  5) Stop"
    echo "  6) Restart"
    echo "  7) Uninstall"
    echo "  8) Exit"
    case "$(ask "Choice" "8")" in
      1) do_install   || true ;;
      2) models_menu  || true ;;
      3) do_status    || true ;;
      4) do_start     || true ;;
      5) do_stop      || true ;;
      6) do_restart   || true ;;
      7) do_uninstall || true ;;
      *) exit 0 ;;
    esac
  done
}

case "${1:-}" in
  install)          do_install ;;
  uninstall)        do_uninstall ;;
  models)           models_menu ;;
  status)           do_status ;;
  start)            do_start ;;
  stop)             do_stop ;;
  restart)          do_restart ;;
  -h|--help|help)   usage ;;
  "")               main_menu ;;
  *)                err "Unknown command: $1"; echo; usage; exit 1 ;;
esac
