---
title: "0004 - Adoption of Oracle Cloud"
description: "Architecture decision record: Adoption of Oracle Cloud as the primary host, 
with a DigitalOcean droplet as a second failure domain"
---

**Date:** 2026-05-23

## Status

Accepted

## Context

The rewrite to microservices, the move to Go, and the choice of NATS as the communication substrate
(
see [ADR 0001](/adr/0001-rewriting-to-microservices/), [ADR 0002](/adr/0002-adoption-of-go-as-primary-service-language/),
and [ADR 0003](/adr/0003-adoption-of-nats-as-communication-bridge/)) all share the same underlying assumption:
this project has to be runnable on a budget that fits a one-person operation. The maintainer is a student, the
project has no revenue, and the goal is to keep it alive and improving without a recurring cloud bill that grows with
ambition.

That changes how we pick a provider. The usual industry calculus (region presence, support tier, managed services,
enterprise compliance) is not the lens. The lens here is:

- What can stay at zero cost, indefinitely, without a 12-month timer running out?
- Where can the data legally live, and under whose laws?
- How few moving pieces can we keep, given that the team is one person?
- Where can the workloads run with the least environmental cost, given that "always on" is the default?

There is also a structural choice inside the provider. Most "always free" providers offer some headroom that can
either be split into many small nodes or collapsed into one larger node. Splitting feels safer (failure domains, easy
rolling restarts), but each additional node carries its own kernel, system services, container runtime, and
monitoring agents. On a small free-tier budget that overhead is not a rounding error, it is a meaningful slice of
the total CPU and RAM. We would rather spend that capacity on workloads.

## Decision

The primary host for the system is **Oracle Cloud Infrastructure (OCI)**, in the **Montreal (ca-montreal-1)** region.
The fleet is composed of three nodes across two providers:

