#!/usr/bin/env python3
"""Idempotent New Relic reconciler for the BagelBot observability stack.

Source of truth is deploy/newrelic/definitions/*. Re-running converges the
account (3823179) back to those definitions: it never duplicates objects, and
it edits in place when something already exists. Safe to run repeatedly and in
CI. It is the companion to the Flux-managed nri-bundle agents (which ship
metrics/logs) — this owns the account-side config the agents cannot: alert
condition thresholds, notification destinations/channels/workflows, and the
Fleet dashboard.

Auth + secrets come from Doppler (project infra, config prd_newrelic), matching
the rest of the fleet's tooling:
  NEWRELIC_USER_API_KEY    NerdGraph user key
  DISCORD_ALERT_WEBHOOK_URL Discord incoming-webhook URL (kept out of git)
  DISCORD_ALERT_ROLE_ID    Discord role to ping in alert embeds

Usage:
  python3 deploy/newrelic/provision.py            # reconcile everything
  python3 deploy/newrelic/provision.py --dry-run  # print planned changes only
  python3 deploy/newrelic/provision.py --only alerts|notifications|dashboard

Scope note on alerts: the two policies (Golden Signals, Kubernetes alert policy)
and their conditions are created by New Relic's guided installs, not here. This
script reconciles the fields we deliberately changed (nrql, enabled, terms) by
matching on policy-name + condition-name. A condition present in the definitions
but absent in the account is reported, not created, so a renamed upstream
condition surfaces loudly instead of silently spawning a duplicate.
"""
import argparse
import atexit
import json
import os
import subprocess
import sys
import tempfile
from collections import namedtuple

HERE = os.path.dirname(os.path.abspath(__file__))
DEFS = os.path.join(HERE, "definitions")
DOPPLER_PROJECT = "infra"
DOPPLER_CONFIG = "prd_newrelic"
NERDGRAPH_URL = "https://api.newrelic.com/graphql"


def doppler(name, required=True):
    r = subprocess.run(
        ["doppler", "secrets", "get", name,
         "--project", DOPPLER_PROJECT, "--config", DOPPLER_CONFIG, "--plain"],
        capture_output=True, text=True)
    if r.returncode != 0 or not r.stdout.strip():
        if required:
            sys.exit(f"missing Doppler secret {name} in {DOPPLER_PROJECT}/{DOPPLER_CONFIG}")
        return None
    return r.stdout.strip()


_CURL_CONFIG = None  # path to the 0600 curl config holding the API-Key header


def init_curl(api_key):
    """Write the API key into a private curl config file rather than passing it
    on the command line. Process arguments are world-readable via ps and
    /proc/<pid>/cmdline, so a key or webhook URL in argv leaks to every other
    user on the host (e.g. a shared CI runner). The config file is created 0600
    and removed on exit; request bodies are streamed over stdin for the same
    reason, so nothing sensitive ever reaches argv."""
    global _CURL_CONFIG
    fd, path = tempfile.mkstemp(prefix="nr-curl-", suffix=".cfg")
    os.chmod(path, 0o600)
    with os.fdopen(fd, "w") as f:
        f.write(f'url = "{NERDGRAPH_URL}"\n')
        f.write('header = "Content-Type: application/json"\n')
        f.write(f'header = "API-Key: {api_key}"\n')
    _CURL_CONFIG = path
    atexit.register(lambda: os.path.exists(path) and os.unlink(path))


def gql(query, variables=None):
    body = {"query": query}
    if variables:
        body["variables"] = variables
    out = subprocess.run(
        ["curl", "-sS", "--config", _CURL_CONFIG, "--data-binary", "@-"],
        input=json.dumps(body), capture_output=True, text=True, check=True).stdout
    r = json.loads(out)
    if "errors" in r:
        raise RuntimeError(json.dumps(r["errors"], indent=1))
    return r["data"]


def load(name):
    return json.load(open(os.path.join(DEFS, name)))


# --------------------------------------------------------------------------- #
# Alerts
# --------------------------------------------------------------------------- #
STATIC_UPDATE = """
mutation($account: Int!, $id: ID!, $condition: AlertsNrqlConditionUpdateStaticInput!) {
  alertsNrqlConditionStaticUpdate(accountId: $account, id: $id, condition: $condition) { id }
}"""
BASELINE_UPDATE = """
mutation($account: Int!, $id: ID!, $condition: AlertsNrqlConditionUpdateBaselineInput!) {
  alertsNrqlConditionBaselineUpdate(accountId: $account, id: $id, condition: $condition) { id }
}"""


