"""Exercise deploy-time Vault injection against isolated Docker resources.

This creates a disposable Portainer server, Vault dev server, standard Portainer
Agent, and two temporary Compose stacks on the current Docker daemon. It uses
only synthetic AppRole credentials and secrets. Existing workloads are never
modified.
"""

import json
import os
import secrets
import subprocess
import time
import urllib.error
import urllib.request
import uuid


def docker(*args, input_text=None, timeout=120):
    result = subprocess.run(
        ["docker", *args],
        input=input_text,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        timeout=timeout,
        check=False,
    )
    if result.returncode != 0:
        raise RuntimeError(f"docker command failed ({result.returncode}): {result.stderr[:600]}")
    return result.stdout.strip()


def main():
    project = "portainer-vault-deploy-" + uuid.uuid4().hex[:10]
    network = project + "-net"
    data_volume = project + "-data"
    config_volume = project + "-configs"
    portainer_name = project + "-server"
    vault_name = project + "-vault"
    agent_name = project + "-agent"
    setup_token = secrets.token_hex(32)
    admin = ""
    base = ""
    vault_address = "https://" + vault_name + ":8200"
    vault_ca_path = None
    local_endpoint = None
    agent_endpoint = None
    stacks = []
    checks = []

    def api(method, path, payload=None, token=None, expected=200):
        headers = {"Content-Type": "application/json"}
        if path == "/users/admin/init":
            headers["X-Setup-Token"] = setup_token
        if token:
            headers["Authorization"] = "Bearer " + token
        request = urllib.request.Request(
            base + "/api" + path,
            data=None if payload is None else json.dumps(payload).encode(),
            headers=headers,
            method=method,
        )
        try:
            with urllib.request.urlopen(request, timeout=300) as response:
                code, body = response.status, response.read()
        except urllib.error.HTTPError as error:
            code, body = error.code, error.read()
        assert code == expected, f"{method} {path} returned {code}, expected {expected}"
        return json.loads(body) if body else None

    def multipart(path, fields, uploaded_file=None):
        boundary = uuid.uuid4().hex
        content = "".join(
            f'--{boundary}\r\nContent-Disposition: form-data; name="{key}"\r\n\r\n{value}\r\n'
            for key, value in fields.items()
        )
        if uploaded_file:
            filename, file_content = uploaded_file
            content += (
                f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="{filename}"\r\n'
                "Content-Type: application/yaml\r\n\r\n"
                f"{file_content}\r\n"
            )
        content += f"--{boundary}--\r\n"
        request = urllib.request.Request(
            base + "/api" + path,
            data=content.encode(),
            headers={
                "Authorization": "Bearer " + admin,
                "Content-Type": "multipart/form-data; boundary=" + boundary,
            },
            method="POST",
        )
        with urllib.request.urlopen(request, timeout=180) as response:
            return json.load(response)

    def vault(*args, input_text=None):
        return docker(
            "exec",
            "-i",
            "-e",
            "VAULT_ADDR=https://127.0.0.1:8200",
            "-e",
            "VAULT_CACERT=" + vault_ca_path,
            "-e",
            "VAULT_TOKEN=synthetic-root-token",
            vault_name,
            "vault",
            *args,
            input_text=input_text,
        )

    def create_role(name, policy):
        vault("policy", "write", name, "-", input_text=policy)
        vault("write", f"auth/approle/role/{name}", f"token_policies={name}", "token_ttl=1h")
        role_id = vault("read", "-field=role_id", f"auth/approle/role/{name}/role-id")
        secret_id = vault("write", "-f", "-field=secret_id", f"auth/approle/role/{name}/secret-id")
        return role_id, secret_id

    def configure_vault(endpoint_id, role):
        role_id, secret_id = role
        ca_pem = docker("exec", vault_name, "cat", vault_ca_path)
        api(
            "PUT",
            f"/endpoints/{endpoint_id}/vault",
            {
                "Address": vault_address,
                "AuthMount": "approle",
                "RoleID": role_id,
                "SecretID": secret_id,
                "CAPEM": ca_pem,
            },
            admin,
        )
        configuration = api("GET", f"/endpoints/{endpoint_id}/vault", token=admin)
        assert configuration["HasSecretID"] is True
        assert "SecretID" not in configuration
        assert secret_id not in json.dumps(configuration)

    def service(stack_project, service_name):
        matches = docker(
            "ps",
            "-aq",
            "--filter",
            "label=com.docker.compose.project=" + stack_project,
            "--filter",
            "label=com.docker.compose.service=" + service_name,
        ).splitlines()
        assert len(matches) == 1
        return json.loads(docker("inspect", matches[0]))[0]

    def wait_for_file(container_id, expected):
        for _ in range(45):
            try:
                actual = docker("exec", container_id, "cat", "/tmp/injected.txt")
                if actual == expected:
                    return
            except RuntimeError:
                pass
            time.sleep(1)
        assert False, "the application did not receive the expected Vault value"

    def wait_for_stack(stack_id, expected_status=1):
        for _ in range(180):
            stack = api("GET", f"/stacks/{stack_id}", token=admin)
            if stack["Status"] != 3:
                break
            time.sleep(1)
        assert stack["Status"] == expected_status, "unexpected Compose deployment status"
        return stack

    def update_stack(stack_id, endpoint_id, compose, expected_status=1):
        api(
            "PUT",
            f"/stacks/{stack_id}?endpointId={endpoint_id}",
            {"StackFileContent": compose, "Env": [], "Prune": True},
            admin,
        )
        return wait_for_stack(stack_id, expected_status)

    def create_stack(name, endpoint_id, path, engine, upload=False):
        compose = (
            "services:\n"
            "  app:\n"
            "    image: python:3.12-slim\n"
            "    command: [python, -c, \"import os,time; open('/tmp/injected.txt','w').write(os.environ['DB_PASSWORD']); time.sleep(3600)\"]\n"
            "  side:\n"
            "    image: nginx:alpine\n"
        )
        mappings = [{
            "ID": uuid.uuid4().hex,
            "Service": "app",
            "Variable": "DB_PASSWORD",
            "Path": path,
            "Key": "DB_PASSWORD",
            "Engine": engine,
        }]
        if upload:
            stack = multipart(
                f"/stacks/create/standalone/file?endpointId={endpoint_id}",
                {
                    "Name": name,
                    "Env": "[]",
                    "VaultMappings": json.dumps({"Mappings": mappings, "InjectOnDeploy": True}),
                },
                uploaded_file=("compose.yaml", compose),
            )
        else:
            stack = api(
                "POST",
                f"/stacks/create/standalone/string?endpointId={endpoint_id}",
                {
                    "Name": name,
                    "StackFileContent": compose,
                    "Env": [],
                    "VaultMappings": {"Mappings": mappings, "InjectOnDeploy": True},
                },
                admin,
            )
        stacks.append((stack["Id"], endpoint_id, name, compose, mappings))
        wait_for_stack(stack["Id"])
        return stack["Id"], name, compose, mappings

    try:
        docker("network", "create", "--label", "com.docker.compose.project=" + project, network)
        docker("volume", "create", data_volume)
        docker("volume", "create", config_volume)
        config_root = json.loads(docker("volume", "inspect", config_volume))[0]["Mountpoint"]

        docker(
            "run", "-d", "--name", vault_name, "--network", network, "--network-alias", vault_name,
            "hashicorp/vault:1.21.0", "server", "-dev", "-dev-listen-address", "0.0.0.0:8200",
            "-dev-root-token-id", "synthetic-root-token", "-dev-tls", "-dev-tls-san", vault_name,
        )
        # Vault's TLS development mode creates a unique CA directory beneath /tmp.
        for _ in range(60):
            try:
                vault_ca_path = docker("exec", vault_name, "sh", "-c", "find /tmp -name vault-ca.pem -print -quit")
                if vault_ca_path:
                    break
            except RuntimeError:
                pass
            time.sleep(1)
        assert vault_ca_path, "the Vault dev TLS CA was not created"
        for _ in range(60):
            try:
                vault("status")
                break
            except RuntimeError:
                time.sleep(1)
        else:
            assert False, "the isolated Vault dev server did not become ready"

        vault("auth", "enable", "approle")
        vault("secrets", "enable", "-path=kv", "kv-v2")
        vault("secrets", "enable", "-path=legacy", "kv")
        vault("kv", "put", "-mount=kv", "apps/backend", "DB_PASSWORD=local-initial")
        vault("kv", "put", "-mount=kv", "apps/forbidden", "DB_PASSWORD=not-readable")
        vault("kv", "put", "-mount=legacy", "apps/agent", "DB_PASSWORD=agent-initial")
        local_role = create_role(
            "local-app",
            'path "kv/data/apps/backend" { capabilities = ["read"] }\n',
        )
        agent_role = create_role(
            "agent-app",
            'path "legacy/apps/agent" { capabilities = ["read"] }\n',
        )

        docker(
            "run", "-d", "--name", portainer_name, "--network", network,
            "-p", "127.0.0.1::9000", "-v", "/var/run/docker.sock:/var/run/docker.sock",
            "-v", data_volume + ":/data", "-v", config_volume + ":" + config_root,
            "-e", "PORTAINER_STACK_CONFIG_ROOT=" + config_root,
            os.environ.get("PORTAINER_TEST_IMAGE", "portainer-custom:2.45.3-vault-deploy"),
            "--http-enabled", "--setup-token", setup_token, "-H", "unix:///var/run/docker.sock",
        )
        base = "http://" + docker("port", portainer_name, "9000/tcp")
        for _ in range(90):
            try:
                api("GET", "/status")
                break
            except (OSError, AssertionError):
                time.sleep(1)
        else:
            assert False, "the isolated Portainer server did not become ready"

        password = secrets.token_urlsafe(24)
        api("POST", "/users/admin/init", {"Username": "admin", "Password": password})
        admin = api("POST", "/auth", {"Username": "admin", "Password": password})["jwt"]
        endpoints = api("GET", "/endpoints", token=admin)
        local = next((item for item in endpoints if item["Type"] == 1), None)
        if local is None:
            local = multipart("/endpoints", {
                "Name": project + "-local",
                "EndpointCreationType": "1",
                "URL": "unix:///var/run/docker.sock",
            })
        local_endpoint = local["Id"]

        member_password = secrets.token_urlsafe(24)
        member = api("POST", "/users", {"Username": project + "-member", "Password": member_password, "Role": 2}, admin)
        member_token = api("POST", "/auth", {"Username": member["Username"], "Password": member_password})["jwt"]
        api("GET", f"/endpoints/{local_endpoint}/vault", token=member_token, expected=403)
        api("PUT", f"/endpoints/{local_endpoint}/vault", {"Address": "https://vault.invalid", "RoleID": "role", "SecretID": "secret"}, member_token, expected=403)

        docker(
            "run", "-d", "--name", agent_name, "--network", network,
            "-v", "/var/run/docker.sock:/var/run/docker.sock",
            "-v", "/var/lib/docker/volumes:/var/lib/docker/volumes",
            os.environ.get("PORTAINER_AGENT_TEST_IMAGE", "portainer/agent:2.45.3"),
        )
        agent = None
        for _ in range(90):
            try:
                agent = multipart("/endpoints", {
                    "Name": project + "-agent",
                    "EndpointCreationType": "2",
                    "URL": "tcp://" + agent_name + ":9001",
                    "TLS": "true",
                    "TLSSkipVerify": "true",
                    "TLSSkipClientVerify": "true",
                })
                break
            except Exception:
                time.sleep(1)
        assert agent is not None
        agent_endpoint = agent["Id"]

        configure_vault(local_endpoint, local_role)
        configure_vault(agent_endpoint, agent_role)
        local_id, local_project, local_compose, local_mappings = create_stack(
            project + "-local-app", local_endpoint, "kv/data/apps/backend", "kv-v2"
        )
        agent_id, agent_project, agent_compose, _ = create_stack(
            project + "-agent-app", agent_endpoint, "legacy/apps/agent", "kv-v1", upload=True
        )

        local_container = service(local_project, "app")
        agent_container = service(agent_project, "app")
        wait_for_file(local_container["Id"], "local-initial")
        wait_for_file(agent_container["Id"], "agent-initial")
        for container, expected in ((local_container, "local-initial"), (agent_container, "agent-initial")):
            env_values = container["Config"]["Env"]
            assert "DB_PASSWORD=" + expected in env_values
            assert container["Config"]["Labels"]["io.portainer.vault.env"] == "DB_PASSWORD"
            stack_id = local_id if container == local_container else agent_id
            response = api("GET", f"/stacks/{stack_id}", token=admin)
            assert expected not in json.dumps(response)
            env_file = f"/data/compose/{stack_id}/stack.env"
            exists = subprocess.run(
                ["docker", "exec", portainer_name, "sh", "-c", "test -f " + env_file],
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
                check=False,
            ).returncode == 0
            if exists:
                assert expected not in docker("exec", portainer_name, "cat", env_file)
        checks.append("Portainer Server resolved distinct environment AppRoles and injected KV v2 locally and KV v1 through the official Agent")

        vault("kv", "put", "-mount=kv", "apps/backend", "DB_PASSWORD=local-rotated")
        vault("kv", "put", "-mount=legacy", "apps/agent", "DB_PASSWORD=agent-rotated")
        assert "DB_PASSWORD=local-initial" in service(local_project, "app")["Config"]["Env"]
        assert "DB_PASSWORD=agent-initial" in service(agent_project, "app")["Config"]["Env"]
        docker("restart", local_container["Id"])
        docker("restart", agent_container["Id"])
        wait_for_file(local_container["Id"], "local-initial")
        wait_for_file(agent_container["Id"], "agent-initial")
        update_stack(local_id, local_endpoint, local_compose)
        update_stack(agent_id, agent_endpoint, agent_compose)
        local_container = service(local_project, "app")
        agent_container = service(agent_project, "app")
        wait_for_file(local_container["Id"], "local-rotated")
        wait_for_file(agent_container["Id"], "agent-rotated")
        checks.append("changing Vault leaves running and restarted containers on their original environment until each stack is redeployed")

        side_before = service(local_project, "side")["Id"]
        api("PUT", f"/stacks/{local_id}/policy", {"Version": 0, "Grants": []}, admin)
        api("PUT", f"/stacks/{local_id}/configurations/targets", {
            "Version": 0,
            "Targets": [{"ID": "app-config", "Service": "app", "Target": "/tmp/app.conf"}],
        }, admin)

        def controls():
            return api("GET", f"/stacks/{local_id}/controls", token=admin)

        def upload(content):
            return api("POST", f"/stacks/{local_id}/configurations", {
                "Version": controls()["Configuration"]["Version"],
                "TargetID": "app-config",
                "Filename": "app.conf",
                "Content": content,
            }, admin)

        def apply(revision, rollback=False):
            action = "rollback" if rollback else "apply"
            return api("POST", f"/stacks/{local_id}/configurations/{revision['ID']}/{action}", {
                "Version": controls()["Configuration"]["Version"],
            }, admin)

        revision_one = upload("configuration-one\n")
        vault("kv", "put", "-mount=kv", "apps/backend", "DB_PASSWORD=apply-current")
        apply(revision_one)
        local_container = service(local_project, "app")
        wait_for_file(local_container["Id"], "apply-current")
        assert docker("exec", local_container["Id"], "cat", "/tmp/app.conf") == "configuration-one"
        assert service(local_project, "side")["Id"] == side_before

        revision_two = upload("configuration-two\n")
        vault("kv", "put", "-mount=kv", "apps/backend", "DB_PASSWORD=rollback-current")
        apply(revision_two)
        local_container = service(local_project, "app")
        wait_for_file(local_container["Id"], "rollback-current")
        vault("kv", "put", "-mount=kv", "apps/backend", "DB_PASSWORD=after-rollback")
        apply(revision_one, rollback=True)
        local_container = service(local_project, "app")
        wait_for_file(local_container["Id"], "after-rollback")
        assert docker("exec", local_container["Id"], "cat", "/tmp/app.conf") == "configuration-one"
        assert service(local_project, "side")["Id"] == side_before
        checks.append("service-only configuration Apply and Rollback each re-read the current Vault value and preserve the other service")

        current = api("GET", f"/stacks/{local_id}/vault", token=admin)["Configuration"]
        blocked_mapping = [dict(local_mappings[0], Path="kv/data/apps/forbidden")]
        api("PUT", f"/stacks/{local_id}/vault", {
            "Version": current["Version"], "Mappings": blocked_mapping, "InjectOnDeploy": True,
        }, admin)
        original_id = service(local_project, "app")["Id"]
        failed = update_stack(local_id, local_endpoint, local_compose, expected_status=4)
        assert service(local_project, "app")["Id"] == original_id
        failed = update_stack(local_id, local_endpoint, local_compose, expected_status=4)
        assert service(local_project, "app")["Id"] == original_id
        assert "secret" not in failed.get("DeploymentStatus", [{}])[-1].get("Message", "").lower()
        checks.append("a denied Vault read, including a retry that normally undeploys first, keeps the active service available")

        current = api("GET", f"/stacks/{local_id}/vault", token=admin)["Configuration"]
        api("PUT", f"/stacks/{local_id}/vault", {
            "Version": current["Version"], "Mappings": local_mappings, "InjectOnDeploy": True,
        }, admin)
        update_stack(local_id, local_endpoint, local_compose)
        wait_for_file(service(local_project, "app")["Id"], "after-rollback")
        checks.append("restoring a valid mapping re-deploys cleanly after a failed retry")

        print(json.dumps({"passed": checks}, indent=2))
    finally:
        for stack_id, endpoint_id, _, _, _ in stacks:
            if admin:
                try:
                    api("DELETE", f"/stacks/{stack_id}?endpointId={endpoint_id}", token=admin, expected=204)
                except Exception:
                    pass
        for _, _, stack_project, _, _ in stacks:
            label = "label=com.docker.compose.project=" + stack_project
            for container_id in docker("ps", "-aq", "--filter", label).splitlines():
                try:
                    docker("rm", "-f", container_id)
                except RuntimeError:
                    pass
            for network_id in docker("network", "ls", "-q", "--filter", label).splitlines():
                try:
                    docker("network", "rm", network_id)
                except RuntimeError:
                    pass
        for container_name in (portainer_name, agent_name, vault_name):
            try:
                docker("rm", "-f", container_name)
            except RuntimeError:
                pass
        for volume_name in (data_volume, config_volume):
            try:
                docker("volume", "rm", volume_name)
            except RuntimeError:
                pass
        try:
            docker("network", "rm", network)
        except RuntimeError:
            pass


if __name__ == "__main__":
    main()
