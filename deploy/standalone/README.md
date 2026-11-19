# Portainer CE 2.45.3 — Docker Standalone controls

Nhánh `codex/standalone-stack-controls` bắt đầu từ tag `2.45.3`, commit
`0fa764dc81c76bb7914d65d8e62b026d7c746899`.

Custom release: `2.45.3-custom.3`. See [container releases](RELEASE.md) for
GitHub Actions, version tags, GHCR images and the release process.

## Phạm vi

- Tùy chỉnh tên, logo, favicon, màu, font và bố cục tại **Settings → Appearance**.
  Xem [hướng dẫn Appearance](../../docs/customization/appearance.md).

- Chính sách build Compose chung: tối đa 50% CPU/RAM mỗi Docker host, mặc định
  một build/lần, UI tại **Settings → Compose build policy**.
  Environment qua Agent cần image Agent tùy chỉnh đi kèm để hỗ trợ luồng BuildKit.
  Xem [hướng dẫn build policy](BUILD-POLICY.md).
- **Edit text / Save** ngay trong Volume browser, hỗ trợ UTF-8/config/JSON,
  kiểm tra xung đột và giữ bản nháp khi lưu lỗi. Xem [hướng dẫn editor](VOLUME-TEXT-EDITOR.md).
- CPU/RAM/dung lượng và mini metrics ở danh sách container, Home và Environments;
  hiển thị từng mount host khi có nguồn local. Xem [hướng dẫn metrics](METRICS.md).
- Group (Team) với 5 role theo environment: Environment Admin, Operator,
  Helpdesk, Standard và Read-only. Xem [hướng dẫn gán role](ROLES.md).
- UI ánh xạ Vault theo service/stack và inventory có thống kê, bộ lọc theo
  environment, stack, tên biến, path và key. Chỉ lưu/hiển thị tham chiếu;
  xem [hướng dẫn Vault mappings](VAULT-MAPPINGS.md).
- Chính sách theo stack, user/team và thao tác: `view`, `config.upload`,
  `config.apply` (bao gồm rollback), `deploy` (start/stop), `edit` (Compose/image).
- Tab **Configuration & permissions** trong stack Compose tạo bằng editor/upload.
- Admin khai báo trước service và đường dẫn file đích. Operator chọn một mục đã
  khai báo; nội dung upload không được dùng làm Compose hoặc đường dẫn host.
- Bản nháp và bản đang áp dụng tách biệt. File UTF-8 `.yaml`, `.yml`, `.json`,
  `.conf`, tối đa 1 MiB; YAML/JSON được kiểm tra cú pháp, `.conf` được ứng dụng kiểm
  tra khi khởi động. Tối đa 100 revisions/stack và 200 sự kiện gần nhất.
- File bất biến có UUID, SHA-256, người upload và thời gian. Mount chỉ đọc;
  apply giữ image ID hiện tại, không build/pull, không cập nhật service phụ thuộc,
  không prune service khác. Compose chờ service running/healthy tối đa 90 giây.
  Nên khai báo healthcheck kiểm tra cấu hình và trạng thái thực của ứng dụng.
- Chỉ cập nhật active revision khi deploy thành công. Khi thất bại, tự tạo lại
  service với bản trước. Nếu phục hồi cũng thất bại hoặc server bị ngắt, giữ
  pending revision/image trong DB; admin dùng **Recover previous configuration**.
- Bản có `AppliedAt` mới được rollback. Các thao tác dùng version để từ chối
  dữ liệu cũ và được tuần tự hóa với update/start/stop của stack.

Quản lý phiên bản cấu hình ở tab stack hỗ trợ **Linux Docker socket cục bộ, Compose một file**.
Chưa hỗ trợ remote Docker TCP, Agent/Edge, Git/auto-update, Swarm, Kubernetes,
Compose nhiều file hoặc Portainer chạy trực tiếp trên Windows. Docker Desktop
Linux containers chạy được khi Portainer và daemon dùng chung đường dẫn Linux.
API từ chối môi trường không phù hợp thay vì áp dụng nhầm đường dẫn mount.
Editor ghi trực tiếp trong Volume browser dùng Docker Agent; xem phạm vi riêng
ở [VOLUME-TEXT-EDITOR.md](VOLUME-TEXT-EDITOR.md).