def reconcile_alerts(dry_run):
    spec = load("alerts.json")
    account = spec["account_id"]
    data = gql("""
    { actor { account(id: %d) { alerts {
        policiesSearch { policies { id name } }
        nrqlConditionsSearch { nrqlConditions { id name policyId enabled nrql { query } } }
    } } } }""" % account)["actor"]["account"]["alerts"]
    pol_name = {p["id"]: p["name"] for p in data["policiesSearch"]["policies"]}
    # index live conditions by (policy name, condition name)
    live = {}
    for c in data["nrqlConditionsSearch"]["nrqlConditions"]:
        live[(pol_name.get(c["policyId"], "?"), c["name"])] = c

    changed = missing = ok = 0
    for want in spec["conditions"]:
        cur = live.get((want["policy"], want["name"]))
        if cur is None:
            print(f"  MISSING  [{want['policy']}] {want['name']} (not in account — skipped)")
            missing += 1
            continue
        needs = (cur["nrql"]["query"] != want["nrql"]) or (cur["enabled"] != want["enabled"])
        if not needs:
            ok += 1
            continue
        changed += 1
        verb = "would set" if dry_run else "set"
        state = "enabled" if want["enabled"] else "DISABLED"
        print(f"  {verb}   [{want['policy']}] {want['name']} -> {state}")
        if dry_run:
            continue
        cond = {"enabled": want["enabled"],
                "nrql": {"query": want["nrql"]},
                "terms": want["terms"]}
        mut = BASELINE_UPDATE if want["type"] == "baseline" else STATIC_UPDATE
        gql(mut, {"account": account, "id": cur["id"], "condition": cond})
    print(f"alerts: {ok} in sync, {changed} changed, {missing} missing")


# --------------------------------------------------------------------------- #
# Notifications
#
# Split into one helper per object kind so each stays shallow (CodeScene: no
# Bumpy Road / Complex Method). Every upsert returns the object id (or None in
# dry-run when it would be created), which the orchestrator threads forward.
# --------------------------------------------------------------------------- #
EXISTING_NOTIFICATIONS = """
{ actor { account(id: %d) { aiNotifications {
    destinations { entities { id name type } }
    channels { entities { id name type destinationId } }
} } } }"""
CREATE_DESTINATION = """
mutation($account: Int!, $destination: AiNotificationsDestinationInput!) {
  aiNotificationsCreateDestination(accountId: $account, destination: $destination) {
    destination { id } error { ... on AiNotificationsResponseError { description } }
  }
}"""
UPDATE_DESTINATION = """
mutation($account: Int!, $id: ID!, $destination: AiNotificationsDestinationUpdate!) {
  aiNotificationsUpdateDestination(accountId: $account, destinationId: $id, destination: $destination) {
    destination { id } error { ... on AiNotificationsResponseError { description } }
  }
}"""
CREATE_CHANNEL = """
mutation($account: Int!, $channel: AiNotificationsChannelInput!) {
  aiNotificationsCreateChannel(accountId: $account, channel: $channel) {
    channel { id } error { ... on AiNotificationsResponseError { description } }
  }
}"""
UPDATE_CHANNEL = """
mutation($account: Int!, $id: ID!, $channel: AiNotificationsChannelUpdate!) {
  aiNotificationsUpdateChannel(accountId: $account, channelId: $id, channel: $channel) {
    channel { id } error { ... on AiNotificationsResponseError { description } }
  }
}"""
WORKFLOW_LOOKUP = """
{ actor { account(id: %d) { aiWorkflows { workflows(filters: {name: "%s"}) {
    entities { id } } } } } }"""
CREATE_WORKFLOW = """
mutation($account: Int!, $workflow: AiWorkflowsCreateWorkflowInput!) {
  aiWorkflowsCreateWorkflow(accountId: $account, createWorkflowData: $workflow) {
    workflow { id } errors { description }
  }
}"""
UPDATE_WORKFLOW = """
mutation($account: Int!, $workflow: AiWorkflowsUpdateWorkflowInput!) {
  aiWorkflowsUpdateWorkflow(accountId: $account, updateWorkflowData: $workflow) {
    workflow { id } errors { description }
  }
}"""


def _die_on_error(result, label):
    """New Relic returns failures in a single `error` object here."""
    if result.get("error"):
        sys.exit(f"{label}: {result['error']}")


