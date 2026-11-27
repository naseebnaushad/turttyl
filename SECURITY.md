# Security policy

turttyl lets AI agents type into real shells, and can optionally give them a root
shell, so security reports matter a lot to us.

## Reporting a vulnerability

Please **do not open a public issue**. Report it privately through GitHub:
**Security → Report a vulnerability** on
[github.com/naseebnaushad/turttyl](https://github.com/naseebnaushad/turttyl/security).

Include what you found, how to reproduce it, and what an attacker could do with it. You
should get a first reply within a few days. Once a fix is out, we are happy to credit
you unless you prefer not to be named.

## Supported versions

turttyl is in alpha. Only the latest commit on `main` gets fixes.

## What is in scope

Especially interesting:

- An agent creating, unlocking or using an **elevated (root) session** without the
  person's action, or reaching one while it is still being unlocked.
- An agent typing into, or reading, a **password prompt**.
- Another user on the machine reaching the control socket
  (`~/.turttyl/control.sock`).
- Passwords or a person's keystrokes ending up in the audit log or any other file.
- Theme or config files leading to script injection in the window.

## Known limits (by design)

- Any process running **as the same user** can talk to the control socket, just as it
  could run commands directly. With elevation turned on, that includes unlocked root
  sessions. The README warns about this; only enable elevation where that is
  acceptable.
- When you type your password for an agent's `sudo` in a normal session, sudo keeps it
  for that terminal for its usual timeout (often 5 minutes).