## Quyền và ranh giới tin cậy

Quyền thực tế là giao của: quyền environment + quyền sở hữu stack CE hiện có +
operation policy. Grant user và team cộng gộp; mọi grant phải có `view`. Admin
luôn được phép. `edit` và `deploy` đều cần cho API update Compose vì API đó cũng
triển khai ngay. Xóa stack cũng cần cả hai. Policy chỉ admin được sửa; policy
rỗng chỉ cho admin. Stack chưa bật policy giữ hành vi CE cũ.

Khi một environment có stack policy, các thao tác Docker trực tiếp, exec/attach,
archive/export, volume browser và tạo stack tùy ý bị chặn với non-admin,
**trừ Environment Admin** được gán rõ ràng trong environment đó. Public Docker
webhooks và auto-update Git cũng bị chặn ở environment có policy hoặc role mới;
chúng không mang danh tính/thao tác đã được cấp quyền.
Stack chưa có policy cũng không thể được non-admin (trừ Environment Admin) sửa ở
environment này. Điều này ngăn việc tạo container khác mount Docker socket hoặc
mount dữ liệu của stack để vượt quyền. Admin tiếp tục có Docker API đầy đủ.
Các màn hình Docker khác vẫn có thể hiển thị thao tác chung, nhưng API trả 403.

Người được cấp `edit` có thể thay Compose và image, vì vậy đây là quyền triển khai
đáng tin cậy, có thể tác động tới Docker host theo cấu hình bảo mật environment.
Không cấp `edit` cho người chỉ vận hành cấu hình. Docker socket/SSH ngoài
Portainer và container đã có đặc quyền host không nằm dưới RBAC của Portainer.
Nên dùng environment dành riêng cho các stack cần chính sách này.

Đã bật policy thì không chuyển stack sang Git hoặc migrate qua API. Những đường
này cần thiết kế riêng để bảo toàn lịch sử file và kiểm tra quyền.

## Build và chạy bằng Docker Compose

Trên Linux Docker host:

```sh
sudo install -d -m 0700 /var/lib/portainer-stack-configs
docker compose -f deploy/standalone/compose.yaml build
docker compose -f deploy/standalone/compose.yaml up -d
```

Mở `https://localhost:9445`. Cổng chỉ bind localhost. Compose dùng volume data
riêng theo project `portainer-custom`; không tự gắn DB của một Portainer khác.
Lần đầu lấy setup token từ `docker compose -f deploy/standalone/compose.yaml logs
portainer` để tạo admin, rồi thêm environment **Docker Standalone / Socket** nếu
chưa có. Thư mục cấu hình phải được mount vào Portainer **cùng đường dẫn tuyệt
đối trên host**; đừng đổi riêng `target` hoặc biến môi trường.

Biến tùy chọn: `PORTAINER_HTTPS_PORT` (9445), `STACK_CONFIG_ROOT`
(`/var/lib/portainer-stack-configs`), `PORTAINER_IMAGE` (`portainer-custom:2.45.3`). Nếu đổi root phải di chuyển cả lịch sử file
và mount tương ứng trước khi khởi động lại. Không chạy đồng thời nhiều instance
Portainer ghi vào cùng DB/root.

Dockerfile build source dùng Go 1.26.10, Node 22.22.1 và lockfile PNPM của tag.
`--ignore-pnpmfile` giữ frozen lockfile khi checkout CE không chứa pnpmfile của
quy trình build upstream. Không cập nhật dependency trong thay đổi này.

Nếu đã build `dist/portainer` (Linux) và `dist/public`:

```sh
docker build -f deploy/standalone/Dockerfile.prebuilt -t portainer-custom:2.45.3 .
docker compose -f deploy/standalone/compose.yaml up -d --no-build
```

Trên **Docker Desktop dùng Linux containers**, có thể dùng named volume thay
cho thư mục bind của Linux host. Trong PowerShell tại repository:

```powershell
docker volume create portainer_stack_configs
$env:STACK_CONFIG_ROOT = docker volume inspect portainer_stack_configs --format '{{.Mountpoint}}'
docker compose -f deploy/standalone/compose.yaml -f deploy/standalone/compose.desktop.yaml build
docker compose -f deploy/standalone/compose.yaml -f deploy/standalone/compose.desktop.yaml up -d --no-build
```

