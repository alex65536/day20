package database

import (
	"github.com/alex65536/day20/internal/roomkeeper"
	"github.com/alex65536/day20/internal/scheduler"
	"github.com/alex65536/day20/internal/userauth"
)

type Room struct {
	Info  roomkeeper.RoomInfo `gorm:"embedded"`
	JobID *string
	Job   *scheduler.RunningJob `gorm:"foreignKey:JobID"`
}

type Contest struct {
	Info         scheduler.ContestInfo   `gorm:"embedded"`
	Data         scheduler.ContestData   `gorm:"embedded"`
	RunningJobs  []scheduler.RunningJob  `gorm:"foreignKey:ContestID"`
	FinishedJobs []scheduler.FinishedJob `gorm:"foreignKey:ContestID"`
	Match        *Match                  `gorm:"foreignKey:ID;references:ContestID"`
}

type Match struct {
	ContestID string                  `gorm:"primaryKey"`
	Settings  scheduler.MatchSettings `gorm:"embedded"`
	Data      scheduler.MatchData     `gorm:"embedded"`
}

type FinishedJobData struct {
	Status roomkeeper.JobStatus `gorm:"embedded;embeddedPrefix:status_"`
	PGN    *string
}

var models = []any{
	&Room{},
	&Contest{},
	&Match{},
	&scheduler.RunningJob{},
	&scheduler.FinishedJob{},
	&userauth.User{},
	&userauth.InviteLink{},
	&userauth.RoomToken{},
}
