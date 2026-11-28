/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package downloader

import (
	"ashokshau/tgmusic/internal/config"
	"ashokshau/tgmusic/internal/utils"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	RiteshDefaultBaseURL = "https://api.riteshyt.in"

	// Streaming & Downloading Endpoints
	EndpointMusicBotPlay = "/api/musicbot/play" // GET /api/musicbot/play/{vidid}?api_key={api_key}
	EndpointDownloads    = "/downloads"          // GET /downloads/{api_key}/youtube.com/{vidid}.mp3 or .mp4?quality={quality}
	EndpointDownload     = "/download"           // GET /download?query={query}&dl_type={audio|video}&quality={quality}&api_key={api_key}

	// Discovery, Search & Platform Endpoints
	EndpointSearch          = "/search"               // GET /search?q={query}&limit={limit}&api_key={api_key}
	EndpointDetails         = "/details"              // GET /details?link={link_or_id}&api_key={api_key}
	EndpointPlaylist        = "/playlist"             // GET /playlist?link={playlist_url_or_id}&limit={limit}&api_key={api_key}
	EndpointVideoInfo       = "/video_info"           // GET /video_info?vidid={vidid}&api_key={api_key}
	EndpointPlatformExtract = "/api/platform/extract" // GET /api/platform/extract?url={platform_url}&api_key={api_key}
	EndpointHealth          = "/health"               // GET /health
)

// RiteshSearchResult represents the search response from /search
type RiteshSearchResult struct {
	Result []RiteshSearchItem `json:"result"`
}

type RiteshSearchItem struct {
	Type        string          `json:"type"`
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	Duration    any             `json:"duration"`
	Thumbnails  []RiteshThumb   `json:"thumbnails"`
	Link        string          `json:"link"`
	Channel     RiteshChannel   `json:"channel"`
	ViewCount   RiteshViewCount `json:"viewCount"`
	PublishedAt string          `json:"publishedTime"`
}

type RiteshThumb struct {
	URL string `json:"url"`
}

type RiteshChannel struct {
	Name string `json:"name"`
}

type RiteshViewCount struct {
	Short string `json:"short"`
	Text  string `json:"text"`
}

// RiteshVideoDetails represents the details response from /details
type RiteshVideoDetails struct {
	Title       string `json:"title"`
	DurationMin string `json:"duration_min"`
	DurationSec any    `json:"duration_sec"`
	Thumbnail   string `json:"thumbnail"`
	VidID       string `json:"vidid"`
	Link        string `json:"link"`
}

// RiteshPlaylistResponse represents the playlist response from /playlist
type RiteshPlaylistResponse struct {
	Videos []RiteshVideoDetails `json:"videos"`
}

// RiteshPlatformExtractResponse represents the multi-platform response from /api/platform/extract
type RiteshPlatformExtractResponse struct {
	Success     bool                   `json:"success"`
	Platform    string                 `json:"platform"`
	ID          string                 `json:"id"`
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	Thumbnail   string                 `json:"thumbnail"`
	Duration    any                    `json:"duration"`
	MediaType   string                 `json:"media_type"`
	DownloadURL string                 `json:"download_url"`
	Formats     []RiteshPlatformFormat `json:"formats"`
}

type RiteshPlatformFormat struct {
	FormatID string `json:"format_id"`
	Type     string `json:"type"`
	URL      string `json:"url"`
	Quality  any    `json:"quality"`
	Ext      string `json:"ext"`
	Bitrate  int64  `json:"bitrate"`
	HasAudio bool   `json:"has_audio"`
}

// RiteshHealthResponse represents the health check response from /health
type RiteshHealthResponse struct {
	Status string `json:"status"`
}

// isRiteshAPI checks if the configured API is riteshyt.in
func isRiteshAPI() bool {
	baseURL := strings.ToLower(config.ApiUrl)
	return baseURL == "" || strings.Contains(baseURL, "riteshyt.in")
}

func getRiteshBaseURL() string {
	baseURL := strings.TrimRight(config.ApiUrl, "/")
	if baseURL == "" {
		return RiteshDefaultBaseURL
	}
	return baseURL
}

func getRiteshAPIKey() string {
	return strings.TrimSpace(config.ApiKey)
}

