# TODO

- write `README.md`
- include copyright info for all 3rd party assets (incl. CSS, JS & chess pieces)
- probably panic completely when one of the HTTP handlers panic (?)
- add chat and event system in rooms
- show current contest info in rooms
- show rooms where the current contest is played
- make contest info page interactive and auto-refreshable via websocket
- validate lengths of chess engine names in `/contests/new`
- add sha256 as a query parameter to static instead of `ServerID`
- optimize sqlite, see [here](https://kerkour.com/sqlite-for-servers) for details
  - note that `STRICT` can be added via `.Set("gorm:table_options", "STRICT")` but it doesn't work well with `gorm`
- limit maximum length of invite links and room tokens
- allow to configure time margin (either in `contests/new` or in static config)
- make neat progress bars to show contest progress
- add gzip compression for Room API handlers
- add tournaments "all vs all"
