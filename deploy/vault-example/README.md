# Vault Agent trong ứng dụng Compose

Ví dụ dùng Vault Agent process supervisor. Agent phải chạy **cùng container và
là tiến trình cha của ứng dụng** để truyền biến môi trường. Một sidecar không
thể sửa môi trường của tiến trình đang chạy trong container khác.

`agent.hcl` ánh xạ `DB_PASSWORD` đến `kv/data/apps/demo`, key
`.Data.data.DB_PASSWORD` của KV v2. Agent dùng AppRole riêng và policy
`app-policy.hcl` chỉ cho phép đọc đường dẫn đó. `DB_HOST` vẫn là biến Compose
thông thường. Khi secret đổi, Agent restart tiến trình con.

Portainer hiện có [UI Vault mappings](../standalone/VAULT-MAPPINGS.md) để lưu
tham chiếu, thống kê theo environment/stack và xuất `env_template` theo service.
Ghép template đã xuất vào `agent.hcl` rồi triển khai ứng dụng. UI không đọc
secret và không tự thay đổi cấu hình Agent đang chạy.

Để dùng với ứng dụng thật:

1. Thay base image Python và `server.py` bằng image/binary ứng dụng; giữ Vault
   binary và command Agent. Sửa `exec.command` theo lệnh chạy ứng dụng.
2. Admin Vault tạo KV v2 mount `kv`, policy trong `app-policy.hcl`, và AppRole
   gắn policy đó. Cấp role ID/secret ID qua quy trình provisioning của ứng dụng,
   có TTL và rotation phù hợp. Không dùng root token hoặc token của Portainer.
3. Tạo file role ID, secret ID và CA ngoài repository trên Docker host; chỉ cho
   user vận hành cần thiết đọc. Container UID 10001 cần đọc được các file mount.
   Compose secrets trên Docker Standalone là file mount; không phải secret
   store được mã hóa. `remove_secret_id_file_after_reading=false` vì mount readonly;
   rotation/provisioning của file này thuộc hệ thống bên ngoài.
4. Cấp `VAULT_ADDR=https://vault.example:8200`, `VAULT_CA_FILE`,
   `VAULT_ROLE_ID_FILE`, `VAULT_SECRET_ID_FILE` và `DB_HOST` qua môi trường shell
   hoặc `.env` riêng, rồi chạy:

```sh
docker compose -f deploy/vault-example/compose.yaml up -d --build
```

HTTP demo chỉ trả `ready` hoặc `secret unavailable`, không xuất mật khẩu. Docker
inspect không hiển thị giá trị `DB_PASSWORD` vì Agent tạo môi trường cho tiến
trình con lúc runtime. Người có quyền root/exec trong container vẫn có thể đọc
process environment; không cấp quyền đó cho operator cấu hình.

Vault binary được pin `1.21.0` cho ví dụ tái lập; có thể build với phiên bản
được tổ chức phê duyệt qua `--build-arg VAULT_VERSION=...`. Server Vault phải dùng
TLS, CA được kiểm tra; compose không bật `VAULT_SKIP_VERIFY`.

Kiểm tra độc lập với Vault giả lập và secret ngẫu nhiên:

```sh
docker build -t portainer-vault-app:example deploy/vault-example
python deploy/vault-example/smoke.py
```

Script tạo Vault dev không publish cổng, AppRole và ứng dụng trên Docker network
riêng; kiểm tra giới hạn quyền AppRole, injection, không lộ trong Docker Config.Env và rotation rồi dọn
container/network. Đây là kiểm tra tích hợp, không phải triển khai Vault production.

Tham khảo: [Vault Agent process supervisor](https://developer.hashicorp.com/vault/docs/agent-and-proxy/agent/process-supervisor),
[AppRole auto-auth](https://developer.hashicorp.com/vault/docs/agent-and-proxy/autoauth/methods/approle).
