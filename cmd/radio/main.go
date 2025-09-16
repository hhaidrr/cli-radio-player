package main

import (
    "bufio"
    "context"
    "errors"
    "flag"
    "fmt"
    "os"
    "os/exec"
    "os/signal"
    "strings"
    "sync"
    "syscall"
    "time"
)

type Station struct {
    Name string
    URL  string
}

var defaultStations = []Station{
    {Name: "Lofi Girl (Relax/Study)", URL: "https://play.streamafrica.net/lofiradio"},
    {Name: "Lofi Hip Hop Radio", URL: "https://stream.nightride.fm/lofi.ogg"},
    {Name: "Chillhop Radio", URL: "https://stream.chillhop.com/live"},
    {Name: "Lofi Jazz", URL: "https://stream.nightride.fm/lofi-jazz.ogg"},
    {Name: "Ambient Lofi", URL: "https://stream.nightride.fm/chillsynth.ogg"},
}

type Player struct {
    mu             sync.Mutex
    cmd            *exec.Cmd
    currentStation int
    volumePercent  int
    isStopped      bool
    visualization  bool
}

func NewPlayer() *Player {
    return &Player{currentStation: 0, volumePercent: 70, visualization: false}
}

func (p *Player) ffplayArgs(url string) []string {
    // ffplay volume uses dB via -af volume=...; map 0-100% to -20..+0 dB approx
    volDb := float64(p.volumePercent)/100*0 - 20*(1-float64(p.volumePercent)/100)
    volFilter := fmt.Sprintf("volume=%fdB", volDb)
    args := []string{"-nodisp", "-autoexit", "-loglevel", "warning", "-af", volFilter, url}
    if p.visualization {
        // Use showwavespic as a lightweight visualization in a separate window
        // However -nodisp disables it; keep nodisp for headless. Toggle simply prints a note.
    }
    return args
}

func (p *Player) Start(url string) error {
    p.mu.Lock()
    defer p.mu.Unlock()
    if p.cmd != nil && p.cmd.Process != nil {
        return errors.New("player already running")
    }
    args := p.ffplayArgs(url)
    p.cmd = exec.Command("ffplay", args...)
    p.cmd.Stdout = os.Stdout
    p.cmd.Stderr = os.Stderr
    if err := p.cmd.Start(); err != nil {
        p.cmd = nil
        return err
    }
    p.isStopped = false
    go func(cmd *exec.Cmd) {
        _ = cmd.Wait()
        p.mu.Lock()
        defer p.mu.Unlock()
        p.cmd = nil
    }(p.cmd)
    return nil
}

func (p *Player) Stop() error {
    p.mu.Lock()
    defer p.mu.Unlock()
    if p.cmd == nil || p.cmd.Process == nil {
        p.isStopped = true
        return nil
    }
    p.isStopped = true
    return p.cmd.Process.Signal(syscall.SIGTERM)
}

func (p *Player) Restart(url string) error {
    _ = p.Stop()
    // slight delay to allow ffplay to exit
    time.Sleep(200 * time.Millisecond)
    return p.Start(url)
}

func (p *Player) SetVolume(percent int) {
    if percent < 0 {
        percent = 0
    }
    if percent > 100 {
        percent = 100
    }
    p.volumePercent = percent
}

func printHeader(volume int, nowPlaying string) {
    fmt.Printf("\n\U0001F50A Volume set to %d%%\n", volume)
    fmt.Printf("\U0001F3B5 Now Playing: %s\n", nowPlaying)
    fmt.Println("\u23F3 Loading stream...")
}

func printHelp() {
    fmt.Println()
    fmt.Println("\U0001F4AA Controls:")
    fmt.Println("  [s] Stop playback")
    fmt.Println("  [v] Change volume")
    fmt.Println("  [l] List all stations")
    fmt.Println("  [viz] Toggle visualization")
    fmt.Println("  [q] Quit")
    fmt.Println("  [h] Show this help")
    fmt.Println("  [1-5] Switch station")
    fmt.Println()
}

func listStations(stations []Station) {
    fmt.Println("Available Stations:")
    for i, s := range stations {
        fmt.Printf("  [%d] %s\n", i+1, s.Name)
    }
}

func interactiveMode(ctx context.Context, p *Player, stations []Station, startIdx int) {
    if startIdx < 0 || startIdx >= len(stations) {
        startIdx = 0
    }
    p.currentStation = startIdx
    now := stations[p.currentStation]
    printHeader(p.volumePercent, now.Name)
    _ = p.Start(now.URL)
    printHelp()
    fmt.Println("Press any key to continue...")
    reader := bufio.NewReader(os.Stdin)
    fmt.Print("radio> ")
    for {
        line, err := reader.ReadString('\n')
        if err != nil {
            if ctx.Err() != nil {
                return
            }
            fmt.Println("input error:", err)
            return
        }
        input := strings.TrimSpace(line)
        switch input {
        case "q":
            _ = p.Stop()
            return
        case "h":
            printHelp()
        case "s":
            _ = p.Stop()
        case "v":
            fmt.Print("Enter volume (0-100): ")
            vline, _ := reader.ReadString('\n')
            vline = strings.TrimSpace(vline)
            var v int
            fmt.Sscanf(vline, "%d", &v)
            p.SetVolume(v)
            fmt.Printf("Volume set to %d%%\n", p.volumePercent)
            // restart if currently playing
            if !p.isStopped {
                _ = p.Restart(stations[p.currentStation].URL)
            }
        case "l":
            listStations(stations)
        case "viz":
            p.visualization = !p.visualization
            state := "OFF"
            if p.visualization {
                state = "ON"
            }
            fmt.Println("Visualization:", state)
        case "1", "2", "3", "4", "5":
            idx := int(input[0]-'1')
            if idx >= 0 && idx < len(stations) {
                p.currentStation = idx
                now = stations[p.currentStation]
                fmt.Println("Switching to:", now.Name)
                _ = p.Restart(now.URL)
            } else {
                fmt.Println("Invalid station number")
            }
        default:
            if input != "" {
                fmt.Println("Unknown command. Press 'h' for help.")
            }
        }
        fmt.Print("radio> ")
    }
}

func main() {
    var (
        flagList bool
        flagInteractive bool
        flagStation int
        flagVolume int
    )
    flag.BoolVar(&flagInteractive, "i", true, "interactive mode")
    flag.BoolVar(&flagList, "list", false, "list stations and exit")
    flag.IntVar(&flagStation, "station", 1, "station number to start (1-5)")
    flag.IntVar(&flagVolume, "volume", 70, "start volume 0-100")
    flag.Parse()

    p := NewPlayer()
    p.SetVolume(flagVolume)

    if flagList {
        listStations(defaultStations)
        return
    }

    startIdx := flagStation - 1
    if startIdx < 0 || startIdx >= len(defaultStations) {
        startIdx = 0
    }

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    sig := make(chan os.Signal, 1)
    signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
    go func() {
        <-sig
        _ = p.Stop()
        cancel()
    }()

    if flagInteractive {
        interactiveMode(ctx, p, defaultStations, startIdx)
        return
    }

    // Standard mode: start and wait until Ctrl+C
    st := defaultStations[startIdx]
    printHeader(p.volumePercent, st.Name)
    if err := p.Start(st.URL); err != nil {
        fmt.Println("Failed to start:", err)
        os.Exit(1)
    }
    printHelp()
    <-ctx.Done()
}


