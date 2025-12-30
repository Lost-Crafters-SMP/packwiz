package hangar

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/packwiz/packwiz/core"
)

const hangarApiServer = "hangar.papermc.io"
const hangarApiBase = "https://" + hangarApiServer + "/api/v1"

type hangarApiClient struct {
	httpClient *http.Client
}

func (c *hangarApiClient) makeGet(endpoint string) (*http.Response, error) {
	req, err := http.NewRequest("GET", hangarApiBase+endpoint, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", core.UserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("invalid response status: %v, body: %s", resp.Status, string(body))
	}

	return resp, nil
}

// API Response structs

type Project struct {
	ID                int                    `json:"id"`
	Name              string                 `json:"name"`
	Slug              string                 `json:"slug,omitempty"`
	Namespace         ProjectNamespace       `json:"namespace"`
	Description       string                 `json:"description"`
	Category          string                 `json:"category"`
	AvatarURL         string                 `json:"avatarUrl"`
	CreatedAt         string                 `json:"createdAt"`
	LastUpdated       string                 `json:"lastUpdated"`
	SupportedPlatforms map[string][]string  `json:"supportedPlatforms"`
	Visibility        string                 `json:"visibility"`
}

type ProjectNamespace struct {
	Owner string `json:"owner"`
	Slug  string `json:"slug"`
}

type ProjectSearchResult struct {
	Pagination Pagination      `json:"pagination"`
	Result     []ProjectResult `json:"result"`
}

