package main

import (
	"log"
	"net/http"
	"path/filepath"
)

func main() {
	// Serve files from the "videos" directory
	fs := http.FileServer(http.Dir("videos"))

	// Route: /hls/*
	http.Handle("/hls/", corsHandler(http.StripPrefix("/hls/", fs)))

	// Start server
	addr := ":8000"
	log.Println("Server listening on", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}

func corsHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		// Required for HLS playback
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,HEAD,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Range, Accept, Origin, X-Requested-With, Content-Type")
		w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Range")

		// Proper HLS MIME types
		switch ext := filepath.Ext(r.URL.Path); ext {
		case ".m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		case ".ts":
			w.Header().Set("Content-Type", "video/mp2t")
		}

		// Preflight OPTIONS request
		if r.Method == http.MethodOptions {
			w.WriteHeader(204)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// func getListOfVideos() []string {
// 	entries, err := os.ReadDir("./videos")
// 	if err != nil {
// 		return nil
// 	}
// 	var videos []string
// 	for _, entry := range entries {
// 		if entry.IsDir() {
// 			continue
// 		}
// 		videos = append(videos, entry.Name())
// 	}
// 	return videos
// }
