Version: 1.3 (Cập nhật: Nâng cấp Toàn diện Hệ thống Chống Gian Lận)
Vision: Nền tảng Logistics Xanh nâng tầm lao động yếu thế & Biến rác thải thành giá trị.
1. MÔ HÌNH KINH TẾ (TOKENOMICS)
Đồng tiền nội bộ: Ecopoint (EP) | Tỷ giá quy đổi: 1 EP = 100 VNĐ.
Kiểu dữ liệu Backend: Số nguyên (Integer).
Dòng tiền Doanh thu nền tảng (Platform Revenue):
Phí Cửa Khẩu: Thu 20% tiền mặt từ quỹ tài trợ CSR của Sponsor.
Phí Vận Hành: Thu 20 EP từ ví của Vựa cho mỗi đơn hàng hoàn tất.
Phí Giao Dịch B2B (Phase 3): Thu 2% phí trên tổng Ecopoint khi Tạp hóa nhận thanh toán.
2. HỆ SINH THÁI ỨNG DỤNG (FRONTEND STACK)
1. ecopoint-landing (Next.js 15): Trang Waitlist gọi vốn.
2. ecoPoint (Flutter App): Dành cho User (Khách hàng).
3. ecoPoint Station (Flutter App): Dành cho Hệ thống Vựa (Có phân Role Admin & Staff).
4. ecoPoint Hero (Native Android: Kotlin + C++ NDK): Dành cho Cô chú ve chai dạo (Freelance). Tối ưu chạy mượt trên máy giá rẻ.
5. ecoPoint Partner (Next.js 15, Responsive): Web Portal cho Đối tác B2B (Nhãn hàng tạo Voucher, Tạp hóa làm POS quét mã trừ điểm).
3. KIẾN TRÚC BACKEND (TECH STACK 2026)
Core Services (Golang 1.22+ & sqlc): Point Service, Booking Service, Quest Service, Reward Service (gRPC).
API Gateway: Node.js 22 + NestJS + GraphQL.
Database: PostgreSQL 16 (PostGIS), Redis 7.
Message Broker: Apache Kafka (KRaft mode).
4. TẤM KHIÊN CHỐNG GIAN LẬN (ANTI-FRAUD SHIELD)
Áp dụng cơ chế "Zero-Trust" (Không tin tưởng ai) để bảo vệ toàn vẹn nguồn vốn CSR.
4.1. Chống Khai Khống (Weight Fraud & Collusion):
Double-Check: App ép Driver chụp ảnh mặt cân điện tử (Proof_of_Scale) và nhập Mã PIN 4 số do User cấp (Mã này hết hạn sau 10 phút).
Quyền Phủ Quyết của Vựa (Trị thông đồng): Điểm Ecopoint của User và Driver sau khi chốt đơn chỉ ở mức PENDING. Rác phải được mang về Vựa. Vựa cân lại và nhập số liệu vào ecoPoint Station. Backend đối chiếu: Nếu khối lượng lệch quá 10%, hủy toàn bộ điểm thưởng của đơn và trừ Trust Score.
4.2. Chống Tự Biên Tự Diễn & Spam (Fake Bookings):
Rate Limiting (Redis): Một User tạo tối đa 5 đơn/tuần. Một cặp User - Driver chỉ được khớp lệnh tối đa 3 đơn/tháng.
Device Fingerprinting: Block theo ID phần cứng của thiết bị. Cấm đăng nhập quá 2 tài khoản User trên cùng 1 điện thoại vật lý (chặn farm nick qua App Cloner).
Time-gap Threshold: Thao tác từ lúc "Nhận đơn" → "Tới nơi" → "Hoàn thành" phải > 3 phút. Thao tác siêu tốc sẽ bị treo để Admin duyệt.
Trust Score: Bơm đơn ảo hoặc boom hàng sẽ bị trừ điểm uy tín, dưới mức chuẩn → Khóa vĩnh viễn.
4.3. Chống Tái Chế Ảo (Round-Tripping):
Ecopoint chỉ chuyển sang trạng thái AVAILABLE (Khả dụng) khi Vựa đã bấm xác nhận "Nhập kho" số lượng rác tương ứng trên hệ thống.
4.4. Chống Bào Khuyến Mãi Tân Thủ (Sybil Attack):
Điểm thưởng hoặc Voucher cho người mới tải app bị KHÓA. User bắt buộc phải có ít nhất 1 cuốc giao rác thành công (được Vựa xác nhận) thì Voucher/EP mới được kích hoạt.
5. LỘ TRÌNH TRIỂN KHAI (PHASED ROLLOUT)
🟢 PHASE 1 (MVP Sống còn): Mở luồng User → Vựa → Lính Vựa → Đổi Voucher.
🟡 PHASE 2 (Tác động xã hội): Kết nối Sponsor bơm tiền. Mở khóa ecoPoint Hero.
🔴 PHASE 3 (The Loop): Mở thanh toán POS cho Tạp hóa trên ecoPoint Partner.