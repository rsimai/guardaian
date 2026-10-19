#!/usr/bin/env bash
# Source this file in an interactive bash shell to install a Return-key hook.
# Usage:
#   source ./bash-hook.sh
#   guardaian-on
#
# The shell prompt is tagged with "(guardaian)" while active and restored when disabled.

export GUARDAIAN_ORIGINAL_PS1="${GUARDAIAN_ORIGINAL_PS1:-${PS1:-}}"

__guardaian_debug() {
  [[ -n "${GUARDAIAN_DEBUG:-}" ]] || return 0
  printf '[guardaian][debug] %s\n' "$*" >&2
}

__guardaian_enable_prompt_wrapper() {
  __guardaian_debug "enable_prompt_wrapper: before PROMPT_COMMAND='${PROMPT_COMMAND-}'"
  if [[ "${PROMPT_COMMAND:-}" != "__guardaian_prompt_command" ]]; then
    export GUARDAIAN_ORIGINAL_PROMPT_COMMAND="${PROMPT_COMMAND:-}"
  fi
  PROMPT_COMMAND='__guardaian_prompt_command'
  __guardaian_debug "enable_prompt_wrapper: after PROMPT_COMMAND='${PROMPT_COMMAND-}'"
}

__guardaian_prompt() {
  if [[ -n "${GUARDAIAN_ACTIVE:-}" && -n "${GUARDAIAN_PENDING_CMD:-}" ]]; then
    PS1="confirm yes> "
    return
  fi
  if [[ -n "${GUARDAIAN_ACTIVE:-}" ]]; then
    PS1="${GUARDAIAN_ORIGINAL_PS1} (guardaian) "
    return
  fi
  PS1="${GUARDAIAN_ORIGINAL_PS1}"
}

__guardaian_prompt_command() {
  __guardaian_debug "prompt_command: active='${GUARDAIAN_ACTIVE-}' pending='${GUARDAIAN_PENDING_CMD-}' PS1='${PS1-}'"
  if [[ -n "${GUARDAIAN_ACTIVE:-}" && -n "${GUARDAIAN_PENDING_CMD:-}" ]]; then
    PS1=""
    printf 'confirm yes> '
    __guardaian_debug "prompt_command: pending prompt printed"
    return
  fi

  if [[ -n "${GUARDAIAN_ORIGINAL_PROMPT_COMMAND:-}" ]]; then
    eval "${GUARDAIAN_ORIGINAL_PROMPT_COMMAND}"
  fi

  __guardaian_prompt
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
  __guardaian_debug "accept_line: raw='${cmd}' pending='${GUARDAIAN_PENDING_CMD-}'"

  READLINE_LINE=""
  READLINE_POINT=0

  # Emulate normal Enter behavior: move to a new output line first.
  printf '\n'

  if [[ -n "${GUARDAIAN_PENDING_CMD:-}" ]]; then
    __guardaian_debug "accept_line: handling pending response='${cmd}'"
    if [[ "${cmd}" == "yes" ]]; then
      history -s -- "$GUARDAIAN_PENDING_CMD"
      eval -- "$GUARDAIAN_PENDING_CMD"
      __guardaian_debug "accept_line: pending command executed"
    else
      printf 'Execution blocked.\n' >&2
      __guardaian_debug "accept_line: pending command blocked"
    fi
    unset GUARDAIAN_PENDING_CMD
    if [[ -n "${GUARDAIAN_SAVED_PS1:-}" ]]; then
      PS1="${GUARDAIAN_SAVED_PS1}"
      unset GUARDAIAN_SAVED_PS1
    fi
    if [[ -n "${GUARDAIAN_SAVED_PROMPT_COMMAND+set}" ]]; then
      PROMPT_COMMAND="${GUARDAIAN_SAVED_PROMPT_COMMAND}"
      unset GUARDAIAN_SAVED_PROMPT_COMMAND
    fi
    __guardaian_debug "accept_line: pending exit PS1='${PS1-}' PROMPT_COMMAND='${PROMPT_COMMAND-}'"
    __guardaian_prompt
    return 0
  fi

  if [[ -z "${cmd//[[:space:]]/}" ]]; then
    return 0
  fi

  history -s -- "$cmd"

  if __guardaian_is_internal_command "$cmd"; then
    __guardaian_debug "accept_line: internal command='${cmd}'"
    eval -- "$cmd"
    __guardaian_prompt
    return 0
  fi

  printf 'Evaluating: %s\n' "$cmd"


  /home/robert/git/guardaian/guard --check-only -- "$cmd"
  status=$?
  __guardaian_debug "accept_line: guard status=${status}"

  if [[ $status -eq 20 ]]; then
    export GUARDAIAN_PENDING_CMD="$cmd"
    printf 'Type yes and press Enter to continue, or type anything else to cancel.\n'
    printf 'confirm yes> (waiting for your response on the next line)\n'
    __guardaian_debug "accept_line: entered pending mode"
    return 0
  elif [[ $status -ne 0 ]]; then
    printf 'Command blocked by guardaian.\n' >&2
    __guardaian_debug "accept_line: command blocked by guard status=${status}"
    return 0
  fi

  eval -- "$cmd"
  __guardaian_debug "accept_line: command executed immediately"
}

__guardaian_install_bindings() {
  [[ $- == *i* ]] || return 0
  bind -x '"\C-m":__guardaian_accept_line'
  bind -x '"\eOM":__guardaian_accept_line'
  __guardaian_debug "install_bindings: done"
}

__guardaian_uninstall_bindings() {
  [[ $- == *i* ]] || return 0
  bind '"\C-m":accept-line'
  bind '"\eOM":accept-line'
  __guardaian_debug "uninstall_bindings: done"
}

__guardaian_disable() {
  unset GUARDAIAN_ACTIVE
  PS1="${GUARDAIAN_ORIGINAL_PS1}"
  __guardaian_uninstall_bindings
  __guardaian_debug "disabled: PS1='${PS1-}' PROMPT_COMMAND='${PROMPT_COMMAND-}'"
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
    __guardaian_debug "on: already active"
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
  __guardaian_debug "on: activated PS1='${PS1-}' PROMPT_COMMAND='${PROMPT_COMMAND-}'"
  printf '[guardaian] enabled\n'
}

# If the hook is re-sourced while active, refresh bindings and prompt.
if [[ -n "${GUARDAIAN_ACTIVE:-}" ]]; then
  __guardaian_enable_prompt_wrapper
  __guardaian_install_bindings
  __guardaian_prompt
fi
