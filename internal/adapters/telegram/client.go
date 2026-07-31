package telegram

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/sudoHG/x-monitor/internal/domain"
	"github.com/sudoHG/x-monitor/internal/infrastructure/config"
	"github.com/sudoHG/x-monitor/internal/ports"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

type Client struct {
	bot      *tgbot.Bot
	location *time.Location
	logger   *zap.Logger
}

var Module = fx.Module(
	"telegram",
	fx.Provide(
		fx.Annotate(New, fx.As(new(ports.Notifier))),
	),
)

func New(cfg config.Config, logger *zap.Logger) (*Client, error) {
	if strings.TrimSpace(cfg.Telegram.Token) == "" {
		return nil, fmt.Errorf(
			"Telegram token environment variable %s is not set",
			cfg.Telegram.TokenEnv,
		)
	}
	location, err := time.LoadLocation(cfg.Scheduler.Timezone)
	if err != nil {
		return nil, fmt.Errorf("load Telegram display timezone: %w", err)
	}
	client, err := tgbot.New(
		cfg.Telegram.Token,
		tgbot.WithSkipGetMe(),
		tgbot.WithCheckInitTimeout(10*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("create Telegram bot: %w", err)
	}
	return &Client{
		bot:      client,
		location: location,
		logger:   logger.Named("telegram"),
	}, nil
}

func (c *Client) Check(ctx context.Context) error {
	_, err := c.bot.GetMe(ctx)
	if err != nil {
		return classifyError(err)
	}
	return nil
}

func (c *Client) Send(ctx context.Context, notification domain.Notification) (domain.Receipt, error) {
	message, err := c.bot.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID: notification.Destination,
		Text:   c.format(notification.Post),
	})
	if err != nil {
		return domain.Receipt{}, classifyError(err)
	}
	return domain.Receipt{
		MessageID: int64(message.ID),
		SentAt:    time.Now().UTC(),
	}, nil
}

func (c *Client) format(post domain.Post) string {
	typeLabel := map[domain.ContentType]string{
		domain.ContentTypeOriginal: "原创",
		domain.ContentTypeReply:    "回复",
		domain.ContentTypeRepost:   "转帖",
		domain.ContentTypeUnknown:  "类型不确定",
	}[post.ContentType]
	if typeLabel == "" {
		typeLabel = "类型不确定"
	}
	published := "不确定"
	if post.PublishedAt != nil {
		published = post.PublishedAt.In(c.location).Format("2006-01-02 15:04:05") +
			"（" + c.location.String() + "）"
	}
	summary := strings.TrimSpace(post.SummaryZH)
	if summary == "" {
		summary = "无法确认摘要。"
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "%s｜%s\n", post.Account, typeLabel)
	fmt.Fprintf(&builder, "发布时间：%s\n", published)
	fmt.Fprintf(&builder, "摘要：%s\n", summary)
	if len(post.Uncertainties) > 0 {
		fmt.Fprintf(&builder, "不确定项：%s\n", strings.Join(post.Uncertainties, "；"))
	}
	fmt.Fprintf(&builder, "原帖：%s", post.StatusURL)
	return builder.String()
}

func classifyError(err error) error {
	if err == nil {
		return nil
	}
	if tgbot.IsTooManyRequestsError(err) {
		var rateLimit *tgbot.TooManyRequestsError
		if errors.As(err, &rateLimit) {
			return &domain.NotificationError{
				Cause:           err,
				RetryAfterDelay: time.Duration(rateLimit.RetryAfter) * time.Second,
			}
		}
	}
	permanent := errors.Is(err, tgbot.ErrorBadRequest) ||
		errors.Is(err, tgbot.ErrorUnauthorized) ||
		errors.Is(err, tgbot.ErrorForbidden) ||
		errors.Is(err, tgbot.ErrorNotFound) ||
		errors.Is(err, tgbot.ErrorConflict)
	return &domain.NotificationError{Cause: err, IsPermanent: permanent}
}

var _ ports.Notifier = (*Client)(nil)