def _die_on_errors(result, label):
    """...and in an `errors` list for the workflow mutations."""
    if result.get("errors"):
        sys.exit(f"{label}: {json.dumps(result['errors'])}")


# Shared reconcile context, passed as one argument so the upsert helpers stay
# under the argument-count limit instead of threading account/dry_run/secrets
# through every signature.
NotifyCtx = namedtuple("NotifyCtx", "account dry_run webhook_url role_id")


def resolve_props(props, webhook_url):
    """Materialize property values, pulling any Doppler `secretRef` at runtime."""
    out = []
    for p in props:
        if "secretRef" not in p:
            out.append({"key": p["key"], "value": p["value"]})
            continue
        val = webhook_url if p["secretRef"] == "DISCORD_ALERT_WEBHOOK_URL" else doppler(p["secretRef"])
        out.append({"key": p["key"], "value": val})
    return out


def channel_props(ch, role_id):
    """A webhook channel carries the embed template file; others carry inline props."""
    if "payloadTemplateFile" not in ch:
        return ch.get("properties", [])
    tmpl = open(os.path.join(DEFS, ch["payloadTemplateFile"])).read().strip()
    return [{"key": "payload", "value": tmpl.replace("__ROLE_ID__", role_id), "label": "Payload Template"}]


def upsert_destination(ctx, d, existing):
    props = resolve_props(d.get("properties", []), ctx.webhook_url)
    cur = existing.get(d["name"])
    if cur:
        if not ctx.dry_run:
            gql(UPDATE_DESTINATION, {"account": ctx.account, "id": cur["id"],
                                     "destination": {"name": d["name"], "properties": props}})
        print(f"  destination ok    {d['name']}")
        return cur["id"]
    print(f"  {'would create' if ctx.dry_run else 'create'} destination {d['name']}")
    if ctx.dry_run:
        return None
    res = gql(CREATE_DESTINATION, {"account": ctx.account,
              "destination": {"type": d["type"], "name": d["name"], "properties": props}})["aiNotificationsCreateDestination"]
    _die_on_error(res, f"destination {d['name']}")
    return res["destination"]["id"]


def upsert_channel(ctx, ch, existing, dest_ids):
    props = channel_props(ch, ctx.role_id)
    cur = existing.get(ch["name"])
    if cur:
        if not ctx.dry_run:
            gql(UPDATE_CHANNEL, {"account": ctx.account, "id": cur["id"],
                                 "channel": {"name": ch["name"], "properties": props}})
        print(f"  channel ok        {ch['name']}")
        return cur["id"]
    print(f"  {'would create' if ctx.dry_run else 'create'} channel {ch['name']}")
    if ctx.dry_run:
        return None
    res = gql(CREATE_CHANNEL, {"account": ctx.account, "channel": {
              "type": ch["type"], "name": ch["name"], "product": ch["product"],
              "destinationId": dest_ids[ch["destination"]], "properties": props}})["aiNotificationsCreateChannel"]
    _die_on_error(res, f"channel {ch['name']}")
    return res["channel"]["id"]


def upsert_workflow(ctx, wf, chan_ids):
    dest_cfg = [{"channelId": chan_ids[name]} for name in wf["channels"]]
    common = {"name": wf["name"], "workflowEnabled": wf["enabled"],
              "destinationsEnabled": True, "mutingRulesHandling": wf["mutingRulesHandling"],
              "destinationConfigurations": dest_cfg}
    live = gql(WORKFLOW_LOOKUP % (ctx.account, wf["name"]))["actor"]["account"]["aiWorkflows"]["workflows"]["entities"]
    if live:
        print(f"  {'would update' if ctx.dry_run else 'update'} workflow {wf['name']}")
        if not ctx.dry_run:
            res = gql(UPDATE_WORKFLOW, {"account": ctx.account, "workflow": dict(common, id=live[0]["id"])})
            _die_on_errors(res["aiWorkflowsUpdateWorkflow"], "workflow update")
        return
    print(f"  {'would create' if ctx.dry_run else 'create'} workflow {wf['name']}")
    if not ctx.dry_run:
        create = dict(common, issuesFilter={"name": wf["issuesFilter"]["name"], "type": "FILTER",
                                            "predicates": wf["issuesFilter"]["predicates"]})
        res = gql(CREATE_WORKFLOW, {"account": ctx.account, "workflow": create})
        _die_on_errors(res["aiWorkflowsCreateWorkflow"], "workflow create")


