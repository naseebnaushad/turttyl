# Report OSC 133 markers so agents know where a command's output starts (C) and
# when it finished, with its exit code (D). Our hooks run first so precmd sees
# the command's real exit code.
__at_ran=
__at_precmd() {
  local ec=$?
  if [[ -n $__at_ran ]]; then printf '\e]133;D;%s\a' "$ec"; fi
  __at_ran=
  printf '\e]133;A\a'
  return $ec
}
__at_preexec() { __at_ran=1; printf '\e]133;C\a' }
precmd_functions=(__at_precmd ${precmd_functions[@]})
preexec_functions=(__at_preexec ${preexec_functions[@]})
