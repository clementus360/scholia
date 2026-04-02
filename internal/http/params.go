package http

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

type Pagination struct {
	Limit  int
	Offset int
}

func NormalizeID(value string) string {
	return strings.TrimSpace(value)
}

func NormalizeVerseID(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func NormalizeSlug(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func ParsePagination(r *http.Request, defaultLimit, maxLimit int) (Pagination, error) {
	pagination := Pagination{Limit: defaultLimit, Offset: 0}

	rawLimit := strings.TrimSpace(r.URL.Query().Get("limit"))
	if rawLimit != "" {
		parsedLimit, err := strconv.Atoi(rawLimit)
		if err != nil || parsedLimit <= 0 || parsedLimit > maxLimit {
			return Pagination{}, fmt.Errorf("invalid limit")
		}
		pagination.Limit = parsedLimit
	}

	rawOffset := strings.TrimSpace(r.URL.Query().Get("offset"))
	if rawOffset != "" {
		parsedOffset, err := strconv.Atoi(rawOffset)
		if err != nil || parsedOffset < 0 {
			return Pagination{}, fmt.Errorf("invalid offset")
		}
		pagination.Offset = parsedOffset
	}

	return pagination, nil
}

func PaginationMeta(pagination Pagination, count int) map[string]any {
	return map[string]any{
		"limit":  pagination.Limit,
		"offset": pagination.Offset,
		"count":  count,
	}
}
