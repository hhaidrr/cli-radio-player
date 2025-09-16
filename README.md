# CLI Lofi Radio (Go + ffplay)

A simple CLI radio player that streams lofi stations using `ffplay` (from FFmpeg).

## Dependencies

- Go 1.20+
- FFmpeg (provides `ffplay`)

On Ubuntu/Debian:

```bash
sudo apt update && sudo apt install -y ffmpeg
```

Verify:

```bash
ffplay -version
```

## Build

```bash
go build ./cmd/radio
```

This produces a `radio` binary in the project root.

## Run

Interactive mode (default):

```bash
./radio
```

Standard mode with flags:

```bash
./radio -station 2 -volume 70 -i=false
```

List stations:

```bash
./radio -list
```

## Controls

- [s] Stop playback
- [v] Change volume (0-100)
- [l] List all stations
- [viz] Toggle visualization note (no window; stub)
- [q] Quit
- [h] Help
- [1-5] Switch station

## Notes

- Volume is applied via an ffmpeg volume filter using an approximate dB mapping.
- Visualization toggle is currently informational only and does not open a visual window in `-nodisp` mode.
