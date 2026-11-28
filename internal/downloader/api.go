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
	"log/slog"
	"time"

	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

type apiData struct {
	Query    string
	ApiUrl   string
	APIKey   string
	Patterns map[string]*regexp.Regexp
}

var apiPatterns = map[string]*regexp.Regexp{
	utils.Apple:      regexp.MustCompile(`(?i)^https?:\/\/music\.apple\.com\/[a-zA-Z-]+\/(?:song\/(?:[^\/]+\/)?\d+|album\/[^\/]+\/\d+(?:\?i=\d+)?|playlist\/[^\/]+\/pl\.[\w.-]+|artist\/[^\/]+\/\d+)(?:\?.*)?$`),
	utils.Spotify:    regexp.MustCompile(`(?i)^(https?://)?([a-z0-9-]+\.)*spotify\.com/(track|playlist|album|artist)/[a-zA-Z0-9]+(\?.*)?$`),
	utils.JioSaavn:   regexp.MustCompile(`(?i)https?:\/\/(?:www\.)?(?:jiosaavn|saavn)\.com\/(?:s\/)?(song|album|playlist|featured)(?:\/[^\/]+)*\/([A-Za-z0-9_,-]+)(?:\/)?(?:\?.*)?$`),
	utils.Deezer:     regexp.MustCompile(`(?i)https?:\/\/(?:www\.)?deezer\.com\/(?:[a-z]{2}\/)?(track|album|playlist)\/(\d+)`),
	utils.SoundCloud: regexp.MustCompile(`(?i)^(https?://)?(www\.)?soundcloud\.com/[a-zA-Z0-9_-]+/(sets/)?[a-zA-Z0-9._-]+(\?.*)?$`),
	utils.Gaana:      regexp.MustCompile(`(?i)https?:\/\/(?:www\.)?gaana\.com\/(song|album|playlist|artist)\/([A-Za-z0-9\-]+)`),
	utils.Tidal:      regexp.MustCompile(`(?i)https?:\/\/(?:www\.|listen\.)?tidal\.com\/(?:browse\/)?(track|album|playlist)\/([a-zA-Z0-9-]+)(?:[\/?].*)?`),
	utils.MXPlayer:   regexp.MustCompile(`(?i)https?:\/\/(?:www\.)?mxplayer\.in\/(?:show|movie)\/.*`),
	utils.Twitch:     regexp.MustCompile(`(?i)https?:\/\/(?:www\.|m\.)?twitch\.tv\/(?:videos|[\w._-]+\/video)\/\d+`),
	utils.TwitchClip: regexp.MustCompile(
		`(?i)https?:\/\/(?:www\.|m\.)?(?:` +
			`twitch\.tv\/clip\/[\w-]+|` +
			`clips\.twitch\.tv\/[\w-]+|` +
			`twitch\.tv\/[\w-]+\/clip\/[\w-]+` +
			`)`,
	),
	utils.Kick:     regexp.MustCompile(`(?i)https?:\/\/(?:www\.)?kick\.com\/[\w._-]+\/videos\/[a-fA-F0-9-]+`),
	utils.KickClip: regexp.MustCompile(`(?i)https?:\/\/(?:www\.)?kick\.com\/[\w._-]+\/clips\/[\w-]+`),
}

func newApiData(query string) *apiData {
	return &apiData{
		Query:    strings.TrimSpace(query),
		ApiUrl:   strings.TrimRight(config.ApiUrl, "/"),
		APIKey:   config.ApiKey,
		Patterns: apiPatterns,
	}
}

func (a *apiData) isValid() bool {
	if a.Query == "" || a.ApiUrl == "" {
		return false
	}
	if !isRiteshAPI() && a.APIKey == "" {
		return false
	}

	for _, pattern := range a.Patterns {
		if pattern.MatchString(a.Query) {
			return true
		}
	}
	return false
}

func (a *apiData) getInfo() (*utils.PlatformTracks, error) {
	if !a.isValid() {
		return nil, errors.New("the provided URL is invalid or the platform is not supported")
	}

	if isRiteshAPI() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		// 1. Check if it is a playlist or album (e.g. Spotify playlist/album/artist)
		if strings.Contains(a.Query, "playlist") || strings.Contains(a.Query, "album") || strings.Contains(a.Query, "artist") {
			if pl, err := GetPlaylistAPI(ctx, a.Query, 50); err == nil && len(pl.Results) > 0 {
				return pl, nil
			}
		}

		// 2. Check details endpoint for single Spotify track
		if strings.Contains(a.Query, "spotify.com/track") {
			if det, err := GetDetailsAPI(ctx, a.Query); err == nil && det.Title != "" {
				link := det.Link
				if link == "" && len(det.VidID) == 11 {
					link = "https://www.youtube.com/watch?v=" + det.VidID
				}
				thumb := det.Thumbnail
				if thumb == "" && len(det.VidID) == 11 {
					thumb = fmt.Sprintf("https://i.ytimg.com/vi/%s/hqdefault.jpg", det.VidID)
				}
				return &utils.PlatformTracks{
					Results: []utils.GetUrlTrack{
						{
							Title:     det.Title,
							Id:        det.VidID,
							Url:       link,
							Thumbnail: thumb,
							Duration:  parseRiteshDuration(det.DurationSec),
							Platform:  utils.Spotify,
						},
					},
				}, nil
			}
		}

		// 3. Multi-platform media extract (100+ platforms)
		if ext, err := ExtractPlatformAPI(ctx, a.Query); err == nil && ext.Success {
			streamURL := ext.DownloadURL
			if streamURL == "" && len(ext.Formats) > 0 {
				streamURL = ext.Formats[0].URL
			}
			id := ext.ID
			if id == "" {
				id = a.Query
			}
			link := a.Query
			if streamURL != "" && config.StreamMode {
				link = streamURL
			}
			return &utils.PlatformTracks{
				Results: []utils.GetUrlTrack{
					{
						Title:     ext.Title,
						Id:        id,
						Url:       link,
						Thumbnail: ext.Thumbnail,
						Duration:  parseRiteshDuration(ext.Duration),
						Platform:  ext.Platform,
					},
				},
			}, nil
		}
	}

	fullURL := fmt.Sprintf("%s/api/get_url?%s", a.ApiUrl, url.Values{"url": {a.Query}}.Encode())
	resp, err := sendRequest(http.MethodGet, fullURL, nil, map[string]string{"X-API-Key": a.APIKey})
	if err != nil {
		return nil, fmt.Errorf("the GetInfo request failed: %w", err)
	}

	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code while fetching info: %s", resp.Status)
	}

	var data utils.PlatformTracks
	if err = json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to decode the GetInfo response: %w", err)
	}
	return &data, nil
}