Nếu image `portainer-custom:2.45.3` đã có, bỏ qua dòng `build`. Giữ nguyên giá trị
`STACK_CONFIG_ROOT` khi chạy lại Compose; đây là đường dẫn **trong Docker daemon**,
không phải đường dẫn ổ C:/D:. Volume external không bị `compose down -v` xóa.

## Ví dụ cấp quyền

1. Admin tạo stack như bình thường và cấp quyền environment, ownership cho user
   hoặc team tương ứng. Bật **Allow stack management for non-administrators**.
2. Trong **Configuration & permissions**, cấp team A: View + Upload config +
   Apply / rollback config; chỉ cấp Deploy/Edit cho nhóm phát hành.
3. Admin khai báo `ID=app`, `Service=backend`,
   `Target=/etc/myapp/application.yaml`. Target có revision là bất biến.
4. Operator upload; reviewer chọn **Review**, xem nội dung và checksum; người có
   quyền apply chọn **Apply this version**. Bản cũ đã thành công có nút rollback.

API mới (đều cần xác thực):

| API | Quyền |
| --- | --- |
| `GET /api/stacks/{id}/controls` | view + ownership |
| `PUT /api/stacks/{id}/policy` | admin |
| `PUT /api/stacks/{id}/configurations/targets` | admin |
| `POST /api/stacks/{id}/configurations` | config.upload |
| `GET /api/stacks/{id}/configurations/{revision}/content` | view |
| `POST /api/stacks/{id}/configurations/{revision}/apply` | config.apply |
| `POST /api/stacks/{id}/configurations/{revision}/rollback` | config.apply |
| `POST /api/stacks/{id}/configurations/recover` | admin |

Upload nhận JSON `{Version, TargetID, Filename, Content}`. Apply/rollback nhận
`{Version}`; UI tự gửi version hiện tại. Policy nhận `{Version, Grants}` với
`{UserID, Actions}` hoặc `{TeamID, Actions}`. Targets nhận `{Version, Targets}`.

## Vault

Ví dụ đầy đủ ở [../vault-example](../vault-example/README.md): Vault Agent chạy
làm process supervisor ngay trong image ứng dụng. Portainer giữ `DB_HOST` và
tham chiếu Vault; Agent tự đăng nhập AppRole của ứng dụng rồi cấp `DB_PASSWORD`
cho tiến trình con. Không lưu giá trị secret trong stack.Env, file upload hoặc
Docker Config.Env. Tab **Vault mappings** lưu tham chiếu và xuất `env_template`
theo service để ghép vào `agent.hcl`. Trang **Vault mappings** ở sidebar thống kê
các tham chiếu đã khai báo. Lưu trên UI chưa tự thay đổi cấu hình runtime;
triển khai Agent theo [hướng dẫn](VAULT-MAPPINGS.md).

## Backup và vận hành

Trong **Settings → Back up Portainer → Store in S3**, admin nhập bucket, region,
prefix tùy chọn và giờ chạy hằng ngày theo UTC. Lưu cấu hình trước khi chọn
**Backup now** để chạy ngay. UI hiển thị kết quả lần chạy gần nhất. Ví dụ,
00:00 UTC tương ứng 07:00 tại Việt Nam. Nếu Portainer Server dừng qua giờ hẹn,
sau khi khởi động lại, hệ thống sẽ thử một lần cho mốc đã lỡ gần nhất rồi tiếp
tục lịch hằng ngày. Các mốc cũ hơn không được chạy bù riêng. Mỗi archive được
upload bằng một S3 PutObject và có giới hạn kích thước 5 GiB.

