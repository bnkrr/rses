#!/usr/bin/env python3
import json
import shlex
import sys


DEFAULT_OPTIONS = [
    "-o",
    "ExitOnForwardFailure=yes",
    "-o",
    "ServerAliveInterval=15",
    "-o",
    "ServerAliveCountMax=3",
    "-o",
    "TCPKeepAlive=yes",
    "-o",
    "BatchMode=yes",
]


def require_string(data, key):
    value = data.get(key)
    if not isinstance(value, str) or not value.strip():
        raise SystemExit(f"missing required field: {key}")
    return value


def optional_string_list(data, key):
    value = data.get(key, [])
    if value is None:
        return []
    if not isinstance(value, list) or not all(
        isinstance(item, str) and item for item in value
    ):
        raise SystemExit(f"{key} must be a list of non-empty strings")
    return value


def shell_join(argv):
    return " ".join(shlex.quote(item) for item in argv)


def reconnect_script(ssh_argv):
    cmd = shell_join(ssh_argv)
    return f"""delay=2
while true; do
  started=$(date +%s)
  echo "starting ssh tunnel: {cmd}"
  {cmd} 2>&1
  code=$?
  now=$(date +%s)
  ran=$((now - started))
  if [ "$ran" -ge 60 ]; then
    delay=2
  else
    case "$delay" in
      2) delay=5 ;;
      5) delay=10 ;;
      *) delay=30 ;;
    esac
  fi
  echo "ssh tunnel exited with $code after ${{ran}}s; reconnecting in ${{delay}}s"
  sleep "$delay"
done
"""


def main():
    payload = json.load(sys.stdin)
    data = payload.get("with")
    if not isinstance(data, dict):
        raise SystemExit("missing required object: with")

    host = require_string(data, "host")
    mode = require_string(data, "mode")
    listen = require_string(data, "listen")
    target = data.get("target")
    ssh_args = optional_string_list(data, "ssh_args")

    argv = ["ssh", "-N", *ssh_args, *DEFAULT_OPTIONS]
    if mode == "local":
        if not isinstance(target, str) or not target.strip():
            raise SystemExit("missing required field: target")
        argv.extend(["-L", f"{listen}:{target}"])
    elif mode == "local-socket":
        if not isinstance(target, str) or not target.strip():
            raise SystemExit("missing required field: target")
        if not target.startswith("/"):
            raise SystemExit("target must be an absolute remote socket path")
        argv.extend(["-L", f"{listen}:{target}"])
    elif mode == "remote":
        if not isinstance(target, str) or not target.strip():
            raise SystemExit("missing required field: target")
        argv.extend(["-R", f"{listen}:{target}"])
    elif mode == "remote-socket":
        if not listen.startswith("/"):
            raise SystemExit("listen must be an absolute remote socket path")
        if not isinstance(target, str) or not target.strip():
            raise SystemExit("missing required field: target")
        argv.extend(["-R", f"{listen}:{target}"])
    elif mode == "socks":
        argv.extend(["-D", listen])
    else:
        raise SystemExit(
            "mode must be one of: local, local-socket, remote, remote-socket, socks"
        )
    argv.append(host)

    json.dump(
        {
            "command": {
                "argv": ["sh", "-c", reconnect_script(argv)],
            }
        },
        sys.stdout,
    )


if __name__ == "__main__":
    main()
