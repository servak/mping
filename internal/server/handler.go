package server

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/servak/mping/internal/api"
	"github.com/servak/mping/internal/watch"
)

const (
	maxBody          = 1 << 20
	defaultEventPage = 1000
	maxEventPage     = 10000
)

type handler struct {
	watches  *watch.Manager
	version  string
	log      *slog.Logger
	shutdown func() // begins to end the server
	mux      *http.ServeMux
}

// newHandler returns the API. All of its responses are JSON, errors included.
func newHandler(watches *watch.Manager, version string, log *slog.Logger, shutdown func()) http.Handler {
	h := &handler{watches: watches, version: version, log: log, shutdown: shutdown, mux: http.NewServeMux()}
	methods := make(map[string][]string)
	handle := func(method, path string, f endpoint, params ...string) {
		methods[path] = append(methods[path], method)
		h.mux.HandleFunc(method+" "+path, func(w http.ResponseWriter, r *http.Request) {
			status, body, err := f.call(w, r, params)
			h.reply(w, r, status, body, err)
		})
	}
	handle("GET", "/healthz", h.health)
	handle("POST", "/v1/shutdown", h.shutDown)
	handle("POST", "/v1/watches", h.create)
	handle("GET", "/v1/watches", h.list)
	handle("GET", "/v1/watches/{id}", h.get)
	handle("DELETE", "/v1/watches/{id}", h.remove, "force")
	handle("POST", "/v1/watches/{id}/extend", h.extend)
	handle("POST", "/v1/watches/{id}/stop", h.stop)
	handle("GET", "/v1/watches/{id}/summary", h.summary, "targets")
	handle("GET", "/v1/watches/{id}/events", h.events, "after", "limit")
	for path, allowed := range methods {
		h.mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Allow", strings.Join(allowed, ", "))
			h.reply(w, r, 0, nil, statusError{http.StatusMethodNotAllowed, api.Errorf(api.CodeInvalid, "method %s is not allowed", r.Method)})
		})
	}
	h.mux.HandleFunc("/", h.noSuchPath)
	return h
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set(api.VersionHeader, h.version)
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	start := time.Now()
	rec := &recorder{ResponseWriter: w}
	defer func() {
		if p := recover(); p != nil {
			h.log.Error("panic", "method", r.Method, "path", r.URL.EscapedPath(), "panic", p, "stack", string(debug.Stack()))
			if rec.status == 0 {
				h.reply(rec, r, 0, nil, api.Errorf(api.CodeInternal, "internal error"))
			}
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			h.log.Info("request", "method", r.Method, "path", r.URL.EscapedPath(), "status", rec.status, "took", time.Since(start))
		}
	}()
	// The mux would redirect these
	if p := r.URL.Path; !strings.HasPrefix(p, "/") || path.Clean(p) != p {
		h.noSuchPath(rec, r)
		return
	}
	h.mux.ServeHTTP(rec, r)
}

func (h *handler) noSuchPath(w http.ResponseWriter, r *http.Request) {
	h.reply(w, r, 0, nil, api.Errorf(api.CodeNotFound, "no such path"))
}

// recorder remembers the status of a response.
type recorder struct {
	http.ResponseWriter
	status int
}

func (r *recorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// statusError is an error with a status that its code does not tell.
type statusError struct {
	status int
	err    *api.Error
}

func (e statusError) Error() string { return e.err.Error() }

var statuses = map[string]int{
	api.CodeInvalid:      http.StatusBadRequest,
	api.CodeNotFound:     http.StatusNotFound,
	api.CodeConflict:     http.StatusConflict,
	api.CodeLimit:        http.StatusTooManyRequests,
	api.CodeUnresolvable: http.StatusUnprocessableEntity,
	api.CodeShuttingDown: http.StatusServiceUnavailable,
	api.CodeInternal:     http.StatusInternalServerError,
}

// reply writes body, if any, or err.
func (h *handler) reply(w http.ResponseWriter, r *http.Request, status int, body any, err error) {
	if err != nil {
		var e *api.Error
		var withStatus statusError
		switch {
		case errors.As(err, &withStatus):
			status, e = withStatus.status, withStatus.err
		case errors.As(err, &e):
			status = statuses[e.Code]
		default:
			status, e = http.StatusInternalServerError, api.Errorf(api.CodeInternal, "%v", err)
		}
		if status == http.StatusInternalServerError {
			h.log.Error("request failed", "method", r.Method, "path", r.URL.EscapedPath(), "error", e.Message)
		}
		body = api.ErrorBody{Error: e}
	}
	if body == nil {
		w.WriteHeader(status)
		return
	}
	b, err := json.Marshal(body)
	if err != nil {
		panic(err) // nothing has been written yet
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(append(b, '\n'))
}

// endpoint handles a request. A nil body is none.
type endpoint func(w http.ResponseWriter, r *http.Request, q url.Values) (status int, body any, err error)

// call checks the query, whose parameters may be given once each.
func (f endpoint) call(w http.ResponseWriter, r *http.Request, params []string) (int, any, error) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return 0, nil, api.Errorf(api.CodeInvalid, "invalid query: %v", err)
	}
	for name, values := range q {
		if !slices.Contains(params, name) || len(values) > 1 {
			return 0, nil, api.Errorf(api.CodeInvalid, "unknown or repeated parameter %q", name)
		}
	}
	return f(w, r, q)
}