Portainer Server sử dụng IAM role với credentials tạm thời. Trên EC2, gắn
instance profile và bảo đảm container truy cập được IMDSv2. Ngoài AWS, cấu hình
IAM Roles Anywhere credential helper qua AWS profile và `credential_process`,
đồng thời cung cấp certificate và private key cho helper trong container. Cấp
role quyền `s3:PutObject` trên prefix đã chọn. Nếu bucket dùng SSE-KMS, cấp thêm
quyền KMS theo cấu hình của bucket. Xem [AWS SDK for Go](https://docs.aws.amazon.com/sdk-for-go/v2/developer-guide/configure-gosdk.html)
và [IAM Roles Anywhere](https://docs.aws.amazon.com/sdkref/latest/guide/access-rolesanywhere.html).

Backup bao gồm database, file dữ liệu Portainer và các revision cấu hình stack
còn được metadata tham chiếu trong `STACK_CONFIG_ROOT`. Portainer kiểm tra hash
trước khi tạo bản backup. File cấu hình còn lưu trên đĩa sau khi stack bị xóa
không còn metadata liên kết và cần được quản lý riêng. Dữ liệu ứng dụng trong
volumes và bind mounts vẫn cần cơ chế backup riêng. Không upload mật khẩu hoặc
private key vào phần cấu hình stack; dùng Vault cho secrets.

Để khôi phục, tải archive từ S3 rồi chọn **Restore Portainer from backup** khi
thiết lập Portainer mới. Mount `STACK_CONFIG_ROOT` tại đường dẫn đã cấu hình
trước đó và đặt `PORTAINER_STACK_CONFIG_ROOT` trỏ tới mount này. Các revision đã
có được kiểm tra hash, file xung đột sẽ khiến restore dừng trước khi thay database.
Backup tạo bằng phiên bản cũ không chứa revision file, nên cần khôi phục riêng
`STACK_CONFIG_ROOT` tương ứng với bản backup đó. Metadata thiếu file sẽ bị kiểm
tra hash từ chối apply.

Các field mới là bổ sung tùy chọn nên không cần migration phá hủy dữ liệu cũ.
Không rollback binary về CE gốc trên environment đang dựa vào operation policy:
CE gốc không hiểu các quyền mới. Cần cô lập quyền người dùng trước khi đổi binary.

## Kiểm chứng

Khi cập nhật Compose để xóa hoặc đổi tên service, override chỉ thêm mount vào
service còn được khai báo; không tạo lại service cũ và không tự chuyển cấu hình
sang tên service mới. Metadata, ActiveRevision và lịch sử được giữ nguyên. Nếu
deploy lỗi, Compose cũ được khôi phục trước khi stack rời trạng thái Deploying,
và target cũ vẫn giữ cấu hình để triển khai lại. Khi deploy thành công, admin có
thể dùng **Retire missing service target** trong tab Configuration, sau đó khai báo target mới cho service
thay thế. Lịch sử revision được giữ lại; target ID đã có lịch sử không được tái
sử dụng hoặc đổi sang service/path khác. Apply/rollback chỉ khả dụng khi target
và service còn tồn tại. Các thao tác ghi được khóa riêng theo từng stack, nên
healthcheck của stack A không giữ khóa thao tác của stack B.
Việc dọn container của service đã xóa/đổi tên vẫn theo tùy chọn `Prune` của
Compose update; retire target chỉ thay đổi metadata, không xóa container.

```sh
go test ./pkg/authorization ./api/stacks/stackconfig ./api/http/security \
  ./api/http/handler/stacks ./api/http/proxy/factory/docker \
  ./api/stacks/deployments ./api/http/handler/webhooks \
  ./api/http/handler/resourcecontrols
go test ./pkg/libstack/compose -run TestConfigOnly
pnpm typecheck
pnpm test app/react/docker/stacks/ItemView/StackControls/ConfigurationTab.test.tsx
python deploy/standalone/tests/smoke.py
```

Smoke test tạo Portainer/data/config volume và stack hai service có tên ngẫu
nhiên; thử quyền, upload, apply, lỗi nginx và tự phục hồi, rollback, readonly
mount, image ID và container service không liên quan. Test còn đổi/xóa service
đang có ActiveRevision qua API update thật, kiểm tra khôi phục Compose khi lỗi,
retire target, giữ lịch sử và áp dụng target thay thế. Script dọn tài nguyên do
chính nó tạo. Không chạy trên Docker host thiếu dung lượng hoặc đang nâng cấp.

Kết quả thực tế và giới hạn kiểm thử: [VALIDATION.md](VALIDATION.md).
