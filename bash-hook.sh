#!/usr/bin/env bash
# Source this file in a bash shell to install the pre-exec guardaian hook.
# Usage:
#   source ./bash-hook.sh
#   guardaian-on
#
# The shell prompt is tagged with "(guardaian)" while active and restored when disabled.

if [[ -n "${GUARDAIAN_ACTIVE:-}" ]]; then
  export GUARDAIAN_ORIGINAL_PS1="${GUARDAIAN_ORIGINAL_PS1:-${PS1:-}}"
  __guardaian_prompt
  return 0
fi

export GUARDAIAN_ORIGINAL_PS1="${GUARDAIAN_ORIGINAL_PS1:-${PS1:-}}"

__guardaian_prompt() {
  if [[ -n "${GUARDAIAN_ACTIVE:-}" ]]; then
    PS1="${GUARDAIAN_ORIGINAL_PS1} (guardaian) "
  else
    PS1="${GUARDAIAN_ORIGINAL_PS1}"
  fi
}

__guardaian_last_command() {
  fc -ln -1 2>/dev/null | sed '1s/^[[:space:]]*//; $s/[[:space:]]*$//'
}

__guardaian_before_exec() {
  local cmd
  cmd="$(__guardaian_last_command)"

  case "$cmd" in
    ""|*"guardaian-on"*|*"guardaian-off"*|*"__guardaian_"*|*"GUARDAIAN_"*|history\ *|fc\ *|source\ *|source\ .*|export\ *|unset\ *|trap\ *|PS1=*|printf\ *|echo\ *|read\ *|cd\ *|pwd|alias\ *|unalias\ *|type\ *|command\ *|builtin\ *|eval\ *|exec\ *|return\ *|exit\ *|guard\ *|./guard\ *|/home/robert/git/guardaian/guard\ *)
      return 0
      ;;
  esac

  if [[ -n "${GUARDAIAN_ACTIVE:-}" ]]; then
    trap - DEBUG
    if ! /home/robert/git/guardaian/guard --check-only -- "$cmd"; then
      trap '__guardaian_before_exec' DEBUG
      printf '\nCommand blocked by guardaian.\n' >&2
      return 1
    fi
    trap '__guardaian_before_exec' DEBUG
  fi

  return 0
}

__guardaian_disable() {
  unset GUARDAIAN_ACTIVE
  PS1="${GUARDAIAN_ORIGINAL_PS1}"
  trap - DEBUG
}

# Expose a simple toggle for users.
#   guardaian-on    -> enable prompt tag and pre-exec hook
#   guardaian-off   -> restore prompt and disable hook

guardaian-off() {
  __guardaian_disable
}

guardaian-on() {
  if [[ -n "${GUARDAIAN_ACTIVE:-}" ]]; then
    __guardaian_prompt
    return 0
  fi

  export GUARDAIAN_ACTIVE=1
  export GUARDAIAN_ORIGINAL_PS1="${GUARDAIAN_ORIGINAL_PS1:-${PS1:-}}"
  trap '__guardaian_before_exec' DEBUG
  __guardaian_prompt
}
