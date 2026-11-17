# UI ánh xạ Vault và thống kê tham chiếu

## Màn hình sử dụng

**Stack → Vault mappings** cho phép xem, thêm, sửa và xóa ánh xạ:

| Trường | Ví dụ | Ý nghĩa |
| --- | --- | --- |
| Service | `backend` | Service đã được khai báo trong Compose |
| Environment variable | `DB_PASSWORD` | Tên biến ứng dụng nhận từ Vault Agent |
| Vault API path | `kv/data/apps/backend` | Đường dẫn API, gồm `/data/` với KV v2 |
| Key | `password` | Tên key trong dữ liệu KV |
| Engine | `kv-v2` | KV v1 hoặc KV v2 |

Màn hình hiển thị version, thời điểm lưu và ID người thay đổi gần nhất. Một
service không được khai báo trùng tên biến. Tối đa 128 ánh xạ/stack. Tên biến và
key dùng chữ, số, dấu gạch dưới, bắt đầu bằng chữ/gạch dưới, tối đa 128 ký tự.
Path tối đa 512 ký tự; không nhận URL, query string hay đoạn `..`.

**Vault mappings** ở sidebar là trang tổng hợp, gồm:

- Bảng environment → stack → service → biến → path → key, kèm version/người lưu.
- Bộ lọc environment, stack, tên biến, path, key và service. Các điều kiện kết
  hợp bằng AND; bộ lọc văn bản tìm chuỗi con, không phân biệt chữ hoa/thường.
- Thống kê số ánh xạ, tên biến khác nhau, tên path khác nhau, environment, stack
  và service. Service được đếm theo cặp stack/service; path đếm theo chuỗi path,
  không khẳng định các ứng dụng dùng chung một Vault server.
- Phân trang 50 dòng. Thống kê tính trên toàn bộ kết quả đã lọc trước phân trang.
- Link từ mỗi stack mở lại tab ánh xạ. Link trong tab stack mở inventory với
  environment/stack đã được chọn sẵn.

Các dòng, lựa chọn bộ lọc và thống kê đều được giới hạn quyền **trước** khi trả
về trình duyệt. Không tiết lộ tên/path/số lượng của stack ngoài quyền xem.

## Quyền

Global Administrator và Environment Admin được quản lý ánh xạ trong phạm vi
được cấp. Các role khác chỉ xem nếu có quyền environment và quyền xem stack.
Policy theo stack vẫn áp dụng: user phải có ownership và grant `view` nếu stack
có policy, trừ Environment Admin. Read-only không được sửa, kể cả có grant theo
stack. Thay đổi role/thành viên Team được kiểm tra lại cho request tiếp theo.

Mỗi lần ghi gửi version hiện tại; API trả 409 khi dữ liệu đã đổi. UI giữ nội
dung đang nhập để người dùng reload, đối chiếu và lưu lại. Backend cập nhật
metadata trong transaction, bảo toàn các field khác của stack. Không cho sửa
khi stack đang deploy hoặc chờ phục hồi cấu hình.

## Dữ liệu và Vault Agent

Portainer lưu **tham chiếu**, không kết nối Vault để đọc giá trị. API không nhận
field value, token, secret hoặc credential; các field lạ bị từ chối. Inventory
không trả Compose, `stack.Env`, cấu hình đăng nhập hay giá trị biến môi trường.
Metadata nằm trong dữ liệu stack và đi cùng backup `/data`.

**Saved configuration, runtime not verified** là trạng thái của bản này. Lưu
ánh xạ không tự triển khai lại container, không sửa `stack.Env` và không tự dò
đọc cấu hình của các Agent đang chạy. Các ánh xạ cũ đặt trong `agent.hcl` bên
ngoài Portainer cần được khai báo lại ở UI để xuất hiện trong thống kê.

Sau khi lưu, chọn service và **Preview Agent template → Download template**.
File tải về chỉ chứa các stanza `env_template` của service đó, dùng các
tham chiếu đã lưu. Ví dụ:

```hcl
env_template "DB_PASSWORD" {
  contents = "{{ with secret \"kv/data/apps/backend\" }}{{ .Data.data.password }}{{ end }}"
  error_on_missing_key = true
}
```

Ghép stanza vào `agent.hcl` của ứng dụng, giữ `auto_auth`, Vault address/TLS,
`template_config` và `exec` của ứng dụng. Bật `exit_on_retry_failure = true` trong
`template_config`. Agent dùng quyền riêng của ứng dụng để đọc secret và truyền
biến cho tiến trình con. Triển khai qua quy trình phát hành thông thường, hoặc
qua cơ chế file cấu hình hiện có nếu quản trị viên đã khai báo target phù hợp.
Agent phải là tiến trình cha của ứng dụng theo
[Vault Agent process supervisor](https://developer.hashicorp.com/vault/docs/agent-and-proxy/agent/process-supervisor).
Không chạy render template trong Portainer.

UI quản lý ánh xạ hỗ trợ stack Docker Compose editor/upload, một file, đang
được Portainer quản lý. Không có tự động khám phá secret, kiểm tra path tồn tại,
quản lý Vault policy/AppRole, hoặc xác nhận cấu hình đã áp dụng vào runtime.
Nếu service bị xóa khỏi Compose, tab đánh dấu tham chiếu cũ để admin sửa/xóa;
export không chấp nhận service không còn tồn tại.

## API

| API | Kết quả / quyền |
| --- | --- |
| `GET /api/stacks/{id}/vault` | Configuration, Services, CanEdit; cần quyền xem stack |
| `PUT /api/stacks/{id}/vault` | `{Version, Mappings}`; global/environment admin |
| `GET /api/stacks/{id}/vault/template?service=backend` | Template tham chiếu và version đã lưu; cần quyền xem |
| `GET /api/stacks/vault/mappings` | Items, Summary, Environments, Stacks; chỉ dữ liệu được phép xem |

Inventory nhận `environmentId`, `stackId`, `variable`, `path`, `key`, `service`,
`offset` và `limit` (1–200, mặc định 50). DTO mới không bổ sung bất kỳ trường
chứa giá trị secret nào. Xóa ánh xạ bằng cách gửi danh sách còn lại và version;
danh sách rỗng xóa toàn bộ ánh xạ của stack.

## Kiểm tra

```powershell
docker build -f deploy/standalone/Dockerfile -t portainer-custom:2.45.3-vault-ui .
python deploy/standalone/tests/vault_mappings_smoke.py
```

Script dùng các tài khoản và tài nguyên tạm thời để kiểm tra quyền, bộ lọc,
pagination, lưu sau restart, từ chối trường secret, và thu hồi membership. Sau
đó dùng đúng template xuất từ API để chạy thử AppRole/Vault Agent, xác nhận
injection và rotation bằng secret tổng hợp. Không gọi Vault production.
