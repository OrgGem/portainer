"""Opt-in real Docker verification of managed Compose build budgets.

Uses disposable Portainer/Agent servers and uniquely named stacks. Refuses to
run if a managed builder already exists on this daemon, to protect live builds.
Requires Docker Linux containers, Python 3, and the custom Portainer image.
"""
import hashlib
import json
import os
import secrets
import subprocess
import time
import urllib.error
import urllib.request
import uuid


def docker(*args):
    return subprocess.check_output(["docker", *args], text=True, timeout=180).strip()


def main():
    info = json.loads(docker("info", "--format", "{{json .}}"))
    builder = "buildx_buildkit_portainer-compose-" + hashlib.sha256(info["ID"].encode()).hexdigest()[:20]
    if subprocess.run(["docker", "inspect", builder], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0:
        raise RuntimeError("Managed builder already exists; use an isolated Docker daemon for this test")
    prefix = "portainer-build-test-" + uuid.uuid4().hex[:8]
    server, agent, network, data = [prefix + suffix for suffix in ("-server", "-agent", "-net", "-data")]
    setup_token = secrets.token_hex(32)
    base, admin = "", None
    stacks, images = [], []
    checks = []

    def api(method, path, body=None, expected=200, token=None):
        headers = {"Content-Type": "application/json"}
        if token or admin:
            headers["Authorization"] = "Bearer " + (token or admin)
        if path == "/users/admin/init":
            headers["X-Setup-Token"] = setup_token
        req = urllib.request.Request(base + "/api" + path, data=None if body is None else json.dumps(body).encode(), headers=headers, method=method)
        try:
            with urllib.request.urlopen(req, timeout=30) as result:
                code, content = result.status, result.read()
        except urllib.error.HTTPError as error:
            code, content = error.code, error.read()
        assert code == expected, (method, path, code, content[:1500])
        return json.loads(content) if content else None

    def endpoint(fields):
        boundary = uuid.uuid4().hex
        body = "".join(f'--{boundary}\r\nContent-Disposition: form-data; name="{k}"\r\n\r\n{v}\r\n' for k, v in fields.items()) + f"--{boundary}--\r\n"
        req = urllib.request.Request(base + "/api/endpoints", data=body.encode(), headers={"Authorization": "Bearer " + admin, "Content-Type": "multipart/form-data; boundary=" + boundary}, method="POST")
        with urllib.request.urlopen(req, timeout=120) as result:
            return json.load(result)["Id"]

    def start_build(suffix, endpoint_id, instruction):
        name = prefix + "-" + suffix
        image = name + ":test"
        images.append(image)
        dockerfile = "FROM python:3.13-alpine\nRUN " + instruction + "\n"
        compose = "services:\n  app:\n    image: " + image + "\n    build:\n      context: .\n      no_cache: true\n      dockerfile_inline: |\n" + "".join("        " + line + "\n" for line in dockerfile.splitlines()) + '    command: ["sleep", "600"]\n'
        stack = api("POST", f"/stacks/create/standalone/string?endpointId={endpoint_id}", {"Name": name, "StackFileContent": compose, "Env": []})
        stacks.append((stack["Id"], endpoint_id, name))
        return stack["Id"], image

    def wait_stack(stack_id, status=1):
        for _ in range(240):
            result = api("GET", f"/stacks/{stack_id}")
            if result["Status"] != 3:
                assert result["Status"] == status, result.get("DeploymentStatus")
                return result
            time.sleep(1)
        raise AssertionError("build timed out")

    def assert_limits(cpu, memory):
        inspected = json.loads(docker("inspect", builder))[0]
        host = inspected["HostConfig"]
        quota = info["NCPU"] * 1000 * cpu
        limit = info["MemTotal"] // 100 * memory
        assert host["CpuPeriod"] == 100000 and host["CpuQuota"] == quota, host
        assert host["Memory"] == limit and host["MemorySwap"] == limit, host
        if info.get("CgroupVersion") == "2":
            assert docker("exec", builder, "cat", "/sys/fs/cgroup/cpu.max") == f"{quota} 100000"
            # The kernel rounds memory.max down to its page size.
            actual_memory = int(docker("exec", builder, "cat", "/sys/fs/cgroup/memory.max"))
            assert 0 <= limit - actual_memory < 65536, (limit, actual_memory)
            assert docker("exec", builder, "cat", "/sys/fs/cgroup/memory.swap.max") == "0"
        return inspected["Id"]

    def timing(image):
        return json.loads(docker("run", "--rm", image, "cat", "/build-timing.json"))

    try:
        docker("network", "create", network)
        docker("volume", "create", data)
        docker("run", "-d", "--name", agent, "--network", network, "-v", "/var/run/docker.sock:/var/run/docker.sock", "-v", "/var/lib/docker/volumes:/var/lib/docker/volumes", os.environ.get("PORTAINER_TEST_AGENT_IMAGE", "portainer-agent-custom:2.45.3-build-policy"))
        docker("run", "-d", "--name", server, "--network", network, "-p", "127.0.0.1::9000", "-v", "/var/run/docker.sock:/var/run/docker.sock", "-v", data + ":/data", os.environ.get("PORTAINER_TEST_IMAGE", "portainer-custom:2.45.3-build-policy"), "--http-enabled", "--setup-token", setup_token)
        base = "http://" + docker("port", server, "9000/tcp")
        for _ in range(60):
            try:
                api("GET", "/status"); break
            except OSError:
                time.sleep(1)
        password = secrets.token_urlsafe(24)
        api("POST", "/users/admin/init", {"Username": "admin", "Password": password})
        admin = api("POST", "/auth", {"Username": "admin", "Password": password})["jwt"]
        local = endpoint({"Name": "build-local", "EndpointCreationType": "1", "URL": "unix:///var/run/docker.sock"})
        remote = endpoint({"Name": "build-agent-alias", "EndpointCreationType": "2", "URL": "tcp://" + agent + ":9001", "TLS": "true", "TLSSkipVerify": "true", "TLSSkipClientVerify": "true"})
        default = {"CPUPercent": 50, "MemoryPercent": 50, "MaxConcurrentBuilds": 1, "AutoConfigureBuilderCgroup": False}
        assert api("GET", "/settings")["ComposeBuildPolicy"] == default
        for invalid in ({**default, "CPUPercent": 51}, {**default, "MemoryPercent": 51}, {**default, "MaxConcurrentBuilds": 0}, {**default, "CPUPercent": 2.5}):
            api("PUT", "/settings", {"ComposeBuildPolicy": invalid}, expected=400)
        api("POST", "/users", {"Username": "regular", "Password": password, "Role": 2})
        regular = api("POST", "/auth", {"Username": "regular", "Password": password})["jwt"]
        api("PUT", "/settings", {"ComposeBuildPolicy": default}, expected=403, token=regular)
        checks.append("defaults, 50% ceiling, integer validation and admin-only settings")

        initial, _ = start_build("default", local, "true")
        wait_stack(initial)
        builder_id = assert_limits(50, 50)
        checks.append("default 50% CPU/RAM and disabled swap enforced in Docker and cgroup")

        policy = {"CPUPercent": 5, "MemoryPercent": 1, "MaxConcurrentBuilds": 1, "AutoConfigureBuilderCgroup": False}
        api("PUT", "/settings", {"ComposeBuildPolicy": policy})
        # Each RUN occupies the builder for eight seconds and records wall times.
        # Multiple processes ensure the CPU workload is above the configured quota.
        code = r'import time,multiprocessing,json; start=time.time(); q=multiprocessing.Queue(); exec("def busy():\n end=time.time()+8\n while time.time()<end: pass\n q.put(time.process_time())"); ps=[multiprocessing.Process(target=busy) for _ in range(4)]; [p.start() for p in ps]; [p.join() for p in ps]; json.dump([start,time.time(),sum(q.get() for _ in ps)],open("/build-timing.json","w"))'
        first, image1 = start_build("one", local, "python -c '" + code + "'")
        second, image2 = start_build("two", remote, "python -c '" + code + "; print(2)'")
        wait_stack(first); wait_stack(second)
        t1, t2 = timing(image1), timing(image2)
        assert t1[1] <= t2[0] or t2[1] <= t1[0], (t1, t2)
        assert assert_limits(5, 1) == builder_id
        cpu_budget = info["NCPU"] * policy["CPUPercent"] / 100
        for measurement in (t1, t2):
            assert measurement[2] / (measurement[1] - measurement[0]) <= cpu_budget * 1.3 + 0.05, measurement
        checks.append("local and Agent aliases serialize actual RUN execution and measured CPU stays within quota")

        parallel_policy = {**policy, "MemoryPercent": 2, "MaxConcurrentBuilds": 2}
        api("PUT", "/settings", {"ComposeBuildPolicy": parallel_policy})
        parallel1, pi1 = start_build("parallel-one", local, "python -c '" + code + "; print(3)'")
        parallel2, pi2 = start_build("parallel-two", remote, "python -c '" + code + "; print(4)'")
        wait_stack(parallel1); wait_stack(parallel2)
        pt1, pt2 = timing(pi1), timing(pi2)
        assert max(pt1[0], pt2[0]) < min(pt1[1], pt2[1]), (pt1, pt2)
        total_cpu = (pt1[2] + pt2[2]) / (max(pt1[1], pt2[1]) - min(pt1[0], pt2[0]))
        assert total_cpu <= cpu_budget * 1.3 + 0.05, total_cpu
        assert assert_limits(5, 2) == builder_id
        checks.append("two concurrent builds overlap but share the same measured CPU and RAM budget")
        api("PUT", "/settings", {"ComposeBuildPolicy": policy})

        # Exceed the enforced RAM budget inside the builder. No app should deploy.
        allocation = info["MemTotal"] // 100 + 128 * 1024 * 1024
        failed, failed_image = start_build("oom", local, f'python -c "x=bytearray({allocation}); print(len(x))"')
        failed_state = wait_stack(failed, status=4)
        failure_message = json.dumps(failed_state.get("DeploymentStatus", [])).lower()
        assert any(reason in failure_message for reason in (
            "137", "out of memory", "oom", "cannot allocate memory",
        )), failure_message
        assert not docker("ps", "-aq", "--filter", "label=com.docker.compose.project=" + prefix + "-oom")
        recovered, _ = start_build("recovered", remote, "true")
        wait_stack(recovered)
        assert_limits(5, 1)
        checks.append("memory exhaustion fails build without deployment; next queued build succeeds")

        docker("restart", server)
        admin = None
        # Docker may allocate a new published port when HostPort was empty.
        base = "http://" + docker("port", server, "9000/tcp")
        for _ in range(60):
            try:
                api("GET", "/system/status"); break
            except OSError:
                time.sleep(1)
        else:
            raise AssertionError("Portainer did not become ready after restart")
        admin = api("POST", "/auth", {"Username": "admin", "Password": password})["jwt"]
        assert api("GET", "/settings")["ComposeBuildPolicy"] == policy
        after_restart, _ = start_build("restart", local, "true")
        wait_stack(after_restart)
        assert assert_limits(5, 1) == builder_id
        checks.append("policy persists and managed builder is safely reused after server restart")
        print(json.dumps({"passed": checks, "hostCPUs": info["NCPU"], "hostMemory": info["MemTotal"],
                          "measuredSerialCPUCores": [t[2] / (t[1] - t[0]) for t in (t1, t2)],
                          "measuredParallelCPUCores": total_cpu, "cpuTestBudgetCores": cpu_budget}, indent=2))
    except Exception:
        print(docker("logs", "--tail", "100", server), flush=True)
        raise
    finally:
        # Only remove resources owned by this test; never prune the daemon.
        for _, _, name in stacks:
            ids = docker("ps", "-aq", "--filter", "label=com.docker.compose.project=" + name).splitlines()
            if ids:
                subprocess.run(["docker", "rm", "-f", *ids], capture_output=True)
            nets = docker("network", "ls", "-q", "--filter", "label=com.docker.compose.project=" + name).splitlines()
            if nets:
                subprocess.run(["docker", "network", "rm", *nets], capture_output=True)
        subprocess.run(["docker", "rm", "-f", server, agent, builder], capture_output=True)
        subprocess.run(["docker", "volume", "rm", data, builder + "_state"], capture_output=True)
        subprocess.run(["docker", "network", "rm", network], capture_output=True)
        if images:
            subprocess.run(["docker", "image", "rm", *images], capture_output=True)


if __name__ == "__main__":
    main()
