# Kết nối Vault và ánh xạ biến môi trường

Portainer đọc HashiCorp Vault khi triển khai stack Docker Compose. Giá trị được
truyền cho đúng service dưới dạng biến môi trường, ứng dụng đọc biến như bình
thường. Vault được quản lý bên ngoài Portainer.

## Cấu hình kết nối theo environment

Global Administrator mở **Environments → environment → HashiCorp Vault** và
nhập URL HTTPS, AppRole RoleID, SecretID và auth mount. Auth mount mặc định là
`approle`. Nếu Vault dùng CA nội bộ, thêm chứng chỉ CA dạng PEM. Portainer
Server phải kết nối được tới Vault URL, kể cả khi Docker host dùng Portainer
Agent.

SecretID chỉ được nhập và không được trả lại trên UI. Để trống SecretID khi cập
nhật để giữ giá trị cũ. Khi đổi URL Vault, nhập lại SecretID. Thông tin kết nối
được lưu cùng dữ liệu Portainer. Mã hóa database của Portainer bảo vệ dữ liệu
này khi instance đã bật tùy chọn mã hóa. Giới hạn quyền truy cập bản backup
`/data` phù hợp với cách instance đang lưu dữ liệu.

Bên quản lý Vault cung cấp AppRole có quyền đọc các path cần thiết. Cần biết
engine là KV v1 hay KV v2 và tên key chứa giá trị.

## Ánh xạ cho stack

Trong **Create stack**, chọn Editor hoặc Upload và thêm **Vault mappings**
trước khi triển khai. Với stack hiện có, mở **Stack → Vault mappings**, lưu
ánh xạ và chọn **Enable deployment injection**. Mỗi ánh xạ gồm:

| Trường | Ví dụ | Ý nghĩa |
| --- | --- | --- |
| Service | `backend` | Service trong Compose |
| Environment variable | `DB_PASSWORD` | Tên biến ứng dụng đọc |
| Vault API path | `kv/data/apps/backend` | API path, gồm `/data/` với KV v2 |
| Key | `password` | Key trong dữ liệu KV |
| Engine | `kv-v2` | KV v1 hoặc KV v2 |

Khi deploy, Portainer đăng nhập AppRole, đọc các path cần thiết và đưa giá trị
vào env của service. Nếu biến trùng tên với env trong Compose, giá trị từ
mapping được ưu tiên cho service đó. KV key cần có giá trị chuỗi. Mỗi
service chỉ ánh xạ một lần cho mỗi tên biến và mỗi stack có tối đa 128 ánh xạ.

Global Administrator và Environment Admin được quản lý ánh xạ trong phạm vi
được cấp. Quyền deploy và policy theo stack vẫn áp dụng. Mỗi lần lưu gửi version
hiện tại. Nếu API trả 409, reload, đối chiếu và lưu lại.

## Vòng đời giá trị

Lưu mapping không thay đổi container đang chạy. Deploy hoặc redeploy đọc giá trị
mới. Nếu Vault không truy cập được, AppRole bị từ chối hoặc thiếu key, deploy
thất bại. Việc đổi giá trị trong Vault không tự cập nhật app. Restart container
hiện có tiếp tục dùng env lúc container được tạo. Deploy lại để đọc giá trị mới.

Apply hoặc rollback file cấu hình đọc lại Vault cho service được tạo lại.
Rollback file cấu hình dùng secret hiện tại của Vault. Nếu đổi tên hoặc xóa
service trong Compose, cập nhật mapping tương ứng trước lần deploy tiếp theo.

Giá trị đọc từ Vault được đưa vào Docker Config.Env và có thể được đọc bởi
người có quyền Docker API hoặc `docker inspect`. Bảng ENV trong Portainer che
giá trị của các biến được đánh dấu là từ Vault. Các bản backup Docker hoặc
công cụ quản trị có quyền đọc cấu hình container cũng có thể tiếp cận env.

Stack cũ có mapping Agent template giữ trạng thái injection tắt khi nâng cấp.
Chọn **Enable deployment injection** nếu muốn Portainer đọc Vault khi deploy.
Nút xuất Agent template vẫn hữu ích cho ứng dụng đang dùng Agent riêng.