def _index_ids(items, upsert):
    """Map each item name -> its upserted id, dropping the None a dry-run
    returns for objects it would create. Kept as a comprehension so the caller
    has no nested loop/branch to reconcile (CodeScene: no Bumpy Road)."""
    return {name: oid
            for name, oid in ((it["name"], upsert(it)) for it in items)
            if oid}


def reconcile_notifications(dry_run, webhook_url, role_id):
    spec = load("notifications.json")
    ctx = NotifyCtx(spec["account_id"], dry_run, webhook_url, role_id)
    existing = gql(EXISTING_NOTIFICATIONS % ctx.account)["actor"]["account"]["aiNotifications"]
    dest_by_name = {d["name"]: d for d in existing["destinations"]["entities"]}
    chan_by_name = {c["name"]: c for c in existing["channels"]["entities"]}

    dest_ids = _index_ids(spec["destinations"], lambda d: upsert_destination(ctx, d, dest_by_name))
    chan_ids = _index_ids(spec["channels"], lambda ch: upsert_channel(ctx, ch, chan_by_name, dest_ids))

    if dry_run and any(ch["name"] not in chan_ids for ch in spec["channels"]):
        print("  (dry-run) workflow wiring skipped until channels exist")
        return
    upsert_workflow(ctx, spec["workflow"], chan_ids)
    print("notifications: reconciled")


# --------------------------------------------------------------------------- #
# Dashboard
# --------------------------------------------------------------------------- #
DASHBOARD_LOOKUP = """
{ actor { entitySearch(query: "type = 'DASHBOARD' AND name = '%s'") {
    results { entities { guid name } } } } }"""
DASHBOARD_CREATE = """
mutation($account: Int!, $dashboard: DashboardInput!) {
  dashboardCreate(accountId: $account, dashboard: $dashboard) {
    entityResult { guid } errors { description }
  }
}"""
DASHBOARD_UPDATE = """
mutation($guid: EntityGuid!, $dashboard: DashboardInput!) {
  dashboardUpdate(guid: $guid, dashboard: $dashboard) {
    entityResult { guid } errors { description }
  }
}"""


def _dashboard_account(spec):
    """dashboard.json carries no top-level account id; every widget embeds it."""
    return spec["pages"][0]["widgets"][0]["rawConfiguration"]["nrqlQueries"][0]["accountIds"][0]


def _update_dashboard(guid, dashboard_input, dry_run):
    print(f"  {'would update' if dry_run else 'update'} dashboard {dashboard_input['name']} ({guid})")
    if dry_run:
        return
    res = gql(DASHBOARD_UPDATE, {"guid": guid, "dashboard": dashboard_input})
    _die_on_errors(res["dashboardUpdate"], "dashboard update")


def _create_dashboard(account, dashboard_input, dry_run):
    print(f"  {'would create' if dry_run else 'create'} dashboard {dashboard_input['name']}")
    if dry_run:
        return
    res = gql(DASHBOARD_CREATE, {"account": account, "dashboard": dashboard_input})
    _die_on_errors(res["dashboardCreate"], "dashboard create")


def reconcile_dashboard(dry_run):
    spec = load("dashboard.json")
    found = gql(DASHBOARD_LOOKUP % spec["name"].replace("'", "\\'"))
    ents = [e for e in found["actor"]["entitySearch"]["results"]["entities"] if e["name"] == spec["name"]]
    dashboard_input = {"name": spec["name"], "description": spec["description"],
                       "permissions": spec["permissions"], "pages": spec["pages"]}
    if ents:
        _update_dashboard(ents[0]["guid"], dashboard_input, dry_run)
    else:
        _create_dashboard(_dashboard_account(spec), dashboard_input, dry_run)
    print("dashboard: reconciled")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--dry-run", action="store_true")
    ap.add_argument("--only", choices=["alerts", "notifications", "dashboard"])
    args = ap.parse_args()

    init_curl(doppler("NEWRELIC_USER_API_KEY"))

    want = {args.only} if args.only else {"alerts", "notifications", "dashboard"}
    if "alerts" in want:
        print("== alerts ==")
        reconcile_alerts(args.dry_run)
    if "notifications" in want:
        print("== notifications ==")
        webhook = doppler("DISCORD_ALERT_WEBHOOK_URL")
        role = doppler("DISCORD_ALERT_ROLE_ID")
        reconcile_notifications(args.dry_run, webhook, role)
    if "dashboard" in want:
        print("== dashboard ==")
        reconcile_dashboard(args.dry_run)


if __name__ == "__main__":
    main()
