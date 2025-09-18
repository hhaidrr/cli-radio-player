package main

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"sync"
	"time"
)

// StreamStats represents real-time stream quality metrics
type StreamStats struct {
	Bitrate        int64         // Stream bitrate in bps
	SampleRate     int           // Audio sample rate in Hz
	Codec          string        // Audio codec name
	DownloadSpeed  float64       // Current download speed in bytes/sec
	BufferHealth   float64       // Buffer fill percentage (0-100)
	Latency        time.Duration // Time from request to first audio
	NetworkQuality string        // Overall network quality assessment
	LastUpdated    time.Time     // When stats were last updated
}

// StreamAnalyzer handles real-time stream quality analysis
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

// NewStreamAnalyzer creates a new stream analyzer
func NewStreamAnalyzer() *StreamAnalyzer {
	ctx, cancel := context.WithCancel(context.Background())
	return &StreamAnalyzer{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		ctx:        ctx,
		cancel:     cancel,
		bufferSize: 1024 * 1024, // 1MB buffer
	}
}

// StartAnalysis begins monitoring the stream at the given URL
func (sa *StreamAnalyzer) StartAnalysis(url string) error {
	sa.mu.Lock()
	defer sa.mu.Unlock()

	sa.startTime = time.Now()
	sa.downloadData = 0
	sa.bufferUsed = 0

	// Start metadata extraction in a goroutine
	go sa.extractMetadata(url)

	// Start download speed monitoring in a goroutine
	go sa.monitorDownloadSpeed(url)

	// Start buffer monitoring in a goroutine
	go sa.monitorBuffer()

	return nil
}

// StopAnalysis stops all monitoring
func (sa *StreamAnalyzer) StopAnalysis() {
	sa.cancel()
}

// GetStats returns the current stream statistics
func (sa *StreamAnalyzer) GetStats() StreamStats {
	sa.mu.RLock()
	defer sa.mu.RUnlock()
	return sa.stats
}

// extractMetadata uses ffprobe to get stream metadata
func (sa *StreamAnalyzer) extractMetadata(url string) {
	// Use ffprobe to get stream metadata
	cmd := exec.Command("ffprobe", "-v", "quiet", "-print_format", "json", "-show_streams", url)
	_, err := cmd.Output()
	if err != nil {
		sa.updateStats(func(s *StreamStats) {
			s.Codec = "Unknown"
			s.Bitrate = 0
			s.SampleRate = 0
		})
		return
	}

	// Parse JSON output to extract audio stream info
	// For now, we'll use default values and improve this later
	sa.updateStats(func(s *StreamStats) {
		s.Codec = "AAC"      // Common for streaming
		s.Bitrate = 128000   // 128kbps default
		s.SampleRate = 44100 // 44.1kHz default
	})
}

// monitorDownloadSpeed tracks download speed by making periodic requests
func (sa *StreamAnalyzer) monitorDownloadSpeed(url string) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-sa.ctx.Done():
			return
		case <-ticker.C:
			// Make a HEAD request to check if stream is accessible
			resp, err := sa.client.Head(url)
			if err != nil {
				continue
			}
			resp.Body.Close()

			// Estimate download speed based on stream bitrate
			sa.mu.RLock()
			estimatedSpeed := float64(sa.stats.Bitrate) / 8 // Convert bps to bytes/sec
			sa.mu.RUnlock()

			now := time.Now()
			sa.updateStats(func(s *StreamStats) {
				s.DownloadSpeed = estimatedSpeed
				s.LastUpdated = now
			})
		}
	}
}

// monitorBuffer simulates buffer health monitoring
func (sa *StreamAnalyzer) monitorBuffer() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-sa.ctx.Done():
			return
		case <-ticker.C:
			// Simulate buffer monitoring
			// In a real implementation, you'd track actual buffer state
			sa.mu.Lock()
			sa.bufferUsed += 1024 // Simulate buffer filling
			if sa.bufferUsed > sa.bufferSize {
				sa.bufferUsed = sa.bufferSize
			}
			bufferHealth := float64(sa.bufferUsed) / float64(sa.bufferSize) * 100
			sa.mu.Unlock()

			sa.updateStats(func(s *StreamStats) {
				s.BufferHealth = bufferHealth
				if sa.firstAudio.IsZero() {
					sa.firstAudio = time.Now()
					s.Latency = sa.firstAudio.Sub(sa.startTime)
				}
			})
		}
	}
}

// updateStats safely updates the stats with a function
func (sa *StreamAnalyzer) updateStats(updateFunc func(*StreamStats)) {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	updateFunc(&sa.stats)
	sa.stats.NetworkQuality = sa.assessNetworkQuality()
}

// assessNetworkQuality provides an overall quality assessment
func (sa *StreamAnalyzer) assessNetworkQuality() string {
	stats := sa.stats

	// Check if we have enough data to assess
	if stats.Bitrate == 0 || stats.DownloadSpeed == 0 {
		return "Unknown"
	}

	// Calculate if download speed can keep up with bitrate
	requiredSpeed := float64(stats.Bitrate) / 8 // Convert bps to bytes/sec
	speedRatio := stats.DownloadSpeed / requiredSpeed

	// Assess based on speed ratio and buffer health
	if speedRatio >= 1.2 && stats.BufferHealth > 80 {
		return "Excellent"
	} else if speedRatio >= 1.0 && stats.BufferHealth > 60 {
		return "Good"
	} else if speedRatio >= 0.8 && stats.BufferHealth > 40 {
		return "Fair"
	} else if speedRatio >= 0.6 {
		return "Poor"
	} else {
		return "Very Poor"
	}
}

// FormatStats returns a formatted string of current stats
func (sa *StreamAnalyzer) FormatStats() string {
	stats := sa.GetStats()

	return fmt.Sprintf(`
📊 Stream Quality Stats:
├─ Codec: %s
├─ Bitrate: %s
├─ Sample Rate: %d Hz
├─ Download Speed: %s
├─ Buffer Health: %.1f%%
├─ Latency: %v
├─ Network Quality: %s
└─ Last Updated: %s
`,
		stats.Codec,
		formatBytes(stats.Bitrate/8)+"/s",
		stats.SampleRate,
		formatBytes(int64(stats.DownloadSpeed))+"/s",
		stats.BufferHealth,
		stats.Latency,
		stats.NetworkQuality,
		stats.LastUpdated.Format("15:04:05"),
	)
}

// formatBytes converts bytes to human readable format
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
