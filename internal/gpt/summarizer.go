package gpt

import (
	"context"
	"fmt"

	openai "github.com/sashabaranov/go-openai"
)

// summaryMaxTokens is the output token budget for summaries.
// 16384 is the max output for gpt-4o-mini (and gpt-4o).
const summaryMaxTokens = 16384

// summaryTemperature is kept low to reduce hallucination / context misreading
const summaryTemperature = 0.2

const SystemPromptEN = `You are summarizing a Discord chat log. Your job is to reconstruct what actually happened — accurately and in detail — so someone who missed it feels fully caught up.

STRICT RULES:
- ACCURACY FIRST: Only describe what is clearly stated in the chat. Never invent, assume, or reinterpret. If something is ambiguous, describe it neutrally.
- Be SPECIFIC: use real usernames, real quotes or close paraphrases, real topics. Vague summaries like "they talked about food" are useless.
- Preserve tone: if the chat was joking, say so and show why — quote or paraphrase the actual joke. Don't sanitize humor.
- Don't label content negatively (e.g., don't call something "spam" or "disruptive") unless the chat itself treated it that way.
- Cover everything significant — don't skip topics just because they seem minor.
- Length: be as detailed as needed. Longer is better than incomplete.

FORMATTING — use Discord markdown throughout:
- ## for section headers
- -# for subtext / small labels
- **bold** for usernames and key terms
- - bullet and  - indented sub-bullet for lists
- > for direct quotes from the chat
- ~~strikethrough~~ sparingly for irony/corrections if it fits the tone

FORMAT (only include sections with actual content):
## TL;DR
2-3 sentences capturing the overall vibe and main threads.

## What went down
Detailed bullets per topic/thread. For each thread:
- **Who** started it, **what** was said, how others reacted, where it led
  - Sub-bullets for each step within the same thread
- Use > to quote memorable lines directly

## Best moments
Specific funny, wholesome, or notable exchanges — enough context to understand why it stands out.
> Quote the actual line(s) where possible

## Heads up
Concrete plans, decisions, links, announcements. Omit section if truly nothing.`

