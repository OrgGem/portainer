# Only references belong here. The application identity is mounted separately.
exit_after_auth = false
pid_file = "/tmp/vault-agent.pid"

auto_auth {
  method "approle" {
    config = {
      role_id_file_path = "/run/secrets/vault_role_id"
      secret_id_file_path = "/run/secrets/vault_secret_id"
      # Compose secrets are read-only. Rotate/re-provision this file externally.
      remove_secret_id_file_after_reading = false
    }
  }
}

template_config {
  static_secret_render_interval = "30s"
  exit_on_retry_failure = true
}

env_template "DB_PASSWORD" {
  contents = "{{ with secret \"kv/data/apps/demo\" }}{{ .Data.data.DB_PASSWORD }}{{ end }}"
  error_on_missing_key = true
}

exec {
  command = ["/usr/local/bin/python", "/app/server.py"]
  restart_on_secret_changes = "always"
  restart_stop_signal = "SIGTERM"
}
