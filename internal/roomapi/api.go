package roomapi

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/alex65536/day20/internal/delta"
	"github.com/alex65536/go-chess/chess"
	"github.com/alex65536/go-chess/clock"
)

type ErrorCode int

const (
	ErrInvalidCode ErrorCode = iota
	ErrNeedsResync
	ErrNoSuchRoom
	ErrNoJob
	ErrJobCanceled
	ErrBadToken
)

func MatchesError(err error, code ErrorCode) bool {
	var apiErr *Error
	return errors.As(err, &apiErr) && apiErr.Code == code
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
	RoomID string       `json:"room_id"`
	From   delta.Cursor `json:"from"`
	Delta  *delta.State `json:"delta"`
	Done   bool         `json:"done,omitempty"`
	Error  string       `json:"error,omitempty"`
}

type UpdateResponse struct{}

type JobEngine struct {
	Name string `json:"name"`
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

type JobRequest struct {
	RoomID  string        `json:"room_id"`
	Timeout time.Duration `json:"timeout"`
}

type JobResponse struct {
	Job Job `json:"job"`
}

type HelloRequest struct{}

type HelloResponse struct {
	RoomID string `json:"room_id"`
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
