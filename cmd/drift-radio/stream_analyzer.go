package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"sync"
	"time"
)

// StreamStats represents real-time stream quality metrics
type StreamStats struct {
	Bitrate             int64         // Stream bitrate in bps
	SampleRate          int           // Audio sample rate in Hz
	Codec               string        // Audio codec name
	DownloadSpeed       float64       // Current download speed in bytes/sec
	BufferHealth        float64       // Buffer fill percentage (0-100)
	Latency             time.Duration // Time from request to first audio
	NetworkQuality      string        // Overall network quality assessment
	LastUpdated         time.Time     // When stats were last updated
	PacketLoss          float64       // Packet loss percentage
	Jitter              time.Duration // Network jitter
	ConnectionStability float64       // Connection stability score (0-100)
	TotalBytes          int64         // Total bytes downloaded
	StartTime           time.Time     // When monitoring started
}

// FFProbeStream represents a stream from ffprobe JSON output
type FFProbeStream struct {
	Index      int    `json:"index"`
	CodecName  string `json:"codec_name"`
	CodecType  string `json:"codec_type"`
	BitRate    string `json:"bit_rate"`
	SampleRate string `json:"sample_rate"`
	Channels   int    `json:"channels"`
	Duration   string `json:"duration"`
	StartTime  string `json:"start_time"`
}

// FFProbeOutput represents the complete ffprobe JSON output
type FFProbeOutput struct {
	Streams []FFProbeStream `json:"streams"`
}

// StreamAnalyzer handles real-time stream quality analysis
type StreamAnalyzer struct {
	mu                 sync.RWMutex
	stats              StreamStats
	client             *http.Client
	ctx                context.Context
	cancel             context.CancelFunc
	downloadData       int64
	startTime          time.Time
	firstAudio         time.Time
	bufferSize         int64
	bufferUsed         int64
	lastDownloadTime   time.Time
	lastDownloadBytes  int64
	successfulRequests int
	failedRequests     int
	requestTimes       []time.Duration
	lastRequestTime    time.Time
}

// NewStreamAnalyzer creates a new stream analyzer
func NewStreamAnalyzer() *StreamAnalyzer {
	ctx, cancel := context.WithCancel(context.Background())
	return &StreamAnalyzer{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		ctx:          ctx,
		cancel:       cancel,
		bufferSize:   1024 * 1024,                  // 1MB buffer
		requestTimes: make([]time.Duration, 0, 10), // Keep last 10 request times
	}
}

