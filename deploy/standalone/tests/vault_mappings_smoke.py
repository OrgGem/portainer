"""Verify Vault references, inventory ACL/filtering and exported Agent templates.

Creates a disposable Portainer and test stacks on the current Docker daemon.
The nested Vault test uses synthetic secrets on its own private network.
"""
import json
import os
import pathlib
import secrets
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
import uuid


def docker(*args):
    return subprocess.check_output(["docker", *args], text=True, timeout=120).strip()


def main():
    project = "portainer-vault-ui-test-" + uuid.uuid4().hex[:10]
    volume, server = project + "-data", project + "-server"
    setup_token = secrets.token_hex(32)
    base, admin = "", ""
    stacks, checks = [], []

    def api(method, path, payload=None, token=None, expected=200):
        headers = {"Content-Type": "application/json"}
        if path == "/users/admin/init":
            headers["X-Setup-Token"] = setup_token
        if token:
            headers["Authorization"] = "Bearer " + token
        req = urllib.request.Request(base + "/api" + path, data=None if payload is None else json.dumps(payload).encode(), headers=headers, method=method)
        try:
            with urllib.request.urlopen(req, timeout=180) as result:
                code, body = result.status, result.read()
        except urllib.error.HTTPError as error:
            code, body = error.code, error.read()
        assert code == expected, f"{method} {path}: expected {expected}, got {code}: {body[:600]!r}"
        return json.loads(body) if body else None

    try:
        docker("volume", "create", volume)
        docker("run", "-d", "--name", server, "-p", "127.0.0.1::9000", "-v", "/var/run/docker.sock:/var/run/docker.sock", "-v", volume + ":/data", os.environ.get("PORTAINER_TEST_IMAGE", "portainer-custom:2.45.3-vault-ui"), "--http-enabled", "--setup-token", setup_token, "-H", "unix:///var/run/docker.sock")
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
            fields = {"Name": "vault-test", "EndpointCreationType": "1", "URL": "unix:///var/run/docker.sock"}
            body = "".join(f'--{boundary}\r\nContent-Disposition: form-data; name="{key}"\r\n\r\n{value}\r\n' for key, value in fields.items()) + f"--{boundary}--\r\n"
            req = urllib.request.Request(base + "/api/endpoints", data=body.encode(), headers={"Authorization": "Bearer " + admin, "Content-Type": "multipart/form-data; boundary=" + boundary}, method="POST")
            with urllib.request.urlopen(req, timeout=120) as result:
                endpoints = [json.load(result)]
        endpoint = endpoints[0]["Id"]
        readers = api("POST", "/teams", {"Name": "vault-readers"}, admin)["Id"]
        env_admins = api("POST", "/teams", {"Name": "vault-admins"}, admin)["Id"]
        viewer_id = api("POST", "/users", {"Username": "viewer", "Password": password, "Role": 2}, admin)["Id"]
        editor_id = api("POST", "/users", {"Username": "editor", "Password": password, "Role": 2}, admin)["Id"]
        membership = api("POST", "/team_memberships", {"UserID": viewer_id, "TeamID": readers, "Role": 2}, admin)["Id"]
        api("POST", "/team_memberships", {"UserID": editor_id, "TeamID": env_admins, "Role": 2}, admin)
        viewer = api("POST", "/auth", {"Username": "viewer", "Password": password})["jwt"]
        editor = api("POST", "/auth", {"Username": "editor", "Password": password})["jwt"]
        api("PUT", f"/endpoints/{endpoint}", {"TeamAccessPolicies": {str(readers): {"RoleId": 4, "StandaloneRole": "read-only"}, str(env_admins): {"RoleId": 1, "StandaloneRole": "environment-admin"}}}, admin)
        for suffix in ("a", "b"):
            stack = api("POST", f"/stacks/create/standalone/string?endpointId={endpoint}", {"Name": project + "-" + suffix, "StackFileContent": "services:\n  web:\n    image: nginx:alpine\n", "Env": [{"name": "PRIVATE_ENV", "value": "must-not-appear-in-vault-inventory"}]}, admin)
            stacks.append(stack["Id"])
            for _ in range(120):
                stack = api("GET", f"/stacks/{stacks[-1]}", token=admin)
                if stack["Status"] != 3:
                    break
                time.sleep(1)
            assert stack["Status"] == 1
            if suffix == "a":
                api("PUT", f"/resource_controls/{stack['ResourceControl']['Id']}", {"Public": False, "AdministratorsOnly": False, "Users": [], "Teams": [readers]}, admin)
        a, b = stacks
        mappings = [
            {"ID": "password", "Service": "web", "Variable": "DB_PASSWORD", "Path": "kv/data/apps/demo", "Key": "DB_PASSWORD", "Engine": "kv-v2"},
            {"ID": "username", "Service": "web", "Variable": "DB_USER", "Path": "kv/data/apps/demo", "Key": "username", "Engine": "kv-v2"},
        ]
        private_mapping = {"ID": "private", "Service": "web", "Variable": "PRIVATE_VARIABLE", "Path": "kv/data/private-path", "Key": "private", "Engine": "kv-v2"}
        saved = api("PUT", f"/stacks/{a}/vault", {"Version": 0, "Mappings": mappings}, editor)
        assert saved["Version"] == 1 and saved["UpdatedBy"] == editor_id
        api("PUT", f"/stacks/{b}/vault", {"Version": 0, "Mappings": [private_mapping]}, admin)
        api("PUT", f"/stacks/{a}/vault", {"Version": 0, "Mappings": []}, editor, 409)
        api("PUT", f"/stacks/{a}/vault", {"Version": 1, "Mappings": []}, viewer, 403)
        for field in ("Value", "Token", "Secret"):
            rejected = {**mappings[0], field: "synthetic-value-must-not-be-stored"}
            result = api("PUT", f"/stacks/{a}/vault", {"Version": 1, "Mappings": [rejected]}, editor, 400)
            assert "synthetic-value" not in json.dumps(result)
        checks.append("Environment Admin manages versioned references; Read-only cannot write; credential/value fields are rejected")
        inventory = api("GET", "/stacks/vault/mappings?limit=1", token=viewer)
        assert len(inventory["Items"]) == 1 and inventory["Summary"]["Mappings"] == 2
        assert inventory["Summary"]["Paths"] == 1 and inventory["Summary"]["Stacks"] == 1
        assert [item["ID"] for item in inventory["Stacks"]] == [a]
        for forbidden in ("private-path", "PRIVATE_VARIABLE", "PRIVATE_ENV", "must-not-appear"):
            assert forbidden not in json.dumps(inventory)
        assert api("GET", "/stacks/vault/mappings", token=admin)["Summary"]["Mappings"] == 3
        for query in (f"environmentId={endpoint}&stackId={a}&variable=password&path=APPS&key=DB_PASSWORD&service=WEB", "variable=DB_PASSWORD"):
            filtered = api("GET", "/stacks/vault/mappings?" + query, token=viewer)
            assert filtered["Summary"]["Mappings"] == 1 and filtered["Items"][0]["Variable"] == "DB_PASSWORD"
        assert api("GET", f"/stacks/vault/mappings?stackId={b}", token=viewer)["Summary"]["Mappings"] == 0
        api("GET", f"/stacks/{b}/vault", token=viewer, expected=403)
        api("GET", f"/stacks/{b}/vault/template?service=web", token=viewer, expected=403)
        checks.append("inventory combines filters, paginates after counting and excludes private stack metadata from rows, options and totals")
        api("PUT", f"/stacks/{a}/policy", {"Version": 0, "Grants": []}, admin)
        assert api("GET", "/stacks/vault/mappings", token=viewer)["Summary"]["Mappings"] == 0
        api("GET", f"/stacks/{a}/vault", token=viewer, expected=403)
        api("PUT", f"/stacks/{a}/policy", {"Version": 1, "Grants": [{"TeamID": readers, "Actions": ["view"]}]}, admin)
        api("PUT", f"/stacks/{a}/vault", {"Version": 1, "Mappings": [mappings[0]]}, editor)
        assert api("GET", f"/stacks/{a}/vault", token=viewer)["CanEdit"] is False
        exported = api("GET", f"/stacks/{a}/vault/template?service=web", token=viewer)
        assert exported["Version"] == 2 and "env_template" in exported["Content"] and "DB_USER" not in exported["Content"]
        checks.append("stack view policy also guards inventory and template export; removing mappings updates counts and exports")
        docker("restart", server)
        base = "http://" + docker("port", server, "9000/tcp")
        for _ in range(60):
            try:
                api("GET", "/status")
                break
            except (OSError, AssertionError):
                time.sleep(1)
        # CE invalidates JWT sessions when the server restarts. Reauthenticate
        # to verify persisted references, then test revocation with this JWT.
        admin = api("POST", "/auth", {"Username": "admin", "Password": password})["jwt"]
        viewer = api("POST", "/auth", {"Username": "viewer", "Password": password})["jwt"]
        assert api("GET", f"/stacks/{a}/vault", token=viewer)["Configuration"]["Version"] == 2
        api("DELETE", f"/team_memberships/{membership}", token=admin, expected=204)
        assert api("GET", "/stacks/vault/mappings", token=viewer)["Summary"]["Mappings"] == 0
        api("GET", f"/stacks/{a}/vault", token=viewer, expected=403)
        checks.append("references survive restart and membership revocation removes inventory access using the same JWT")
        with tempfile.TemporaryDirectory(prefix=project) as directory:
            template_path = pathlib.Path(directory, "exported.hcl")
            template_path.write_text(exported["Content"])
            env = {**os.environ, "VAULT_ENV_TEMPLATE_FILE": str(template_path)}
            script = pathlib.Path(__file__).resolve().parents[2] / "vault-example" / "smoke.py"
            subprocess.run([sys.executable, str(script)], env=env, check=True, timeout=240)
        checks.append("the exported template works with real Vault AppRole/Agent injection and secret rotation")
        print(json.dumps({"passed": checks}, indent=2))
    finally:
        for stack in stacks:
            if admin:
                try:
                    api("DELETE", f"/stacks/{stack}?endpointId={endpoint}", token=admin, expected=204)
                except Exception:
                    pass
        for suffix in ("a", "b"):
            label = "label=com.docker.compose.project=" + project + "-" + suffix
            for cid in docker("ps", "-aq", "--filter", label).splitlines():
                docker("rm", "-f", cid)
            for nid in docker("network", "ls", "-q", "--filter", label).splitlines():
                docker("network", "rm", nid)
        subprocess.run(["docker", "rm", "-f", server], capture_output=True, timeout=60)
        subprocess.run(["docker", "volume", "rm", volume], capture_output=True, timeout=60)


if __name__ == "__main__":
    main()
