#!/usr/bin/env python3
import json
import sys


def require_string(data, key):
    value = data.get(key)
    if not isinstance(value, str) or not value.strip():
        raise SystemExit(f"missing required field: {key}")
    return value


def main():
    payload = json.load(sys.stdin)
    data = payload.get("with")
    if not isinstance(data, dict):
        raise SystemExit("missing required object: with")

    hostname = require_string(data, "hostname")
    listen = require_string(data, "listen")
    target = require_string(data, "target")

    json.dump(
        {
            "command": {
                "argv": [
                    "cloudflared",
                    "access",
                    "tcp",
                    "--hostname",
                    hostname,
                    "--destination",
                    target,
                    "--url",
                    listen,
                ]
            }
        },
        sys.stdout,
    )


if __name__ == "__main__":
    main()