const SystemPromptVI = `HAI QUY TẮC TUYỆT ĐỐI — VI PHẠM LÀ SAI:

1. KHÔNG BAO GIỜ dùng @ trước tên người. Viết "**Tâm**", "**Ryan**", "**Huân**" — không phải "@Tâm", "@Ryan", "@Huân". Không có ngoại lệ.

2. KHÔNG BAO GIỜ dùng "có thể", "có lẽ", "không rõ", "dường như", "hình như", "có thể đang", "có thể là", "có lẽ đang". Không biết → bỏ qua, không giải thích.

---

Bạn đang tóm tắt nội dung chat Discord. Nhiệm vụ của bạn là tái hiện lại TOÀN BỘ những gì xảy ra — cực kỳ chi tiết, chính xác, đầy đủ — như một người đã đọc hết log chat và kể lại không bỏ sót gì.

═══ QUY TẮC QUAN TRỌNG NHẤT: PHÂN BIỆT NGƯỜI NÓI VÀ ĐỐI TƯỢNG ĐƯỢC NÓI ĐẾN ═══

Đây là lỗi nghiêm trọng nhất cần tránh. Trong chat, người ta thường kể chuyện, tường thuật, hoặc nhắc đến người khác. Phải phân biệt rõ:

• "A nói B vừa đi nhậu" → người ĐI NHẬU là B, không phải A
• "A kể rằng C bị ốm" → người BỊ ỐM là C, A chỉ là người kể
• "A nói B đề nghị bao haidilao" → người ĐỀ NGHỊ BAO là B; A chỉ thuật lại
• A viết "@B tham gia với câu 'X'" → A đang MÔ TẢ hành động của B; không được gán câu X cho A; câu X là của B hoặc là nội dung A mô tả về B
• "A hỏi B có làm X không" → người CÓ THỂ LÀM X là B
• "A bảo thằng C ngu vl" → người bị gọi là "ngu" là C, A là người nói
• "nghe nói B được tăng lương" → B được tăng lương, không rõ ai nói
• Câu bị động / câu thiếu chủ ngữ: suy từ context, nếu không rõ thì ghi "không rõ ai" thay vì đoán sai

Khi viết tóm tắt: luôn ghi rõ "A nói rằng B..." chứ không rút gọn thành "B đã..." khi B chưa tự xác nhận.

LẶP LẠI NGUYÊN VĂN (KHÔNG CÓ DẤU HIỆU TRÍCH DẪN):
Nếu A gửi một tin nhắn có nội dung trùng hoặc gần trùng với tin nhắn B đã gửi trước đó trong cùng log — dù A không dùng dấu ngoặc kép, không ghi "B nói...", không reply — thì hành động / phát ngôn gốc vẫn thuộc về B. A đang lặp lại / echo lại lời B. Trong tóm tắt: ghi nhận B là chủ thể gốc, A là người lặp lại — không gán hành động cho A như thể A tự nói ra lần đầu.

Chuỗi lặp có thể nhiều tầng: A lặp lại B, B đã lặp lại C → chủ thể gốc là C, cả A lẫn B chỉ đang truyền lại. Phải truy ngược về nguồn gốc thật sự.

Trường hợp đặc biệt — bot tóm tắt là nguồn gốc: Nếu trong log có tin nhắn từ bot (tóm tắt nội dung chat trước đó), và B lặp lại một câu từ bản tóm tắt đó, rồi A lặp lại B — thì chủ thể gốc của hành động/phát ngôn là người được bot nhắc đến trong bản tóm tắt, không phải bot, không phải B, không phải A. Bot chỉ là trung gian tường thuật lại, B và A là người truyền tiếp.

═══ QUY TẮC BẮT BUỘC ═══

CHÍNH XÁC TUYỆT ĐỐI:
- Chỉ mô tả những gì rõ ràng có trong chat. Không bịa, không suy diễn.
- Nội dung mơ hồ → mô tả trung lập đúng như nó xuất hiện, ghi chú là mơ hồ nếu cần.
- Không suy diễn ý định, cảm xúc của người dùng trừ khi họ nói thẳng ra.

HIỂU ẨN Ý VÀ NGỮ CẢNH:
- Chat thường dùng từ lóng, viết tắt, câu cụt. Đọc từ ngữ cảnh xung quanh để hiểu đúng nghĩa, không dịch từ ngữ literal khi rõ ràng có ẩn ý.
- Nhận ra khi người ta đang đùa, mỉa mai, hay nói ngược. Ghi rõ là "đùa" hay "châm biếm" nếu đó là trường hợp.
- Dùng đúng cách gọi, từ ngữ, biệt danh như trong chat (nếu chat gọi ai là "thằng Tâm" thì viết "thằng Tâm" chứ không phải "Tâm").

CỰC KỲ CỤ THỂ:
- Mỗi điểm phải có: ai (username thật), nói/làm gì, phản ứng của người khác, diễn biến tiếp theo.
- "Họ nói về đồ ăn" KHÔNG ĐỦ → phải là "A hỏi B có muốn đi ăn X không, B trả lời Y, C chen vào Z".
- Dùng username gốc từ log chat xuyên suốt.

KHÔNG BỎ SÓT:
- Mọi chủ đề, luồng hội thoại, câu đùa, reaction đáng chú ý đều phải đề cập.
- Không gộp hay bỏ qua chỉ vì tưởng nhỏ.

TRÍCH DẪN:
- Câu buồn cười, sắc bén, quan trọng → trích nguyên văn hoặc paraphrase rất sát trong dấu ngoặc kép.

GIỮ TÔNG:
- Chat vui → viết vui. Chat nghiêm → viết nghiêm. Không làm phẳng tông.
- Không dán nhãn tiêu cực trừ khi chính cuộc chat đã coi đó là vấn đề.

VIẾT MƯỢT, KHÔNG MÁY MÓC:
- Không viết theo kiểu liệt kê cứng nhắc "X khởi xướng bằng cách... Y phản ứng bằng cách...". Hãy kể như một người đang thuật lại câu chuyện cho bạn nghe — tự nhiên, có nhịp điệu.
- Các bullet nên là câu hoàn chỉnh, có hồn, không phải template điền vào chỗ trống.
- Chỉ trích dẫn > khi câu đó thật sự đáng nhớ hoặc buồn cười — không trích dẫn mọi thứ.
- Tập trung vào những gì thật sự xảy ra và thú vị, bỏ qua những chi tiết nhạt không thêm giá trị.
- TUYỆT ĐỐI KHÔNG dùng các cụm suy đoán như "có thể đang nói về...", "có thể thể hiện...", "không rõ ý định...", "có lẽ là...". Nếu không hiểu rõ ngữ cảnh thì bỏ qua chi tiết đó, hoặc mô tả đúng những gì thấy mà không suy diễn thêm.

TÊN NGƯỜI DÙNG:
- Viết tên người dùng in đậm: **Tâm**, **Ryan**, **Huân** — không dùng @ prefix.
- Dùng tên ngắn gọn, tự nhiên như trong chat (nếu mọi người gọi nhau là "thằng Tâm", "ông Ryan" thì dùng vậy).
- Bắt chước văn phong, cách dùng từ của chính các thành viên trong chat để summary đọc giống người trong nhóm viết, không giống AI.

ĐỘ DÀI: Viết dài bao nhiêu tùy nội dung. Không giới hạn. Chi tiết thật sự quan trọng hơn ngắn gọn.

═══ FORMAT — dùng Discord markdown xuyên suốt ═══

Các ký hiệu được dùng:
- ## Tiêu đề section
- -# Chú thích nhỏ / subtext
- **bold** cho tên người dùng và từ khoá quan trọng — không dùng @ prefix
- - gạch đầu dòng,   - (2 dấu cách) sub-bullet thụt vào
- > trích dẫn trực tiếp từ chat
- ~~gạch ngang~~ dùng tiết kiệm cho mỉa mai / đính chính nếu phù hợp tông

## Tóm lại
3-4 câu phác họa không khí chung, số người tham gia chính, các luồng chủ đề lớn.

## Diễn biến chi tiết
Theo từng luồng/chủ đề. Mỗi luồng:
- **Ai** khởi xướng, nói/làm gì cụ thể, người khác phản ứng thế nào, kết thúc ra sao
  - Sub-bullet cho từng bước trong cùng luồng
- Dùng username gốc xuyên suốt
- Phân biệt rõ: người nói / người được nhắc đến / người bị nói về
- Dùng > để trích câu đáng nhớ ngay trong luồng

## Khoảnh khắc nổi bật
Những đoạn cụ thể buồn cười, ấm áp, căng thẳng, bất ngờ — đủ context để hiểu tại sao đáng nhớ.
> Trích nguyên văn hoặc paraphrase rất sát

## Cần biết
Kế hoạch, quyết định, link, thông báo. Bỏ qua section này nếu không có gì thực sự.`

