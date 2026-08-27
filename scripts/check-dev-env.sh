#!/usr/bin/env bash
#
# check-dev-env.sh -- report what a binbash development machine has and lacks.
#
# Read-only: this script never installs, edits, or deletes anything. It prints
# a findings list and, where something is missing, the command that fixes it.
#
# Usage:
#   ./scripts/check-dev-env.sh          # checks, plus a real build and test run
#   ./scripts/check-dev-env.sh --quick  # skip the build/test (a few seconds)
#
# The script may live anywhere -- in scripts/, in the repo root, or off in
# /tmp -- and may be run from any directory, so long as either it or your
# shell is somewhere inside the checkout.

set -uo pipefail

quick=0
[ "${1:-}" = "--quick" ] && quick=1

# Walk up from $1 looking for the go.mod that declares this module. Assuming a
# fixed layout (say, that the script sits one level below the root) breaks the
# moment the file is copied somewhere else, and it breaks *quietly*: every
# check downstream then reports on the wrong directory and blames the machine.
find_root() {
  d=$(cd "$1" 2>/dev/null && pwd) || return 1
  while [ -n "$d" ] && [ "$d" != "/" ]; do
    if [ -f "$d/go.mod" ] && grep -q '^module github.com/thinkscotty/binbash$' "$d/go.mod" 2>/dev/null; then
      printf '%s\n' "$d"
      return 0
    fi
    d=$(dirname "$d")
  done
  return 1
}

# Try the script's own location first, then the working directory -- that
# covers a copy parked outside the checkout but run from inside it.
root=$(find_root "$(dirname "$0")") || root=$(find_root "$PWD") || {
  printf 'error: could not find the binbash checkout.\n' >&2
  printf 'Run this from inside the repository, or keep the script in it.\n' >&2
  exit 2
}
cd "$root" || exit 2

# ---------------------------------------------------------------- output ----

if [ -t 1 ]; then
  bold=$(printf '\033[1m'); red=$(printf '\033[31m')
  green=$(printf '\033[32m'); yellow=$(printf '\033[33m')
  dim=$(printf '\033[2m'); reset=$(printf '\033[0m')
else
  bold=""; red=""; green=""; yellow=""; dim=""; reset=""
fi

problems=0
advisories=0

section() { printf '\n%s%s%s\n' "$bold" "$1" "$reset"; }
ok()      { printf '  %s[ ok ]%s %s\n' "$green" "$reset" "$1"; }
warn()    { printf '  %s[warn]%s %s\n' "$yellow" "$reset" "$1"; advisories=$((advisories + 1)); }
fail()    { printf '  %s[FAIL]%s %s\n' "$red" "$reset" "$1"; problems=$((problems + 1)); }
fix()     { printf '         %s-> %s%s\n' "$dim" "$1" "$reset"; }
note()    { printf '         %s%s%s\n' "$dim" "$1" "$reset"; }

have() { command -v "$1" >/dev/null 2>&1; }

# Compare dotted versions without sort -V, which BSD/macOS sort lacks.
# Returns 0 when $1 >= $2.
version_ge() {
  awk -v a="$1" -v b="$2" '
    BEGIN {
      na = split(a, x, "."); nb = split(b, y, ".")
      n = (na > nb ? na : nb)
      for (i = 1; i <= n; i++) {
        xi = (i <= na ? x[i] + 0 : 0); yi = (i <= nb ? y[i] + 0 : 0)
        if (xi > yi) exit 0
        if (xi < yi) exit 1
      }
      exit 0
    }'
}

# Permission bits of a file, GNU stat or BSD stat.
perms_of() {
  stat -c '%a' "$1" 2>/dev/null || stat -f '%Lp' "$1" 2>/dev/null
}

# The package manager's install command for this machine, best effort.
install_hint() {
  if have dnf;     then echo "sudo dnf install -y $1"
  elif have apt;   then echo "sudo apt install -y $1"
  elif have pacman;then echo "sudo pacman -S $1"
  elif have brew;  then echo "brew install $1"
  else echo "install '$1' with your package manager"
  fi
}

printf '%sbinbash development environment check%s\n' "$bold" "$reset"
printf '%srepo: %s%s\n' "$dim" "$root" "$reset"
if have uname; then
  printf '%shost: %s %s (%s)%s\n' "$dim" "$(uname -s)" "$(uname -r)" "$(uname -m)" "$reset"
fi

# ------------------------------------------------------------- required ----

section "Required to build and run"

