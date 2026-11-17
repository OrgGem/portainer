# Edit / Save nhanh file trong volume

Trong **Volumes → Browse**, chọn **Edit text** cạnh file. Cửa sổ editor có số
dòng, tô màu JSON/YAML, **Save**, **Reload file**, **Close** và phím
**Ctrl/Cmd+S**. Save giữ cửa sổ mở và hiện **Saved and verified** sau khi đọc lại
đúng nội dung. Danh sách file được refresh sau khi lưu.

## Phạm vi

- Portainer CE tùy chỉnh 2.45.3, environment **Docker Agent**, Agent **2.45.3**
  đã kiểm thử. Dùng Agent chính thức, không cần image Agent tùy chỉnh.
- File UTF-8 tối đa **1 MiB**: `.conf`, `.json`, `.yaml`, `.txt`, `.env` và các
  file văn bản khác; không chỉ dựa vào đuôi file để kết luận là text.
- File nhị phân/UTF-8 không hợp lệ bị từ chối. JSON có đuôi `.json` phải hợp lệ
  trước khi lưu; JSON lỗi sẵn vẫn mở được để sửa.
- Giữ BOM UTF-8 và LF/CRLF của file. File trộn LF/CRLF hoặc chỉ CR không được
  sửa nhanh để tránh thay đổi định dạng âm thầm; dùng download/upload.
- Có thể lưu file text rỗng; file `.json` rỗng bị từ chối vì không phải JSON hợp lệ.
- Lưu trực tiếp vào file đang có, giữ inode, owner/group/mode theo hành vi ghi
  file của Agent. Không tự restart container, apply stack config hay ghi vào
  kho phiên bản cấu hình stack. Ứng dụng có thể cần reload/restart để dùng file.
- Không bổ sung editor cho host filesystem, Docker socket không có Agent,
  Kubernetes hay Edge Agent. Upload/download/delete hiện có vẫn dùng API cũ.

## Bản nháp và xung đột

API trả SHA-256 của toàn bộ bytes làm `Version`. Save kiểm tra phiên bản hiện
tại trước khi ghi. File đã đổi trả **409**; bản nháp trong editor được giữ nguyên,
người dùng có thể copy trước khi chọn **Reload file**. Đóng/reload/chuyển trang
khi có thay đổi chưa lưu sẽ có nhắc bỏ bản nháp. Không lưu nội dung editor vào
localStorage hoặc cache React Query.

Các Save qua editor trên cùng instance Portainer được tuần tự hóa. Đây không
phải khóa filesystem: ứng dụng, upload cũ hoặc công cụ khác vẫn có thể ghi trong
khoảng giữa kiểm tra và lưu. Server đọc lại để xác nhận; nếu không xác nhận
được, UI giữ bản nháp và yêu cầu reload trước khi thử lại. Việc ghi của Agent là
ghi trực tiếp, không phải atomic rename hay cơ chế backup/versioning.

## Phân quyền

- Global Admin và **Environment Admin** được sửa volume trong environment được
  cấp. Quyền Agent mới của Environment Admin giới hạn ở volume, không mở host
  browser hoặc API v1 tùy ý.
- Read-only, Helpdesk, Operator và Standard theo bộ role tùy chỉnh tiếp tục
  không có quyền đọc/ghi file tùy ý.
- User CE chưa gán role tùy chỉnh tuân theo quyền volume/stack hiện có và
  `allowVolumeBrowserForRegularUsers`. Stack policy vẫn chặn đường ghi trực tiếp
  theo ranh giới đã thiết lập trước đó.
- Mỗi request kiểm tra lại quyền; nội dung file có thể chứa thông tin nhạy cảm,
  nên editor chỉ xuất hiện khi có cả quyền đọc và upload file. Metadata không
  được dùng để bỏ qua kiểm tra quyền ở backend.

## API

```text
GET /api/endpoints/{id}/docker/v2/browse/text?volumeID={volume}&path=/app/config.json
PUT /api/endpoints/{id}/docker/v2/browse/text?volumeID={volume}&path=/app/config.json
```

GET và PUT thành công trả `{ "Content": "...", "Version": "sha256..." }`.
PUT yêu cầu cả hai trường, từ chối trường lạ. File phải có sẵn khi đọc kiểm tra.
Response có `Cache-Control: no-store`; giới hạn text 1 MiB, thời gian request
30 giây; ID volume và đường dẫn được kiểm tra trước khi gọi Agent.

Agent 2.45.3 `WriteBigFile` ở API upload v2 không dùng `O_TRUNC`, có thể để lại
bytes cuối khi upload nội dung ngắn hơn. Editor dùng API ghi v1 tương thích
trong Agent **sau khi** hoàn tất kiểm tra quyền volume v2; API v1 đó dùng
`os.WriteFile` và truncate đúng. Không cấp trực tiếp quyền proxy v1 cho role.
Nguồn: [Agent filesystem 2.45.3](https://github.com/portainer/agent/blob/2.45.3/filesystem/filesystem.go),
[Agent browse handlers](https://github.com/portainer/agent/blob/2.45.3/http/handler/browse/browse_put.go).

## Kiểm thử

```powershell
$env:PORTAINER_TEST_IMAGE = 'portainer-custom:2.45.3-volume-editor'
python deploy/standalone/tests/volume_editor_smoke.py
```

Script tạo Portainer DB, Agent, network, volume và tài khoản test riêng, rồi dọn
đúng các resource đó. Chỉ cổng UI test được bind localhost; Agent ở trong network
test. Không sửa volume của ứng dụng đang chạy.
