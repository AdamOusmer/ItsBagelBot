#!/usr/bin/env python3
"""Verify bidirectional UDP reachability for K3s wireguard-native endpoints."""

import argparse
import json
import socket


def listen(bind: str, port: int, expected: int, timeout: float) -> None:
    tokens = set()
    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
        sock.bind((bind, port))
        sock.settimeout(timeout)
        while len(tokens) < expected:
            payload, _ = sock.recvfrom(512)
            tokens.add(payload.decode("utf-8"))
    print(json.dumps({"tokens": sorted(tokens)}))


def send(endpoint: str, port: int, token: str) -> None:
    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
        sock.sendto(token.encode("utf-8"), (endpoint, port))


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("mode", choices=("listen", "send"))
    parser.add_argument("--bind", default="127.0.0.1")
    parser.add_argument("--endpoint", default="")
    parser.add_argument("--port", type=int, default=51820)
    parser.add_argument("--token", default="")
    parser.add_argument("--expected", type=int, default=1)
    parser.add_argument("--timeout", type=float, default=15.0)
    args = parser.parse_args()
    if args.mode == "listen":
        listen(args.bind, args.port, args.expected, args.timeout)
    else:
        send(args.endpoint, args.port, args.token)


if __name__ == "__main__":
    main()