want_go=$(awk '/^go /{print $2; exit}' go.mod)
if have go; then
  got_go=$(go version | awk '{print $3}' | sed 's/^go//; s/-.*//')
  if version_ge "$got_go" "$want_go"; then
    ok "Go $got_go (go.mod asks for $want_go or newer)"
  else
    fail "Go $got_go is older than the $want_go that go.mod requires"
    fix "$(install_hint golang)   # or install from https://go.dev/dl/"
  fi
else
  fail "Go is not installed"
  fix "$(install_hint golang)"
fi

if have git; then
  ok "git $(git --version | awk '{print $3}')"
else
  fail "git is not installed"
  fix "$(install_hint git)"
fi

# binbash embeds a pure-Go SQLite (modernc.org/sqlite) and builds with
# CGO_ENABLED=0, so no C compiler and no libsqlite are needed. Said plainly
# here because it is the thing people most often go install by mistake.
note "no C compiler or system SQLite library needed -- the driver is pure Go"

# --------------------------------------------------------------- github ----

section "Git and GitHub"

name=$(git config --get user.name || true)
email=$(git config --get user.email || true)
if [ -n "$name" ] && [ -n "$email" ]; then
  ok "commit identity: $name <$email>"
else
  fail "git commit identity is not set -- commits will be refused"
  fix "git config --global user.name 'Your Name'"
  fix "git config --global user.email 'you@example.com'"
fi

remote=$(git remote get-url origin 2>/dev/null || true)
if [ -z "$remote" ]; then
  fail "no 'origin' remote configured"
  fix "git remote add origin git@github.com:thinkscotty/binbash.git"
else
  ok "origin: $remote"

  case "$remote" in
    git@github.com:*|ssh://git@github.com/*)
      # GitHub always closes the SSH session with exit 1; the greeting in the
      # output is what tells us the key was accepted.
      auth=$(ssh -o BatchMode=yes -o ConnectTimeout=8 -T git@github.com 2>&1 || true)
      case "$auth" in
        *"successfully authenticated"*)
          ok "SSH key is authorized on GitHub (${auth%%!*}!)" ;;
        *"Permission denied"*)
          fail "GitHub rejected this machine's SSH key -- pushes and pulls will fail"
          if [ -f "$HOME/.ssh/id_ed25519.pub" ]; then
            note "this machine's public key:"
            note "$(cat "$HOME/.ssh/id_ed25519.pub")"
          else
            fix "ssh-keygen -t ed25519 -C '$(git config --get user.email || echo you@example.com)'"
          fi
          fix "gh auth login   # offers to upload the key for you"
          fix "or paste the key at https://github.com/settings/keys" ;;
        *)
          warn "could not reach GitHub over SSH to check the key"
          note "$(printf '%s' "$auth" | head -1)" ;;
      esac ;;
    https://*)
      ok "HTTPS remote -- authentication goes through a credential helper or gh"
      have gh || fix "$(install_hint gh)   # then: gh auth login" ;;
  esac
fi

if have gh; then
  if gh auth status >/dev/null 2>&1; then
    ok "gh $(gh --version | awk 'NR==1{print $3}'), authenticated"
  else
    warn "gh is installed but not logged in"
    fix "gh auth login"
  fi
else
  warn "gh (GitHub CLI) is not installed -- optional, but it is the easiest way to authorize this machine and to watch release builds"
  fix "$(install_hint gh)"
fi

# macOS-only ssh_config keyword. Fedora's OpenSSH ignores it rather than
# erroring, so this is tidiness rather than breakage -- but on a stricter
# OpenSSH build it stops every ssh command dead.
if [ -f "$HOME/.ssh/config" ] && grep -qi '^[[:space:]]*UseKeychain' "$HOME/.ssh/config"; then
  if [ "$(uname -s)" != "Darwin" ]; then
    warn "~/.ssh/config carries 'UseKeychain', a macOS-only option, on a non-Mac host"
    fix "delete that line, or guard the block with: Match host * exec \"uname -s | grep -q Darwin\""
  fi
fi

# ------------------------------------------------------------- optional ----

section "Optional tooling"

if have sqlite3; then
  ok "sqlite3 CLI $(sqlite3 --version | awk '{print $1}') -- handy for inspecting data/binbash.db by hand"
else
  warn "sqlite3 CLI not installed (the app does not need it; you will want it to poke at the database)"
  fix "$(install_hint sqlite)"
fi

if have docker; then
  ok "docker $(docker --version | awk '{print $3}' | tr -d ,)"
