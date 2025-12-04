package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

type VideoMapping struct {
	OriginalName string `json:"original_name"`
	TranscodeDir string `json:"transcode_dir"`
	Status       string `json:"status"` // "processing", "completed", "failed"
}

type TranscodeState struct {
	mu       sync.RWMutex
	mappings map[string]*VideoMapping // key: original video name
	filePath string
}

var state *TranscodeState

func main() {
	// Initialize state
	state = &TranscodeState{
		mappings: make(map[string]*VideoMapping),
		filePath: "transcode_mappings.json",
	}

	// Load existing mappings
	if err := state.load(); err != nil {
		log.Printf("Warning: Could not load mappings: %v", err)
	}

	// Ensure directories exist
	os.MkdirAll("videos", 0755)
	os.MkdirAll("transcoded", 0755)

	// Serve transcoded files
	fs := http.FileServer(http.Dir("transcoded"))
	http.Handle("/hls/", corsHandler(http.StripPrefix("/hls/", fs)))

	// API routes
	http.HandleFunc("/api/videos", handleGetVideos)
	http.HandleFunc("/api/transcode", handleTranscode)
	http.HandleFunc("/api/status/", handleStatus)

	// Start server
	addr := ":8000"
	log.Println("Server listening on", addr)
	log.Println("Endpoints:")
	log.Println("  GET  /api/videos - List all videos")
	log.Println("  POST /api/transcode?video=<name> - Transcode a video")
	log.Println("  GET  /api/status/<video> - Get transcode status")
	log.Println("  GET  /hls/<dir>/master.m3u8 - Stream transcoded video")

	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}

func corsHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,HEAD,OPTIONS,POST")
		w.Header().Set("Access-Control-Allow-Headers", "Range, Accept, Origin, X-Requested-With, Content-Type")
		w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Range")

		switch ext := filepath.Ext(r.URL.Path); ext {
		case ".m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		case ".ts":
			w.Header().Set("Content-Type", "video/mp2t")
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func handleGetVideos(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get all videos from the videos folder
	entries, err := os.ReadDir("./videos")
	if err != nil {
		http.Error(w, "Could not read videos directory", http.StatusInternalServerError)
		return
	}

	type VideoInfo struct {
		Name         string `json:"name"`
		Transcoded   bool   `json:"transcoded"`
		Status       string `json:"status,omitempty"`
		StreamURL    string `json:"stream_url,omitempty"`
		TranscodeDir string `json:"transcode_dir,omitempty"`
	}

	var videos []VideoInfo
	state.mu.RLock()
	defer state.mu.RUnlock()

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		// Skip non-video files
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".mp4" && ext != ".mkv" && ext != ".avi" && ext != ".mov" && ext != ".webm" {
			continue
		}

		info := VideoInfo{
			Name:       name,
			Transcoded: false,
		}

		if mapping, exists := state.mappings[name]; exists {
			info.Transcoded = mapping.Status == "completed"
			info.Status = mapping.Status
			info.TranscodeDir = mapping.TranscodeDir
			if mapping.Status == "completed" {
				info.StreamURL = fmt.Sprintf("/hls/%s/master.m3u8", mapping.TranscodeDir)
			}
		}

		log.Println(info)

		videos = append(videos, info)
	}

	log.Println(videos)

	json.NewEncoder(w).Encode(videos)
}

