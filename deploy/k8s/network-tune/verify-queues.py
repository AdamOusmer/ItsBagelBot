#!/usr/bin/env python3
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
"""Read-only validation of the node1-3 eight-queue CPU plan; run on the host."""

import argparse
import json
from pathlib import Path
import subprocess


def output(*args):
    return subprocess.check_output(args, text=True)


def number(path):
    return int(path.read_text().strip().replace(",", ""), 16)


def verify_layout(root):
    dev = root.name
    channels = json.loads(output("ethtool", "--json", "-l", dev))[0]
    assert channels["combined"] == 8, (dev, channels)
    assert len(list((root / "queues").glob("rx-*"))) == 8, dev
    assert len(list((root / "queues").glob("tx-*"))) == 8, dev
    assert int((root / "tx_queue_len").read_text()) == 5000, dev


def device_irqs(virtio):
    irqs = {}
    for irq in Path("/sys/kernel/irq").iterdir():
        action = (irq / "actions").read_text().strip()
        if action.startswith(virtio + "-"):
            irqs[action] = irq.name
    return irqs


def verify_irq(irq, cpu, allow_pending):
    root = Path("/proc/irq") / irq
    expected = str(cpu)
    requested = (root / "smp_affinity_list").read_text().strip()
    assert requested == expected, (irq, "smp_affinity_list", requested)
    effective = (root / "effective_affinity_list").read_text().strip()
    if effective == expected:
        return False
    # Linux may defer effective migration until the next interrupt.
    # Allow this only during installation; recheck strictly after traffic.
    assert allow_pending, (irq, "effective_affinity_list", effective)
    return True


def verify_queue_masks(root, queue, mask):
    rx = root / "queues" / f"rx-{queue}"
    tx = root / "queues" / f"tx-{queue}"
    assert number(rx / "rps_cpus") == 0, (root.name, queue, "RPS")
    assert int((rx / "rps_flow_cnt").read_text()) == 0, (root.name, queue, "RFS")
    assert number(tx / "xps_cpus") == mask, (root.name, queue, "XPS")


def verify_steering(root, allow_pending):
    virtio = root.joinpath("device").resolve().name
    irqs = device_irqs(virtio)
    masks = [0x5, 0x8, 0x10, 0x20, 0x40, 0x80, 0x500, 0xA00]
    pending = []
    for queue, mask in enumerate(masks):
        for direction in ("input", "output"):
            irq = irqs[f"{virtio}-{direction}.{queue}"]
            if verify_irq(irq, queue + 2, allow_pending):
                pending.append(irq)
        verify_queue_masks(root, queue, mask)
    return pending


def verify_fq(dev):
    qdiscs = json.loads(output("tc", "-j", "qdisc", "show", "dev", dev))
    mq = next(q for q in qdiscs if q["kind"] == "mq" and q.get("root"))
    expected = {mq["handle"] + format(i, "x") for i in range(1, 9)}
    assert {q.get("parent") for q in qdiscs if q["kind"] == "fq"} == expected, dev


def verify_offloads(dev):
    features = dict(line.strip().split(": ", 1) for line in output("ethtool", "-k", dev).splitlines()[1:] if ": " in line)
    for name in ("generic-receive-offload", "generic-segmentation-offload", "tcp-segmentation-offload", "rx-udp-gro-forwarding"):
        assert features[name].split()[0] == "on", (dev, name, features[name])
    assert features["rx-gro-list"].split()[0] == "off", dev


def verify_saved_profile(dev):
    uuid = output("nmcli", "-g", "GENERAL.CON-UUID", "device", "show", dev).strip()
    saved = output("nmcli", "-g", "ethtool.channels-combined,ethtool.ring-rx,ethtool.ring-tx,ethtool.feature-rx-udp-gro-forwarding,ethtool.feature-rx-gro-list,link.tx-queue-length", "connection", "show", uuid).splitlines()
    assert saved == ["8", "1024", "256", "on", "off", "5000"], (dev, "saved profile", saved)


def verify_device(dev, allow_pending_irq=False):
    root = Path("/sys/class/net") / dev
    verify_layout(root)
    pending = verify_steering(root, allow_pending_irq)
    verify_fq(dev)
    verify_offloads(dev)
    verify_saved_profile(dev)
    return {"interface": dev, "queues": 8, "irq_cpus": "2-9", "rps": False, "fq_leaves": 8, "udp_gro_forwarding": True, "pending_irqs": pending}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--allow-pending-irq", action="store_true", help="installation only: report deferred IRQ migration; rerun without this flag after traffic")
    args = parser.parse_args()
    results = [verify_device(dev, args.allow_pending_irq) for dev in ("eth0", "eth1")]
    for unit in ("fleet-irq-affinity",):
        assert output("systemctl", "is-enabled", unit).strip() == "enabled", unit
        assert output("systemctl", "is-active", unit).strip() == "active", unit
    for unit in ("bagelbot-network-performance", "fleet-eth1-tuning"):
        legacy = subprocess.run(["systemctl", "is-enabled", unit], text=True, capture_output=True)
        assert legacy.stdout.strip() == "disabled", (unit, legacy.stdout)
    print(json.dumps({"passed": True, "devices": results}))
