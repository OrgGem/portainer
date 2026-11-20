"""Exercise Docker Agent entry permissions against a disposable Portainer server."""
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
    project = "portainer-agent-ping-" + uuid.uuid4().hex[:10]
    network = project + "-net"
    data = project + "-data"
    server = project + "-server"
    agent = project + "-agent"
    setup_token = secrets.token_hex(32)
    base = ""
    checks = []

    def request(method, path, payload=None, token=None, expected=200):
        headers = {"Content-Type": "application/json"}
        if path == "/users/admin/init":
            headers["X-Setup-Token"] = setup_token
        if token:
            headers["Authorization"] = "Bearer " + token
        body = None if payload is None else json.dumps(payload).encode()
        req = urllib.request.Request(base + "/api" + path, data=body, headers=headers, method=method)
        try:
            with urllib.request.urlopen(req, timeout=90) as response:
                status, content = response.status, response.read()
        except urllib.error.HTTPError as error:
            status, content = error.code, error.read()
        assert status == expected, f"{method} {path}: expected {expected}, got {status}: {content[:300]!r}"
        if content[:1] in (b"{", b"["):
            return json.loads(content)
        return content

    def create_agent_endpoint(name, group_id, admin):
        fields = {
            "Name": name,
            "EndpointCreationType": "2",
            "URL": "tcp://" + agent + ":9001",
            "GroupID": str(group_id),
            "TLS": "true",
            "TLSSkipVerify": "true",
            "TLSSkipClientVerify": "true",
        }
        boundary = uuid.uuid4().hex
        body = "".join(
            f'--{boundary}\r\nContent-Disposition: form-data; name="{key}"\r\n\r\n{value}\r\n'
            for key, value in fields.items()
        ) + f"--{boundary}--\r\n"
        req = urllib.request.Request(
            base + "/api/endpoints",
            data=body.encode(),
            headers={
                "Authorization": "Bearer " + admin,
                "Content-Type": "multipart/form-data; boundary=" + boundary,
            },
            method="POST",
        )
        with urllib.request.urlopen(req, timeout=120) as response:
            return json.load(response)

    try:
        docker("network", "create", network)
        docker("volume", "create", data)
        docker(
            "run", "-d", "--name", agent, "--network", network,
            "-v", "/var/run/docker.sock:/var/run/docker.sock",
            os.environ.get("PORTAINER_TEST_AGENT_IMAGE", "portainer/agent:2.45.3"),
        )
        docker(
            "run", "-d", "--name", server, "--network", network,
            "-p", "127.0.0.1::9000", "-v", data + ":/data",
            os.environ["PORTAINER_TEST_IMAGE"],
            "--http-enabled", "--setup-token", setup_token,
        )
        base = "http://" + docker("port", server, "9000/tcp")
        for _ in range(90):
            try:
                request("GET", "/status")
                break
            except (OSError, AssertionError):
                time.sleep(1)
        else:
            raise AssertionError("Portainer did not become ready")

        password = secrets.token_urlsafe(24)
        request("POST", "/users/admin/init", {"Username": "admin", "Password": password})
        admin = request("POST", "/auth", {"Username": "admin", "Password": password})["jwt"]

        for access_source in ("direct", "team"):
            user_name = project + "-" + access_source
            user = request("POST", "/users", {"Username": user_name, "Password": password, "Role": 2}, admin)
            token = request("POST", "/auth", {"Username": user_name, "Password": password})["jwt"]
            group = request("POST", "/endpoint_groups", {"Name": project + "-" + access_source}, admin)
            endpoint = create_agent_endpoint(project + "-" + access_source, group["Id"], admin)
            policy = {"RoleId": 5, "StandaloneRole": "operator"}

            if access_source == "direct":
                request("PUT", f"/endpoints/{endpoint['Id']}", {
                    "UserAccessPolicies": {str(user["Id"]): policy},
                }, admin)
            else:
                team = request("POST", "/teams", {"Name": project + "-operators"}, admin)
                request("POST", "/team_memberships", {
                    "UserID": user["Id"], "TeamID": team["Id"], "Role": 2,
                }, admin)
                request("PUT", f"/endpoint_groups/{group['Id']}", {
                    "TeamAccessPolicies": {str(team["Id"]): policy},
                }, admin)

            request("GET", f"/endpoints/{endpoint['Id']}?excludeSnapshot=true", token=token)
            request("GET", f"/endpoints/{endpoint['Id']}/docker/_ping", token=token)
            version = request("GET", f"/endpoints/{endpoint['Id']}/docker/version", token=token)
            info = request("GET", f"/endpoints/{endpoint['Id']}/docker/info", token=token)
            request("GET", f"/endpoints/{endpoint['Id']}/docker/ping", token=token, expected=204)
            request("GET", f"/docker/{endpoint['Id']}/dashboard", token=token)
            assert version.get("ApiVersion"), version
            assert info.get("ID"), info
            checks.append(f"{access_source} Operator opened Agent environment, version/info/ping/dashboard succeeded")

        print(json.dumps({"passed": checks, "agent": "2.45.3"}, indent=2))
    finally:
        for container in (server, agent):
            subprocess.run(["docker", "rm", "-f", container], capture_output=True, timeout=60)
        subprocess.run(["docker", "volume", "rm", data], capture_output=True, timeout=60)
        subprocess.run(["docker", "network", "rm", network], capture_output=True, timeout=60)


if __name__ == "__main__":
    main()
