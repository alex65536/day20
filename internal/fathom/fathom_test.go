package fathom

import (
	"path/filepath"
	"testing"

	"github.com/alex65536/go-chess/chess"
	"github.com/stretchr/testify/require"
)

func TestSimple(t *testing.T) {
	require.NoError(t, Init(filepath.Join("testdata", "syzygy")))
	defer Free()
	for _, tc := range []struct {
		fen string
		wdl WDL
		bad bool
	}{
		{
			fen: "5k2/5P2/4K3/8/8/8/8/8 w - - 0 1",
			wdl: WDLDraw,
		},
		{
			fen: "5k2/5P2/4K3/8/8/8/8/8 b - - 0 1",
			wdl: WDLLoss,
		},
		{
			fen: "8/8/8/8/8/4K3/5P2/5k2 w - - 0 1",
			wdl: WDLWin,
		},
		{
			fen: "8/8/8/8/8/4K3/5P2/5k2 b - - 0 1",
			wdl: WDLLoss,
		},
		{
			fen: "K7/8/8/8/3Q4/8/8/7k w - - 0 1",
			bad: true,
		},
	} {
		b, err := chess.BoardFromFEN(tc.fen)
		require.NoError(t, err)
		wdl, ok := ProbeWDL(b)
		if tc.bad {
			require.False(t, ok, "fen %q: probe fail expected", tc.fen)
		} else {
			require.True(t, ok, "fen %q: probe pass expected", tc.fen)
			require.Equal(t, tc.wdl, wdl, "fen %q", tc.fen)
		}
	}
}
