package domain

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type ContentType string

const (
	ContentTypeOriginal ContentType = "original"
	ContentTypeReply    ContentType = "reply"
	ContentTypeRepost   ContentType = "repost"
	ContentTypeUnknown  ContentType = "unknown"
)

var (
	statusPathPattern = regexp.MustCompile(`^/([^/]+)/status/([0-9]+)$`)
	accountPattern    = regexp.MustCompile(`^@[A-Za-z0-9_]{1,15}$`)
)

type Post struct {
	StatusURL     string
	StatusID      string
	Account       string
	ContentType   ContentType
	PublishedAt   *time.Time
	OriginalText  string
	SummaryZH     string
	Uncertainties []string
	RawJSON       string
}

func ParseContentType(value string) ContentType {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(ContentTypeOriginal):
		return ContentTypeOriginal
	case string(ContentTypeReply):
		return ContentTypeReply
	case string(ContentTypeRepost), "retweet", "quote":
		return ContentTypeRepost
	default:
		return ContentTypeUnknown
	}
}

func ValidateAccount(handle string) error {
	if !accountPattern.MatchString(handle) {
		return fmt.Errorf("invalid X account handle %q", handle)
	}
	return nil
}

func NormalizeAccount(handle string) string {
	handle = strings.TrimSpace(handle)
	if !strings.HasPrefix(handle, "@") {
		handle = "@" + handle
	}
	return handle
}

func EqualAccount(left, right string) bool {
	return strings.EqualFold(NormalizeAccount(left), NormalizeAccount(right))
}

func CanonicalizeStatusURL(raw string) (canonical string, statusID string, handle string, err error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "", "", fmt.Errorf("parse status URL: %w", err)
	}
	if parsed.Scheme != "https" {
		return "", "", "", errors.New("status URL must use https")
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "x.com" && host != "www.x.com" && host != "twitter.com" && host != "www.twitter.com" {
		return "", "", "", fmt.Errorf("unsupported status URL host %q", parsed.Hostname())
	}
	match := statusPathPattern.FindStringSubmatch(strings.TrimSuffix(parsed.EscapedPath(), "/"))
	if len(match) != 3 {
		return "", "", "", errors.New("status URL must have /<handle>/status/<numeric-id> path")
	}
	handle, statusID = match[1], match[2]
	if err := ValidateAccount("@" + handle); err != nil {
		return "", "", "", err
	}
	canonical = "https://x.com/" + handle + "/status/" + statusID
	return canonical, statusID, handle, nil
}
