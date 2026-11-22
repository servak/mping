// Package api holds what the mping server and its clients exchange: JSON over
// HTTP. Durations are integers with their unit in the name, times are RFC 3339
// with the zone of the server.
//
// Strings that others than the client determine (errors, names of targets) have
// gone through Clean.
package api

import (
	"fmt"
	"time"

	"github.com/servak/mping/internal/ui/shared"
)

// VersionHeader is set on every response.
const VersionHeader = "Mping-Version"

// CreateRequest asks for a new watch (POST /v1/watches).
type CreateRequest struct {
	Name            string   `json:"name"`
	Targets         []string `json:"targets"`                    // as on the command line; the server expands them
	DurationS       int64    `json:"duration_s,omitempty"`       // 0: the default, 2h
	IntervalMs      int64    `json:"interval_ms,omitempty"`      // 0: 1000
	TimeoutMs       int64    `json:"timeout_ms,omitempty"`       // 0: 1000
	SourceInterface string   `json:"source_interface,omitempty"` // name or IP address
}

// ExtendRequest moves the end of a watch to DurationS from now (POST
// /v1/watches/{id}/extend).
type ExtendRequest struct {
	DurationS int64 `json:"duration_s"`
}

// ShutdownRequest ends the server (POST /v1/shutdown), with Force also while
// watches are running.
type ShutdownRequest struct {
	Force bool `json:"force"`
}

// The states of a watch.
const (
	StateRunning  = "running"
	StateStopping = "stopping" // waiting for the last replies
	StateStopped  = "stopped"
)

// The reasons why a watch is stopping or stopped.
const (
	ReasonRequested = "requested"
	ReasonExpired   = "expired"
	ReasonFailed    = "failed"
	ReasonShutdown  = "shutdown"
)

// Watch describes a watch.
type Watch struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	State           string     `json:"state"`
	Reason          string     `json:"reason,omitempty"` // unless running
	Error           string     `json:"error,omitempty"`  // of ReasonFailed
	StartedAt       time.Time  `json:"started_at"`
	ExpiresAt       time.Time  `json:"expires_at"`
	StoppedAt       *time.Time `json:"stopped_at,omitempty"`
	IntervalMs      int64      `json:"interval_ms"`
	TimeoutMs       int64      `json:"timeout_ms"`
	SourceInterface string     `json:"source_interface,omitempty"`
	TargetCount     int        `json:"target_count"`
	Counts          Counts     `json:"counts"` // of the targets by status
}

// WatchDetail is a watch with its definition (GET /v1/watches/{id}).
type WatchDetail struct {
	Watch
	Targets []string `json:"targets"` // as given, not expanded
}

// WatchList is the answer to GET /v1/watches, oldest first.
type WatchList struct {
	Watches []Watch `json:"watches"`
}

// The statuses of a target, worst first.
const (
	// StatusDown is a target with an ongoing outage (3 or more consecutive
	// failures), or that has failed and never answered. A recovery counts once
	// the results have settled: until then ConsecutiveFailures is already 0.
	StatusDown = "down"
	// StatusDegraded is any other target with a failure since the start, be it
	// now (ConsecutiveFailures > 0) or long ago (LastFailAt, Outages).
	StatusDegraded = "degraded"
	// StatusPending is a target without any result yet.
	StatusPending = "pending"
	// StatusOK is a target without any failure.
	StatusOK = "ok"
)

// Counts are the numbers of targets by status.
type Counts struct {
	Down     int `json:"down"`
	Degraded int `json:"degraded"`
	Pending  int `json:"pending"`
	OK       int `json:"ok"`
}

// Outage is an outage of a target.
type Outage struct {
	shared.OutageReport
	// EndUnknown is set when the watch was stopped during the outage, which
	// then stays Ongoing, with its duration up to that moment.
	EndUnknown bool `json:"end_unknown,omitempty"`
}

// TargetSummary is the state of a target, with the fields of batch -o json.
type TargetSummary struct {
	Status              string `json:"status"`
	ConsecutiveFailures int    `json:"consecutive_failures"`
	shared.TargetReport
	Outages []Outage `json:"outages"` // only the last ones; OutageCount is of all
}

// Summary is the state of a watch (GET /v1/watches/{id}/summary).
type Summary struct {
	Watch
	AsOf time.Time `json:"as_of"` // when a stopped watch stopped
	// Targets are those that are not ok, worst first, or all (targets=all).
	Targets        []TargetSummary `json:"targets"`
	OmittedTargets int             `json:"omitted_targets"` // not ok, but beyond the limit of Targets
	GapCount       int             `json:"gap_count"`
	LastGap        *Gap            `json:"last_gap,omitempty"`
	LastEventSeq   int64           `json:"last_event_seq"` // to follow with events?after=
	DroppedEvents  int64           `json:"dropped_events"` // too old to be kept
}

