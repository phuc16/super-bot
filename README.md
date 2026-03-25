# Discord Super Bot

Bot Discord theo dõi hoạt động, phân tích tương tác và tóm tắt tin nhắn bằng AI.

## Tính năng

- **Export & Tóm tắt AI** — Export tin nhắn kèm tóm tắt bằng GPT
- **Theo dõi hoạt động** — Thống kê tương tác, trao role thành viên tích cực
- **Báo cáo hàng tuần** — Tự động gửi báo cáo engagement lên Discord
- **Phát hiện thành viên không hoạt động** — Identify inactive members
- **Cấu hình linh hoạt** — Mỗi server có cài đặt riêng qua lệnh `/config`

---

## Yêu cầu

- Go 1.21+
- MySQL 8.0+ (hoặc Docker)
- Discord Bot Token
- OpenAI API Key

---

## Hướng dẫn cài đặt

### Bước 1 — Tạo Discord Bot

1. Vào [Discord Developer Portal](https://discord.com/developers/applications) → **New Application**
2. Vào tab **Bot** → **Add Bot** → Copy **Token** (đây là `DISCORD_TOKEN`)
3. Bật các **Privileged Gateway Intents**:
   - ✅ Server Members Intent
   - ✅ Message Content Intent
4. Vào tab **OAuth2 → URL Generator**:
   - Scopes: `bot`, `applications.commands`
   - Bot Permissions: `Read Messages`, `Send Messages`, `Manage Roles`, `Read Message History`
5. Mở URL được tạo ra → Thêm bot vào server của bạn

### Bước 2 — Lấy Server ID

1. Trong Discord, vào **Settings → Advanced** → Bật **Developer Mode**
2. Chuột phải vào tên server → **Copy Server ID** (đây là `DISCORD_GUILD_ID`)

### Bước 3 — Cấu hình môi trường

```bash
git clone https://github.com/phuc16/VCG-super-bot.git
cd VCG-super-bot
go mod download

cp env.example .env
```

Chỉnh sửa file `.env` — **bắt buộc** phải điền 2 giá trị sau:

```env
DISCORD_TOKEN=token_của_bot_bạn
DISCORD_GUILD_ID=id_server_của_bạn
```

Xem toàn bộ các biến cấu hình tại mục [Environment Variables](#environment-variables) bên dưới.

### Bước 4 — Khởi động Database

**Dùng Docker (khuyến nghị):**

```bash
docker-compose -f docker/docker-compose.yml up -d mysql
```

**Hoặc tự cài MySQL** rồi tạo database và user tương ứng với các giá trị `DB_*` trong `.env`.

### Bước 5 — Chạy Bot

**Development:**

```bash
go run cmd/bot/main.go
```

**Production (build binary):**

```bash
go build -o bot ./cmd/bot
./bot
```

**Docker (chạy cả bot lẫn MySQL):**

```bash
docker-compose -f docker/docker-compose.yml up -d
docker-compose -f docker/docker-compose.yml logs -f bot
```

Bot sẽ tự động chạy migration database khi khởi động.

---

## Đổi sang server khác

Chỉ cần thay 2 dòng trong `.env` rồi restart bot:

```env
DISCORD_TOKEN=token_bot_mới
DISCORD_GUILD_ID=id_server_mới
```

---

## Commands

| Lệnh | Mô tả | Quyền |
|------|-------|-------|
| `/export` | Export tin nhắn kèm tóm tắt AI | Tất cả |
| `/activity` | Xem thống kê hoạt động | Tất cả |
| `/leaderboard` | Top 10 thành viên tích cực | Tất cả |
| `/report` | Tạo/xem báo cáo tuần | Admin |
| `/inactive` | Tìm thành viên không hoạt động | Admin |
| `/config` | Cài đặt server | Admin |
| `/help` | Xem hướng dẫn | Tất cả |

### Chi tiết `/export`

```
/export type:recent hours:24 language:vi
/export type:date_range from:"2025-01-01" end:"2025-01-07"
/export type:date_range from:"2025-01-01"
```

- `type`: `recent` hoặc `date_range`
- `hours`: Số giờ nhìn lại (1–168, mặc định 1) — dùng khi `type:recent`
- `from`: Ngày bắt đầu `YYYY-MM-DD` hoặc `YYYY-MM-DD HH:MM` — dùng khi `type:date_range`
- `end`: Ngày kết thúc (mặc định: hiện tại)
- `language`: `vi` (mặc định) hoặc `en`

### Chi tiết `/config`

Cấu hình server sau khi bot đã vào:

```
/config set reports_channel:#channel-name
/config set active_role:Tích Cực
/config set inactivity_days:14
/config set timezone:Asia/Ho_Chi_Minh
```

---

## Environment Variables

| Biến | Bắt buộc | Mặc định | Mô tả |
|------|----------|---------|-------|
| `DISCORD_TOKEN` | ✅ | — | Token của Discord bot |
| `DISCORD_GUILD_ID` | ✅ | — | ID của server Discord |
| `OPENAI_API_KEY` | ✅* | — | API key OpenAI (*không cần nếu `MOCK_GPT_API=true`) |
| `DB_HOST` | ✅ | `localhost` | Host MySQL |
| `DB_PORT` | | `3306` | Port MySQL |
| `DB_USER` | ✅ | — | User MySQL |
| `DB_PASSWORD` | ✅ | — | Password MySQL |
| `DB_NAME` | ✅ | — | Tên database |
| `APP_ENV` | | `development` | `development` / `staging` / `production` |
| `LOG_LEVEL` | | `info` | `debug` / `info` / `warn` / `error` |
| `PORT` | | `8080` | Port HTTP health check |
| `TIMEZONE` | | `UTC` | Timezone (vd: `Asia/Ho_Chi_Minh`) |
| `GPT_MODEL` | | `gpt-4-turbo-preview` | Model OpenAI |
| `GPT_MAX_TOKENS` | | `4096` | Số token tối đa |
| `GPT_DAILY_BUDGET_USD` | | `10.0` | Ngân sách GPT mỗi ngày (USD) |
| `GPT_MAX_CONCURRENT_REQUESTS` | | `5` | Số request GPT chạy song song |
| `RATE_LIMIT_EXPORT_PER_HOUR` | | `3` | Số lần dùng `/export` mỗi giờ |
| `MOCK_GPT_API` | | `false` | `true` để test không cần OpenAI key |
| `DEBUG_MODE` | | `false` | Bật debug mode |

---

## Database Migrations

Migration chạy tự động khi bot khởi động. Nếu cần chạy thủ công:

```bash
# Áp dụng tất cả migration
./bot migrate up

# Rollback migration gần nhất
./bot migrate down
```

---

## Background Tasks

Các tác vụ tự động chạy theo lịch:

| Tác vụ | Lịch | Mô tả |
|--------|------|-------|
| Trao role tích cực | Thứ Hai 00:00 | Top 5 thành viên hoạt động nhất |
| Báo cáo tuần | Thứ Hai 00:05 | Gửi báo cáo lên channel đã cấu hình |
| Đồng bộ username | Mỗi ngày 03:00 | Cập nhật username/avatar từ Discord |
| Lưu trữ dữ liệu | Mỗi ngày 02:00 | Archive data cũ hơn 90 ngày |
| Dọn dẹp | Mỗi ngày 04:00 | Xóa vĩnh viễn dữ liệu đã soft-delete |

---

## Health Check

Bot expose các endpoint tại port 8080:

```
GET /health   # Trạng thái tổng thể (database + Discord + GPT)
GET /ready    # Readiness check
GET /live     # Liveness check
```

---

## Cấu trúc project

```
.
├── cmd/bot/              # Entry point
├── internal/
│   ├── discord/          # Discord client & commands
│   ├── gpt/              # Tích hợp OpenAI
│   ├── services/         # Business logic
│   ├── scheduler/        # Background tasks
│   ├── storage/          # Database layer
│   ├── config/           # Cấu hình
│   └── models/           # Data models
├── pkg/logger/           # Logging
├── scripts/migrations/   # SQL migrations
└── docker/               # Docker config
```
