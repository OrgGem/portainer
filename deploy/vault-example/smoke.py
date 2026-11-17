"""Verify AppRole authentication, runtime injection and rotation in disposable containers.

Uses a development Vault on an isolated Docker network, with synthetic secrets.
Requires the portainer-vault-app:example image. No Vault ports are published.
"""
import hashlib
import json
import os
import pathlib
import secrets
import subprocess
import tempfile
import time
import uuid


def run(*args, input=None):
    return subprocess.check_output(["docker", *args], input=input, text=True, timeout=120).strip()


def main():
    prefix = "portainer-vault-test-" + uuid.uuid4().hex[:10]
    vault_name, app_name = prefix + "-vault", prefix + "-app"
    token = secrets.token_urlsafe(24)

    def vault(*args, input=None):
        return run("exec", "-i", "-e", "VAULT_TOKEN=" + token, "-e", "VAULT_ADDR=http://127.0.0.1:8200", vault_name, "vault", *args, input=input)

    probe = """import pathlib,hashlib
for p in pathlib.Path('/proc').glob('[0-9]*/cmdline'):
 try:
  cmd=p.read_bytes()
  if cmd.startswith(b'/usr/local/bin/python\\x00/app/server.py'):
   env=(p.parent/'environ').read_bytes().split(b'\\x00')
   v=next(e.split(b'=',1)[1] for e in env if e.startswith(b'DB_PASSWORD='))
   print(hashlib.sha256(v).hexdigest())
 except (OSError,StopIteration): pass
"""

    def wait_value(value):
        digest = hashlib.sha256(value.encode()).hexdigest()
        for _ in range(50):
            if run("exec", app_name, "python", "-c", probe) == digest:
                return
            time.sleep(2)
        raise AssertionError("Vault Agent did not deliver the expected secret to its child process")

    try:
        run("network", "create", prefix)
        run("run", "-d", "--name", vault_name, "--network", prefix, "--network-alias", "vault", "-e", "VAULT_DEV_ROOT_TOKEN_ID=" + token, "hashicorp/vault:1.21.0", "server", "-dev", "-dev-listen-address=0.0.0.0:8200")
        for _ in range(30):
            try:
                vault("status")
                break
            except subprocess.CalledProcessError:
                time.sleep(1)
        vault("secrets", "enable", "-path=kv", "kv-v2")
        vault("auth", "enable", "approle")
        policy = pathlib.Path(__file__).with_name("app-policy.hcl").read_text()
        vault("policy", "write", "demo-app", "-", input=policy)
        vault("write", "auth/approle/role/demo-app", "token_policies=demo-app", "token_ttl=10m", "token_max_ttl=1h", "secret_id_ttl=10m")
        role = json.loads(vault("read", "-format=json", "auth/approle/role/demo-app/role-id"))["data"]["role_id"]
        secret = json.loads(vault("write", "-f", "-format=json", "auth/approle/role/demo-app/secret-id"))["data"]["secret_id"]
        first, second = secrets.token_urlsafe(24), secrets.token_urlsafe(24)
        vault("kv", "put", "kv/apps/demo", "DB_PASSWORD=" + first)
        vault("kv", "put", "kv/apps/other", "DB_PASSWORD=forbidden-test-value")
        app_token = json.loads(vault("write", "-format=json", "auth/approle/login", "role_id=" + role, "secret_id=" + secret))["auth"]["client_token"]
        denied = subprocess.run(["docker", "exec", "-e", "VAULT_TOKEN=" + app_token, "-e", "VAULT_ADDR=http://127.0.0.1:8200", vault_name, "vault", "kv", "get", "kv/apps/other"], capture_output=True, text=True, timeout=30)
        assert denied.returncode != 0 and "permission denied" in denied.stderr.lower()
        with tempfile.TemporaryDirectory(prefix=prefix) as directory:
            pathlib.Path(directory, "vault_role_id").write_text(role)
            pathlib.Path(directory, "vault_secret_id").write_text(secret)
            command = []
            entrypoint = []
            if os.environ.get("VAULT_ENV_TEMPLATE_FILE"):
                original = pathlib.Path(__file__).with_name("agent.hcl").read_text()
                exported = pathlib.Path(os.environ["VAULT_ENV_TEMPLATE_FILE"]).read_text()
                combined = original[:original.index('env_template "DB_PASSWORD"')] + exported + original[original.index("\nexec {"):]
                pathlib.Path(directory, "agent.hcl").write_text(combined)
                entrypoint = ["--entrypoint", "vault"]
                command = ["agent", "-config=/run/secrets/agent.hcl"]
            run("run", "-d", "--name", app_name, "--network", prefix, "--read-only", "--tmpfs", "/tmp:uid=10001,gid=10001,mode=0700", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "-v", directory + ":/run/secrets:ro", "-e", "VAULT_ADDR=http://vault:8200", "-e", "DB_HOST=database.internal", *entrypoint, "portainer-vault-app:example", *command)
            wait_value(first)
            inspect = json.loads(run("inspect", app_name))[0]
            assert not any(value.startswith("DB_PASSWORD=") for value in inspect["Config"]["Env"])
            vault("kv", "put", "kv/apps/demo", "DB_PASSWORD=" + second)
            wait_value(second)
            print(json.dumps({"passed": ["application AppRole reads only its configured Vault path", "Agent injects DB_PASSWORD into its child without putting it in Docker Config.Env", "rotation restarts the application with the updated secret"]}, indent=2))
            run("rm", "-f", app_name)
    finally:
        for name in (app_name, vault_name):
            subprocess.run(["docker", "rm", "-f", name], capture_output=True, timeout=60)
        subprocess.run(["docker", "network", "rm", prefix], capture_output=True, timeout=60)


if __name__ == "__main__":
    main()
