"""Run against a disposable Portainer and resources on the current Docker daemon.

Requires: Docker, Python 3, and a locally built portainer-custom:2.45.3 image.
All resources use a random project name; existing workloads are never mutated.
"""
import json
import os
import secrets
import subprocess
import time
import urllib.error
import urllib.request
import uuid


def docker(*args):
    return subprocess.check_output(["docker", *args], text=True, timeout=120).strip()


def main():
    project = "portainer-controls-test-" + uuid.uuid4().hex[:10]
    data_volume, config_volume = project + "-data", project + "-configs"
    container_name = project + "-server"
    stack_id = None
    base = ""
    admin = ""
    setup_token = secrets.token_hex(32)
    checks = []

    def api(method, path, payload=None, token=None, expected=200):
        headers = {"Content-Type": "application/json"}
        if path == "/users/admin/init":
            headers["X-Setup-Token"] = setup_token
        if token:
            headers["Authorization"] = "Bearer " + token
        req = urllib.request.Request(base + "/api" + path, data=None if payload is None else json.dumps(payload).encode(), headers=headers, method=method)
        try:
            with urllib.request.urlopen(req, timeout=290) as result:
                code, body = result.status, result.read()
        except urllib.error.HTTPError as error:
            code, body = error.code, error.read()
        assert code == expected, f"{method} {path}: expected {expected}, got {code}: {body[:800]!r}"
        return json.loads(body) if body else None

    def controls():
        return api("GET", f"/stacks/{stack_id}/controls", token=admin)

    def service(name):
        ids = docker("ps", "-aq", "--filter", "label=com.docker.compose.project=" + project, "--filter", "label=com.docker.compose.service=" + name).splitlines()
        assert len(ids) == 1, ids
        return json.loads(docker("inspect", ids[0]))[0]

    def upload(content, target="web"):
        return api("POST", f"/stacks/{stack_id}/configurations", {"Version": controls()["Configuration"]["Version"], "TargetID": target, "Filename": "app.conf", "Content": content}, uploader)

    def apply(revision, rollback=False, expected=200):
        return api("POST", f"/stacks/{stack_id}/configurations/{revision['ID']}/{'rollback' if rollback else 'apply'}", {"Version": controls()["Configuration"]["Version"]}, applier, expected)

    def update_compose(content, expected_status=1):
        api("PUT", f"/stacks/{stack_id}?endpointId={endpoint}", {"StackFileContent": content, "Env": [], "Prune": True}, admin)
        for _ in range(120):
            state = api("GET", f"/stacks/{stack_id}", token=admin)
            if state["Status"] != 3:
                break
            time.sleep(1)
        assert state["Status"] == expected_status, state.get("DeploymentStatus")
        return state

    try:
        docker("volume", "create", data_volume)
        docker("volume", "create", config_volume)
        config_root = json.loads(docker("volume", "inspect", config_volume))[0]["Mountpoint"]
        docker("run", "-d", "--name", container_name, "-p", "127.0.0.1::9000", "-v", "/var/run/docker.sock:/var/run/docker.sock", "-v", data_volume + ":/data", "-v", config_volume + ":" + config_root, "-e", "PORTAINER_STACK_CONFIG_ROOT=" + config_root, os.environ.get("PORTAINER_TEST_IMAGE", "portainer-custom:2.45.3"), "--http-enabled", "--setup-token", setup_token, "-H", "unix:///var/run/docker.sock")
        base = "http://" + docker("port", container_name, "9000/tcp")
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
            with urllib.request.urlopen(req, timeout=120) as result:
                endpoints = [json.load(result)]
        endpoint = endpoints[0]["Id"]
        users = []
        tokens = []
        for username in ("uploader", "applier", "viewer"):
            users.append(api("POST", "/users", {"Username": username, "Password": password, "Role": 2}, admin)["Id"])
            tokens.append(api("POST", "/auth", {"Username": username, "Password": password})["jwt"])
        uploader, applier, viewer = tokens
        team = api("POST", "/teams", {"Name": "config-operators"}, admin)["Id"]
        api("POST", "/team_memberships", {"UserID": users[1], "TeamID": team, "Role": 2}, admin)
        api("PUT", f"/endpoints/{endpoint}", {"UserAccessPolicies": {str(user): {"RoleId": 0} for user in users}}, admin)
        api("PUT", f"/endpoints/{endpoint}/settings", {"allowStackManagementForRegularUsers": True}, admin)
        compose = "services:\n  web:\n    image: nginx:alpine\n    healthcheck:\n      test: [CMD, nginx, -t]\n      interval: 2s\n      timeout: 2s\n      retries: 2\n  side:\n    image: nginx:alpine\n"
        stack = api("POST", f"/stacks/create/standalone/string?endpointId={endpoint}", {"Name": project, "StackFileContent": compose, "Env": []}, admin)
        stack_id = stack["Id"]
        for _ in range(180):
            stack = api("GET", f"/stacks/{stack_id}", token=admin)
            if stack["Status"] != 3:
                break
            time.sleep(1)
        assert stack["Status"] == 1, stack.get("DeploymentStatus")
        rc = stack["ResourceControl"]["Id"]
        api("PUT", f"/resource_controls/{rc}", {"Public": False, "AdministratorsOnly": False, "Users": users, "Teams": [team]}, admin)
        api("PUT", f"/stacks/{stack_id}/policy", {"Version": 0, "Grants": [{"UserID": users[0], "Actions": ["view", "config.upload"]}, {"TeamID": team, "Actions": ["view", "config.apply"]}, {"UserID": users[2], "Actions": ["view"]}]}, admin)
        api("PUT", f"/stacks/{stack_id}/configurations/targets", {"Version": 0, "Targets": [{"ID": "web", "Service": "web", "Target": "/etc/nginx/conf.d/default.conf"}]}, admin)
        web_before, side_before = service("web"), service("side")
        first = upload('server { listen 80; location / { return 200 "revision-one"; } }\n# ${LITERAL_VALUE}\n')
        assert not controls()["Configuration"]["Targets"][0].get("ActiveRevision")
        assert service("web")["Id"] == web_before["Id"]
        checks.append("upload creates a draft without recreating containers")
        api("POST", f"/stacks/{stack_id}/configurations/{first['ID']}/apply", {"Version": controls()["Configuration"]["Version"]}, uploader, 403)
        api("PUT", f"/stacks/{stack_id}", {"StackFileContent": compose.replace("nginx:alpine", "nginx:latest")}, applier, 403)
        api("POST", f"/stacks/{stack_id}/stop?endpointId={endpoint}", {}, applier, 403)
        api("POST", f"/endpoints/{endpoint}/docker/containers/{web_before['Id']}/restart", {}, applier, 403)
        api("GET", f"/endpoints/{endpoint}/docker/containers/{web_before['Id']}/archive?path=/etc", token=viewer, expected=403)
        api("PUT", f"/resource_controls/{rc}", {"Public": True}, uploader, 403)
        api("POST", f"/stacks/create/standalone/string?endpointId={endpoint}", {"Name": project+"-bypass", "StackFileContent": compose}, uploader, 403)
        checks.append("operation grants and direct Docker / ownership bypasses are enforced")
        apply(first)
        web_after = service("web")
        assert web_after["Id"] != web_before["Id"]
        assert web_after["Image"] == web_before["Image"]
        assert service("side")["Id"] == side_before["Id"]
        mount = next(m for m in web_after["Mounts"] if m["Destination"] == "/etc/nginx/conf.d/default.conf")
        assert mount["RW"] is False
        assert "${LITERAL_VALUE}" in docker("exec", web_after["Id"], "cat", "/etc/nginx/conf.d/default.conf")
        checks.append("apply mounts immutable content read-only and recreates only the selected service with the same image")
        bad = upload("this is not a valid nginx directive;\n")
        apply(bad, expected=500)
        config = controls()["Configuration"]
        assert config["Targets"][0]["ActiveRevision"] == first["ID"]
        assert not config.get("PendingRevision")
        assert not next(r for r in config["Revisions"] if r["ID"] == bad["ID"]).get("AppliedAt")
        assert "revision-one" in docker("exec", service("web")["Id"], "cat", "/etc/nginx/conf.d/default.conf")
        checks.append("failed apply restores the previous service configuration and does not mark the bad revision successful")
        second = upload('server { listen 80; location / { return 200 "revision-two"; } }\n')
        apply(second)
        apply(first, rollback=True)
        assert "revision-one" in docker("exec", service("web")["Id"], "cat", "/etc/nginx/conf.d/default.conf")
        assert service("side")["Id"] == side_before["Id"]
        checks.append("rollback restores a previously successful revision without touching the other service")

        # Exercise the real update/deployment/Compose rollback path with an
        # ActiveRevision, rather than editing Compose on disk in a fixture.
        for operation, candidate in (
            ("rename", compose.replace("  web:\n", "  api:\n")),
            ("delete", "services:\n  side:\n    image: nginx:alpine\n"),
        ):
            original_config = controls()["Configuration"]
            assert original_config["Targets"][0]["ActiveRevision"] == first["ID"]
            # Invalid Compose fails before Docker mutations. Both the original
            # Compose and all active pointers/history must remain recoverable.
            invalid = candidate.replace("image: nginx:alpine", "image: ''", 1)
            failed = update_compose(invalid, expected_status=4)
            assert "neither an image nor a build context" in failed["DeploymentStatus"][-1]["Message"]
            assert set(controls()["Services"]) == {"web", "side"}
            assert controls()["Configuration"] == original_config
            assert "revision-one" in docker("exec", service("web")["Id"], "cat", "/etc/nginx/conf.d/default.conf")
            update_compose(compose)
            assert controls()["Configuration"] == original_config
            checks.append(f"failed service {operation} restores Compose and preserves ActiveRevision/history")

            side_id = service("side")["Id"]
            update_compose(candidate)
            current = controls()
            assert "web" not in current["Services"]
            assert current["Configuration"] == original_config
            assert not docker("ps", "-aq", "--filter", "label=com.docker.compose.project=" + project, "--filter", "label=com.docker.compose.service=web")
            assert service("side")["Id"] == side_id
            if operation == "rename":
                assert not any(m["Destination"] == "/etc/nginx/conf.d/default.conf" for m in service("api")["Mounts"])
            # Even the idempotent active revision must not report success for
            # a service absent from the current Compose.
            apply(first, expected=409)
            apply(second, rollback=True, expected=409)
            assert controls()["Configuration"] == original_config
            checks.append(f"service {operation} with ActiveRevision succeeds without resurrecting web or transferring its mount")
            if operation == "rename":
                update_compose(compose)
                assert "revision-one" in docker("exec", service("web")["Id"], "cat", "/etc/nginx/conf.d/default.conf")

        current = controls()["Configuration"]
        api("PUT", f"/stacks/{stack_id}/configurations/targets", {"Version": current["Version"], "Targets": []}, admin)
        retired = controls()["Configuration"]
        assert not retired["Targets"]
        assert retired["Revisions"] == current["Revisions"]
        assert "revision-one" in api("GET", f"/stacks/{stack_id}/configurations/{first['ID']}/content", token=viewer)["Content"]
        api("PUT", f"/stacks/{stack_id}/configurations/targets", {"Version": retired["Version"], "Targets": [{"ID": "web", "Service": "side", "Target": "/etc/nginx/conf.d/default.conf"}]}, admin, 409)
        api("PUT", f"/stacks/{stack_id}/configurations/targets", {"Version": retired["Version"], "Targets": [{"ID": "side-new", "Service": "side", "Target": "/etc/nginx/conf.d/default.conf"}]}, admin)
        replacement = upload('server { listen 80; location / { return 200 "replacement"; } }\n', target="side-new")
        apply(replacement)
        assert "replacement" in docker("exec", service("side")["Id"], "cat", "/etc/nginx/conf.d/default.conf")
        checks.append("missing-service target can retire; history remains readable, ID reuse is denied and a replacement target applies")
        print(json.dumps({"passed": checks}, indent=2))
    finally:
        # Delete only the exact random project created by this invocation.
        if stack_id and admin:
            try:
                api("DELETE", f"/stacks/{stack_id}?endpointId={endpoint}", token=admin, expected=204)
            except Exception:
                pass
        for cid in docker("ps", "-aq", "--filter", "label=com.docker.compose.project=" + project).splitlines():
            docker("rm", "-f", cid)
        for nid in docker("network", "ls", "-q", "--filter", "label=com.docker.compose.project=" + project).splitlines():
            docker("network", "rm", nid)
        subprocess.run(["docker", "rm", "-f", container_name], capture_output=True, timeout=60)
        for volume in (data_volume, config_volume):
            subprocess.run(["docker", "volume", "rm", volume], capture_output=True, timeout=60)


if __name__ == "__main__":
    main()
