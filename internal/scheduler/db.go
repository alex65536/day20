package scheduler

import (
	"context"
	"errors"

	"github.com/alex65536/day20/internal/roomkeeper"
)

var ErrNoSuchContest = errors.New("no such contest")

type DB interface {
	ListActiveRooms(ctx context.Context) ([]roomkeeper.RoomFullData, error)
	ListRunningContestsFull(ctx context.Context) ([]ContestFullData, error)
	ListRunningJobs(ctx context.Context) ([]RunningJob, error)
	ListContests(ctx context.Context) ([]ContestFullData, error)
	CreateContest(ctx context.Context, info ContestInfo, data ContestData) error
	UpdateContest(ctx context.Context, contestID string, data ContestData) error
	GetContest(ctx context.Context, contestID string) (ContestInfo, ContestData, error)
	CreateRunningJob(ctx context.Context, job *RunningJob) error
	FinishRunningJob(ctx context.Context, job *FinishedJob) error
	ListContestSucceededJobs(ctx context.Context, contestID string) ([]FinishedJob, error)
}