type Pagination struct {
	Count  int `json:"count"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

type ProjectResult struct {
	ID                int                    `json:"id"`
	Name              string                 `json:"name"`
	Namespace         ProjectNamespace       `json:"namespace"`
	Description       string                 `json:"description"`
	Category          string                 `json:"category"`
	AvatarURL         string                 `json:"avatarUrl"`
	CreatedAt         string                 `json:"createdAt"`
	LastUpdated       string                 `json:"lastUpdated"`
	SupportedPlatforms map[string][]string  `json:"supportedPlatforms"`
	Visibility        string                 `json:"visibility"`
}

type Version struct {
	ID                    int                           `json:"id"`
	Name                  string                        `json:"name"`
	ProjectID             int                           `json:"projectId"`
	Author                string                        `json:"author"`
	CreatedAt             string                        `json:"createdAt"`
	Description           string                        `json:"description"`
	Channel               VersionChannel                `json:"channel"`
	Downloads             map[string]PlatformDownload   `json:"downloads"`
	PluginDependencies    map[string][]PluginDependency `json:"pluginDependencies"`
	PlatformDependencies  map[string][]string           `json:"platformDependencies"`
	Visibility            string                        `json:"visibility"`
	ReviewState           string                        `json:"reviewState"`
	PinnedStatus          string                        `json:"pinnedStatus"`
}

type VersionChannel struct {
	Name        string   `json:"name"`
	Color       string   `json:"color"`
	CreatedAt   string   `json:"createdAt"`
	Description string   `json:"description"`
	Flags       []string `json:"flags"`
}

type PlatformDownload struct {
	DownloadURL string   `json:"downloadUrl"`
	ExternalURL string   `json:"externalUrl"`
	FileInfo    FileInfo `json:"fileInfo"`
}

type FileInfo struct {
	Name      string `json:"name"`
	SHA256Hash string `json:"sha256Hash"`
	SizeBytes  int64  `json:"sizeBytes"`
}

type PluginDependency struct {
	Name        string `json:"name"`
	ProjectID   *int   `json:"projectId"`
	ExternalURL string `json:"externalUrl"`
	Platform    string `json:"platform"`
	Required    bool   `json:"required"`
}

type VersionListResult struct {
	Pagination Pagination `json:"pagination"`
	Result     []Version  `json:"result"`
}

type PlatformVersions struct {
	Platform int      `json:"platform"`
	Versions []string `json:"versions"`
}

// API request functions

func (c *hangarApiClient) searchProjects(query string, platform string, gameVersion string, limit int, offset int) (ProjectSearchResult, error) {
	var result ProjectSearchResult

	params := url.Values{}
	if query != "" {
		params.Set("query", query)
		// Prioritize exact matches (default is true, but being explicit)
		params.Set("prioritizeExactMatch", "true")
	}
	if platform != "" {
		params.Set("platform", platform)
	}
	if gameVersion != "" {
		params.Set("version", gameVersion)
	}
	params.Set("limit", strconv.Itoa(limit))
	params.Set("offset", strconv.Itoa(offset))

	endpoint := "/projects?" + params.Encode()
	resp, err := c.makeGet(endpoint)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()

	err = json.NewDecoder(resp.Body).Decode(&result)
	return result, err
}

func (c *hangarApiClient) getProject(slugOrId string) (Project, error) {
	var project Project

	// Try the non-deprecated format first (using slugOrId directly)
	endpoint := "/projects/" + url.PathEscape(slugOrId)
	resp, err := c.makeGet(endpoint)
	if err != nil {
		// If that fails and slugOrId contains a slash, try deprecated format
		if strings.Contains(slugOrId, "/") {
			parts := strings.SplitN(slugOrId, "/", 2)
			if len(parts) == 2 {
				endpoint = "/projects/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1])
				resp, err = c.makeGet(endpoint)
			}
		}
		if err != nil {
			return project, err
		}
	}
	defer resp.Body.Close()

	err = json.NewDecoder(resp.Body).Decode(&project)
	return project, err
}

func (c *hangarApiClient) getVersions(slugOrId string, limit int, offset int) (VersionListResult, error) {
	var result VersionListResult

	params := url.Values{}
	params.Set("limit", strconv.Itoa(limit))
	params.Set("offset", strconv.Itoa(offset))

	// Try the non-deprecated format first (using slugOrId directly)
	endpoint := "/projects/" + url.PathEscape(slugOrId) + "/versions?" + params.Encode()
	resp, err := c.makeGet(endpoint)
	if err != nil {
		// If that fails and slugOrId contains a slash, try deprecated format
		if strings.Contains(slugOrId, "/") {
			parts := strings.SplitN(slugOrId, "/", 2)
			if len(parts) == 2 {
				endpoint = "/projects/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]) + "/versions?" + params.Encode()
				resp, err = c.makeGet(endpoint)
			}
		}
		if err != nil {
			return result, err
		}
	}
	defer resp.Body.Close()

	err = json.NewDecoder(resp.Body).Decode(&result)
	return result, err
}

func (c *hangarApiClient) getVersion(slugOrId string, nameOrId string) (Version, error) {
	var version Version

	// Try the non-deprecated format first (using slugOrId directly)
	endpoint := "/projects/" + url.PathEscape(slugOrId) + "/versions/" + url.PathEscape(nameOrId)
	resp, err := c.makeGet(endpoint)
	if err != nil {
		// If that fails and slugOrId contains a slash, try deprecated format
		if strings.Contains(slugOrId, "/") {
			parts := strings.SplitN(slugOrId, "/", 2)
			if len(parts) == 2 {
				endpoint = "/projects/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]) + "/versions/" + url.PathEscape(nameOrId)
				resp, err = c.makeGet(endpoint)
			}
		}
		if err != nil {
			return version, err
		}
	}
	defer resp.Body.Close()

	err = json.NewDecoder(resp.Body).Decode(&version)
	return version, err
}

func (c *hangarApiClient) getLatestVersion(slugOrId string, channel string) (string, error) {
	params := url.Values{}
	if channel != "" {
		params.Set("channel", channel)
	}

	// The /latest endpoint returns plain text, not JSON, so we need a custom request
	makeGetText := func(endpoint string) (*http.Response, error) {
		req, err := http.NewRequest("GET", hangarApiBase+endpoint, nil)
		if err != nil {
			return nil, err
		}

		req.Header.Set("User-Agent", core.UserAgent)
		req.Header.Set("Accept", "text/plain")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("invalid response status: %v, body: %s", resp.Status, string(body))
		}

		return resp, nil
	}

	// Try the non-deprecated format first (using slugOrId directly)
	endpoint := "/projects/" + url.PathEscape(slugOrId) + "/latest"
	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}

	resp, err := makeGetText(endpoint)
	if err != nil {
		// If that fails and slugOrId contains a slash, try deprecated format
		if strings.Contains(slugOrId, "/") {
			parts := strings.SplitN(slugOrId, "/", 2)
			if len(parts) == 2 {
				endpoint = "/projects/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]) + "/latest"
				if len(params) > 0 {
					endpoint += "?" + params.Encode()
				}
				resp, err = makeGetText(endpoint)
			}
		}
		if err != nil {
			return "", err
		}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(body), nil
}

func (c *hangarApiClient) getPlatformVersions(platform string) ([]PlatformVersions, error) {
	var result []PlatformVersions

	endpoint := "/platforms/" + url.PathEscape(platform) + "/versions"
	resp, err := c.makeGet(endpoint)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()

	err = json.NewDecoder(resp.Body).Decode(&result)
	return result, err
}