// decode reads the body of r, which must be one JSON object of the type of v.
func decode(r *http.Request, v any) error {
	if t, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || t != "application/json" {
		return statusError{http.StatusUnsupportedMediaType, api.Errorf(api.CodeInvalid, "the content type must be application/json")}
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	err := dec.Decode(v)
	if err == nil {
		if err = dec.Decode(&struct{}{}); err == io.EOF {
			return nil
		}
		err = errors.Join(errors.New("more than one JSON value"), err)
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return statusError{http.StatusRequestEntityTooLarge, api.Errorf(api.CodeInvalid, "the body is limited to %d bytes", tooLarge.Limit)}
	}
	return api.Errorf(api.CodeInvalid, "invalid body: %v", err)
}

// option returns whether a parameter is there, which has only one value.
func option(q url.Values, name, value string) (bool, error) {
	if q.Has(name) && q.Get(name) != value {
		return false, api.Errorf(api.CodeInvalid, "%s must be %s", name, value)
	}
	return q.Has(name), nil
}

// number returns a parameter between lo and hi, or def if it is absent.
func number(q url.Values, name string, def, lo, hi int64) (int64, error) {
	if !q.Has(name) {
		return def, nil
	}
	n, err := strconv.ParseInt(q.Get(name), 10, 64)
	if err != nil || n < lo || n > hi {
		return 0, api.Errorf(api.CodeInvalid, "%s must be a number between %d and %d", name, lo, hi)
	}
	return n, nil
}

func (h *handler) health(http.ResponseWriter, *http.Request, url.Values) (int, any, error) {
	return http.StatusOK, api.Health{Status: "ok", Version: h.version, PID: os.Getpid()}, nil
}

func (h *handler) shutDown(_ http.ResponseWriter, r *http.Request, _ url.Values) (int, any, error) {
	var req api.ShutdownRequest
	if err := decode(r, &req); err != nil {
		return 0, nil, err
	}
	if !req.Force {
		if err := h.watches.Close(); err != nil {
			return 0, nil, err
		}
	}
	h.shutdown()
	return http.StatusAccepted, struct{}{}, nil
}

func (h *handler) create(w http.ResponseWriter, r *http.Request, _ url.Values) (int, any, error) {
	var req api.CreateRequest
	if err := decode(r, &req); err != nil {
		return 0, nil, err
	}
	created, err := h.watches.Create(r.Context(), req)
	if err != nil {
		return 0, nil, err
	}
	w.Header().Set("Location", "/v1/watches/"+created.ID)
	return http.StatusCreated, created, nil
}

func (h *handler) list(http.ResponseWriter, *http.Request, url.Values) (int, any, error) {
	return http.StatusOK, api.WatchList{Watches: h.watches.List()}, nil
}

func (h *handler) get(_ http.ResponseWriter, r *http.Request, _ url.Values) (int, any, error) {
	detail, err := h.watches.Get(r.PathValue("id"))
	return http.StatusOK, detail, err
}

func (h *handler) remove(_ http.ResponseWriter, r *http.Request, q url.Values) (int, any, error) {
	force, err := option(q, "force", "true")
	if err != nil {
		return 0, nil, err
	}
	return http.StatusNoContent, nil, h.watches.Delete(r.PathValue("id"), force)
}

func (h *handler) extend(_ http.ResponseWriter, r *http.Request, _ url.Values) (int, any, error) {
	var req api.ExtendRequest
	if err := decode(r, &req); err != nil {
		return 0, nil, err
	}
	extended, err := h.watches.Extend(r.PathValue("id"), req.DurationS)
	return http.StatusOK, extended, err
}

func (h *handler) stop(_ http.ResponseWriter, r *http.Request, _ url.Values) (int, any, error) {
	if n, _ := r.Body.Read(make([]byte, 1)); n > 0 {
		return 0, nil, api.Errorf(api.CodeInvalid, "stop takes no body")
	}
	stopped, err := h.watches.Stop(r.PathValue("id"))
	return http.StatusAccepted, stopped, err
}

func (h *handler) summary(_ http.ResponseWriter, r *http.Request, q url.Values) (int, any, error) {
	all, err := option(q, "targets", "all")
	if err != nil {
		return 0, nil, err
	}
	summary, err := h.watches.Summary(r.PathValue("id"), all)
	return http.StatusOK, summary, err
}

func (h *handler) events(_ http.ResponseWriter, r *http.Request, q url.Values) (int, any, error) {
	after, err := number(q, "after", 0, 0, 1<<62)
	if err != nil {
		return 0, nil, err
	}
	limit, err := number(q, "limit", defaultEventPage, 1, maxEventPage)
	if err != nil {
		return 0, nil, err
	}
	events, err := h.watches.Events(r.PathValue("id"), after, int(limit))
	return http.StatusOK, events, err
}