- **Oracle ARM node (primary, free tier).** A single Ampere A1 (`VM.Standard.A1.Flex`) instance sized to the full
  Always Free ARM allocation (4 OCPU, 24 GB RAM). It runs **Oracle Linux** because the distribution is built and
  tuned by the same company that designs the Ampere-based shape: kernel, packages, and default tuning all match the
  hardware, and it is the most optimized stack we can run on this node at no cost. The node is backed by a **100 GB
  boot volume provisioned at 120 VPUs/GB** (Oracle's Ultra High Performance tier). At that VPU level the volume's
  IOPS and throughput ceiling sits above the network bandwidth available to the shape, so the disk stops being the
  bottleneck for anything we run: any I/O pressure we hit will show up on the network or on the CPU before it shows
  up on the volume. All core workloads (Vakey master, NATS hub, ...) live here. We deliberately
  collapse the ARM allocation into one node rather than two, so that we pay the OS and agent overhead once instead
  of twice and leave more headroom for the workloads themselves.
- **DigitalOcean Intel droplet (student credits, backup and AMD64 fallback).** A Premium Intel 2 vCPU / 4 GB RAM droplet
  running Ubuntu. It sits on a different provider and a different network, which gives us a real second failure
  domain, and it provides an x86 host for anything that is not yet ARM-clean (third-party binaries, occasional debug
  tooling). This is the only line item we don't have permanently, and it is sized to fit the leftover credits until
  they expire.
- **Oracle AMD micro node (free tier, sentinel role).** A `VM.Standard.E2.1.Micro` x86 instance from the Always Free
  AMD allocation, running Ubuntu. Its job is narrow: act as a **valkey sentinel**, so that quorum can be achieved .
  Keeping it small and single-purpose matches its role and avoids putting any real workload on a 1/8 CPU node.

**Network topology.** Both Oracle nodes (ARM and AMD micro) live in the same OCI Virtual Cloud Network, on a private
subnet, alongside the managed database that backs the system. Keeping the database on a private subnet keeps it off
the public internet and removes a class of exposure we have no reason to accept. Workloads that need the database
talk to it over the VCN, with no NAT, no public ingress, and no per-instance firewall rules tracking which IP can
reach the database today.

The DigitalOcean droplet does not share that VCN, so it cannot reach the private subnet directly. We bridge it in
through **Tailscale**: both Oracle nodes run the Tailscale daemon as subnet routers advertising the VCN private
range, and the DigitalOcean droplet joins the same tailnet as a client. From the droplet's perspective, the database
endpoint is reachable on its private IP through the encrypted tailnet, with the Oracle nodes acting as relays.

This is also the second reason there are two Oracle nodes. They offer high-availability for the Digital Ocean node.
Because both nodes advertise the same subnet route, the relay function for the DigitalOcean droplet
is itself highly available: if one Oracle node is unreachable, Tailscale routes traffic through the other. The HA of
the database and the HA of the relay path collapse into the same two-node arrangement. This allows the Digital Ocean
to temporarily work alone if maintenance or the primary node dies.

Why Oracle Cloud specifically:

- **Cost.** The Always Free tier covers the bulk of what we need without a 12-month expiry. For a student-run
  project this is the difference between "the project keeps running" and "the project gets turned off when the
  trial ends."
- **Always Free ARM capacity.** Oracle's Always Free ARM allocation (4 OCPU, 24 GB RAM on Ampere A1) is, by a wide
  margin, the most generous no-cost compute available from a major cloud. Nothing else in the comparable tier comes
  close.
- **Managed Database.** Oracle's Always Free databases are insanely generous. Offering a HeatWave database with a
  MySQL engine of 8Gb of ram and 50Gb of storage and the AI accelerator having 16Gb of ram. They also offer us two
  Managed Oracle Database of 20Gb of storage each and even more storage with other services. Making it by far sufficient
  for the workload expected and giving us enough time to grow before worrying about any storage limitations.
- **Montreal supply.** ARM Always Free capacity is region-dependent and frequently exhausted. The Montreal region
  has been one of the more reliable regions to actually obtain Ampere A1 capacity at the free tier, which is what
  turned this from a paper plan into a working deployment. To avoid reclaims of the instances, we turned the account
  into a Pay-as-You-Go account allowing us to bypass the Free tier capacity and have access to a broader pool of
  instances.
- **Canadian jurisdiction.** Hosting in Montreal keeps the workloads and any operational data inside Canada,
  under Canadian law. That is the legal frame we want to operate under given where the maintainer lives, and it
  avoids cross-border data questions we are not interested in answering.
- **Eco-friendly footprint.** The Quebec power grid is among the cleanest in North America (mostly hydroelectric).
  An "always on" small fleet has a meaningfully lower carbon footprint here than in regions backed by gas or coal,
  which matters when the workloads run continuously.

Why one big ARM node instead of two smaller ARM nodes: every node has fixed overhead (kernel, systemd, container
runtime, monitoring agent, the NATS or database base memory). At our scale that overhead is large enough relative to
the total budget that doubling it costs us more in capacity than it gains us in resilience. Resilience comes from
the AMD sentinel and the DigitalOcean droplet on a different provider, not from a second copy of the same ARM
overhead on the same host.

Why Oracle Linux on ARM and Ubuntu elsewhere: Oracle Linux is the most optimized choice for the Ampere shape because
the vendor of the OS is the vendor of the hardware and the cloud. On the AMD micro and the DigitalOcean droplet there
is no equivalent advantage, and Ubuntu has the better package availability and the operational familiarity we already
have.

The dominant factor across all of the above is cost for a one-person, student-run operation. Every other criterion
(region, jurisdiction, OS choice, node count) was decided after that filter had narrowed the options.

## Consequences

- The main capacity sits on free-tier-eligible resources, billed through a pay-as-you-go account. The bill stays at
  $0 while we operate inside the Always Free shapes, and PAYG removes the idle-reclaim and account-suspension
  behavior that pure free-tier accounts can hit.
- A single primary node means a host or hypervisor failure takes the primary workloads down. The micro AMD node and
  the DigitalOcean droplet are the only things keeping us from a hard outage in that case, so their roles must stay
  narrow and well understood.
- The fleet is mixed-architecture (ARM64 on Oracle, AMD64 on the micro and on Intel x86 DigitalOcean). Container images
  have to be built multi-arch, and we have to remember which workloads can or cannot run on which host. This is mostly a
  CI concern: build matrices need both targets.
- The fleet is mixed-OS (Oracle Linux + Ubuntu). Package managers, service defaults, and update cadence differ.
  Anything that is not containerized has to be installed twice with two slightly different recipes. We keep that
  set as small as possible. So far the set contains the k3s agent and the Tailscale daemon. For the AMD node we have
  a bare-metal Valkey sentinel deployment.
- **Tailscale is a load-bearing dependency.** Reachability between the DigitalOcean droplet and the database depends
  on the tailnet being healthy and on at least one Oracle node advertising the subnet route. We accept this in
  exchange for not exposing the database publicly and not building our own VPN. The two-Oracle arrangement means a
  single relay failure does not break that path.
- **The database is reachable only from inside the tailnet or the VCN.** Losing both Oracle nodes simultaneously
  takes the database offline regardless of the DigitalOcean droplet's state, because the droplet is a client of the
  database, not a copy of it. The two-Oracle arrangement is what bounds that risk to the coincident failure of two
  free-tier hosts.
- DigitalOcean is the only recurring bill. It is small, deliberately, so it can be carried by a student budget, but
  it is the line item to watch if the project grows in a direction that needs more from it.
- Some lock-in to Oracle exists at the fleet level (free tier terms, account, region), but the workloads themselves
  are containerized Go services on commodity primitives (NATS, database, Linux), so the cost of moving them to another
  provider is bounded.
- The maintainer is the billing owner. If life moves, the account moves with it, so the bootstrap procedure for the
  whole fleet (provider account, region request, image build, secrets) is part of the project documentation, not
  tribal knowledge.

## Alternatives considered

- **AWS Free Tier.** The 12-month limit defeats the central requirement: "always on" without a clock running out.
  After the trial, the equivalent capacity is well above what a student-run project can absorb. Montreal (ca-central-1)
  is also one of the pricier regions for paid usage.
- **Google Cloud Free Tier.** The Always Free `e2-micro` is far too small for the workloads (1-2 vCPU burst, 1 GB
  RAM, single zone), and there is no ARM equivalent in the no-cost tier. Useful for an experiment, not for a fleet.
- **Azure Free.** Similar shape to AWS: limited-time credits, no Always Free tier at a useful size, and pricing
  beyond the trial does not match the budget.
- **hetzner / OVH / vultr.** Cheap and honest pricing, but paid from day one. Even at a few dollars per node per
  month, the recurring cost compounds across a fleet and crosses the line of "indefinitely sustainable on a student
  budget." They remain a strong option if the project's funding situation changes.
- **DigitalOcean for everything.** Operationally the simplest (one provider, one console, one billing line), but
  the full fleet on DigitalOcean is a real monthly bill. We keep one DigitalOcean droplet for the value it brings
  (different provider, different failure domain, AMD64 host) without making it carry the whole system.
- **Self-hosting on residential hardware.** Zero cloud bill, but residential ISPs have terms against running
  always-on services, uplink reliability is not what production workloads need, and the power and cooling costs are
  not zero either. The initial cost is also to count for a student.
- **Splitting the Oracle ARM allocation into two smaller ARM nodes.** Considered for failure-domain reasons, but
  the OS and agent overhead would eat a noticeable slice of the free tier, and the resilience gain is limited
  because both nodes still live on the same Oracle region. The AMD node and the DigitalOcean droplet give us
  a real second failure domain at a lower capacity cost.
