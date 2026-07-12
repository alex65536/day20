package fathom

/*
#cgo CFLAGS: -I vendor/
#include "tbprobe.c"
*/
import "C"

import (
	"fmt"

	"github.com/alex65536/go-chess/chess"
)

type WDL uint

const (
	WDLLoss        WDL = C.TB_LOSS
	WDLBlessedLoss WDL = C.TB_BLESSED_LOSS
	WDLDraw        WDL = C.TB_DRAW
	WDLCursedWin   WDL = C.TB_CURSED_WIN
	WDLWin         WDL = C.TB_WIN
)

func (w WDL) String() string {
	switch w {
	case WDLLoss:
		return "loss"
	case WDLBlessedLoss:
		return "blessed loss"
	case WDLDraw:
		return "draw"
	case WDLCursedWin:
		return "cursed win"
	case WDLWin:
		return "win"
	default:
		return "invalid"
	}
}

var inited = false

func Init(path string) error {
	if inited {
		return fmt.Errorf("already inited")
	}
	if !C.tb_init(C.CString(path)) {
		return fmt.Errorf("could not initialize fathom")
	}
	inited = true
	return nil
}

func Free() {
	if inited {
		C.tb_free()
		inited = false
	}
}

func bbPieceAll(b *chess.Board, p chess.Piece) chess.Bitboard {
	return b.BbPiece(chess.ColorWhite, p) | b.BbPiece(chess.ColorBlack, p)
}

func ProbeWDL(b *chess.Board) (WDL, bool) {
	if !inited {
		return 0, false
	}
	if b.Castling() != chess.CastlingRightsEmpty {
		// Castling rights are not supported by Syzygy tables, and I'm lazy to figure out how to
		// pass them to Fathom properly, so return early here.
		return 0, false
	}
	ep := C.uint(0)
	if epCoord, ok := b.EpDest().TryGet(); ok {
		ep = C.uint(epCoord.FlippedRank())
	}
	res := C.tb_probe_wdl(
		C.uint64_t(b.BbColor(chess.ColorWhite).FlippedRank()),
		C.uint64_t(b.BbColor(chess.ColorBlack).FlippedRank()),
		C.uint64_t(bbPieceAll(b, chess.PieceKing).FlippedRank()),
		C.uint64_t(bbPieceAll(b, chess.PieceQueen).FlippedRank()),
		C.uint64_t(bbPieceAll(b, chess.PieceRook).FlippedRank()),
		C.uint64_t(bbPieceAll(b, chess.PieceBishop).FlippedRank()),
		C.uint64_t(bbPieceAll(b, chess.PieceKnight).FlippedRank()),
		C.uint64_t(bbPieceAll(b, chess.PiecePawn).FlippedRank()),
		C.uint(b.MoveCounter()),
		/* castling */ 0,
		ep,
		b.Side() == chess.ColorWhite,
	)
	if res == C.TB_RESULT_FAILED {
		return 0, false
	}
	return WDL(res), true
}
