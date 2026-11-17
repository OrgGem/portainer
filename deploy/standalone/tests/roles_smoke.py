"""Exercise scoped team roles through a disposable Portainer on real Docker.

Uses only randomly named test resources; requires a locally built image.
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
    project = "portainer-roles-test-" + uuid.uuid4().hex[:10]
    volume, server = project + "-data", project + "-server"
    setup_token = secrets.token_hex(32)
    base, admin = "", ""
    stacks, extras, checks = [], [], []

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
        if body and body[:1] in (b"{", b"["):
            return json.loads(body)
        return None

    try:
        docker("volume", "create", volume)
        docker("run", "-d", "--name", server, "-p", "127.0.0.1::9000", "-v", "/var/run/docker.sock:/var/run/docker.sock", "-v", volume + ":/data", os.environ.get("PORTAINER_TEST_IMAGE", "portainer-custom:2.45.3-roles"), "--http-enabled", "--setup-token", setup_token, "-H", "unix:///var/run/docker.sock")
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
            with urllib.request.urlopen(req, timeout=120) as result:
                endpoints = [json.load(result)]
        endpoint, group = endpoints[0]["Id"], endpoints[0]["GroupId"]
        proxy = f"/endpoints/{endpoint}/docker"
        users, teams, tokens, memberships = {}, {}, {}, {}
        roles = {"environment-admin": 1, "operator": 5, "helpdesk": 2, "standard": 3, "read-only": 4}
        for role in roles:
            users[role] = api("POST", "/users", {"Username": role, "Password": password, "Role": 2}, admin)["Id"]
            teams[role] = api("POST", "/teams", {"Name": role}, admin)["Id"]
            memberships[role] = api("POST", "/team_memberships", {"UserID": users[role], "TeamID": teams[role], "Role": 2}, admin)["Id"]
            tokens[role] = api("POST", "/auth", {"Username": role, "Password": password})["jwt"]
        team_policies = {str(teams[r]): {"RoleId": roles[r], "StandaloneRole": r} for r in roles if r != "read-only"}
        api("PUT", f"/endpoints/{endpoint}", {"TeamAccessPolicies": team_policies}, admin)

        def group_role(role):
            api("PUT", f"/endpoint_groups/{group}", {"TeamAccessPolicies": {str(teams['read-only']): {"RoleId": roles[role], "StandaloneRole": role}}}, admin)

        group_role("read-only")
        ea, op, hd, standard, ro = [tokens[r] for r in roles]
        for role in roles:
            profile = api("GET", f"/users/{users[role]}", token=tokens[role])
            assert profile["Role"] == 2
            assert profile["StandaloneEndpointRoles"][str(endpoint)] == role
            me = api("GET", "/users/me", token=tokens[role])
            assert me["StandaloneEndpointRoles"][str(endpoint)] == role
        baseline_stacks = {role: api("GET", f"/docker/{endpoint}/dashboard", token=tokens[role])["stacks"] for role in roles}
        compose = "services:\n  web:\n    image: nginx:alpine\n"
        for suffix in ("a", "b"):
            name = project + "-" + suffix
            stack = api("POST", f"/stacks/create/standalone/string?endpointId={endpoint}", {"Name": name, "StackFileContent": compose, "Env": []}, admin)
            stacks.append(stack["Id"])
            for _ in range(120):
                stack = api("GET", f"/stacks/{stacks[-1]}", token=admin)
                if stack["Status"] != 3:
                    break
                time.sleep(1)
            assert stack["Status"] == 1, stack.get("DeploymentStatus")
            if suffix == "a":
                control = stack["ResourceControl"]["Id"]
                api("PUT", f"/resource_controls/{control}", {"Public": False, "AdministratorsOnly": False, "Users": [], "Teams": [teams['read-only'], teams['standard']]}, admin)
        a, b = stacks
        ca, cb = [docker("ps", "-q", "--filter", "label=com.docker.compose.project=" + project + "-" + suffix) for suffix in ("a", "b")]
        assert ca and cb
        for role in roles:
            dashboard = api("GET", f"/docker/{endpoint}/dashboard", token=tokens[role])
            assert dashboard["stacks"] == baseline_stacks[role] + (1 if role in ("standard", "read-only") else 2), dashboard
            api("GET", f"/docker/{endpoint}/images", token=tokens[role])
        for token in (ro, standard):
            api("GET", f"/docker/{endpoint}/containers/{cb}/gpus", token=token, expected=403)
        api("POST", f"/docker/{endpoint}/containers/{cb}/recreate", {"PullImage": False}, standard, 403)
        api("POST", proxy + f"/commit?container={cb}", {}, standard, 403)
        checks.append("current-user roles, helper dashboard/image routes and inherited container ownership are enforced")
        for token in (ro, standard):
            visible = {item['Id'] for item in api("GET", "/stacks", token=token)}
            assert a in visible and b not in visible
            containers = {item['Id'][:12] for item in api("GET", proxy + "/containers/json?all=true", token=token)}
            assert ca[:12] in containers and cb[:12] not in containers
        for token in (hd, op, ea):
            assert {a, b}.issubset({item['Id'] for item in api("GET", "/stacks", token=token)})
            api("GET", f"/stacks/{b}", token=token)
        checks.append("team ownership and all-resource visibility match the five roles")
        for token in (ro, hd):
            for method, path, payload in [("POST", f"/containers/{ca}/restart", {}), ("POST", "/containers/create", {"Image": "nginx:alpine"}), ("DELETE", f"/containers/{ca}", None), ("POST", f"/containers/{ca}/exec", {"Cmd": ["true"]}), ("GET", f"/containers/{ca}/archive?path=/etc", None)]:
                api(method, proxy + path, payload, token, 403)
            api("POST", f"/stacks/{a}/stop?endpointId={endpoint}", {}, token, 403)
        api("POST", proxy + f"/containers/{ca}/restart", {}, standard, 204)
        api("POST", proxy + f"/containers/{cb}/restart", {}, standard, 403)
        api("POST", proxy + f"/containers/{cb}/restart", {}, op, 204)
        api("POST", proxy + f"/containers/{cb}/exec", {"Cmd": ["true"]}, op, 201)
        api("POST", proxy + "/containers/create", {"Image": "nginx:alpine"}, op, 403)
        api("DELETE", proxy + f"/containers/{cb}", token=op, expected=403)
        api("PUT", f"/stacks/{b}?endpointId={endpoint}", {"StackFileContent": compose}, op, 403)
        checks.append("Read-only and Helpdesk cannot mutate; Operator operates existing workloads; Standard obeys ownership")
        created = api("POST", proxy + "/containers/create?name=" + project + "-extra", {"Image": "nginx:alpine", "Labels": {"portainer.roles.test": project}}, ea, 201)["Id"]
        extras.append(created)
        api("DELETE", proxy + f"/containers/{created}", token=ea, expected=204)
        extras.remove(created)
        api("PUT", "/settings", {}, ea, 403)
        api("PUT", f"/endpoints/{endpoint}", {}, ea, 403)
        api("PUT", f"/resource_controls/{control}", {"Public": True}, ea, 403)
        api("PUT", f"/resource_controls/{control}", {"Public": True}, ro, 403)
        checks.append("Environment Admin manages Docker resources without becoming global admin or changing access grants")
        group_role("operator")
        api("POST", proxy + f"/containers/{cb}/restart", {}, ro, 204)
        group_role("read-only")
        api("POST", proxy + f"/containers/{cb}/restart", {}, ro, 403)
        api("PUT", f"/endpoints/{endpoint}", {"UserAccessPolicies": {str(users['read-only']): {"RoleId": 5, "StandaloneRole": "operator"}}}, admin)
        api("POST", proxy + f"/containers/{cb}/restart", {}, ro, 204)
        api("PUT", f"/endpoints/{endpoint}", {"UserAccessPolicies": {}}, admin)
        api("POST", proxy + f"/containers/{cb}/restart", {}, ro, 403)
        checks.append("role changes and direct-user precedence take effect with the existing JWT")
        api("PUT", f"/stacks/{a}/policy", {"Version": 0, "Grants": [{"TeamID": teams['read-only'], "Actions": ["view", "config.upload", "config.apply", "deploy", "edit"]}]}, admin)
        for method, path, payload in [("POST", f"/stacks/{a}/configurations", {}), ("POST", f"/stacks/{a}/configurations/1/apply", {}), ("PUT", f"/stacks/{a}", {"StackFileContent": compose})]:
            api(method, path, payload, ro, 403)
        permissions = api("GET", f"/stacks/{a}/controls", token=ea)["Permissions"]
        assert set(permissions) == {"view", "config.upload", "config.apply", "deploy", "edit"}, permissions
        assert api("GET", f"/stacks/{a}/controls", token=ro)["Permissions"] == ["view"]
        checks.append("stack action grants cannot elevate Read-only; Environment Admin can manage protected stacks")
        api("DELETE", f"/team_memberships/{memberships['read-only']}", token=admin, expected=204)
        api("GET", proxy + "/containers/json", token=ro, expected=403)
        assert a not in {item['Id'] for item in api("GET", "/stacks", token=ro)}
        checks.append("removing team membership revokes access without issuing a new JWT")
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
        for cid in extras:
            subprocess.run(["docker", "rm", "-f", cid], capture_output=True, timeout=60)
        subprocess.run(["docker", "rm", "-f", server], capture_output=True, timeout=60)
        subprocess.run(["docker", "volume", "rm", volume], capture_output=True, timeout=60)


if __name__ == "__main__":
    main()
