# Group và role cho Docker Standalone

Portainer **Team** là group người dùng. **Environment group** là nhóm environment;
hai khái niệm này khác nhau. Bản tùy chỉnh CE 2.45.3 cho phép gán role cho Team
hoặc user trên từng environment và kế thừa từ environment group.

## Bộ role

| Role | Phạm vi tài nguyên | Thao tác |
| --- | --- | --- |
| Environment administrator | Toàn environment được gán | Tạo, sửa, xóa tài nguyên Docker và quản lý stack Compose |
| Operator | Toàn environment được gán | Xem, start/stop/restart, cập nhật container hiện có, console; không tạo/xóa tài nguyên hay sửa Compose |
| Helpdesk | Toàn environment được gán | Xem thông tin/logs; không ghi, console hay đọc file qua archive/export |
| Standard user | Tài nguyên sở hữu bởi user/Team hoặc public | Quản lý tài nguyên và triển khai Compose trong phạm vi được cấp |
| Read-only user | Tài nguyên sở hữu bởi user/Team hoặc public | Xem thông tin/logs; không ghi, console hay đọc file qua archive/export |

Đây là bộ role tùy chỉnh lấy mô hình từ
[Portainer Business roles](https://docs.portainer.io/admin/user/roles), không phải
triển khai đầy đủ Business Edition. Operator hiện không đổi image/recreate qua
Compose. Không bổ sung Edge Administrator, Namespace roles, Swarm hay Kubernetes.
Environment Admin không được quản trị user, role, ownership, settings toàn
Portainer hoặc các API quản trị host/plugin/Swarm. **Global Administrator** vẫn
là loại tài khoản riêng, chỉ admin toàn hệ thống mới cấp được.

Environment Admin được browse/upload/download/rename/delete và Edit text trong
volume qua Docker Agent. Các role tùy chỉnh khác không được đọc/ghi file qua
volume browser. Quyền này không cấp host browser hoặc proxy Agent v1 tùy ý;
xem [editor volume](VOLUME-TEXT-EDITOR.md).

## Cách gán

1. Vào **Users → Teams**, tạo Team và thêm thành viên.
2. Vào **Environments → environment → Access management**; chọn Team ở
   **Group or user**, chọn **Environment role**, bấm **Assign role**.
3. Muốn áp dụng cho nhiều environment: vào **Groups → environment group →
   Access**, gán Team/role tại đây. Màn hình từng environment hiển thị quyền kế thừa.
4. Role của Team đã gán có thể đổi bằng **Save role** hoặc bỏ bằng **Remove access**.
5. Với Read-only/Standard, global admin cấp thêm ownership của stack/tài nguyên
   cho Team nếu tài nguyên chưa public. Chỉ cấp quyền environment không tự cấp
   quyền sở hữu tài nguyên riêng tư.

Chỉ global admin được thay đổi các gán quyền. Team leader hiện có vẫn quản lý
thành viên trong Team của mình theo CE; vì vậy hãy coi việc bổ nhiệm team leader
là quyền cấp/thu hồi role được gán cho Team đó.

## Giải quyết nhiều quyền

Thứ tự ưu tiên giữ theo màn hình effective access hiện có:

1. User trực tiếp trên environment.
2. User kế thừa từ environment group.
3. Team trực tiếp trên environment.
4. Team kế thừa từ environment group.

Trong cùng một tầng Team, role ưu tiên cao nhất theo thứ tự **Read-only →
Standard → Helpdesk → Operator → Environment administrator** được chọn. Các
role không cộng gộp. Khi muốn quản lý hoàn toàn bằng Team, xóa/đổi các grant
trực tiếp cho user; grant trực tiếp cũ có thể ưu tiên hơn role Read-only của Team.

Nếu hai Team cùng tầng có cùng role, grant có `StandaloneRole` rõ ràng được
ưu tiên hơn grant CE legacy; Team ID chỉ dùng để phân xử khi cả hai cùng loại.
Standard vẫn tuân thủ `AllowStackManagementForRegularUsers`; Environment Admin
được miễn giới hạn này và giới hạn bind mount/privileged dành cho regular user.
`commit` image chỉ dành cho Environment Admin trong bộ role mới. Với tài khoản
CE legacy, `commit` vẫn phải qua kiểm tra ownership của container.

Backend đọc quyền và membership hiện tại cho mỗi request mới. Đổi role hoặc xóa
thành viên không cần cấp lại JWT. Refresh trình duyệt để cập nhật nút trên UI.
Kết nối streaming/console đã mở và thao tác đang chạy không bị ngắt chủ động.

## Kết hợp với quyền theo stack

Read-only và Helpdesk luôn bị chặn ghi, kể cả stack policy có cấp Upload/Apply/
Deploy/Edit. Standard và Operator vẫn phải thỏa ownership + action grants khi
stack có policy; Operator không được Edit. Environment Admin được quản lý mọi
stack trong environment, nhưng việc sửa policy, khai báo config target và phục
hồi thủ công vẫn do global admin.

Khi environment có ít nhất một stack policy, rào chắn Docker trực tiếp hiện có
vẫn chặn mọi non-admin trừ Environment Admin: exec, archive, tạo container tùy ý
và những thao tác có thể vượt qua action grants. Dùng API/tab Configuration để
Upload/Apply/Rollback. Public Docker webhooks và Git auto-update bị chặn khi có
stack policy **hoặc** role môi trường rõ ràng, vì không có user thực hiện để
kiểm tra quyền.

Role kiểm soát API, không phải sandbox giữa các workload chung Docker daemon.
Người được tạo container/sửa Compose có thể ảnh hưởng host theo thiết lập bảo mật
environment; console có quyền của tiến trình trong container. Chỉ giao các
quyền này cho người tin cậy. Người có Docker socket/SSH bên ngoài Portainer không
chịu ràng buộc bởi role. Dùng Vault cho secret thay vì biến/Compose/file có thể
đọc trên màn hình inspect.

## Tương thích và dữ liệu

API gán quyền vẫn dùng `PUT /api/endpoints/{id}` hoặc
`PUT /api/endpoint_groups/{id}`. Ví dụ Team 7 có Read-only:

```json
{
  "TeamAccessPolicies": {
    "7": { "RoleId": 4, "StandaloneRole": "read-only" }
  }
}
```

`StandaloneRole` nhận `environment-admin`, `operator`, `helpdesk`, `standard`,
`read-only`. Khi cập nhật map policy phải giữ lại các grant khác muốn bảo toàn.
UI tự giữ các grant còn lại trong bản dữ liệu đã tải.

Không diễn giải lại `RoleId` cũ: CE từng lưu `RoleId=1` cho grant thông thường.
Policy thiếu `StandaloneRole` giữ hành vi CE cũ; admin phải chủ động chọn role
mới. Group Access có lựa chọn **Legacy CE access (all environment types)**,
lưu `RoleId=1` và bỏ `StandaloneRole`, để cấp quyền cho group hỗn hợp. Không cần migration
phá hủy dữ liệu. Không chạy lại image CE gốc trên DB này để tiếp tục sử dụng
role, vì CE gốc không thực thi field bổ sung.

Các role áp dụng cho Docker trực tiếp và Docker Agent. Với environment ngoài
phạm vi này, grant role mới được kế thừa sẽ bị từ chối thay vì được hiểu thành
quyền CE không giới hạn. Dùng legacy access cho group hỗn hợp hoặc tách group
nếu muốn áp dụng role riêng cho Docker. Quản lý phiên bản file cấu hình ở tab stack vẫn chỉ hỗ trợ Linux
socket cục bộ theo [README](README.md). Bộ kiểm thử role tổng quát dùng Docker
socket cục bộ; editor volume và quyền đọc/ghi, thu hồi quyền đã được kiểm thử
với Agent 2.45.3 thật. Đây chưa phải kiểm thử toàn bộ tính năng qua Agent.

Chạy `python deploy/standalone/tests/roles_smoke.py` với
`PORTAINER_TEST_IMAGE=portainer-custom:2.45.3-roles` để kiểm tra bằng tài khoản,
Team, stack và container tạm thời. Script xóa tài nguyên do chính nó tạo.