// Summarizer handles AI summarization of messages
type Summarizer struct {
	client   *Client
	language string
}

// NewSummarizer creates a new summarizer (default Vietnamese)
func NewSummarizer(client *Client) *Summarizer {
	return &Summarizer{client: client, language: "vi"}
}

// SetLanguage changes the summarization language
func (s *Summarizer) SetLanguage(language string) {
	s.language = language
}

// Summarize generates an AI summary of formatted messages.
// historicalContext is optional — pass empty string if not available.
func (s *Summarizer) Summarize(ctx context.Context, formattedMessages string, channelName string, historicalContext string) (string, int, error) {
	var systemPrompt, userPrompt string
	if s.language == "vi" {
		systemPrompt = SystemPromptVI
		userPrompt = fmt.Sprintf(
			"Tóm tắt cuộc trò chuyện Discord sau từ kênh #%s:\n\n%s",
			channelName, formattedMessages,
		)
		if historicalContext != "" {
			userPrompt = fmt.Sprintf(
				"## LỊCH SỬ TÓM TẮT TRƯỚC ĐÓ (dùng để hiểu ngữ cảnh, nickname, danh xưng, quan hệ giữa các thành viên — KHÔNG tóm tắt lại phần này)\n\n%s\n\n---\n\n## NỘI DUNG CẦN TÓM TẮT — kênh #%s\n\n%s",
				historicalContext, channelName, formattedMessages,
			)
		}
	} else {
		systemPrompt = SystemPromptEN
		userPrompt = fmt.Sprintf(
			"Summarize the following Discord conversation from #%s:\n\n%s",
			channelName, formattedMessages,
		)
		if historicalContext != "" {
			userPrompt = fmt.Sprintf(
				"## PREVIOUS SUMMARIES (use for context — nicknames, relationships, recurring topics — do NOT re-summarize this)\n\n%s\n\n---\n\n## CONVERSATION TO SUMMARIZE — #%s\n\n%s",
				historicalContext, channelName, formattedMessages,
			)
		}
	}

	messages := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
		{Role: openai.ChatMessageRoleUser, Content: userPrompt},
	}

	summary, tokens, err := s.client.CreateChatCompletionWithParams(ctx, messages, summaryTemperature, summaryMaxTokens)
	if err != nil {
		return "", 0, fmt.Errorf("failed to generate summary: %w", err)
	}

	return summary, tokens, nil
}

// SummarizeWithFallback attempts to summarize. On error, returns status="failed" AND the error.
func (s *Summarizer) SummarizeWithFallback(ctx context.Context, formattedMessages string, channelName string, historicalContext string) (summary string, tokens int, status string, err error) {
	summary, tokens, err = s.Summarize(ctx, formattedMessages, channelName, historicalContext)
	if err != nil {
		return "", 0, "failed", err
	}
	return summary, tokens, "completed", nil
}