func (a *apiData) search() (*utils.PlatformTracks, error) {
	if a.isValid() {
		return a.getInfo()
	}

	if isRiteshAPI() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return SearchAPI(ctx, a.Query, 5)
	}

	fullURL := fmt.Sprintf("%s/api/search?%s", a.ApiUrl, url.Values{
		"query": {a.Query},
		"limit": {"5"},
	}.Encode())

	resp, err := sendRequest(http.MethodGet, fullURL, nil, map[string]string{"X-API-Key": a.APIKey})
	if err != nil {
		return nil, fmt.Errorf("the search request failed: %w", err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code during search: %s", resp.Status)
	}

	var data utils.PlatformTracks
	if err = json.NewDecoder(resp.Body).Decode(&data); err != nil {
		slog.Warn("Failed to decode search response", "error", err)
		return nil, fmt.Errorf("failed to decode the search response: %w", err)
	}

	return &data, nil
}

func (a *apiData) getTrack() (*utils.TrackInfo, error) {
	if isRiteshAPI() {
		info, err := a.getInfo()
		if err != nil {
			return nil, err
		}
		if len(info.Results) == 0 {
			return nil, errors.New("no tracks found")
		}
		track := info.Results[0]
		cdnURL := ""
		if strings.HasPrefix(track.Url, "http") && (strings.Contains(track.Url, "/api/platform/stream") || strings.Contains(track.Url, "/downloads") || strings.Contains(track.Url, "/download")) {
			cdnURL = track.Url
		}
		return &utils.TrackInfo{
			Id:       track.Id,
			URL:      track.Url,
			Platform: track.Platform,
			CdnURL:   cdnURL,
		}, nil
	}

	fullURL := fmt.Sprintf("%s/api/track?%s", a.ApiUrl, url.Values{"url": {a.Query}}.Encode())
	resp, err := sendRequest(http.MethodGet, fullURL, nil, map[string]string{"X-API-Key": a.APIKey})
	if err != nil {
		slog.Warn("GetTrack request failed", "error", err)
		return nil, fmt.Errorf("the GetTrack request failed: %w", err)
	}

	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code while fetching the track: %s", resp.Status)
	}

	var data utils.TrackInfo
	if err = json.NewDecoder(resp.Body).Decode(&data); err != nil {
		slog.Warn("Failed to decode the GetTrack response", "error", err)
		return nil, fmt.Errorf("failed to decode the GetTrack response: %w", err)
	}

	return &data, nil
}

func (a *apiData) downloadTrack(info *utils.TrackInfo, video bool) (string, error) {
	// if the track is from YouTube and video:true
	yt := newYouTubeData(a.Query)
	if info.Platform == utils.YouTube && video {
		return yt.downloadTrack(info, video)
	}

	if isRiteshAPI() {
		if config.StreamMode {
			if info.CdnURL != "" {
				return info.CdnURL, nil
			}
			if len(info.Id) == 11 && !strings.ContainsAny(info.Id, " /?:&=#") {
				return GetStreamURL(info.Id, video), nil
			}
			return GetStreamURL(a.Query, video), nil
		}

		if info.CdnURL != "" {
			return downloadFile(info.CdnURL, "", false)
		}
		return DownloadTrackToFile(a.Query, video, 720)
	}

	filePath, err := processDownload(info)
	if err != nil {
		if info.Platform == utils.YouTube {
			return yt.downloadTrack(info, video)
		}
		return "", fmt.Errorf("the download process failed: %w", err)
	}

	if strings.Contains(a.ApiUrl, filePath) {
		return downloadFile(filePath, "", false)
	}

	return filePath, nil
}

// getRecommendations retrieves recommended tracks for a given track URL from the API gateway.
func (a *apiData) getRecommendations(ctx context.Context, rawURL string) (*utils.PlatformTracks, error) {
	if isRiteshAPI() {
		yt := newYouTubeData(rawURL)
		return yt.getRecommendations(ctx, rawURL)
	}

	if a.ApiUrl == "" || a.APIKey == "" {
		return nil, errors.New("API URL or API key is not configured")
	}

	urlToUse := rawURL
	if urlToUse == "" {
		urlToUse = a.Query
	}
	if urlToUse == "" {
		return nil, errors.New("no URL provided for recommendations")
	}

	fullURL := fmt.Sprintf("%s/api/recommendations?%s", a.ApiUrl, url.Values{"url": {urlToUse}}.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", a.APIKey)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("the GetRecommendations request failed: %w", err)
	}

	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code while fetching recommendations: %s", resp.Status)
	}

	var data utils.PlatformTracks
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to decode the GetRecommendations response: %w", err)
	}
	return &data, nil
}
