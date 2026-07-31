package telegram

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/stretchr/testify/require"
	"github.com/sudoHG/x-monitor/internal/domain"
	"github.com/sudoHG/x-monitor/internal/infrastructure/config"
	"go.uber.org/zap"
)

type capturedRequest struct {
	path   string
	chatID string
	text   string
}

func TestNewRequiresTokenEnvironmentValue(t *testing.T) {
	t.Parallel()

	_, err := New(config.Config{
		Telegram: config.Telegram{TokenEnv: "XMONITOR_TELEGRAM_TOKEN"},
	}, zap.NewNop())

	require.ErrorContains(t, err, "XMONITOR_TELEGRAM_TOKEN")
}

type recordingHTTPClient struct {
	captured chan capturedRequest
}

func (c recordingHTTPClient) Do(request *http.Request) (*http.Response, error) {
	_ = request.ParseMultipartForm(1 << 20)
	c.captured <- capturedRequest{
		path:   request.URL.Path,
		chatID: request.FormValue("chat_id"),
		text:   request.FormValue("text"),
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body: io.NopCloser(strings.NewReader(`{
		  "ok": true,
		  "result": {
		    "message_id": 77,
		    "date": 1785463200,
		    "chat": {"id": -100123, "type": "private"}
		  }
		}`)),
	}, nil
}

func TestFormatNotification(t *testing.T) {
	t.Parallel()

	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	published := time.Date(2026, 7, 31, 1, 2, 3, 0, time.UTC)
	client := &Client{location: location}

	message := client.format(domain.Post{
		StatusURL:   "https://x.com/CryptoDinduz/status/100",
		Account:     "@CryptoDinduz",
		ContentType: domain.ContentTypeOriginal,
		PublishedAt: &published,
		SummaryZH:   "这是一条测试摘要。",
		Uncertainties: []string{
			"原文部分内容无法确认",
		},
	})

	require.Equal(t, `@CryptoDinduz｜原创
发布时间：2026-07-31 09:02:03（Asia/Shanghai）
摘要：这是一条测试摘要。
不确定项：原文部分内容无法确认
原帖：https://x.com/CryptoDinduz/status/100`, message)
}

func TestSendUsesTelegramSDKAndReturnsReceipt(t *testing.T) {
	t.Parallel()

	captured := make(chan capturedRequest, 1)
	bot, err := tgbot.New(
		"test-token",
		tgbot.WithSkipGetMe(),
		tgbot.WithServerURL("https://telegram.invalid"),
		tgbot.WithHTTPClient(time.Second, recordingHTTPClient{captured: captured}),
	)
	require.NoError(t, err)
	client := &Client{bot: bot, location: time.UTC}

	receipt, err := client.Send(t.Context(), domain.Notification{
		Destination: "-100123",
		Post: domain.Post{
			Account:     "@2442lll",
			ContentType: domain.ContentTypeReply,
			StatusURL:   "https://x.com/2442lll/status/200",
			SummaryZH:   "Telegram HTTP 集成测试。",
		},
	})

	require.NoError(t, err)
	require.Equal(t, int64(77), receipt.MessageID)
	request := <-captured
	require.Equal(t, "/bottest-token/sendMessage", request.path)
	require.Equal(t, "-100123", request.chatID)
	require.Contains(t, request.text, "@2442lll｜回复")
	require.Contains(t, request.text, "https://x.com/2442lll/status/200")
}
