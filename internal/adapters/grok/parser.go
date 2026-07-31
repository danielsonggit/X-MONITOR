package grok

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sudoHG/x-monitor/internal/domain"
	"github.com/sudoHG/x-monitor/internal/ports"
)

type responseDocument struct {
	SchemaVersion int            `json:"schema_version"`
	Posts         []responsePost `json:"posts"`
}

type responsePost struct {
	Account       string   `json:"account"`
	Type          string   `json:"type"`
	PublishedAt   string   `json:"published_at"`
	StatusURL     string   `json:"status_url"`
	Text          string   `json:"text"`
	SummaryZH     string   `json:"summary_zh"`
	Uncertainties []string `json:"uncertainties"`
}

func parsePosts(answer string, request ports.SearchRequest) ([]domain.Post, error) {
	documentJSON, err := extractJSONObject(answer)
	if err != nil {
		return nil, err
	}
	var document responseDocument
	if err := json.Unmarshal([]byte(documentJSON), &document); err != nil {
		return nil, fmt.Errorf("decode Grok structured result: %w", err)
	}
	if document.SchemaVersion != 1 {
		return nil, fmt.Errorf("unsupported Grok result schema version %d", document.SchemaVersion)
	}
	allowed := make(map[string]string, len(request.Accounts))
	for _, account := range request.Accounts {
		allowed[strings.ToLower(account)] = account
	}
	allowedTypes := make(map[domain.ContentType]struct{}, len(request.ContentTypes))
	for _, contentType := range request.ContentTypes {
		allowedTypes[contentType] = struct{}{}
	}
	seen := make(map[string]struct{}, len(document.Posts))
	result := make([]domain.Post, 0, len(document.Posts))
	for _, row := range document.Posts {
		account, ok := allowed[strings.ToLower(domain.NormalizeAccount(row.Account))]
		if !ok {
			continue
		}
		canonical, statusID, urlHandle, err := domain.CanonicalizeStatusURL(row.StatusURL)
		if err != nil || !domain.EqualAccount(account, "@"+urlHandle) {
			continue
		}
		if _, ok := seen[canonical]; ok {
			continue
		}
		seen[canonical] = struct{}{}
		uncertainties := append([]string(nil), row.Uncertainties...)
		var publishedAt *time.Time
		if strings.TrimSpace(row.PublishedAt) == "" {
			uncertainties = appendUnique(uncertainties, "发布时间无法确认")
		} else {
			parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(row.PublishedAt))
			if err != nil {
				uncertainties = appendUnique(uncertainties, "发布时间格式无法确认")
			} else {
				parsed = parsed.UTC()
				if parsed.Before(request.WindowStart) || parsed.After(request.WindowEnd) {
					continue
				}
				publishedAt = &parsed
			}
		}
		contentType := domain.ParseContentType(row.Type)
		if contentType == domain.ContentTypeUnknown {
			uncertainties = appendUnique(uncertainties, "内容类型无法确认")
		} else if len(allowedTypes) > 0 {
			if _, ok := allowedTypes[contentType]; !ok {
				continue
			}
		}
		summary := strings.TrimSpace(row.SummaryZH)
		if summary == "" {
			summary = "原文摘要无法确认。"
			uncertainties = appendUnique(uncertainties, "摘要无法确认")
		}
		raw, _ := json.Marshal(row)
		result = append(result, domain.Post{
			StatusURL:     canonical,
			StatusID:      statusID,
			Account:       account,
			ContentType:   contentType,
			PublishedAt:   publishedAt,
			OriginalText:  strings.TrimSpace(row.Text),
			SummaryZH:     summary,
			Uncertainties: uncertainties,
			RawJSON:       string(raw),
		})
	}
	sort.SliceStable(result, func(left, right int) bool {
		if result[left].PublishedAt == nil {
			return false
		}
		if result[right].PublishedAt == nil {
			return true
		}
		return result[left].PublishedAt.Before(*result[right].PublishedAt)
	})
	return result, nil
}

func extractJSONObject(answer string) (string, error) {
	value := strings.TrimSpace(answer)
	if strings.HasPrefix(value, "```") {
		lines := strings.Split(value, "\n")
		if len(lines) >= 3 {
			lines = lines[1 : len(lines)-1]
			value = strings.Join(lines, "\n")
		}
	}
	start := strings.IndexByte(value, '{')
	end := strings.LastIndexByte(value, '}')
	if start < 0 || end < start {
		return "", fmt.Errorf("Grok answer did not contain a JSON object")
	}
	return value[start : end+1], nil
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
