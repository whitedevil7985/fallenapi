package downloader

import (
	"ashokshau/tgmusic/internal/config"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestParseRiteshDuration(t *testing.T) {
	tests := []struct {
		input    any
		expected int32
	}{
		{213, 213},
		{int32(120), 120},
		{int64(450), 450},
		{float64(75.5), 75},
		{"03:33", 213},
		{"01:00:00", 3600},
		{"45", 45},
		{json.Number("180"), 180},
		{nil, 0},
		{"", 0},
	}

	for _, tc := range tests {
		got := parseRiteshDuration(tc.input)
		if got != tc.expected {
			t.Errorf("parseRiteshDuration(%v) = %d; expected %d", tc.input, got, tc.expected)
		}
	}
}

func TestStreamURLGeneration(t *testing.T) {
	origURL := config.ApiUrl
	origKey := config.ApiKey
	origType := config.StreamType
	defer func() {
		config.ApiUrl = origURL
		config.ApiKey = origKey
		config.StreamType = origType
	}()

	config.ApiUrl = "https://api.riteshyt.in"
	config.ApiKey = "testkey123"

	// 1. MusicBot Stream URL
	musicbotURL := GetMusicBotStreamURL("dQw4w9WgXcQ")
	expectedMusicBot := "https://api.riteshyt.in/api/musicbot/play/dQw4w9WgXcQ?api_key=testkey123"
	if musicbotURL != expectedMusicBot {
		t.Errorf("GetMusicBotStreamURL mismatch: got %s, expected %s", musicbotURL, expectedMusicBot)
	}

	// 2. Direct Stream Audio URL
	directAudioURL := GetDirectStreamURL("dQw4w9WgXcQ", false, 0)
	expectedDirectAudio := "https://api.riteshyt.in/downloads/testkey123/youtube.com/dQw4w9WgXcQ.mp3"
	if directAudioURL != expectedDirectAudio {
		t.Errorf("GetDirectStreamURL audio mismatch: got %s, expected %s", directAudioURL, expectedDirectAudio)
	}

	// 3. Direct Stream Video URL
	directVideoURL := GetDirectStreamURL("dQw4w9WgXcQ", true, 720)
	expectedDirectVideo := "https://api.riteshyt.in/downloads/testkey123/youtube.com/dQw4w9WgXcQ.mp4?quality=720"
	if directVideoURL != expectedDirectVideo {
		t.Errorf("GetDirectStreamURL video mismatch: got %s, expected %s", directVideoURL, expectedDirectVideo)
	}

	// 4. Download Stream URL
	dlStreamURL := GetDownloadStreamURL("dQw4w9WgXcQ", false, 720)
	if dlStreamURL == "" || dlStreamURL != "https://api.riteshyt.in/download?api_key=testkey123&dl_type=audio&query=dQw4w9WgXcQ" {
		t.Errorf("GetDownloadStreamURL mismatch: got %s", dlStreamURL)
	}

	// 5. GetStreamURL with different stream types
	config.StreamType = "musicbot"
	if got := GetStreamURL("dQw4w9WgXcQ", false); got != expectedMusicBot {
		t.Errorf("GetStreamURL musicbot audio mismatch: got %s", got)
	}
	if got := GetStreamURL("dQw4w9WgXcQ", true); got != expectedDirectVideo {
		t.Errorf("GetStreamURL musicbot video mismatch: got %s", got)
	}

	config.StreamType = "direct"
	if got := GetStreamURL("dQw4w9WgXcQ", false); got != expectedDirectAudio {
		t.Errorf("GetStreamURL direct audio mismatch: got %s", got)
	}

	config.StreamType = "download"
	if got := GetStreamURL("dQw4w9WgXcQ", false); got != dlStreamURL {
		t.Errorf("GetStreamURL download mismatch: got %s", got)
	}
}

func TestMockRiteshAPIs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case EndpointHealth:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))

		case EndpointSearch:
			q := r.URL.Query().Get("q")
			if q != "testquery" {
				http.Error(w, "bad query", http.StatusBadRequest)
				return
			}
			resp := RiteshSearchResult{
				Result: []RiteshSearchItem{
					{
						ID:       "testvid1234",
						Title:    "Test Video Title",
						Duration: 200,
						Thumbnails: []RiteshThumb{
							{URL: "https://i.ytimg.com/vi/testvid1234/default.jpg"},
						},
						Link: "https://youtube.com/watch?v=testvid1234",
						Channel: RiteshChannel{
							Name: "Test Channel",
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)

		case EndpointDetails:
			link := r.URL.Query().Get("link")
			resp := RiteshVideoDetails{
				Title:       "Details Title",
				VidID:       link,
				DurationSec: 180,
				Link:        "https://youtu.be/" + link,
				Thumbnail:   "https://thumb.url",
			}
			_ = json.NewEncoder(w).Encode(resp)

		case EndpointPlaylist:
			resp := RiteshPlaylistResponse{
				Videos: []RiteshVideoDetails{
					{
						Title:       "Playlist Item 1",
						VidID:       "vid11111111",
						DurationSec: 150,
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)

		case EndpointPlatformExtract:
			resp := RiteshPlatformExtractResponse{
				Success:     true,
				Platform:    "spotify",
				ID:          "spotify123",
				Title:       "Spotify Track",
				DownloadURL: "/api/platform/stream?id=spotify123",
				Duration:    210,
			}
			_ = json.NewEncoder(w).Encode(resp)

		case EndpointVideoInfo:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"title":  "Video Info Title",
				"status": "success",
			})

		case EndpointDownload:
			w.Header().Set("Content-Type", "audio/mpeg")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("fake audio content"))

		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	origURL := config.ApiUrl
	origKey := config.ApiKey
	origDir := config.DownloadsDir
	defer func() {
		config.ApiUrl = origURL
		config.ApiKey = origKey
		config.DownloadsDir = origDir
	}()

	config.ApiUrl = server.URL
	config.ApiKey = "test_key"
	tmpDir := t.TempDir()
	config.DownloadsDir = tmpDir

	ctx := context.Background()

	// 1. Health API
	ok, err := CheckHealthAPI(ctx)
	if err != nil || !ok {
		t.Fatalf("CheckHealthAPI failed: %v", err)
	}

	// 2. Search API
	searchTracks, err := SearchAPI(ctx, "testquery", 5)
	if err != nil {
		t.Fatalf("SearchAPI failed: %v", err)
	}
	if len(searchTracks.Results) != 1 || searchTracks.Results[0].Id != "testvid1234" {
		t.Errorf("SearchAPI unexpected result: %+v", searchTracks)
	}

	// 3. Details API
	details, err := GetDetailsAPI(ctx, "vid12345678")
	if err != nil {
		t.Fatalf("GetDetailsAPI failed: %v", err)
	}
	if details.Title != "Details Title" {
		t.Errorf("GetDetailsAPI unexpected title: %s", details.Title)
	}

	// 4. Playlist API
	playlistTracks, err := GetPlaylistAPI(ctx, "PL12345", 10)
	if err != nil {
		t.Fatalf("GetPlaylistAPI failed: %v", err)
	}
	if len(playlistTracks.Results) != 1 || playlistTracks.Results[0].Id != "vid11111111" {
		t.Errorf("GetPlaylistAPI unexpected result: %+v", playlistTracks)
	}

	// 5. ExtractPlatform API
	extract, err := ExtractPlatformAPI(ctx, "https://open.spotify.com/track/123")
	if err != nil {
		t.Fatalf("ExtractPlatformAPI failed: %v", err)
	}
	if !extract.Success || extract.Title != "Spotify Track" {
		t.Errorf("ExtractPlatformAPI unexpected result: %+v", extract)
	}
	if extract.DownloadURL != fmt.Sprintf("%s/api/platform/stream?id=spotify123", server.URL) {
		t.Errorf("ExtractPlatformAPI URL prefix mismatch: %s", extract.DownloadURL)
	}

	// 6. Video Info API
	vinfo, err := GetVideoInfoAPI(ctx, "testvid1234")
	if err != nil {
		t.Fatalf("GetVideoInfoAPI failed: %v", err)
	}
	if vinfo["title"] != "Video Info Title" {
		t.Errorf("GetVideoInfoAPI unexpected title: %v", vinfo["title"])
	}

	// 7. DownloadTrackToFile
	filePath, err := DownloadTrackToFile("testvid1234", false, 720)
	if err != nil {
		t.Fatalf("DownloadTrackToFile failed: %v", err)
	}
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Errorf("Downloaded file does not exist at: %s", filePath)
	}
	expectedFile := filepath.Join(tmpDir, "testvid1234.mp3")
	if filePath != expectedFile {
		t.Errorf("DownloadTrackToFile path mismatch: got %s, expected %s", filePath, expectedFile)
	}
}
