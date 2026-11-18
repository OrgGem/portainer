// Package useractivity exposes administrator-only authentication logs and a
// read-only projection of the stack activity already present in the datastore.
package useractivity

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/gorilla/mux"
	"github.com/portainer/portainer/api/authlog"
	"github.com/portainer/portainer/api/dataservices"
	"github.com/portainer/portainer/api/http/security"
	httperror "github.com/portainer/portainer/pkg/libhttp/error"
	"github.com/portainer/portainer/pkg/libhttp/response"
)

type Handler struct {
	*mux.Router
	DataStore dataservices.DataStore
	AuthLogs  *authlog.Store
}

func NewHandler(bouncer security.BouncerService, dataStore dataservices.DataStore, authLogs *authlog.Store) *Handler {
	h := &Handler{Router: mux.NewRouter(), DataStore: dataStore, AuthLogs: authLogs}
	for _, route := range []struct {
		path      string
		auth, csv bool
	}{
		{"/useractivity/authlogs", true, false},
		{"/useractivity/authlogs.csv", true, true},
		{"/useractivity/logs", false, false},
		{"/useractivity/logs.csv", false, true},
	} {
		route := route
		endpoint := httperror.LoggerHandler(func(w http.ResponseWriter, r *http.Request) *httperror.HandlerError {
			return h.serveLogs(w, r, route.auth, route.csv)
		})
		secured := bouncer.AdminAccess(endpoint)
		h.Handle(route.path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			secured.ServeHTTP(w, r)
		})).Methods(http.MethodGet)
	}
	return h
}

type result[T any] struct {
	Logs         []T                  `json:"logs"`
	TotalCount   int                  `json:"totalCount"`
	SourceStatus authlog.SourceStatus `json:"sourceStatus"`
}

func (h *Handler) serveLogs(w http.ResponseWriter, r *http.Request, auth, export bool) *httperror.HandlerError {
	q, err := parseQuery(r.URL.Query(), auth)
	if err != nil {
		return httperror.BadRequest("Invalid query parameters", errQuery)
	}
	if auth {
		return h.authLogs(w, r, q, export)
	}
	return h.activityLogs(w, r, q, export)
}

func (h *Handler) authLogs(w http.ResponseWriter, r *http.Request, q query, export bool) *httperror.HandlerError {
	rows, status, err := h.AuthLogs.Read(r.Context())
	if err != nil {
		return cancellation(err)
	}
	if export && (!status.Available || status.Partial) {
		return sourceError()
	}
	filtered := make([]authlog.Event, 0, len(rows))
	for _, row := range rows {
		if err := r.Context().Err(); err != nil {
			return cancellation(err)
		}
		if !q.matchesTime(row.Timestamp) || len(q.contexts) > 0 && !q.contexts[row.Context] || len(q.types) > 0 && !q.types[row.Type] {
			continue
		}
		if !q.matchesText(row.Username, row.Username, methodName(row.Context), eventName(row.Type), row.Origin, strconv.Itoa(row.StatusCode)) {
			continue
		}
		filtered = append(filtered, row)
	}
	sort.Slice(filtered, func(i, j int) bool {
		a, b := filtered[i], filtered[j]
		var comparison int
		switch q.sortBy {
		case "username":
			comparison = compareText(a.Username, b.Username)
		case "context":
			comparison = compareInt(int64(a.Context), int64(b.Context))
		case "type":
			comparison = compareInt(int64(a.Type), int64(b.Type))
		case "origin":
			comparison = compareText(a.Origin, b.Origin)
		default:
			comparison = compareInt(a.Timestamp, b.Timestamp)
		}
		return less(comparison, a.ID, b.ID, q.sortDesc)
	})
	if err := r.Context().Err(); err != nil {
		return cancellation(err)
	}
	if export {
		return writeAuthCSV(w, r, filtered)
	}
	return response.JSON(w, result[authlog.Event]{Logs: page(filtered, q), TotalCount: len(filtered), SourceStatus: status})
}

