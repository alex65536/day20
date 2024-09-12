package roomapi

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/alex65536/day20/internal/delta"
	"github.com/alex65536/day20/internal/util"
	httputil "github.com/alex65536/day20/internal/util/http"
	"github.com/alex65536/go-chess/chess"
	"github.com/alex65536/go-chess/clock"
)

const ProtoVersion = 1

type ErrorCode int

const (
	ErrInvalidCode ErrorCode = iota
	ErrNeedsResync
	ErrNoSuchRoom
	ErrNoJob
	ErrNoJobRunning
	ErrBadToken
	ErrBadRequest
	ErrIncompatibleProto
	ErrLocked
)

func MatchesError(err error, code ErrorCode) bool {
	var apiErr *Error
	return errors.As(err, &apiErr) && apiErr.Code == code
}

func IsErrorRetriable(err error) bool {
	if apiErr := (*Error)(nil); errors.As(err, &apiErr) {
		return apiErr.Code == ErrLocked
	}
	if httpErr := (*httputil.HTTPError)(nil); errors.As(err, &httpErr) {
		return false
	}
	return true
}

type Error struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("room error %v: %v", e.Code, e.Message)
}

var _ error = (*Error)(nil)

type UpdateRequest struct {
	RoomID    string          `json:"room_id"`
	From      delta.Cursor    `json:"from"`
	Delta     *delta.State    `json:"delta"`
	Timestamp delta.Timestamp `json:"ts"`
	Done      bool            `json:"done,omitempty"`
	Error     string          `json:"error,omitempty"`
}

type UpdateResponse struct{}

type JobEngine struct {
	Name string `json:"name"`
}

func (e JobEngine) Clone() JobEngine {
	return e
}

type Job struct {
	FixedTime      *time.Duration  `json:"fixed_time,omitempty"`
	TimeControl    *clock.Control  `json:"time_control,omitempty"`
	StartBoard     *chess.RawBoard `json:"start_board,omitempty"`
	StartMoves     []chess.UCIMove `json:"start_moves,omitempty"`
	ScoreThreshold int32           `json:"score_threshold,omitempty"`
	TimeMargin     *time.Duration  `json:"time_margin"`
	White          JobEngine       `json:"white"`
	Black          JobEngine       `json:"black"`
}

func cloneTrivial[T any](a *T) *T {
	if a == nil {
		return nil
	}
	b := *a
	return &b
}

func clone[T util.Clonable[T]](a *T) *T {
	if a == nil {
		return nil
	}
	b := (*a).Clone()
	return &b
}

func (j Job) Clone() Job {
	j.FixedTime = cloneTrivial(j.FixedTime)
	j.TimeControl = clone(j.TimeControl)
	j.StartBoard = cloneTrivial(j.StartBoard)
	j.StartMoves = slices.Clone(j.StartMoves)
	j.TimeMargin = cloneTrivial(j.TimeMargin)
	j.White = j.White.Clone()
	j.Black = j.Black.Clone()
	return j
}

type JobRequest struct {
	RoomID  string        `json:"room_id"`
	Timeout time.Duration `json:"timeout"`
}

type JobResponse struct {
	Job Job `json:"job"`
}

type HelloRequest struct {
	SupportedProtoVersions []int `json:"supported_proto_versions"`
}

type HelloResponse struct {
	RoomID       string `json:"room_id"`
	ProtoVersion int    `json:"proto_version"`
}

type ByeRequest struct {
	RoomID string `json:"room_id"`
}

type ByeResponse struct{}

type API interface {
	Update(ctx context.Context, req *UpdateRequest) (*UpdateResponse, error)
	Job(ctx context.Context, req *JobRequest) (*JobResponse, error)
	Hello(ctx context.Context, req *HelloRequest) (*HelloResponse, error)
	Bye(ctx context.Context, req *ByeRequest) (*ByeResponse, error)
}
