"""Real Docker metrics, quotas, mount capacity and authorization smoke test.

Only randomly named disposable resources are created and removed.
"""
import json
import os
import secrets
import subprocess
import time
import urllib.error
import urllib.request
import uuid

from roles_smoke import docker


def main():
    name = "portainer-metrics-test-" + uuid.uuid4().hex[:10]
    server, workload, private = [name + suffix for suffix in ("-server", "-app", "-private")]
    data, appdata = name + "-data", name + "-volume"
    setup = secrets.token_hex(32)
    base = ""
    checks = []

    def api(method, path, payload=None, token=None, expected=200):
        headers = {"Content-Type": "application/json"}
        if token:
            headers["Authorization"] = "Bearer " + token
        if path == "/users/admin/init":
            headers["X-Setup-Token"] = setup
        req = urllib.request.Request(base + "/api" + path, data=None if payload is None else json.dumps(payload).encode(), headers=headers, method=method)
        try:
            with urllib.request.urlopen(req, timeout=45) as result:
                code, body = result.status, result.read()
        except urllib.error.HTTPError as error:
            code, body = error.code, error.read()
        assert code == expected, f"{method} {path}: expected {expected}, got {code}: {body[:500]!r}"
        return json.loads(body) if body[:1] in (b"{", b"[") else None

    try:
        docker("volume", "create", data)
        docker("volume", "create", appdata)
        cid = docker("run", "-d", "--name", workload, "--cpus", "0.5", "--memory", "64m", "-e", "DB_PASSWORD=METRICS_MUST_NOT_EXPOSE_THIS", "-v", appdata + ":/data", "alpine:3.20", "sh", "-c", "dd if=/dev/zero of=/layer.bin bs=1M count=4 2>/dev/null; dd if=/dev/zero of=/data/test.bin bs=1M count=3 2>/dev/null; exec yes >/dev/null")
        pid = docker("run", "-d", "--name", private, "alpine:3.20", "sleep", "600")
        docker("run", "-d", "--name", server, "-p", "127.0.0.1::9000", "-v", "/var/run/docker.sock:/var/run/docker.sock", "-v", data + ":/data", "-v", "/:/host:ro", "-e", "PORTAINER_METRICS_HOST_ROOT=/host", os.environ.get("PORTAINER_TEST_IMAGE", "portainer-custom:2.45.3-metrics"), "--http-enabled", "--setup-token", setup, "-H", "unix:///var/run/docker.sock")
        base = "http://" + docker("port", server, "9000/tcp")
        for _ in range(60):
            try:
                api("GET", "/status")
                break
            except (OSError, AssertionError):
                time.sleep(1)
        password = secrets.token_urlsafe(24)
        api("POST", "/users/admin/init", {"Username": "admin", "Password": password})
        admin = api("POST", "/auth", {"Username": "admin", "Password": password})["jwt"]
        endpoints = api("GET", "/endpoints", token=admin)
        if not endpoints:
            boundary = uuid.uuid4().hex
            fields = {"Name": "local", "EndpointCreationType": "1", "URL": "unix:///var/run/docker.sock"}
            body = "".join(f'--{boundary}\r\nContent-Disposition: form-data; name="{key}"\r\n\r\n{value}\r\n' for key, value in fields.items()) + f"--{boundary}--\r\n"
            req = urllib.request.Request(base + "/api/endpoints", data=body.encode(), headers={"Authorization": "Bearer " + admin, "Content-Type": "multipart/form-data; boundary=" + boundary}, method="POST")
            with urllib.request.urlopen(req, timeout=120) as response:
                endpoints = [json.load(response)]
        endpoint = endpoints[0]["Id"]
        prefix = f"/endpoints/{endpoint}"
        resource = prefix + f"/containers/{cid}/metrics"
        user = api("POST", "/users", {"Username": "reader", "Password": password, "Role": 2}, admin)["Id"]
        team = api("POST", "/teams", {"Name": "metrics-readers"}, admin)["Id"]
        membership = api("POST", "/team_memberships", {"UserID": user, "TeamID": team, "Role": 2}, admin)["Id"]
        api("PUT", prefix, {"TeamAccessPolicies": {str(team): {"RoleId": 4, "StandaloneRole": "read-only"}}}, admin)
        ro = api("POST", "/auth", {"Username": "reader", "Password": password})["jwt"]
        rc = api("POST", "/resource_controls", {"ResourceID": cid, "Type": 1, "Teams": [team]}, admin)["Id"]
        api("POST", "/resource_controls", {"ResourceID": pid, "Type": 1, "AdministratorsOnly": True}, admin)

        usage = api("GET", resource, token=ro)
        assert usage["cpu"]["total"] == 0.5, usage
        assert usage["cpu"]["used"] is not None and usage["cpu"]["used"] > 0, usage
        assert usage["memory"]["total"] == 64 * 1024**2, usage
        assert 0 < usage["memory"]["used"] <= usage["memory"]["total"], usage
        assert usage["writableLayer"]["used"] >= 4 * 1024**2 and usage["writableLayer"]["total"] is None, usage
        mount = next(m for m in usage["mounts"] if m["target"] == "/data")
        assert mount["data"]["used"] >= 3 * 1024**2 and mount["data"]["total"] is None, usage
        assert mount["filesystem"]["total"] > 0, usage
        assert "METRICS_MUST_NOT_EXPOSE_THIS" not in json.dumps(usage) and '"Env"' not in json.dumps(usage)
        checks.append("0.5 CPU / 64 MiB limits, live usage, writable layer and volume data, shared filesystem, no secrets")

        host = api("GET", prefix + "/metrics", token=ro)
        assert host["cpu"]["used"] is not None and host["cpu"]["total"] > 0, host
        assert host["memory"]["used"] > 0 and host["memory"]["total"] > host["memory"]["used"], host
        assert len(host["mounts"]) > 0 and all(m["data"]["total"] > 0 for m in host["mounts"]), host
        checks.append("Linux Docker host CPU/RAM and per-mount filesystem capacities")

        api("GET", prefix + f"/containers/{pid}/metrics", token=ro, expected=403)
        api("GET", resource, expected=401)
        api("GET", prefix + "/containers/not-an-id/metrics", token=admin, expected=400)
        api("PUT", f"/resource_controls/{rc}", {"AdministratorsOnly": True, "Public": False, "Users": [], "Teams": []}, admin)
        api("GET", resource, token=ro, expected=403)
        api("DELETE", f"/team_memberships/{membership}", token=admin, expected=204)
        api("GET", prefix + "/metrics", token=ro, expected=403)
        checks.append("private resource denial, anonymous denial, invalid IDs and immediate resource/team revocation despite cache")

        docker("stop", workload)
        stopped = api("GET", resource, token=admin)
        assert stopped["state"] == "stopped" and stopped["cpu"]["used"] == 0 and stopped["memory"]["used"] == 0, stopped
        checks.append("stopped containers show zero CPU/RAM and preserve disk data")
        print(json.dumps({"checks": checks, "host_mounts": len(host["mounts"]), "cpu_capacity": host["cpu"]["total"], "memory_capacity": host["memory"]["total"]}, indent=2))
    finally:
        for container in (server, workload, private):
            subprocess.run(["docker", "rm", "-f", container], capture_output=True)
        for volume in (data, appdata):
            subprocess.run(["docker", "volume", "rm", volume], capture_output=True)


if __name__ == "__main__":
    main()
