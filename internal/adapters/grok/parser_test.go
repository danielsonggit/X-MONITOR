package grok

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/sudoHG/x-monitor/internal/domain"
	"github.com/sudoHG/x-monitor/internal/ports"
)

func TestParsePostsValidatesFiltersCanonicalizesAndSorts(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	answer := `some harmless wrapper text
{
  "schema_version": 1,
  "posts": [
    {
      "account": "@2442lll",
      "type": "reply",
      "published_at": "2026-07-31T01:30:00Z",
      "status_url": "https://twitter.com/2442lll/status/200",
      "text": "later",
      "summary_zh": "较晚的回复。",
      "uncertainties": []
    },
    {
      "account": "@CryptoDinduz",
      "type": "original",
      "published_at": "2026-07-31T00:30:00Z",
      "status_url": "https://x.com/CryptoDinduz/status/100?utm_source=test",
      "text": "earlier",
      "summary_zh": "较早的原创帖。",
      "uncertainties": []
    },
    {
      "account": "@CryptoDinduz",
      "type": "original",
      "published_at": "2026-07-31T00:30:00Z",
      "status_url": "https://twitter.com/CryptoDinduz/status/100",
      "text": "duplicate",
      "summary_zh": "重复结果。",
      "uncertainties": []
    },
    {
      "account": "@not-allowed",
      "type": "original",
      "published_at": "2026-07-31T01:00:00Z",
      "status_url": "https://x.com/not_allowed/status/300",
      "text": "not allowed",
      "summary_zh": "不应出现。",
      "uncertainties": []
    },
    {
      "account": "@2442lll",
      "type": "reply",
      "published_at": "2026-07-30T20:00:00Z",
      "status_url": "https://x.com/2442lll/status/400",
      "text": "outside window",
      "summary_zh": "不应出现。",
      "uncertainties": []
    }
  ]
}`
	posts, err := parsePosts(answer, ports.SearchRequest{
		Accounts:    []string{"@CryptoDinduz", "@2442lll"},
		WindowStart: start,
		WindowEnd:   end,
	})

	require.NoError(t, err)
	require.Len(t, posts, 2)
	require.Equal(t, "https://x.com/CryptoDinduz/status/100", posts[0].StatusURL)
	require.Equal(t, domain.ContentTypeOriginal, posts[0].ContentType)
	require.Equal(t, "https://x.com/2442lll/status/200", posts[1].StatusURL)
	require.Equal(t, domain.ContentTypeReply, posts[1].ContentType)
}

func TestParsePostsMarksUncertainFieldsWithoutInventing(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 31, 1, 0, 0, 0, time.UTC)
	posts, err := parsePosts(`{
	  "schema_version": 1,
	  "posts": [{
	    "account": "@iruletrenches",
	    "type": "",
	    "published_at": "",
	    "status_url": "https://x.com/iruletrenches/status/999",
	    "text": "",
	    "summary_zh": "",
	    "uncertainties": []
	  }]
	}`, ports.SearchRequest{
		Accounts:    []string{"@iruletrenches"},
		WindowStart: now.Add(-time.Hour),
		WindowEnd:   now,
	})

	require.NoError(t, err)
	require.Len(t, posts, 1)
	require.Nil(t, posts[0].PublishedAt)
	require.Equal(t, domain.ContentTypeUnknown, posts[0].ContentType)
	require.Equal(t, "原文摘要无法确认。", posts[0].SummaryZH)
	require.ElementsMatch(t, []string{
		"发布时间无法确认",
		"内容类型无法确认",
		"摘要无法确认",
	}, posts[0].Uncertainties)
}

func TestParsePostsRejectsUnsupportedSchema(t *testing.T) {
	t.Parallel()

	_, err := parsePosts(`{"schema_version":2,"posts":[]}`, ports.SearchRequest{})

	require.ErrorContains(t, err, "unsupported Grok result schema")
}
