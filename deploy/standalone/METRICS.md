# CPU / RAM / dung lượng trong danh sách

Áp dụng cho bản tùy chỉnh Portainer CE 2.45.3, Docker Standalone Linux.

## Giao diện

- **Containers**: cột **CPU / RAM / Storage**, mỗi hàng có số đang dùng / giới
  hạn, thanh tỷ lệ và sparkline. CPU tính theo core (0,50 core = nửa CPU).
- **Home** và **Environments**: CPU/RAM host, filesystem đầu tiên và danh sách
  **More host filesystems** để xem từng mount. Phần này nằm ngoài link mở
  environment nên mở danh sách mount không làm chuyển trang.
- Hiện mount đầu tiên ngay trên hàng; mở **More volume / bind mounts** để xem
  các mount còn lại, tên volume, đường dẫn trong container và trạng thái read-only.
- CPU/RAM lấy khoảng 10 giây/lần khi hàng nằm gần vùng nhìn thấy; dừng polling
  khi tab trình duyệt ẩn. Sparkline giữ tối đa 30 mẫu trong component hiện tại,
  không phải cơ sở dữ liệu lịch sử giám sát. Khi chưa biết quota, sparkline dùng
  thang đo theo các mẫu dung lượng gần nhất; không vẽ thanh phần trăm. Dung lượng Docker cache tối đa 60 giây;
  thời gian mẫu storage nằm trong tooltip dưới metrics.
- Lỗi hoặc không có số liệu hiển thị **Unavailable**. Không biến dữ liệu thiếu
  thành 0; container dừng mới hiển thị CPU/RAM bằng 0.

## Ý nghĩa của số đo

| Số đo | Đang dùng | Tổng/giới hạn |
| --- | --- | --- |
| Container CPU | Delta Docker CPU stats, đơn vị core | Nhỏ nhất giữa host CPU, NanoCPUs/CpuQuota và cpuset nếu có |
| Container RAM | Docker memory usage trừ inactive file cache khi có | Hard memory limit, tối đa bằng RAM host; không dùng memory reservation làm hard limit |
| Writable layer | Docker `SizeRw`; không cộng image layers | `StorageOpt.size` khi Docker khai báo; nếu thiếu: no known quota |
| Volume data | Docker system df volume `UsageData.Size` khi có | Không giả định volume có quota; thiếu quota hiển thị rõ |
| Shared filesystem | Blocks đã dùng của filesystem chứa mount source | Tổng filesystem dùng chung, **không phải quota riêng của volume** |
| Host RAM | `MemTotal - MemAvailable` | RAM Linux host/VM |
| Host CPU | Delta `/proc/stat`, trừ idle và iowait | CPU host/VM |

Bind mount không có phép đo dung lượng thư mục qua Docker; UI vẫn có thể hiển
thị filesystem chứa nó. Không quét `du` đệ quy trên dữ liệu ứng dụng. Các mount
có thể cùng một filesystem nên **không cộng các tổng mount với nhau**. Disk I/O
không được dùng làm dung lượng ổ. Filesystem host loại pseudo mounts, overlay
của từng container, mount runtime Docker/containerd; giữ tối đa 128 mount.

## Cấu hình nguồn host

`compose.yaml` đã thêm mount chỉ đọc `/:/host` và:

```yaml
environment:
  PORTAINER_METRICS_HOST_ROOT: /host
  PORTAINER_METRICS_ENDPOINT_URL: unix:///var/run/docker.sock
```

Bộ đọc host chỉ chạy khi **Type = Docker** và URL environment trùng đúng URL
đã cấu hình. Các đường dẫn mount nguồn chỉ được đọc trong `/host` bằng Go
`os.OpenRoot`, không được thoát qua `..` hoặc symlink. Portainer đã có Docker
socket; mount host phục vụ đọc `/proc` và filesystem capacity, không cần
`privileged` hay mount socket mới.

Trên Docker Desktop, CPU/RAM là số đo **Linux VM của Docker**, không phải tổng
CPU/RAM Windows. Ổ Windows chỉ xuất hiện nếu đã được mount vào Linux VM;
không kiểm kê trực tiếp Windows. Filesystem chứa dữ liệu Docker được đưa lên đầu.
Mount mới tạo sau khi khởi động Portainer có thể
cần recreate Portainer để xuất hiện trong mount namespace (Compose dùng bind
propagation mặc định). Không tự thay đổi quota hay mount của container ứng dụng.

Nếu không muốn cấp host root: bỏ mount `/host` và biến `PORTAINER_METRICS_HOST_ROOT`.
Container CPU/RAM, writable layer và kích thước named volume vẫn hoạt động;
host CPU/RAM chỉ có capacity từ Docker, usage và từng filesystem sẽ unavailable.

## Docker remote / Agent

Container metrics dùng cùng Docker proxy và `X-PortainerAgent-Target` hiện có.
Remote TCP không được đo bằng `/proc` của máy chạy Portainer. Agent 2.45.3 có
`/host/docker-storage`: có thể trả capacity/available của phân vùng Docker;
used ở nguồn này bao gồm phần reserved blocks do API chỉ trả available.
Agent hiện tại không trả host CPU/RAM usage hay mọi filesystem mount. Các ô
thiếu nguồn hiển thị unavailable; muốn đủ số đo cần bổ sung collector ở từng
remote host. Không tự cài collector lên máy remote.

Tham khảo mã nguồn Agent:
[host info](https://github.com/portainer/agent/blob/2.45.3/http/handler/host/host_info.go),
[Docker storage](https://github.com/portainer/agent/blob/2.45.3/http/handler/host/host_docker_storage.go).

## API và phân quyền

```text
GET /api/endpoints/{id}/metrics
GET /api/endpoints/{id}/containers/{containerId}/metrics
```

API kiểm tra quyền environment ở mỗi request. Container luôn đi qua inspect
proxy mới để kiểm tra quyền tài nguyên/stack/team trước khi dùng cache; không
cache quyết định cho phép. Chỉ trả DTO metrics đã chọn trường, không trả Docker
Env, Compose, command, label hay secret. Cache chia theo environment, URL,
user và Agent target, tối đa 256 entries/32 MiB. Fetch metrics giới hạn 6 request
đồng thời, response tối đa 8 MiB, request timeout 15–20 giây.

Kiểm thử Docker thật:

```powershell
$env:PORTAINER_TEST_IMAGE = 'portainer-custom:2.45.3-metrics'
python deploy/standalone/tests/metrics_smoke.py
```

Script dùng container/volume tên ngẫu nhiên rồi xóa đúng các resource đó.