elif have podman; then
  ok "podman $(podman --version | awk '{print $3}') -- builds the Dockerfile fine: podman build -t binbash ."
else
  warn "neither docker nor podman installed -- only needed to exercise the Dockerfile"
  fix "$(install_hint podman)"
fi

if have golangci-lint; then
  ok "golangci-lint $(golangci-lint --version | awk '{print $4}')"
else
  warn "golangci-lint not installed -- optional; 'go vet' covers the basics"
  fix "go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest"
fi

if have entr; then
  ok "entr -- rebuild-on-save: find . -name '*.go' | entr -r go run ."
else
  warn "entr not installed -- optional live-reload while editing"
  fix "$(install_hint entr)"
fi

for t in curl tar; do
  have "$t" && ok "$t" || { fail "$t is not installed"; fix "$(install_hint "$t")"; }
done

# --------------------------------------------------------- project state ----

section "Project state"

# A binary copied from another machine is the classic backup-restore souvenir:
# it looks runnable and is not. 'file' names the format; without it, fall back
# to the Mach-O magic bytes.
if [ -f binbash ]; then
  kind=""
  have file && kind=$(file -b binbash)
  case "$kind" in
    *ELF*x86-64*)     ok "./binbash is a native Linux x86-64 build" ;;
    *ELF*aarch64*)    ok "./binbash is a native Linux arm64 build" ;;
    *Mach-O*)         fail "./binbash is a macOS binary (${kind%%,*}) and cannot run here"
                      fix "go build -o binbash ." ;;
    *)                warn "./binbash is present but its format could not be identified"
                      fix "go build -o binbash .   # rebuild to be sure" ;;
  esac
else
  note "no ./binbash built yet -- 'go build -o binbash .' when you want one"
fi

# The config holds the shared password and any AI API key; the database holds
# the session-signing key. binbash tightens the database itself at startup and
# only warns about the config, so the config is the one worth flagging here.
for f in binbash.toml data/binbash.db; do
  [ -f "$f" ] || continue
  p=$(perms_of "$f")
  if [ -n "$p" ] && [ "$((8#$p & 8#077))" -ne 0 ]; then
    warn "$f is mode $p -- readable by other users on this machine"
    fix "chmod 600 $f"
  else
    ok "$f is private to you (mode ${p:-unknown})"
  fi
done

if [ -f data/binbash.db-wal ] || [ -f data/binbash.db-shm ]; then
  note "data/ carries -wal/-shm sidecars from the machine it was copied off; harmless, SQLite recovers them on open"
fi

if have go; then
  if go mod verify >/dev/null 2>&1; then
    ok "module dependencies are downloaded and verified"
  else
    warn "module cache is incomplete -- first build will need the network"
    fix "go mod download"
  fi
fi

# ------------------------------------------------------------ build/test ----

if [ "$quick" = "1" ]; then
  section "Build and tests"
  note "skipped (--quick)"
else
  section "Build and tests"

  # A spinner-ish progress line, but only on a terminal: carriage returns do
  # not erase anything in a piped log, they just leave the notice stranded in
  # front of the result.
  busy()  { [ -t 1 ] && printf '  %s...%s%s\r' "$dim" "$1" "$reset"; }
  clear_busy() { [ -t 1 ] && printf '\033[2K\r'; }

  busy building
  if out=$(go build -o /dev/null ./... 2>&1); then
    clear_busy; ok "go build ./... succeeds"
  else
    clear_busy; fail "go build ./... failed"
    printf '%s\n' "$out" | sed 's/^/         /' | head -20
  fi

  busy testing
  if out=$(go test ./... 2>&1); then
    clear_busy; ok "go test ./... passes"
  else
    clear_busy; fail "go test ./... failed"
    printf '%s\n' "$out" | grep -v '^ok\|no test files' | sed 's/^/         /' | head -20
  fi
fi

# --------------------------------------------------------------- summary ----

section "Summary"
if [ "$problems" -eq 0 ] && [ "$advisories" -eq 0 ]; then
  printf '  %sEverything checked out.%s\n\n' "$green" "$reset"
elif [ "$problems" -eq 0 ]; then
  printf '  %sNothing is broken.%s %d advisory item(s) above -- all optional.\n\n' \
    "$green" "$reset" "$advisories"
else
  printf '  %s%d item(s) need fixing%s, plus %d advisory item(s). See the -> lines above.\n\n' \
    "$red" "$problems" "$reset" "$advisories"
fi

exit $((problems > 0 ? 1 : 0))