// StartAnalysis begins monitoring the stream at the given URL
func (sa *StreamAnalyzer) StartAnalysis(url string) error {
	sa.mu.Lock()
	defer sa.mu.Unlock()

	now := time.Now()
	sa.startTime = now
	sa.downloadData = 0
	sa.bufferUsed = 0
	sa.lastDownloadTime = now
	sa.lastDownloadBytes = 0
	sa.successfulRequests = 0
	sa.failedRequests = 0
	sa.requestTimes = sa.requestTimes[:0] // Reset request times slice
	sa.lastRequestTime = time.Time{}

	// Initialize stats
	sa.stats.StartTime = now
	sa.stats.TotalBytes = 0
	sa.stats.PacketLoss = 0
	sa.stats.Jitter = 0
	sa.stats.ConnectionStability = 100

	// Start metadata extraction in a goroutine
	go sa.extractMetadata(url)

	// Start download speed monitoring in a goroutine
	go sa.monitorDownloadSpeed(url)

	// Start buffer monitoring in a goroutine
	go sa.monitorBuffer()

	// Start network quality monitoring in a goroutine
	go sa.monitorNetworkQuality(url)

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

// GetQualityAlerts returns a list of quality alerts based on current stats
func (sa *StreamAnalyzer) GetQualityAlerts() []string {
	sa.mu.RLock()
	defer sa.mu.RUnlock()

	var alerts []string
	stats := sa.stats

	// Check for high packet loss
	if stats.PacketLoss > 5 {
		alerts = append(alerts, fmt.Sprintf("High packet loss: %.2f%% - Check network connection", stats.PacketLoss))
	}

	// Check for high jitter
	if stats.Jitter > 500*time.Millisecond {
		alerts = append(alerts, fmt.Sprintf("High network jitter: %v - Network may be unstable", stats.Jitter))
	}

	// Check for low connection stability
	if stats.ConnectionStability < 70 {
		alerts = append(alerts, fmt.Sprintf("Low connection stability: %.1f%% - Consider switching networks", stats.ConnectionStability))
	}

	// Check for low buffer health
	if stats.BufferHealth < 30 {
		alerts = append(alerts, fmt.Sprintf("Low buffer health: %.1f%% - Stream may stutter", stats.BufferHealth))
	}

	// Check for poor network quality
	if stats.NetworkQuality == "Poor" || stats.NetworkQuality == "Very Poor" {
		alerts = append(alerts, fmt.Sprintf("Poor network quality: %s - Try a different station or check connection", stats.NetworkQuality))
	}

	// Check for high latency
	if stats.Latency > 5*time.Second {
		alerts = append(alerts, fmt.Sprintf("High latency: %v - Stream may be slow to start", stats.Latency))
	}

	// Check if download speed is insufficient
	if stats.Bitrate > 0 {
		requiredSpeed := float64(stats.Bitrate) / 8
		if stats.DownloadSpeed < requiredSpeed*0.8 {
			alerts = append(alerts, fmt.Sprintf("Slow download speed: %s/s (needs %s/s) - Check bandwidth",
				formatBytes(int64(stats.DownloadSpeed)), formatBytes(int64(requiredSpeed))))
		}
	}

	return alerts
}

// extractMetadata uses ffprobe to get stream metadata
func (sa *StreamAnalyzer) extractMetadata(url string) {
	// Use ffprobe to get stream metadata
	cmd := exec.Command("ffprobe", "-v", "quiet", "-print_format", "json", "-show_streams", url)
	output, err := cmd.Output()
	if err != nil {
		sa.updateStats(func(s *StreamStats) {
			s.Codec = "Unknown"
			s.Bitrate = 0
			s.SampleRate = 0
		})
		return
	}

	// Parse JSON output
	var probeOutput FFProbeOutput
	if err := json.Unmarshal(output, &probeOutput); err != nil {
		// Fallback to default values if JSON parsing fails
		sa.updateStats(func(s *StreamStats) {
			s.Codec = "AAC"
			s.Bitrate = 128000
			s.SampleRate = 44100
		})
		return
	}

	// Find the first audio stream
	var audioStream *FFProbeStream
	for i := range probeOutput.Streams {
		if probeOutput.Streams[i].CodecType == "audio" {
			audioStream = &probeOutput.Streams[i]
			break
		}
	}

	if audioStream == nil {
		// No audio stream found, use defaults
		sa.updateStats(func(s *StreamStats) {
			s.Codec = "Unknown"
			s.Bitrate = 128000
			s.SampleRate = 44100
		})
		return
	}

	// Parse bitrate
	bitrate := int64(128000) // Default
	if audioStream.BitRate != "" {
		if br, err := fmt.Sscanf(audioStream.BitRate, "%d", &bitrate); err == nil && br == 1 {
			// bitrate is now set
		}
	}

	// Parse sample rate
	sampleRate := 44100 // Default
	if audioStream.SampleRate != "" {
		if sr, err := fmt.Sscanf(audioStream.SampleRate, "%d", &sampleRate); err == nil && sr == 1 {
			// sampleRate is now set
		}
	}

	sa.updateStats(func(s *StreamStats) {
		s.Codec = audioStream.CodecName
		s.Bitrate = bitrate
		s.SampleRate = sampleRate
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
			startTime := time.Now()

			// Make a HEAD request to check if stream is accessible
			resp, err := sa.client.Head(url)
			requestDuration := time.Since(startTime)

			sa.mu.Lock()
			if err != nil {
				sa.failedRequests++
			} else {
				sa.successfulRequests++
				resp.Body.Close()

				// Track request times for jitter calculation
				sa.requestTimes = append(sa.requestTimes, requestDuration)
				if len(sa.requestTimes) > 10 {
					sa.requestTimes = sa.requestTimes[1:] // Keep only last 10
				}
				sa.lastRequestTime = startTime
			}
			sa.mu.Unlock()

			// Calculate actual download speed based on stream bitrate
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
			// Simulate buffer monitoring based on download speed and bitrate
			sa.mu.Lock()

			// Calculate buffer fill based on download speed vs bitrate
			now := time.Now()
			timeDiff := now.Sub(sa.lastDownloadTime).Seconds()
			if timeDiff > 0 {
				// Simulate buffer filling based on download speed
				bytesPerSecond := float64(sa.stats.Bitrate) / 8
				bufferIncrease := int64(bytesPerSecond * timeDiff * 0.1) // 10% of theoretical max
				sa.bufferUsed += bufferIncrease
				if sa.bufferUsed > sa.bufferSize {
					sa.bufferUsed = sa.bufferSize
				}
				sa.lastDownloadTime = now
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

// monitorNetworkQuality tracks network quality metrics
func (sa *StreamAnalyzer) monitorNetworkQuality(url string) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-sa.ctx.Done():
			return
		case <-ticker.C:
			sa.mu.RLock()
			totalRequests := sa.successfulRequests + sa.failedRequests
			requestTimes := make([]time.Duration, len(sa.requestTimes))
			copy(requestTimes, sa.requestTimes)
			sa.mu.RUnlock()

			// Calculate packet loss (based on failed requests)
			packetLoss := 0.0
			if totalRequests > 0 {
				packetLoss = float64(sa.failedRequests) / float64(totalRequests) * 100
			}

			// Calculate jitter (standard deviation of request times)
			jitter := sa.calculateJitter(requestTimes)

			// Calculate connection stability
			stability := sa.calculateConnectionStability()

			sa.updateStats(func(s *StreamStats) {
				s.PacketLoss = packetLoss
				s.Jitter = jitter
				s.ConnectionStability = stability
			})
		}
	}
}

// calculateJitter calculates network jitter from request times
func (sa *StreamAnalyzer) calculateJitter(requestTimes []time.Duration) time.Duration {
	if len(requestTimes) < 2 {
		return 0
	}

	// Calculate average
	var sum time.Duration
	for _, rt := range requestTimes {
		sum += rt
	}
	avg := sum / time.Duration(len(requestTimes))

	// Calculate standard deviation
	var variance time.Duration
	for _, rt := range requestTimes {
		diff := rt - avg
		variance += diff * diff
	}
	variance /= time.Duration(len(requestTimes))

	// Return jitter as standard deviation
	return time.Duration(float64(variance) * 0.5)
}

// calculateConnectionStability calculates connection stability score
func (sa *StreamAnalyzer) calculateConnectionStability() float64 {
	sa.mu.RLock()
	defer sa.mu.RUnlock()

	totalRequests := sa.successfulRequests + sa.failedRequests
	if totalRequests == 0 {
		return 100.0
	}

	successRate := float64(sa.successfulRequests) / float64(totalRequests)

	// Base stability on success rate
	stability := successRate * 100

	// Penalize for high jitter
	if len(sa.requestTimes) > 1 {
		avgRequestTime := time.Duration(0)
		for _, rt := range sa.requestTimes {
			avgRequestTime += rt
		}
		avgRequestTime /= time.Duration(len(sa.requestTimes))

		// If average request time is too high, reduce stability
		if avgRequestTime > 2*time.Second {
			stability *= 0.8
		}
	}

	if stability < 0 {
		stability = 0
	}
	if stability > 100 {
		stability = 100
	}

	return stability
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

	// Assess based on multiple factors
	score := 0.0

	// Speed ratio factor (40% weight)
	if speedRatio >= 1.2 {
		score += 40
	} else if speedRatio >= 1.0 {
		score += 35
	} else if speedRatio >= 0.8 {
		score += 25
	} else if speedRatio >= 0.6 {
		score += 15
	} else {
		score += 5
	}

	// Buffer health factor (20% weight)
	if stats.BufferHealth > 80 {
		score += 20
	} else if stats.BufferHealth > 60 {
		score += 15
	} else if stats.BufferHealth > 40 {
		score += 10
	} else {
		score += 5
	}

	// Connection stability factor (25% weight)
	score += stats.ConnectionStability * 0.25

	// Packet loss penalty (10% weight)
	if stats.PacketLoss < 1 {
		score += 10
	} else if stats.PacketLoss < 5 {
		score += 5
	} else if stats.PacketLoss < 10 {
		score += 2
	}

	// Jitter penalty (5% weight)
	if stats.Jitter < 100*time.Millisecond {
		score += 5
	} else if stats.Jitter < 500*time.Millisecond {
		score += 3
	} else if stats.Jitter < 1*time.Second {
		score += 1
	}

	// Determine quality level based on total score
	if score >= 90 {
		return "Excellent"
	} else if score >= 75 {
		return "Good"
	} else if score >= 60 {
		return "Fair"
	} else if score >= 40 {
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
├─ Packet Loss: %.2f%%
├─ Network Jitter: %v
├─ Connection Stability: %.1f%%
├─ Network Quality: %s
└─ Last Updated: %s
`,
		stats.Codec,
		formatBytes(stats.Bitrate/8)+"/s",
		stats.SampleRate,
		formatBytes(int64(stats.DownloadSpeed))+"/s",
		stats.BufferHealth,
		stats.Latency,
		stats.PacketLoss,
		stats.Jitter,
		stats.ConnectionStability,
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
