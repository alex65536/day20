package database

import (
	"github.com/alex65536/day20/internal/roomapi"
	"github.com/alex65536/day20/internal/roomkeeper"
	"github.com/alex65536/day20/internal/userauth"
)

type RunningJob struct {
	Job roomapi.Job `gorm:"embedded"`
}

type Room struct {
	Info  roomkeeper.RoomInfo `gorm:"embedded"`
	JobID *string
	Job   *RunningJob
}

type FinishedJobData struct {
	Status roomkeeper.JobStatus `gorm:"embedded;embeddedPrefix:status_"`
	PGN    *string
}

type FinishedJob struct {
	Job  roomapi.Job     `gorm:"embedded"`
	Data FinishedJobData `gorm:"embedded"`
}

var models = []any{
	&Room{},
	&RunningJob{},
	&FinishedJob{},
	&userauth.User{},
	&userauth.InviteLink{},
	&userauth.RoomToken{},
}
