# Discord Super Bot

[English](README.md) | **Tiếng Việt**

Bot Discord cho cộng đồng: game Ma Sói, duel điểm, tóm tắt tin nhắn bằng AI, keyword command tự đặt và đăng bài AI định kỳ.

## Tính năng

- **Ma Sói (`/werewolf`)** — Game social deduction đầy đủ cho 5–20 người, 11 vai trò, chơi trực tiếp trong Discord
- **Duel (`/duel`)** — Thách đấu xúc xắc, cược điểm, có streak, bảng xếp hạng và giải đấu hẹn giờ
- **Export & Tóm tắt AI (`/export`)** — Export tin nhắn trong kênh kèm tóm tắt bằng GPT
- **Keyword command (`/addcom`)** — Tự tạo lệnh `/tên` trả về text và/hoặc ảnh
- **Topic AI (`/addtopic`)** — Bot tự viết và đăng bài về chủ đề cho sẵn theo lịch
- **Roast (`/roast`)** — AI "cà khịa" một thành viên dựa trên tin nhắn gần đây

---

## Yêu cầu

- Go 1.24+
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
   - Bot Permissions: xem mục [Quyền bot cần có](#quyền-bot-cần-có) bên dưới
5. Mở URL được tạo ra → Thêm bot vào server của bạn

### Bước 2 — Lấy Server ID

1. Trong Discord, vào **Settings → Advanced** → Bật **Developer Mode**
2. Chuột phải vào tên server → **Copy Server ID** (đây là `DISCORD_GUILD_ID`)

### Bước 3 — Cấu hình môi trường

```bash
git clone https://github.com/phuc16/super-bot.git
cd super-bot
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

## Quyền bot cần có

| Quyền | Dùng để làm gì |
|-------|----------------|
| View Channel | Đọc kênh |
| Send Messages | Đăng thông báo, kết quả |
| Read Message History | `/export`, `/roast` cần đọc tin nhắn cũ |
| Manage Roles | Khoá chat kênh khi chơi Ma Sói, làm câm lặng mục tiêu của Kẻ Phá Hoại |
| Create Private Threads | Tạo thread riêng cho cả bàn và cho bầy Sói |
| Send Messages in Threads | Gửi phiếu cắn, tóm tắt trong thread |
| Manage Threads | Khoá thread theo ngày/đêm, thêm–bớt thành viên, xoá thread khi hết ván |

**Ma Sói cần đủ cả 7 quyền trên.** `/werewolf start` kiểm tra trước khi mở bàn và báo rõ thiếu quyền nào — thay vì mở ván rồi hỏng giữa chừng.

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
| `/werewolf` | Trò chơi Ma Sói | Tất cả |
| `/duel` | Thách đấu xúc xắc, cược điểm | Tất cả |
| `/duel-leaderboard` | Bảng xếp hạng duel | Tất cả |
| `/duel-history` | Lịch sử đấu của bạn | Tất cả |
| `/duel-challenge-all` | Hẹn giờ giải đấu duel | Admin |
| `/duel-reset` | Reset toàn bộ điểm duel của server | Admin |
| `/export` | Export tin nhắn kèm tóm tắt AI | Tất cả |
| `/roast` | AI cà khịa một thành viên | Tất cả |
| `/addcom` | Thêm keyword command | Admin |
| `/delcom` | Xoá keyword command | Admin |
| `/listcom` | Xem danh sách keyword command | Tất cả |
| `/addtopic` | Thêm topic để bot tự viết định kỳ | Admin |
| `/deltopic` | Xoá topic AI | Admin |
| `/listtopic` | Xem danh sách topic AI | Tất cả |
| `/posttopic` | Đăng ngay một bài AI về chủ đề bất kỳ | Admin |
| `/help` | Xem hướng dẫn (`lang:vi` hoặc `lang:en`) | Tất cả |

### Chi tiết `/werewolf`

```
/werewolf start                                  # mở bàn
/werewolf start reveal-on-death:false            # chết không lộ vai trò
/werewolf start custom-roles:true                # tự chọn số lượng từng vai
/werewolf status                                 # xem ván đang chạy
/werewolf leave                                  # rời ván
/werewolf force-stop                             # huỷ ván (Admin)
```

- **5–20 người chơi.** Mỗi kênh chạy một ván tại một thời điểm; nhiều kênh chạy song song được.
- **11 vai trò:** Dân Làng, Ma Sói, Tiên Tri, Bác Sĩ, Phù Thủy, Thợ Săn, Sói Con, Sói Nguyền, Kẻ Phản Bội, Kẻ Phá Hoại, Thần Tình Yêu.
- `reveal-on-death` (mặc định `true`) — có công bố vai trò của người chết hay không. Ảnh hưởng cả danh sách vai trò hiện trong thông báo đêm.
- `custom-roles` (mặc định `false`) — người mở bàn tự chọn số lượng từng vai qua bảng riêng. Chế độ này **không kiểm tra cân bằng**: muốn lệch hẳn về một phe cũng được.
- Xem `/help werewolf` để biết chi tiết từng vai và điều kiện thắng.

### Chi tiết `/export`

```
/export type:recent hours:24 language:vi
/export type:date_range from:"2026-01-01" end:"2026-01-07"
/export type:since_last
```

- `type`: `recent`, `date_range`, hoặc `since_last`
- `hours`: Số giờ nhìn lại — dùng khi `type:recent`
- `from` / `end`: `YYYY-MM-DD` hoặc `YYYY-MM-DD HH:MM` — dùng khi `type:date_range` (`end` mặc định là hiện tại)
- `language`: `vi` (mặc định) hoặc `en`
- `public`: `true` để đăng công khai thay vì chỉ mình bạn thấy

### Chi tiết `/addcom`

```
/addcom keyword:gei response:"cái đồ đáng iu" image:<file>
```

Sau đó gõ `/gei` trong kênh là bot trả lời.

Ảnh được **lưu lại trong database**, không phải lưu link. Link attachment của Discord có chữ ký hết hạn sau vài giờ và không gia hạn được — bản cũ lưu link nên ảnh biến mất sau một thời gian ngắn. Giới hạn 8 MB mỗi ảnh.

---

## Environment Variables

| Biến | Bắt buộc | Mặc định | Mô tả |
|------|----------|---------|-------|
| `DISCORD_TOKEN` | ✅ | — | Token của Discord bot |
| `DISCORD_GUILD_ID` | ✅ | — | ID của server Discord |
| `OPENAI_API_KEY` | ✅* | — | API key OpenAI (*không cần nếu `MOCK_GPT_API=true`) |
| `DB_HOST` | | `localhost` | Host MySQL |
| `DB_PORT` | | `3306` | Port MySQL |
| `DB_USER` | | `superbot` | User MySQL |
| `DB_PASSWORD` | | *(rỗng)* | Password MySQL |
| `DB_NAME` | | `superbot_dev` | Tên database |
| `APP_ENV` | | `development` | `development` / `staging` / `production` |
| `LOG_LEVEL` | | `info` | `debug` / `info` / `warn` / `error` |
| `PORT` | | `8080` | Port HTTP health check |
| `TIMEZONE` | | `UTC` | Timezone (vd: `Asia/Ho_Chi_Minh`) |
| `GPT_MODEL` | | `gpt-4o-mini` | Model OpenAI |
| `GPT_MAX_TOKENS` | | `4096` | Số token tối đa |
| `GPT_DAILY_BUDGET_USD` | | `0.034` | Ngân sách GPT mỗi ngày (USD) |
| `GPT_MAX_CONCURRENT_REQUESTS` | | `5` | Số request GPT chạy song song |
| `GPT_REQUEST_TIMEOUT_SECONDS` | | `90` | Timeout mỗi request GPT (giây) |
| `RATE_LIMIT_EXPORT_PER_HOUR` | | `3` | Số lần dùng `/export` mỗi giờ |
| `MOCK_GPT_API` | | `false` | `true` để test không cần OpenAI key |
| `DEBUG_MODE` | | `false` | Bật debug mode |

Các biến `DB_*` đều có giá trị mặc định cho môi trường dev, nhưng **phải đặt lại khi chạy thật**. `GPT_DAILY_BUDGET_USD` mặc định rất thấp (`0.034`) — nâng lên nếu dùng nhiều lệnh AI, nếu không bot sẽ dừng gọi GPT khi hết ngân sách trong ngày.

---

## Database Migrations

Migration chạy tự động khi bot khởi động, đọc file SQL trong `scripts/migrations/`
theo thứ tự tên file. Chạy thủ công:

```bash
./bot migrate up        # áp dụng migration còn thiếu
./bot migrate down      # rollback migration gần nhất
./bot migrate version   # xem version hiện tại
```

Trạng thái ván Ma Sói **chỉ nằm trong bộ nhớ** — restart bot giữa ván là mất ván đó. Bot sẽ tự mở khoá lại các kênh đang bị khoá khi tắt.

---

## Background Tasks

| Tác vụ | Lịch | Mô tả |
|--------|------|-------|
| Đăng bài AI | Mỗi phút | Kiểm tra topic đến hạn (`/addtopic`) và đăng bài |

Giải đấu duel hẹn giờ qua `/duel-challenge-all` chạy trong bộ nhớ, không qua scheduler — cũng mất khi restart.

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
│   ├── discord/
│   │   ├── commands/     # Mỗi slash command một file; werewolf tách theo giai đoạn
│   │   └── events/       # Handler cho MESSAGE_CREATE (keyword command)
│   ├── gpt/              # Tích hợp OpenAI
│   ├── services/         # Business logic
│   ├── scheduler/        # Background tasks
│   ├── storage/          # Database layer + migrator
│   ├── config/           # Cấu hình
│   └── models/           # Data models
├── pkg/logger/           # Logging
├── scripts/migrations/   # SQL migrations
└── docker/               # Docker config
```
