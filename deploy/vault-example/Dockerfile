ARG VAULT_VERSION=1.21.0
FROM hashicorp/vault:${VAULT_VERSION} AS vault
FROM python:3.13-alpine
COPY --from=vault /bin/vault /usr/local/bin/vault
COPY server.py /app/server.py
COPY agent.hcl /etc/vault/agent.hcl
ENV VAULT_DISABLE_MLOCK=true
EXPOSE 8080
USER 10001:10001
ENTRYPOINT ["vault", "agent", "-config=/etc/vault/agent.hcl"]
