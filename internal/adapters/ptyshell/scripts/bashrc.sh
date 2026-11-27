# turttyl shell integration for bash (generated; do not edit).
# Loads the usual login files, then reports OSC 133 markers so agents know
# where a command's output starts (C) and when it finished, with its exit code (D).
# Works with bash 3.2 (macOS), so it uses a DEBUG trap instead of PS0.
[ -r /etc/profile ] && . /etc/profile
if [ -r "$HOME/.bash_profile" ]; then . "$HOME/.bash_profile"
elif [ -r "$HOME/.bash_login" ]; then . "$HOME/.bash_login"
elif [ -r "$HOME/.profile" ]; then . "$HOME/.profile"
fi
[ -r "$HOME/.bash_profile" ] || { [ -r "$HOME/.bashrc" ] && . "$HOME/.bashrc"; }

__at_ran=
__at_armed=
__at_prompt() {
  local ec=$?
  __at_armed=
  if [ -n "$__at_ran" ]; then printf '\033]133;D;%s\007' "$ec"; fi
  __at_ran=
  printf '\033]133;A\007'
  return $ec
}
# Runs last in PROMPT_COMMAND: from here on, the next command is the user's.
__at_arm() { __at_armed=1; }
__at_debug() {
  [ -n "$__at_armed" ] || return 0
  [ -n "$COMP_LINE" ] && return 0
  case "$BASH_COMMAND" in __at_*) return 0 ;; esac
  __at_armed=
  __at_ran=1
  printf '\033]133;C\007'
}
PROMPT_COMMAND="__at_prompt${PROMPT_COMMAND:+;$PROMPT_COMMAND};__at_arm"
trap '__at_debug' DEBUG
