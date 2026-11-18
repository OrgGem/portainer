package useractivity

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
)

type query struct {
	after, before     *int64
	keyword, username string
	sortBy            string
	sortDesc          bool
	offset, limit     int
	contexts, types   map[int]bool
	endpointID        *int
	stackID           *int
	source, action    string
	status            string
}

var errQuery = errors.New("invalid query parameters")

func parseQuery(values url.Values, auth bool) (query, error) {
	q := query{sortBy: "timestamp", sortDesc: true, limit: 25}
	for _, vals := range values {
		if len(vals) != 1 {
			return q, errQuery
		}
	}
	parseTime := func(key string) (*int64, error) {
		if _, exists := values[key]; !exists {
			return nil, nil
		}
		n, err := strconv.ParseInt(values.Get(key), 10, 64)
		if err != nil {
			return nil, errQuery
		}
		return &n, nil
	}
	var err error
	if q.after, err = parseTime("after"); err != nil {
		return q, err
	}
	if q.before, err = parseTime("before"); err != nil {
		return q, err
	}
	if q.after != nil && q.before != nil && *q.after > *q.before {
		return q, errQuery
	}
	q.keyword = strings.ToLower(values.Get("keyword"))
	q.username = strings.ToLower(values.Get("username"))
	if v, exists := values["sortBy"]; exists {
		q.sortBy = v[0]
	}
	validSort := map[string]bool{"timestamp": true, "username": true, "context": true}
	if auth {
		validSort["type"] = true
		validSort["origin"] = true
	} else {
		validSort["action"] = true
		validSort["status"] = true
		validSort["source"] = true
	}
	if !validSort[q.sortBy] {
		return q, errQuery
	}
	if v, exists := values["sortDesc"]; exists {
		if v[0] != "true" && v[0] != "false" {
			return q, errQuery
		}
		q.sortDesc = v[0] == "true"
	}
	if _, exists := values["offset"]; exists {
		q.offset, err = strconv.Atoi(values.Get("offset"))
		if err != nil || q.offset < 0 {
			return q, errQuery
		}
	}
	if _, exists := values["limit"]; exists {
		q.limit, err = strconv.Atoi(values.Get("limit"))
		if err != nil || q.limit < 1 || q.limit > 100 {
			return q, errQuery
		}
	}
	if auth {
		if q.contexts, err = parseEnums(values, "contexts", 0, 3); err != nil {
			return q, err
		}
		if q.types, err = parseEnums(values, "types", 1, 3); err != nil {
			return q, err
		}
	} else {
		if q.endpointID, err = parseID(values, "endpointId"); err != nil {
			return q, err
		}
		if q.stackID, err = parseID(values, "stackId"); err != nil {
			return q, err
		}
		q.source, q.action, q.status = values.Get("source"), values.Get("action"), values.Get("status")
		if _, exists := values["source"]; exists && !oneOf(q.source, "configuration", "deployment", "metadata") {
			return q, errQuery
		}
		if _, exists := values["action"]; exists && !oneOf(q.action, "upload", "apply", "rollback", "recover", "deployment", "create", "update", "unknown") {
			return q, errQuery
		}
		if _, exists := values["status"]; exists && !oneOf(q.status, "success", "failure", "in_progress", "inactive", "unknown") {
			return q, errQuery
		}
	}
	return q, nil
}

func parseEnums(values url.Values, key string, low, high int) (map[int]bool, error) {
	if _, exists := values[key]; !exists {
		return nil, nil
	}
	result := make(map[int]bool)
	for _, part := range strings.Split(values.Get(key), ",") {
		n, err := strconv.Atoi(part)
		if err != nil || n < low || n > high {
			return nil, errQuery
		}
		result[n] = true
	}
	return result, nil
}

func parseID(values url.Values, key string) (*int, error) {
	if _, exists := values[key]; !exists {
		return nil, nil
	}
	n, err := strconv.Atoi(values.Get(key))
	if err != nil || n < 1 {
		return nil, errQuery
	}
	return &n, nil
}

func oneOf(value string, options ...string) bool {
	for _, option := range options {
		if value == option {
			return true
		}
	}
	return false
}

func (q query) matchesTime(timestamp int64) bool {
	return (q.after == nil || timestamp >= *q.after) && (q.before == nil || timestamp < *q.before)
}

func (q query) matchesText(username string, values ...string) bool {
	if q.username != "" && !strings.Contains(strings.ToLower(username), q.username) {
		return false
	}
	if q.keyword == "" {
		return true
	}
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), q.keyword) {
			return true
		}
	}
	return false
}
