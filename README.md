# Day20

Runs and displays confrontations between chess engines.

![./doc/screenshot.png](Screenshot)

## Main features

- Run matches between chess engines
  * UCI is supported, WinBoard is not supported for now
- Watch the games between engines live
- Analyze the statistical significance of match results

## Structure

The project consists of two parts: Battlefield and Day20 Server.

### Battlefield

Battlefield is a CLI tool to run matches between engines.
It can be used, for example, to compare two versions of a chess engine and decide which one is better.

### Day20 Server

This is a server to run matches between engines.
Unlike Battlefield, it allows you to watch the games live and supports running the games in a distributed environment, on many computers at once.
Also it provides a neat web UI.

Server consists of two parts: `day20-server` and `day20-room`.
`day20-server` is the main part that does web UI, scheduling, talking to the database and all such stuff.
`day20-room` runs chess engines and reports games to `day20-server` via API.

## Installation and configuration

### Battlefield

Just do

```
go install github.com/alex65536/day20/cmd/bfield@latest
```

### Day20 Server

First, configure the server part. Install

```
go install github.com/alex65536/day20/cmd/day20-server@latest
```

Create configuration and place it into `day20.toml`.

```toml
addr = "0.0.0.0"
# Replace YOUR_DOMAIN with the domain where you want to run it.
host = "YOUR_DOMAIN"
# Create the subdirectory `secret/` beforehands. The file `secret.toml` will be created automatically in it.
secrets-path = "secret/secret.toml"

# Omit `[https]` section if you don't want to enable HTTPS.
[https]
# Create the subdirectory `secret/` beforehands.
cache-path = "secret/cert"

[db]
path = "day20.db"
```

Finally, run the server:

```
day20-server -o day20.toml
```

The server is up now! Grab the invite link from the logs and register the admin user with it.

Then, configure the rooms. You can have as many rooms as you want.
Also note that static IP is not required to run a room, you can run it on any device that has stable Internet connection.

First, go to your profile in the web UI, click Room tokens and create a new room token.
Download it and save it into `token.txt`.

Then, install

```
go install github.com/alex65536/day20/cmd/day20-room@latest
```

Create configuration and place it into `day20.toml`.

```
# Number of matches allowed to run in parallel. Should not exceed number of CPU cores.
rooms = 4
# Replace YOUR_DOMAIN with the domain where you want to run it.
url = "https://YOUR_DOMAIN/api/room"
token-file = "token.txt"

[engines]
# Create the directory `engines/` and place all the engines you want to use with Day20 there.
allow-dirs = ["engines"]
```

Finally, run the room:

```
day20-server -o day20.toml
```

After this, the newly created rooms should appear on the main page in the web UI.

Now, you are ready to run some matches between engines.

## Tech stack

- Server backend and Battlefield: [Go](https://go.dev/)
- Database: [SQLite](https://sqlite.org/)
- Frontend: a bunch of Go's `text/template`, vanilla JavaScript and [HTMX](https://htmx.org/)

## Used libraries and assets

For Go dependencies, see [go.mod](go.mod).

Others:
* [Chessboard.js](https://github.com/oakmac/chessboardjs) (MIT License)
* [Picnic CSS](https://github.com/franciscop/picnic) (MIT License)
* [Typicons](https://github.com/stephenhutchings/typicons.font) (SIL Open Font License 1.1)
* Chess pieces by [Colin M. L. Burnett](https://en.wikipedia.org/wiki/User:Cburnett) (GFDL, BSD or GPL)
* [HTMX](https://github.com/bigskysoftware/htmx) and [extensions](https://github.com/bigskysoftware/htmx-extensions) (Zero-Clause BSD)
* [jQuery](https://github.com/jquery/jquery) (required by Chessboard.js) (MIT License)

## Thanks to

Thanks to Graham Banks `<gbanksnz at gmail.com>` for opening books used by Day20.

Thanks to Jay Honnold for [node-tlcv](https://github.com/jhonnold/node-tlcv), which was a source of inspiration for Day20.

## Mini FAQ

**Q:** Why is this named Day20?

**A:** [I don't know!](https://youtu.be/fzsKhT3wHgI?t=330)

**Q:** Which operating systems are supported?

**A:** Hopefully, every one which is able to run Go. Windows is supported by Battlefield, but `day20-server` and `day20-room` were not tested under Windows.

**Q:** Why HTMX, not \<insert JS framework name here\>?

**A:** First, simplicity. Second, I never worked with any serious JS framework, and HTMX seemed simple enough if you have only plain HTML/CSS/JS knowledge. Third, it would complicate building the application substantially.

**Q:** Why SQLite? It's a toy database, isn't it?

**A:** Currently, the server cannot scale up to one instance, so anything more complex will not get any benefits. So, SQLite is chosen due to simplicity. Though, Day20 uses `gorm` and it wouldn't take much effort to support, for example, Postgres.

**Q:** Rewrite it in Rust!

**A:** No.

**Q:** My question is not in FAQ!

**A:** If there is indeed something that should be documented or needs clarification, you may file an issue. But please note that spamming, trolling and all such stuff is not allowed.

## License

Day20 is free software: you can redistribute it and/or modify it under the terms of the GNU General Public License as published by the Free Software Foundation, either version 3 of the License, or (at your option) any later version.

Day20 is distributed in the hope that it will be useful, but WITHOUT ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the [GNU General Public License](LICENSE) for more details.
