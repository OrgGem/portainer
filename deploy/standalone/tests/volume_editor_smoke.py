"""Exercise quick text editing through a real Portainer 2.45.3 Agent.

All resources are disposable and randomly named. KEEP_UI is for a local manual
browser smoke; its generated receipt contains only synthetic test credentials.
"""
import concurrent.futures
import hashlib
import json
import os
from pathlib import Path
import secrets
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

from roles_smoke import docker


def main():
    name = "portainer-text-test-" + uuid.uuid4().hex[:10]
    server, agent, helper = [name + s for s in ("-server", "-agent", "-helper")]
    data, files, private = [name + s for s in ("-db", "-files", "-private")]
    network = name + "-net"
    setup = secrets.token_hex(32)
    base = ""
    keep = False
    checks = []

    def api(method, path, payload=None, token=None, expected=200):
        headers = {"Content-Type": "application/json"}
        if token:
            headers["Authorization"] = "Bearer " + token
        if path == "/users/admin/init":
            headers["X-Setup-Token"] = setup
        request = urllib.request.Request(base + "/api" + path, data=None if payload is None else json.dumps(payload).encode(), headers=headers, method=method)
        try:
            with urllib.request.urlopen(request, timeout=60) as response:
                status, body = response.status, response.read()
        except urllib.error.HTTPError as error:
            status, body = error.code, error.read()
        assert status == expected, f"{method} {path}: expected {expected}, got {status}: {body[:500]!r}"
        return json.loads(body) if body[:1] in (b"{", b"[") else body

    def write_file(path, data):
        assert path in ("/data/nested/app.json", "/data/app.conf")
        subprocess.run(["docker", "exec", "-i", helper, "sh", "-c", "cat > " + path], input=data, check=True, capture_output=True)

    try:
        docker("network", "create", network)
        for volume in (data, files, private):
            docker("volume", "create", volume)
        docker("run", "-d", "--name", helper, "--network", network, "-v", files + ":/data", "-v", private + ":/private", "alpine:3.20", "sleep", "1800")
        docker("exec", helper, "sh", "-c", "mkdir /data/nested; printf 'private' > /private/app.conf; printf '\\377\\000' > /data/binary.bin; dd if=/dev/zero of=/data/big.txt bs=1M count=2 2>/dev/null; touch /data/empty.conf")
        initial = b'{"message":"original long configuration text with trailing content"}\n'
        write_file("/data/nested/app.json", initial)
        write_file("/data/app.conf", b'\xef\xbb\xbfhello=world\r\nsecond=line\r\n')
        docker("exec", helper, "sh", "-c", "chown 1234:1235 /data/nested/app.json; chmod 640 /data/nested/app.json")
        docker("run", "-d", "--name", agent, "--network", network, "-v", "/var/run/docker.sock:/var/run/docker.sock", "-v", "/var/lib/docker/volumes:/var/lib/docker/volumes", "portainer/agent:2.45.3")
        docker("run", "-d", "--name", server, "--network", network, "-p", "127.0.0.1::9000", "-v", data + ":/data", os.environ.get("PORTAINER_TEST_IMAGE", "portainer-custom:2.45.3-volume-editor"), "--http-enabled", "--setup-token", setup)
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
        fields = {"Name": "Text editor test Agent", "EndpointCreationType": "2", "URL": "tcp://" + agent + ":9001", "TLS": "true", "TLSSkipVerify": "true", "TLSSkipClientVerify": "true"}
        boundary = uuid.uuid4().hex
        body = "".join(f'--{boundary}\r\nContent-Disposition: form-data; name="{key}"\r\n\r\n{value}\r\n' for key, value in fields.items()) + f"--{boundary}--\r\n"
        request = urllib.request.Request(base + "/api/endpoints", data=body.encode(), headers={"Authorization": "Bearer " + admin, "Content-Type": "multipart/form-data; boundary=" + boundary}, method="POST")
        with urllib.request.urlopen(request, timeout=120) as response:
            endpoint = json.load(response)["Id"]
        prefix = f"/endpoints/{endpoint}/docker"

        def file_url(path="/nested/app.json", volume=files):
            return prefix + "/v2/browse/text?" + urllib.parse.urlencode({"volumeID": volume, "path": path})

        original = api("GET", file_url(), token=admin)
        assert original["Content"].encode() == initial
        assert original["Version"] == hashlib.sha256(initial).hexdigest()
        result = api("PUT", file_url(), {"Content": '{"x":1}', "Version": original["Version"]}, admin)
        assert docker("exec", helper, "cat", "/data/nested/app.json") == '{"x":1}'
        assert docker("exec", helper, "stat", "-c", "%u:%g:%a", "/data/nested/app.json") == "1234:1235:640"
        assert result["Version"] == hashlib.sha256(b'{"x":1}').hexdigest()
        checks.append("real Agent save truncates shorter JSON and preserves file owner/group/mode")

        conf = api("GET", file_url("/app.conf"), token=admin)
        assert conf["Content"] == '\ufeffhello=world\r\nsecond=line\r\n'
        conf = api("PUT", file_url("/app.conf"), {"Content": '\ufeffshort=yes\r\n', "Version": conf["Version"]}, admin)
        raw = subprocess.check_output(["docker", "exec", helper, "cat", "/data/app.conf"])
        assert raw == b'\xef\xbb\xbfshort=yes\r\n'
        api("PUT", file_url("/app.conf"), {"Content": "", "Version": conf["Version"]}, admin)
        assert api("GET", file_url("/app.conf"), token=admin)["Content"] == ""
        assert api("GET", file_url("/empty.conf"), token=admin)["Content"] == ""
        checks.append("UTF-8 BOM/CRLF preserved and zero-byte files read/saved correctly")

        api("PUT", file_url(), {"Content": "{invalid", "Version": result["Version"]}, admin, expected=400)
        api("PUT", file_url(), {"Version": result["Version"]}, admin, expected=400)
        api("GET", file_url("/binary.bin"), token=admin, expected=415)
        api("GET", file_url("/big.txt"), token=admin, expected=413)
        api("GET", file_url("/../etc/passwd"), token=admin, expected=400)
        api("GET", prefix + "/v2/browse/text?path=/etc/passwd", token=admin, expected=400)
        api("GET", file_url(), expected=401)
        checks.append("invalid JSON, missing content, binary/oversized files and path traversal rejected")

        write_file("/data/nested/app.json", b'{"external":true}')
        api("PUT", file_url(), {"Content": '{"stale":true}', "Version": result["Version"]}, admin, expected=409)
        assert docker("exec", helper, "cat", "/data/nested/app.json") == '{"external":true}'
        snapshot = api("GET", file_url(), token=admin)

        def concurrent_save(index):
            request = urllib.request.Request(base + "/api" + file_url(), data=json.dumps({"Content": json.dumps({"writer": index}), "Version": snapshot["Version"]}).encode(), headers={"Authorization": "Bearer " + admin, "Content-Type": "application/json"}, method="PUT")
            try:
                with urllib.request.urlopen(request, timeout=60) as response:
                    return response.status
            except urllib.error.HTTPError as error:
                return error.code
        with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
            statuses = sorted(pool.map(concurrent_save, (1, 2)))
        assert statuses == [200, 409], statuses
        checks.append("external changes detected and simultaneous editor saves serialize to success/conflict")

        roles = {"environment-admin": 1, "standard": 3, "read-only": 4, "helpdesk": 2, "operator": 5}
        policies, tokens, memberships, users = {}, {}, {}, {}
        for role, role_id in roles.items():
            user = api("POST", "/users", {"Username": role, "Password": password, "Role": 2}, admin)["Id"]
            team = api("POST", "/teams", {"Name": role}, admin)["Id"]
            users[role] = user
            memberships[role] = api("POST", "/team_memberships", {"UserID": user, "TeamID": team, "Role": 2}, admin)["Id"]
            policies[str(team)] = {"RoleId": role_id, "StandaloneRole": role}
            tokens[role] = api("POST", "/auth", {"Username": role, "Password": password})["jwt"]
        legacy = api("POST", "/users", {"Username": "legacy-owner", "Password": password, "Role": 2}, admin)["Id"]
        legacy_token = api("POST", "/auth", {"Username": "legacy-owner", "Password": password})["jwt"]
        api("PUT", f"/endpoints/{endpoint}", {"TeamAccessPolicies": policies, "UserAccessPolicies": {str(legacy): {"RoleId": 1}}}, admin)
        api("PUT", f"/endpoints/{endpoint}/settings", {"allowVolumeBrowserForRegularUsers": True}, admin)
        daemon = api("GET", prefix + "/info", token=admin)["ID"]
        own_rc = api("POST", "/resource_controls", {"ResourceID": files + "_" + daemon, "Type": 3, "Users": [legacy]}, admin)["Id"]
        api("POST", "/resource_controls", {"ResourceID": private + "_" + daemon, "Type": 3, "AdministratorsOnly": True}, admin)
        for role in ("standard", "read-only", "helpdesk", "operator"):
            api("GET", file_url(), token=tokens[role], expected=403)
            api("PUT", file_url(), {"Content": "{}", "Version": snapshot["Version"]}, tokens[role], expected=403)
        ea = tokens["environment-admin"]
        current = api("GET", file_url(), token=ea)
        api("PUT", file_url(), {"Content": '{"edited":"environment-admin"}', "Version": current["Version"]}, ea)
        api("GET", prefix + "/v2/browse/get?path=/etc/hostname", token=ea, expected=403)
        api("GET", prefix + "/v2/browse/text?path=/etc/hostname", token=ea, expected=403)
        current = api("GET", file_url(), token=legacy_token)
        api("PUT", file_url(), {"Content": '{"edited":"volume-owner"}', "Version": current["Version"]}, legacy_token)
        api("GET", file_url("/app.conf", private), token=legacy_token, expected=403)
        api("PUT", f"/resource_controls/{own_rc}", {"AdministratorsOnly": True, "Public": False, "Users": [], "Teams": []}, admin)
        api("GET", file_url(), token=legacy_token, expected=403)
        api("DELETE", f"/team_memberships/{memberships['environment-admin']}", token=admin, expected=204)
        api("GET", file_url(), token=ea, expected=403)
        checks.append("environment admin/legacy volume ownership work; scoped read roles, host access and revoked memberships/ownership denied")
        print(json.dumps({"passed": checks, "agent": "2.45.3"}, indent=2))

        if os.environ.get("PORTAINER_VOLUME_EDITOR_KEEP_UI") == "1":
            receipt = Path('.tmp/validation/volume-editor-browser.json')
            receipt.write_text(json.dumps({"server": server, "agent": agent, "helper": helper, "volumes": [data, files, private], "network": network, "url": base, "password": password, "endpoint": endpoint, "volume": files}))
            keep = True
            print("Browser test receipt: " + str(receipt.resolve()))
    finally:
        if not keep:
            for container in (server, agent, helper):
                subprocess.run(["docker", "rm", "-f", container], capture_output=True)
            for volume in (data, files, private):
                subprocess.run(["docker", "volume", "rm", volume], capture_output=True)
            subprocess.run(["docker", "network", "rm", network], capture_output=True)


if __name__ == "__main__":
    main()
