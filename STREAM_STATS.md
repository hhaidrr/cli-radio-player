# Stream Quality Stats Feature

This document describes the new stream quality monitoring feature added to the drift-radio CLI application.

## Overview

The stream quality stats feature provides real-time monitoring of audio stream quality, including network performance, buffer health, and stream metadata. This helps users understand the quality of their streaming experience and troubleshoot any issues.

## Features

### Real-time Metrics

- **Codec Information**: Shows the audio codec being used (e.g., AAC, MP3)
- **Bitrate**: Displays the stream bitrate in bits per second
- **Sample Rate**: Shows the audio sample rate in Hz
- **Download Speed**: Real-time download speed in bytes per second
- **Buffer Health**: Percentage of buffer utilization (0-100%)
- **Latency**: Time from stream request to first audio playback
- **Network Quality**: Overall assessment (Excellent, Good, Fair, Poor, Very Poor)

### Commands

- `stats` - Toggle real-time stats display on/off
- `show` - Display current stream stats once
- `h` - Show help (includes new commands)

## Usage

### Interactive Mode

1. Start the application: `./drift-radio`
2. Select a station (1-5) or use the default
3. Type `stats` to enable real-time stats display
4. Type `show` to view current stats once
5. Type `stats` again to disable real-time display

### Real-time Display

When stats are enabled, the screen will update every 3 seconds with:
- Current stream quality metrics
- Network performance indicators
- Buffer health status
- Overall quality assessment

## Technical Implementation

### StreamAnalyzer

The `StreamAnalyzer` struct handles all quality monitoring:

```go
type StreamAnalyzer struct {
    mu           sync.RWMutex
    stats        StreamStats
    client       *http.Client
    ctx          context.Context
    cancel       context.CancelFunc
    downloadData int64
    startTime    time.Time
    firstAudio   time.Time
    bufferSize   int64
    bufferUsed   int64
}
```

### Key Methods

- `StartAnalysis(url)` - Begins monitoring a stream
- `StopAnalysis()` - Stops all monitoring
- `GetStats()` - Returns current statistics
- `FormatStats()` - Returns formatted display string

### Monitoring Components

1. **Metadata Extraction**: Uses `ffprobe` to extract stream information
2. **Download Speed**: Monitors network performance via HTTP requests
3. **Buffer Monitoring**: Simulates buffer health tracking
4. **Latency Measurement**: Tracks time to first audio

## Quality Assessment

The system provides an overall network quality assessment based on:

- **Speed Ratio**: Download speed vs. required bitrate
- **Buffer Health**: Buffer utilization percentage
- **Connection Stability**: Based on successful requests

### Quality Levels

- **Excellent**: Speed ratio ≥ 1.2, Buffer > 80%
- **Good**: Speed ratio ≥ 1.0, Buffer > 60%
- **Fair**: Speed ratio ≥ 0.8, Buffer > 40%
- **Poor**: Speed ratio ≥ 0.6
- **Very Poor**: Speed ratio < 0.6

## Dependencies

- `ffprobe` - For stream metadata extraction (part of FFmpeg)
- `ffplay` - For audio playback (part of FFmpeg)
- `yt-dlp` - For YouTube URL resolution

## Future Enhancements

- JSON parsing of ffprobe output for accurate metadata
- Real buffer monitoring integration with ffplay
- Historical quality tracking
- Quality alerts and notifications
- Export stats to file

## Troubleshooting

### Common Issues

1. **"No stream analysis available"**: Ensure ffprobe is installed
2. **Stats not updating**: Check network connectivity
3. **Inaccurate bitrate**: May need to improve ffprobe JSON parsing

### Debug Mode

To debug stream analysis issues, check the console output for warnings about stream analysis failures.

## Example Output

```
📊 Stream Quality Stats:
├─ Codec: AAC
├─ Bitrate: 128.0 KB/s
├─ Sample Rate: 44100 Hz
├─ Download Speed: 16.0 KB/s
├─ Buffer Health: 85.2%
├─ Latency: 1.2s
├─ Network Quality: Good
└─ Last Updated: 15:04:05
```