func (h *Handler) activityLogs(w http.ResponseWriter, r *http.Request, q query, export bool) *httperror.HandlerError {
	rows, err := activitySnapshot(r.Context(), h.DataStore)
	status := authlog.SourceStatus{Available: true}
	if err != nil {
		if r.Context().Err() != nil {
			return cancellation(r.Context().Err())
		}
		status = authlog.SourceStatus{Message: "Stack activity is unavailable."}
		rows = []Activity{}
	}
	if export && !status.Available {
		return sourceError()
	}
	filtered := make([]Activity, 0, len(rows))
	for _, row := range rows {
		if err := r.Context().Err(); err != nil {
			return cancellation(err)
		}
		if !q.matchesTime(row.Timestamp) || q.endpointID != nil && row.EnvironmentID != *q.endpointID || q.stackID != nil && row.StackID != *q.stackID {
			continue
		}
		if q.source != "" && row.Source != q.source || q.action != "" && row.Action != q.action || q.status != "" && row.Status != q.status {
			continue
		}
		if !q.matchesText(row.Username, row.Username, row.Context, row.StackName, row.Source, row.Action, row.Status, fmt.Sprint(row.Payload["summary"]), strconv.Itoa(row.StackID)) {
			continue
		}
		filtered = append(filtered, row)
	}
	sort.Slice(filtered, func(i, j int) bool {
		a, b := filtered[i], filtered[j]
		var comparison int
		switch q.sortBy {
		case "username":
			comparison = compareText(a.Username, b.Username)
		case "context":
			comparison = compareText(a.Context, b.Context)
		case "action":
			comparison = compareText(a.Action, b.Action)
		case "status":
			comparison = compareText(a.Status, b.Status)
		case "source":
			comparison = compareText(a.Source, b.Source)
		default:
			comparison = compareInt(a.Timestamp, b.Timestamp)
		}
		return less(comparison, a.ID, b.ID, q.sortDesc)
	})
	if err := r.Context().Err(); err != nil {
		return cancellation(err)
	}
	if export {
		return writeActivityCSV(w, r, filtered)
	}
	return response.JSON(w, result[Activity]{Logs: page(filtered, q), TotalCount: len(filtered), SourceStatus: status})
}

func sourceError() *httperror.HandlerError {
	return httperror.NewError(http.StatusServiceUnavailable, "Log source is unavailable or incomplete", errors.New("log source is unavailable or incomplete"))
}

func cancellation(err error) *httperror.HandlerError {
	return httperror.NewError(http.StatusRequestTimeout, "Request canceled", err)
}

func compareText(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) }
func compareInt(a, b int64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
func less(comparison int, idA, idB string, desc bool) bool {
	if comparison == 0 {
		return idA < idB
	}
	if desc {
		return comparison > 0
	}
	return comparison < 0
}

func page[T any](rows []T, q query) []T {
	if q.offset >= len(rows) {
		return []T{}
	}
	end := q.offset + min(q.limit, len(rows)-q.offset)
	return rows[q.offset:end]
}

func methodName(method int) string {
	switch method {
	case authlog.Internal:
		return "Internal"
	case authlog.LDAP:
		return "LDAP"
	case authlog.OAuth:
		return "OAuth"
	default:
		return "Unknown"
	}
}
func eventName(event int) string {
	switch event {
	case authlog.Success:
		return "Success"
	case authlog.Failure:
		return "Failure"
	case authlog.Logout:
		return "Logout"
	default:
		return "Unknown"
	}
}

func writeAuthCSV(w http.ResponseWriter, r *http.Request, rows []authlog.Event) *httperror.HandlerError {
	return writeCSV(w, r.Context(), "authentication-logs.csv", []string{"Time", "User", "Method", "Event", "Result / HTTP status", "Origin", "Rate limited sample", "Suppressed rate limited"}, len(rows), func(i int) []string {
		row := rows[i]
		return []string{isoTime(row.Timestamp), row.Username, methodName(row.Context), eventName(row.Type), fmt.Sprintf("%s (%d)", eventName(row.Type), row.StatusCode), row.Origin, strconv.FormatBool(row.RateLimited), strconv.FormatUint(row.SuppressedRateLimited, 10)}
	})
}

func writeActivityCSV(w http.ResponseWriter, r *http.Request, rows []Activity) *httperror.HandlerError {
	return writeCSV(w, r.Context(), "activity-logs.csv", []string{"Time", "User", "Environment", "Stack", "Stack ID", "Source", "Action", "Status", "Safe details"}, len(rows), func(i int) []string {
		row := rows[i]
		details, _ := json.Marshal(row.Payload)
		return []string{isoTime(row.Timestamp), row.Username, row.Context, row.StackName, strconv.Itoa(row.StackID), row.Source, row.Action, row.Status, string(details)}
	})
}

func isoTime(timestamp int64) string { return time.Unix(timestamp, 0).UTC().Format(time.RFC3339) }

func writeCSV(w http.ResponseWriter, ctx context.Context, filename string, header []string, count int, rowAt func(int) []string) *httperror.HandlerError {
	if err := ctx.Err(); err != nil {
		return cancellation(err)
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	if _, err := w.Write([]byte{0xef, 0xbb, 0xbf}); err != nil {
		return nil
	}
	writer := csv.NewWriter(w)
	writer.UseCRLF = true
	if err := writer.Write(header); err != nil {
		return nil
	}
	for i := 0; i < count; i++ {
		if ctx.Err() != nil {
			return nil
		}
		fields := rowAt(i)
		for j := range fields {
			fields[j] = csvSafe(fields[j])
		}
		if err := writer.Write(fields); err != nil {
			return nil
		}
	}
	writer.Flush()
	return nil
}

func csvSafe(value string) string {
	trimmed := strings.TrimLeftFunc(value, unicode.IsSpace)
	if strings.HasPrefix(value, "\t") || strings.HasPrefix(value, "\r") || strings.HasPrefix(value, "\n") || len(trimmed) > 0 && strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return "'" + value
	}
	return value
}