func handleTranscode(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	videoName := r.URL.Query().Get("video")
	if videoName == "" {
		http.Error(w, "Missing 'video' parameter", http.StatusBadRequest)
		return
	}

	// Check if video exists
	videoPath := filepath.Join("videos", videoName)
	if _, err := os.Stat(videoPath); os.IsNotExist(err) {
		http.Error(w, "Video not found", http.StatusNotFound)
		return
	}

	// Check if already transcoded
	state.mu.RLock()
	mapping, exists := state.mappings[videoName]
	state.mu.RUnlock()

	if exists && mapping.Status == "completed" {
		json.NewEncoder(w).Encode(map[string]string{
			"message":       "Video already transcoded",
			"transcode_dir": mapping.TranscodeDir,
			"stream_url":    fmt.Sprintf("/hls/%s/master.m3u8", mapping.TranscodeDir),
		})
		return
	}

	if exists && mapping.Status == "processing" {
		json.NewEncoder(w).Encode(map[string]string{
			"message": "Video is currently being transcoded",
			"status":  "processing",
		})
		return
	}

	// Create transcode directory
	transcodeDir := strings.TrimSuffix(videoName, filepath.Ext(videoName))
	transcodeFullPath := filepath.Join("transcoded", transcodeDir)
	os.MkdirAll(transcodeFullPath, 0755)

	// Create mapping
	state.mu.Lock()
	state.mappings[videoName] = &VideoMapping{
		OriginalName: videoName,
		TranscodeDir: transcodeDir,
		Status:       "processing",
	}
	state.save()
	state.mu.Unlock()

	// Start transcoding in background
	go transcode(videoPath, transcodeFullPath, videoName)

	json.NewEncoder(w).Encode(map[string]string{
		"message":       "Transcoding started",
		"transcode_dir": transcodeDir,
		"status":        "processing",
	})
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	videoName := strings.TrimPrefix(r.URL.Path, "/api/status/")
	if videoName == "" {
		http.Error(w, "Missing video name", http.StatusBadRequest)
		return
	}

	state.mu.RLock()
	mapping, exists := state.mappings[videoName]
	state.mu.RUnlock()

	if !exists {
		http.Error(w, "Video not found", http.StatusNotFound)
		return
	}

	response := map[string]string{
		"video":         mapping.OriginalName,
		"transcode_dir": mapping.TranscodeDir,
		"status":        mapping.Status,
	}

	if mapping.Status == "completed" {
		response["stream_url"] = fmt.Sprintf("/hls/%s/master.m3u8", mapping.TranscodeDir)
	}

	json.NewEncoder(w).Encode(response)
}

func transcode(videoPath, outputDir, videoName string) {
	log.Printf("Starting transcode for: %s", videoName)

	// Build ffmpeg command
	cmd := exec.Command("ffmpeg",
		"-i", videoPath,
		"-filter:v:0", "scale=w=854:h=480", "-c:v:0", "libx264", "-b:v:0", "300k", "-g", "60",
		"-filter:v:1", "scale=w=1280:h=720", "-c:v:1", "libx264", "-b:v:1", "1500k", "-g", "60",
		"-filter:v:2", "scale=w=1920:h=1080", "-c:v:2", "libx264", "-b:v:2", "3000k", "-g", "60",
		"-map", "0:v", "-map", "0:a", "-map", "0:v", "-map", "0:a", "-map", "0:v", "-map", "0:a",
		"-c:a:0", "aac", "-b:a:0", "64k",
		"-c:a:1", "aac", "-b:a:1", "96k",
		"-c:a:2", "aac", "-b:a:2", "128k",
		"-f", "hls",
		"-hls_time", "5",
		"-hls_list_size", "0",
		"-hls_flags", "independent_segments",
		"-var_stream_map", "v:0,a:0 v:1,a:1 v:2,a:2",
		"-master_pl_name", "master.m3u8",
		"-hls_segment_filename", filepath.Join(outputDir, "stream_%v/chunk%05d.ts"),
		filepath.Join(outputDir, "stream_%v/stream.m3u8"),
	)

	// Run command
	output, err := cmd.CombinedOutput()

	state.mu.Lock()
	defer state.mu.Unlock()

	if err != nil {
		log.Printf("Transcode failed for %s: %v\n%s", videoName, err, string(output))
		state.mappings[videoName].Status = "failed"
	} else {
		log.Printf("Transcode completed for: %s", videoName)
		state.mappings[videoName].Status = "completed"
	}

	state.save()
}

func (s *TranscodeState) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No file yet, that's okay
		}
		return err
	}

	return json.Unmarshal(data, &s.mappings)
}

func (s *TranscodeState) save() error {
	data, err := json.Marshal(s.mappings)
	if err != nil {
		return err
	}

	return os.WriteFile(s.filePath, data, 0644)
}