// The kinds of events, besides the stats.EventKinds.
const (
	EventWatchStarted  = "watch_started"
	EventWatchExtended = "watch_extended"
	EventWatchStopped  = "watch_stopped" // the last one of a watch
	EventGap           = "gap"
)

// Event is something that happened to a watch or one of its targets. The one
// object that Kind names is set.
type Event struct {
	Seq    int64  `json:"seq"` // from 1
	Kind   string `json:"kind"`
	Target string `json:"target,omitempty"` // Host of its TargetSummary
	// Time is when it happened, which for results of probes may be before that
	// of the event before. RecordedAt is when that was established.
	Time       time.Time `json:"time"`
	RecordedAt time.Time `json:"recorded_at"`

	Outage   *Outage     `json:"outage,omitempty"` // outage_start, outage_end
	TTL      *TTLChange  `json:"ttl,omitempty"`
	Peer     *PeerChange `json:"peer,omitempty"`
	Cert     *CertChange `json:"cert,omitempty"`
	Gap      *Gap        `json:"gap,omitempty"`
	Started  *Started    `json:"started,omitempty"`
	Extended *Extended   `json:"extended,omitempty"`
	Stopped  *Stopped    `json:"stopped,omitempty"`
}

// TTLChange is a change of the reply TTL.
type TTLChange struct {
	Old int `json:"old"`
	New int `json:"new"`
	// HopsDelta is positive if replies cross more hops than before, nil if that
	// is not known.
	HopsDelta *int `json:"hops_delta,omitempty"`
}

// PeerChange is a change of the IP:port that answers.
type PeerChange struct {
	Old string `json:"old"`
	New string `json:"new"`
}

// CertChange is a change of the certificate. Its names are left out: whoever
// answers chooses them.
type CertChange struct {
	OldNotAfter time.Time `json:"old_not_after"`
	NewNotAfter time.Time `json:"new_not_after"`
}

// Gap is a period in which the host seems to have been suspended: the wall
// clock advanced more than the monotonic one. That may also have been a
// correction of the wall clock.
type Gap struct {
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	DurationMs int64     `json:"duration_ms"`
}

// Started tells the end of a new watch.
type Started struct {
	ExpiresAt time.Time `json:"expires_at"`
}

// Extended tells the old and the new end of a watch.
type Extended struct {
	OldExpiresAt time.Time `json:"old_expires_at"`
	NewExpiresAt time.Time `json:"new_expires_at"`
}

// Stopped tells why a watch was stopped.
type Stopped struct {
	Reason string `json:"reason"`
	Error  string `json:"error,omitempty"`
}

// Events are the events of a watch from a cursor on (GET
// /v1/watches/{id}/events?after=N&limit=M).
type Events struct {
	// WatchID tells a reader that goes by name when the name has passed to
	// another watch, whose events are numbered anew.
	WatchID string  `json:"watch_id"`
	Events  []Event `json:"events"`
	Next    int64   `json:"next"` // the next after
	More    bool    `json:"more"` // there are further ones already
	// Truncated is set if events before the first one were dropped.
	Truncated bool `json:"truncated"`
}

// Health is the answer to GET /healthz.
type Health struct {
	Status  string `json:"status"` // "ok"
	Version string `json:"version"`
	PID     int    `json:"pid"`
}

// The codes of errors.
const (
	CodeInvalid      = "invalid"       // the request is wrong
	CodeNotFound     = "not_found"     // no such watch or path
	CodeConflict     = "conflict"      // not in this state, or the name is taken
	CodeLimit        = "limit"         // too many watches are running
	CodeUnresolvable = "unresolvable"  // targets cannot be probed
	CodeShuttingDown = "shutting_down" // the server ends and takes no new watches
	CodeInternal     = "internal"
)

// Error is an error of the server.
type Error struct {
	Code    string        `json:"code"`
	Message string        `json:"message"`
	Targets []TargetError `json:"targets,omitempty"` // all that cannot be probed
}

// TargetError tells why a target cannot be probed.
type TargetError struct {
	Target  string `json:"target"`
	Message string `json:"message"`
}

// ErrorBody is the body of a response with an error.
type ErrorBody struct {
	Error *Error `json:"error"`
}

func (e *Error) Error() string { return e.Message }

// Errorf returns an error with a code.
func Errorf(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: Clean(fmt.Sprintf(format, args...), 1000)}
}
