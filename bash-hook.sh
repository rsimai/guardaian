#!/usr/bin/env bash
# Source this file in an interactive bash shell to install a Return-key hook.
# Usage:
#   source ./bash-hook.sh
#   guardaian-on
#
# First Enter: evaluate command risk. Safe commands execute automatically.
# Risky commands enter pending state and require a second-step confirmation.

export GUARDAIAN_ORIGINAL_PS1="${GUARDAIAN_ORIGINAL_PS1:-${PS1:-}}"
export GUARDAIAN_ORIGINAL_PROMPT_COMMAND="${GUARDAIAN_ORIGINAL_PROMPT_COMMAND:-${PROMPT_COMMAND:-}}"
export GUARDAIAN_ROOT="${GUARDAIAN_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)}"
export GUARDAIAN_BIN="${GUARDAIAN_BIN:-${GUARDAIAN_ROOT}/guard}"

__guardaian_prompt() {
  if [[ -n "${GUARDAIAN_ACTIVE:-}" && -n "${GUARDAIAN_PENDING_CMD:-}" ]]; then
    PS1="${GUARDAIAN_ORIGINAL_PS1} (guardaian:confirm) "
    return
  fi
  if [[ -n "${GUARDAIAN_ACTIVE:-}" ]]; then
    PS1="${GUARDAIAN_ORIGINAL_PS1} (guardaian) "
    return
  fi
  PS1="${GUARDAIAN_ORIGINAL_PS1}"
}

__guardaian_prompt_command() {
  if [[ -n "${GUARDAIAN_ORIGINAL_PROMPT_COMMAND:-}" ]]; then
    eval "${GUARDAIAN_ORIGINAL_PROMPT_COMMAND}"
  fi

  __guardaian_prompt
}

__guardaian_enable_prompt_wrapper() {
  if [[ "${PROMPT_COMMAND:-}" != "__guardaian_prompt_command" ]]; then
    export GUARDAIAN_ORIGINAL_PROMPT_COMMAND="${PROMPT_COMMAND:-}"
  fi
  PROMPT_COMMAND='__guardaian_prompt_command'
}

__guardaian_is_internal_command() {
  local cmd
  cmd="$1"

  case "$cmd" in
    guardaian-on|guardaian-off|guardaian-on\ *|guardaian-off\ *|source\ ./bash-hook.sh|source\ ./guard-shell)
      return 0
      ;;
  esac
  return 1
}

__guardaian_accept_line() {
  local cmd status
  cmd="${READLINE_LINE}"

  READLINE_LINE=""
  READLINE_POINT=0

  # Emulate normal Enter behavior: move to a new output line first.
  printf '\n'

  # Second step: consume user response for a pending risky command.
  if [[ -n "${GUARDAIAN_PENDING_CMD:-}" ]]; then
    if [[ "${cmd}" == "yes" ]]; then
      history -s -- "$GUARDAIAN_PENDING_CMD"
      eval -- "$GUARDAIAN_PENDING_CMD"
    else
      printf 'Execution blocked.\n' >&2
    fi
    unset GUARDAIAN_PENDING_CMD
    __guardaian_prompt
    return 0
  fi

  if [[ -z "${cmd//[[:space:]]/}" ]]; then
    return 0
  fi

  history -s -- "$cmd"

  if __guardaian_is_internal_command "$cmd"; then
    eval -- "$cmd"
    __guardaian_prompt
    return 0
  fi

  printf 'Evaluating: %s\n' "$cmd"

  # First step: evaluate risk only.
  "${GUARDAIAN_BIN}" --check-only -- "$cmd"
  status=$?

  if [[ $status -eq 20 ]]; then
    export GUARDAIAN_PENDING_CMD="$cmd"
    printf 'Type yes and press Enter to continue, or type anything else to cancel.\n'
    return 0
  elif [[ $status -ne 0 ]]; then
    printf 'Command blocked by guardaian.\n' >&2
    return 0
  fi

  # Safe command: execute immediately on first Enter.
  eval -- "$cmd"
}

__guardaian_install_bindings() {
  [[ $- == *i* ]] || return 0
  bind -x '"\C-m":__guardaian_accept_line'
  bind -x '"\eOM":__guardaian_accept_line'
}

__guardaian_uninstall_bindings() {
  [[ $- == *i* ]] || return 0
  bind '"\C-m":accept-line'
  bind '"\eOM":accept-line'
}

__guardaian_disable() {
  unset GUARDAIAN_ACTIVE
  unset GUARDAIAN_PENDING_CMD
  PS1="${GUARDAIAN_ORIGINAL_PS1}"
  PROMPT_COMMAND="${GUARDAIAN_ORIGINAL_PROMPT_COMMAND:-}"
  __guardaian_uninstall_bindings
  printf '[guardaian] disabled\n'
}

# Expose a simple toggle for users.
#   guardaian-on    -> enable prompt tag and Return-key guard hook
#   guardaian-off   -> restore prompt and disable hook

guardaian-off() {
  __guardaian_disable
}

guardaian-on() {
  if [[ -n "${GUARDAIAN_ACTIVE:-}" ]]; then
    __guardaian_enable_prompt_wrapper
    __guardaian_install_bindings
    __guardaian_prompt
    printf '[guardaian] enabled\n'
    return 0
  fi

  export GUARDAIAN_ACTIVE=1
  export GUARDAIAN_ORIGINAL_PS1="${GUARDAIAN_ORIGINAL_PS1:-${PS1:-}}"
  __guardaian_enable_prompt_wrapper
  __guardaian_install_bindings
  __guardaian_prompt
  printf '[guardaian] enabled\n'
}

# If the hook is re-sourced while active, refresh bindings and prompt.
if [[ -n "${GUARDAIAN_ACTIVE:-}" ]]; then
  __guardaian_enable_prompt_wrapper
  __guardaian_install_bindings
  __guardaian_prompt
fi
