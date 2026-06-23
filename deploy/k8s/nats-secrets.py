#!/usr/bin/env python3
"""Generate and load the NATS per-account credentials into Doppler.

This is the single source of truth for rotating NATS auth. NATS stores bcrypt
hashes while services use the matching plaintext, and Doppler cannot compute
bcrypt — so plaintext and hash must be generated together. Re-running this script
IS a rotation: it regenerates every password, writes the plaintext to each
service's Doppler project and the bcrypt hashes + leaf-link URLs to the `nats`
Doppler project (which the operator syncs into the `nats-auth-env` secret the
broker reads).

Endpoints (NATS_URL/RPC_URL/LEAF_URL/HUB_URL, ingress *_HOST) live in the k8s
manifests, not here — this only touches credentials.

Usage:
    python3 deploy/k8s/nats-secrets.py --dry-run   # show what would change
    python3 deploy/k8s/nats-secrets.py             # generate + write to Doppler

After a real run the broker hashes change, so the nats + nats-leaf pods must be
restarted to re-read nats-auth-env (env-injected; the conf file hot-reloads but
env does not). The Doppler operator restarts the app services automatically.
"""
import secrets
import subprocess
import sys

import bcrypt

DRY = "--dry-run" in sys.argv
CONFIG = "prd"

# Leaf link target (hub leafnode port) embedded in the leaf remote URLs.
HUB_LEAFNODE = "nats.production.svc.cluster.local:7422"

# service name (account stem) -> Doppler project
SERVICES = {
    "users": "users",
    "commands": "commands",
    "modules": "modules",
    "projector": "projector",
    "outgress": "outgress",
    "worker": "worker",
    "twitch_ingress": "twitch-ingress",
    "dashboard": "dashboard",
    "admin": "admin",
    "transactions": "transactions",  # BUS only, no RPC account
}
NO_RPC = {"transactions"}

# One leaf link per account: the BUS account plus every *_RPC account.
LEAF_ACCOUNTS = [
    "bus", "users", "commands", "modules", "projector",
    "outgress", "worker", "dashboard", "admin", "twitch_ingress",
]


def gen() -> str:
    # URL-safe (hex) so the plaintext is valid inside the leaf nats-leaf:// URLs.
    return secrets.token_hex(24)


def bcrypt_hash(pw: str) -> str:
    # cost 11, $2a prefix — the form the NATS Go server accepts.
    return bcrypt.hashpw(pw.encode(), bcrypt.gensalt(11, prefix=b"2a")).decode()


def doppler_set(project: str, kv: dict[str, str]) -> None:
    keys = ", ".join(sorted(kv))
    if DRY:
        print(f"[dry-run] doppler -p {project} -c {CONFIG} set: {keys}")
        return
    args = ["doppler", "secrets", "set", "-p", project, "-c", CONFIG, "--no-interactive", "--silent"]
    args += [f"{k}={v}" for k, v in kv.items()]
    subprocess.run(args, check=True)
    print(f"  wrote {len(kv)} keys to {project}/{CONFIG}: {keys}")


def main() -> None:
    broker: dict[str, str] = {}  # nats project -> nats-auth-env

    print("== per-service credentials ==")
    for svc, project in SERVICES.items():
        kv: dict[str, str] = {}
        bus_pw = gen()
        kv["NATS_USER"] = f"{svc}_bus"
        kv["NATS_PASSWORD"] = bus_pw
        broker[f"NATS_BCRYPT_{svc.upper()}_BUS"] = bcrypt_hash(bus_pw)
        if svc not in NO_RPC:
            rpc_pw = gen()
            kv["NATS_RPC_USER"] = f"{svc}_rpc"
            kv["NATS_RPC_PASSWORD"] = rpc_pw
            broker[f"NATS_BCRYPT_{svc.upper()}_RPC"] = bcrypt_hash(rpc_pw)
        doppler_set(project, kv)

    # System account (server monitoring; no fleet service uses it).
    broker["NATS_BCRYPT_SYS"] = bcrypt_hash(gen())

    # Leaf links: one hash (hub-side authorization) + one remote URL (leaf-side,
    # embeds the plaintext) per account.
    for acct in LEAF_ACCOUNTS:
        leaf_pw = gen()
        broker[f"NATS_BCRYPT_LEAF_{acct.upper()}"] = bcrypt_hash(leaf_pw)
        broker[f"NATS_LEAF_REMOTE_URL_{acct.upper()}"] = (
            f"nats-leaf://leaf_{acct}:{leaf_pw}@{HUB_LEAFNODE}"
        )

    print("== broker hashes (nats-auth-env via the 'nats' Doppler project) ==")
    doppler_set("nats", broker)

    print("\ndone." if not DRY else "\ndry-run complete (no writes).")


if __name__ == "__main__":
    main()