## Phạm vi và API

Bản này hỗ trợ stack do Portainer quản lý, Docker Standalone local hoặc Docker
host kết nối qua Portainer Agent, nguồn Editor/Upload một file. Apply/rollback
file cấu hình tiếp tục theo phạm vi được nêu trong [README](README.md).

| API | Kết quả / quyền |
| --- | --- |
| `GET /api/endpoints/{id}/vault` | Kết nối Vault và trạng thái SecretID, Global Admin |
| `PUT /api/endpoints/{id}/vault` | Cập nhật kết nối và SecretID chỉ ghi, Global Admin |
| `GET /api/stacks/{id}/vault` | Configuration, Services, CanEdit, InjectionSupported; cần quyền xem stack |
| `PUT /api/stacks/{id}/vault` | Version, Mappings, InjectOnDeploy tùy chọn; global/environment admin |
| `GET /api/stacks/{id}/vault/template?service=backend` | Agent template và version đã lưu |
| `GET /api/stacks/vault/mappings` | Inventory và thống kê của dữ liệu được phép xem |

Trang **Vault mappings** ở sidebar thống kê các tham chiếu đã lưu theo
environment, stack, service, biến, path và key. Thống kê không xác nhận giá trị
hiện có trong container. Các mapping Agent bên ngoài Portainer cần được khai
báo trong UI để xuất hiện ở inventory.

## Kiểm tra và nâng cấp

Script [vault_deploy_smoke.py](tests/vault_deploy_smoke.py) dùng Portainer,
Vault dev, Portainer Agent, network và volume tạm trên Docker daemon hiện tại.
Script tạo dữ liệu AppRole và secret tổng hợp để kiểm tra đường đi deploy. Cần
Python 3, Docker daemon và các image mà script sử dụng. Chạy từ thư mục gốc
repository sau khi đã build image Portainer:

```powershell
$env:PORTAINER_TEST_IMAGE = 'portainer-custom:2.45.3-vault-deploy'
python deploy/standalone/tests/vault_deploy_smoke.py
```

Biến `PORTAINER_TEST_IMAGE` mặc định là tag ở trên. Script mặc định dùng
`portainer/agent:2.45.3`; có thể chọn image Agent khác cho kiểm thử qua
`PORTAINER_AGENT_TEST_IMAGE`. Script có thể pull các image Vault, ứng dụng
và Agent khi Docker host chưa có sẵn. Xem exit code và kết quả từng bước của
chính lần chạy để đánh giá. Kiểm thử này dùng Vault dev tạm, nên cần xác nhận
riêng kết nối và quyền với Vault do bên ngoài quản lý khi áp dụng.

Script [vault_mappings_smoke.py](tests/vault_mappings_smoke.py) kiểm tra luồng
inventory và ví dụ Agent trước đây. Đây là kiểm thử riêng cho chức năng đó.

Khi muốn nâng cấp Portainer đang chạy bằng image mới, xác định trước Compose,
volume `/data` và giá trị `STACK_CONFIG_ROOT` mà instance hiện tại đang dùng,
rồi sao lưu dữ liệu theo quy trình đang áp dụng. Nếu instance được triển khai
bằng các file Compose trong thư mục này, đặt `PORTAINER_IMAGE` thành tag mới,
giữ project, volume, cấu hình và các file override hiện có, rồi áp dụng lại
service Portainer. Ví dụ PowerShell cho cấu hình dùng file Compose chính:

```powershell
$env:PORTAINER_IMAGE = 'portainer-custom:2.45.3-vault-deploy'
docker compose -f deploy/standalone/compose.yaml up -d --no-build portainer
```

Nếu instance dùng `compose.desktop.yaml` hoặc override khác, tiếp tục chỉ định
các file override đã dùng trước đó. Các stack và container ứng dụng không cần
được triển khai lại khi chỉ nâng cấp Portainer. Bật injection và redeploy từng
stack khi sẵn sàng sử dụng Vault.