// parseRiteshDuration parses seconds or formatted time ("03:33", "01:23:45", 213) to int32 seconds.
func parseRiteshDuration(val any) int32 {
	if val == nil {
		return 0
	}
	switch v := val.(type) {
	case int:
		return int32(v)
	case int32:
		return v
	case int64:
		return int32(v)
	case float64:
		return int32(v)
	case float32:
		return int32(v)
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return int32(i)
		}
		if f, err := v.Float64(); err == nil {
			return int32(f)
		}
	case string:
		v = strings.TrimSpace(v)
		if v == "" {
			return 0
		}
		if strings.Contains(v, ":") {
			parts := strings.Split(v, ":")
			var total int32
			for _, part := range parts {
				n, _ := strconv.Atoi(strings.TrimSpace(part))
				total = total*60 + int32(n)
			}
			return total
		}
		if sec, err := strconv.ParseFloat(v, 64); err == nil {
			return int32(sec)
		}
	}
	return 0
}

// GetMusicBotStreamURL returns the dedicated high-speed audio redirect stream URL engineered for Telegram music bots.
// Endpoint: GET /api/musicbot/play/{vidid}?api_key={api_key}
func GetMusicBotStreamURL(vidid string) string {
	baseURL := getRiteshBaseURL()
	apiKey := getRiteshAPIKey()
	vidid = strings.TrimSpace(vidid)
	if apiKey != "" {
		return fmt.Sprintf("%s%s/%s?api_key=%s", baseURL, EndpointMusicBotPlay, url.PathEscape(vidid), url.QueryEscape(apiKey))
	}
	return fmt.Sprintf("%s%s/%s", baseURL, EndpointMusicBotPlay, url.PathEscape(vidid))
}

// GetDirectStreamURL returns the direct stream URL for audio (MP3) or video (MP4).
// Endpoint: GET /downloads/{api_key}/youtube.com/{vidid}.mp3
// Endpoint: GET /downloads/{api_key}/youtube.com/{vidid}.mp4?quality={quality}
func GetDirectStreamURL(vidid string, isVideo bool, quality int) string {
	baseURL := getRiteshBaseURL()
	apiKey := getRiteshAPIKey()
	vidid = strings.TrimSpace(vidid)
	if quality <= 0 {
		quality = 720
	}

	keySegment := apiKey
	if keySegment == "" {
		keySegment = "direct"
	}

	if isVideo {
		return fmt.Sprintf("%s%s/%s/youtube.com/%s.mp4?quality=%d", baseURL, EndpointDownloads, url.PathEscape(keySegment), url.PathEscape(vidid), quality)
	}
	return fmt.Sprintf("%s%s/%s/youtube.com/%s.mp3", baseURL, EndpointDownloads, url.PathEscape(keySegment), url.PathEscape(vidid))
}

// GetDownloadStreamURL returns the stream and download URL with HTTP 206 Range headers support.
// Endpoint: GET /download?query={query}&dl_type={audio|video}&quality={quality}&api_key={api_key}
func GetDownloadStreamURL(query string, isVideo bool, quality int) string {
	baseURL := getRiteshBaseURL()
	apiKey := getRiteshAPIKey()
	dlType := "audio"
	if isVideo {
		dlType = "video"
	}
	if quality <= 0 {
		quality = 720
	}

	params := url.Values{
		"query":   {strings.TrimSpace(query)},
		"dl_type": {dlType},
	}
	if isVideo {
		params.Set("quality", strconv.Itoa(quality))
	}
	if apiKey != "" {
		params.Set("api_key", apiKey)
	}

	return fmt.Sprintf("%s%s?%s", baseURL, EndpointDownload, params.Encode())
}

// GetStreamURL resolves the best streaming URL based on the configured STREAM_TYPE ("musicbot", "direct", "download").
func GetStreamURL(queryOrVidID string, isVideo bool) string {
	vidID := extractVideoID(queryOrVidID)
	if vidID == "" {
		vidID = strings.TrimSpace(queryOrVidID)
	}

	isYouTubeID := len(vidID) == 11 && !strings.ContainsAny(vidID, " /?:&=#")

	switch config.StreamType {
	case "direct":
		if isYouTubeID {
			return GetDirectStreamURL(vidID, isVideo, 720)
		}
		return GetDownloadStreamURL(queryOrVidID, isVideo, 720)

	case "download":
		return GetDownloadStreamURL(queryOrVidID, isVideo, 720)

	default: // "musicbot"
		if !isVideo && isYouTubeID {
			return GetMusicBotStreamURL(vidID)
		}
		if isVideo && isYouTubeID {
			return GetDirectStreamURL(vidID, true, 720)
		}
		return GetDownloadStreamURL(queryOrVidID, isVideo, 720)
	}
}

