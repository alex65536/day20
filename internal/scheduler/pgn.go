package scheduler

import (
	"log/slog"

	"github.com/alex65536/day20/internal/battle"
	"github.com/alex65536/day20/internal/roomkeeper"
	"github.com/alex65536/day20/internal/util/slogx"
)

func addPGNToJobOrAbort(log *slog.Logger, job *FinishedJob, game *battle.GameExt) {
	job.PGN = nil

	if game == nil {
		if job.Status.Kind != roomkeeper.JobAborted {
			job.Status = roomkeeper.NewStatusAborted("no game found in job")
		}
		return
	}

	pgn, err := game.PGN()
	if err != nil {
		log.Warn("could not convert the game into PGN", slogx.Err(err))
		if job.Status.Kind != roomkeeper.JobAborted {
			job.Status = roomkeeper.NewStatusAborted("game cannot be converted into PGN")
		}
		return
	}

	job.PGN = &pgn
}
