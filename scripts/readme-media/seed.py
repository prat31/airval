"""Seed a throwaway `airval dev` instance with realistic demo data for README screenshots.

Usage: python3 seed.py <base-url>. Uses throwaway test credentials on a local instance only.
"""
import http.cookiejar
import json
import sys
import time
import urllib.request

BASE = sys.argv[1]
EMAIL, PASSWORD = "demo@example.test", "readme-demo-1234"

jar = http.cookiejar.CookieJar()
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))


def csrf():
    return next((c.value for c in jar if c.name == "airval_csrf"), "")


def call(method, path, body=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(BASE + path, data=data, method=method)
    req.add_header("content-type", "application/json")
    if method != "GET":
        req.add_header("X-CSRF-Token", csrf())
    with opener.open(req, timeout=120) as r:
        raw = r.read()
        return json.loads(raw) if raw else None


call("POST", "/v1/setup", {"email": EMAIL, "password": PASSWORD, "name": "Avery Chen", "org_name": "Acme AI", "org_slug": "acme"})
org = call("GET", "/v1/auth/me")["orgs"][0]
sample = call("POST", "/v1/projects:sample", {"org_id": org["id"]})
project = sample["project"]
print("project", project["slug"], file=sys.stderr)

suites = call("GET", f"/v1/projects/{project['id']}/suites")["items"]
suite = suites[0]
targets = call("GET", f"/v1/projects/{project['id']}/targets")["items"]
by_name = {t["name"]: t for t in targets}
print("targets", list(by_name), file=sys.stderr)


def wait(run_ids):
    for _ in range(240):
        runs = [call("GET", f"/v1/runs/{rid}") for rid in run_ids]
        if all(r["status"] in ("succeeded", "failed", "cancelled", "errored") for r in runs):
            return runs
        time.sleep(0.5)
    raise SystemExit("runs did not finish")


# A week of history: nightly runs on both targets with varying samples, plus a PR run.
shas = ["9f2c1ab", "4e8d0c7", "b71a3f2", "0c5e9d4", "e3b8a61", "7a4f2c9"]
for i, sha in enumerate(shas):
    for name, t in by_name.items():
        out = call("POST", "/v1/runs", {
            "suite_id": suite["id"],
            "target_ids": [t["id"]],
            "name": f"nightly · {sha}",
            "labels": {"git_sha": sha, "branch": "main"},
            "sampling": {"mode": "random", "percent": 60 + 6 * i, "seed": 100 + i},
        })
        wait([r["id"] for r in out["runs"]])
    print("seeded", sha, file=sys.stderr)

print(json.dumps({"org": org["slug"], "project": project["slug"], "email": EMAIL, "password": PASSWORD}))