// GetDownloadURL returns the media download endpoint URL.
// Endpoint: GET /download?query={query}&dl_type={audio|video}&quality={720}&api_key={api_key}
func GetDownloadURL(query string, isVideo bool, quality int) string {
	return GetDownloadStreamURL(query, isVideo, quality)
}

// DownloadTrackToFile downloads the media from the API to config.DownloadsDir and returns the local file path.
func DownloadTrackToFile(queryOrVidID string, isVideo bool, quality int) (string, error) {
	dlURL := GetDownloadURL(queryOrVidID, isVideo, quality)

	vidID := extractVideoID(queryOrVidID)
	if vidID == "" {
		vidID = strings.TrimSpace(queryOrVidID)
	}

	ext := ".mp3"
	if isVideo {
		ext = ".mp4"
	}

	var targetPath string
	if len(vidID) == 11 && !strings.ContainsAny(vidID, " /?:&=#") {
		targetPath = filepath.Join(config.DownloadsDir, fmt.Sprintf("%s%s", vidID, ext))
	}

	return downloadFile(dlURL, targetPath, false)
}

func sendRiteshRequest(ctx context.Context, method, endpointWithQuery string, body io.Reader) (*http.Response, error) {
	baseURL := getRiteshBaseURL()
	fullURL := baseURL + endpointWithQuery

	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json, */*")

	apiKey := getRiteshAPIKey()
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ritesh API request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("ritesh API error status=%d: %s", resp.StatusCode, strings.TrimSpace(string(respBytes)))
	}

	return resp, nil
}

// SearchAPI searches tracks on YouTube using InnerTube with yt-dlp fallback.
// Endpoint: GET /search?q={query}&limit={limit}&api_key={api_key}
func SearchAPI(ctx context.Context, query string, limit int) (*utils.PlatformTracks, error) {
	if limit <= 0 {
		limit = 5
	}
	params := url.Values{
		"q":     {strings.TrimSpace(query)},
		"limit": {strconv.Itoa(limit)},
	}
	if apiKey := getRiteshAPIKey(); apiKey != "" {
		params.Set("api_key", apiKey)
	}

	resp, err := sendRiteshRequest(ctx, http.MethodGet, fmt.Sprintf("%s?%s", EndpointSearch, params.Encode()), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var searchResp RiteshSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, fmt.Errorf("failed to decode search response: %w", err)
	}

	var results []utils.GetUrlTrack
	for _, item := range searchResp.Result {
		var thumb string
		if len(item.Thumbnails) > 0 {
			thumb = item.Thumbnails[len(item.Thumbnails)-1].URL
		}
		if thumb == "" && len(item.ID) == 11 {
			thumb = fmt.Sprintf("https://i.ytimg.com/vi/%s/hqdefault.jpg", item.ID)
		}

		link := item.Link
		if link == "" && len(item.ID) == 11 {
			link = "https://www.youtube.com/watch?v=" + item.ID
		}

		results = append(results, utils.GetUrlTrack{
			Title:     item.Title,
			Id:        item.ID,
			Url:       link,
			Thumbnail: thumb,
			Duration:  parseRiteshDuration(item.Duration),
			Channel:   item.Channel.Name,
			Views:     item.ViewCount.Short,
			Platform:  utils.YouTube,
		})
	}

	return &utils.PlatformTracks{Results: results}, nil
}

// GetDetailsAPI retrieves single video or Spotify track metadata.
// Endpoint: GET /details?link={link_or_id}&api_key={api_key}
func GetDetailsAPI(ctx context.Context, linkOrID string) (*RiteshVideoDetails, error) {
	params := url.Values{
		"link": {strings.TrimSpace(linkOrID)},
	}
	if apiKey := getRiteshAPIKey(); apiKey != "" {
		params.Set("api_key", apiKey)
	}

	resp, err := sendRiteshRequest(ctx, http.MethodGet, fmt.Sprintf("%s?%s", EndpointDetails, params.Encode()), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var details RiteshVideoDetails
	if err := json.NewDecoder(resp.Body).Decode(&details); err != nil {
		return nil, fmt.Errorf("failed to decode details response: %w", err)
	}

	return &details, nil
}

// GetPlaylistAPI extracts videos and tracks from YouTube Playlists or Spotify Playlists/Albums/Artists.
// Endpoint: GET /playlist?link={playlist_url_or_id}&limit={limit}&api_key={api_key}
func GetPlaylistAPI(ctx context.Context, linkOrID string, limit int) (*utils.PlatformTracks, error) {
	if limit <= 0 {
		limit = 50
	}
	params := url.Values{
		"link":  {strings.TrimSpace(linkOrID)},
		"limit": {strconv.Itoa(limit)},
	}
	if apiKey := getRiteshAPIKey(); apiKey != "" {
		params.Set("api_key", apiKey)
	}

	resp, err := sendRiteshRequest(ctx, http.MethodGet, fmt.Sprintf("%s?%s", EndpointPlaylist, params.Encode()), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var playlistResp RiteshPlaylistResponse
	if err := json.NewDecoder(resp.Body).Decode(&playlistResp); err != nil {
		return nil, fmt.Errorf("failed to decode playlist response: %w", err)
	}

	var results []utils.GetUrlTrack
	for _, v := range playlistResp.Videos {
		link := v.Link
		if link == "" && len(v.VidID) == 11 {
			link = "https://www.youtube.com/watch?v=" + v.VidID
		}
		thumb := v.Thumbnail
		if thumb == "" && len(v.VidID) == 11 {
			thumb = fmt.Sprintf("https://i.ytimg.com/vi/%s/hqdefault.jpg", v.VidID)
		}

		results = append(results, utils.GetUrlTrack{
			Title:     v.Title,
			Id:        v.VidID,
			Url:       link,
			Thumbnail: thumb,
			Duration:  parseRiteshDuration(v.DurationSec),
			Platform:  utils.YouTube,
		})
	}

	return &utils.PlatformTracks{Results: results}, nil
}

// ExtractPlatformAPI extracts rich metadata, download URLs, and stream formats from 100+ social and streaming platforms.
// Endpoint: GET /api/platform/extract?url={platform_url}&api_key={api_key}
func ExtractPlatformAPI(ctx context.Context, targetURL string) (*RiteshPlatformExtractResponse, error) {
	params := url.Values{
		"url": {strings.TrimSpace(targetURL)},
	}
	if apiKey := getRiteshAPIKey(); apiKey != "" {
		params.Set("api_key", apiKey)
	}

	resp, err := sendRiteshRequest(ctx, http.MethodGet, fmt.Sprintf("%s?%s", EndpointPlatformExtract, params.Encode()), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var extractResp RiteshPlatformExtractResponse
	if err := json.NewDecoder(resp.Body).Decode(&extractResp); err != nil {
		return nil, fmt.Errorf("failed to decode platform extract response: %w", err)
	}

	baseURL := getRiteshBaseURL()
	if strings.HasPrefix(extractResp.DownloadURL, "/") {
		extractResp.DownloadURL = baseURL + extractResp.DownloadURL
	}
	for i := range extractResp.Formats {
		if strings.HasPrefix(extractResp.Formats[i].URL, "/") {
			extractResp.Formats[i].URL = baseURL + extractResp.Formats[i].URL
		}
	}

	return &extractResp, nil
}

// GetVideoInfoAPI retrieves technical format lists and media metadata for YouTube video IDs.
// Endpoint: GET /video_info?vidid={vidid}&api_key={api_key}
func GetVideoInfoAPI(ctx context.Context, vidid string) (map[string]any, error) {
	params := url.Values{
		"vidid": {strings.TrimSpace(vidid)},
	}
	if apiKey := getRiteshAPIKey(); apiKey != "" {
		params.Set("api_key", apiKey)
	}

	resp, err := sendRiteshRequest(ctx, http.MethodGet, fmt.Sprintf("%s?%s", EndpointVideoInfo, params.Encode()), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode video info response: %w", err)
	}

	return result, nil
}

// CheckHealthAPI checks API service status and health uptime.
// Endpoint: GET /health
func CheckHealthAPI(ctx context.Context) (bool, error) {
	resp, err := sendRiteshRequest(ctx, http.MethodGet, EndpointHealth, nil)
	if err != nil {
		slog.Warn("Health check failed", "error", err)
		return false, err
	}
	defer resp.Body.Close()

	var health RiteshHealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return false, err
	}

	return strings.EqualFold(health.Status, "ok"), nil
}
