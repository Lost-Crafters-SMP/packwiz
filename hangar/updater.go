package hangar

import (
	"errors"
	"fmt"

	"github.com/mitchellh/mapstructure"
	"github.com/packwiz/packwiz/core"
)

type hangarUpdateData struct {
	ProjectID string `mapstructure:"project-id"`
	VersionID string `mapstructure:"version-id"`
	Platform  string `mapstructure:"platform"`
}

func (u hangarUpdateData) ToMap() (map[string]interface{}, error) {
	newMap := make(map[string]interface{})
	err := mapstructure.Decode(u, &newMap)
	return newMap, err
}

type hangarUpdater struct{}

func (u hangarUpdater) ParseUpdate(updateUnparsed map[string]interface{}) (interface{}, error) {
	var updateData hangarUpdateData
	err := mapstructure.Decode(updateUnparsed, &updateData)
	return updateData, err
}

type cachedStateStore struct {
	ProjectID string
	Version   Version
	Platform  string
}

func (u hangarUpdater) CheckUpdate(mods []*core.Mod, pack core.Pack) ([]core.UpdateCheck, error) {
	results := make([]core.UpdateCheck, len(mods))

	for i, mod := range mods {
		rawData, ok := mod.GetParsedUpdateData("hangar")
		if !ok {
			results[i] = core.UpdateCheck{Error: errors.New("failed to parse update metadata")}
			continue
		}

		data := rawData.(hangarUpdateData)

		// Resolve project ID - get project first to ensure we have the correct format
		// This handles both old format (namespace/slug) and new format (slug or numeric ID)
		project, err := hgDefaultClient.getProject(data.ProjectID)
		if err != nil {
			results[i] = core.UpdateCheck{Error: fmt.Errorf("failed to fetch project: %v", err)}
			continue
		}

		// Use slug for all API calls (unique and what API requires)
		projectID := project.Namespace.Slug
		if projectID == "" {
			// Fallback to numeric ID if slug is not available
			projectID = fmt.Sprintf("%d", project.ID)
		}

		// Get all versions (similar to install code for better platform/MC version filtering)
		versionsResult, err := hgDefaultClient.getVersions(projectID, 25, 0)
		if err != nil {
			results[i] = core.UpdateCheck{Error: fmt.Errorf("failed to fetch versions: %v", err)}
			continue
		}

		if len(versionsResult.Result) == 0 {
			results[i] = core.UpdateCheck{UpdateAvailable: false}
			continue
		}

		// Find the current version to compare against
		var currentVersion *Version
		for _, v := range versionsResult.Result {
			if v.Name == data.VersionID || fmt.Sprintf("%d", v.ID) == data.VersionID {
				currentVersion = &v
				break
			}
		}

		// Filter versions by platform (must have download for the configured platform)
		var compatibleVersions []Version
		for _, v := range versionsResult.Result {
			if _, ok := v.Downloads[data.Platform]; ok {
				compatibleVersions = append(compatibleVersions, v)
			}
		}

		if len(compatibleVersions) == 0 {
			// No versions with download for configured platform
			results[i] = core.UpdateCheck{UpdateAvailable: false}
			continue
		}

		// Find the latest version (first in list should be newest, but verify it's newer than current)
		latestVersion := compatibleVersions[0]

		// If we found the current version, check if latest is actually newer
		if currentVersion != nil {
			// Check if latest is the same as current
			if latestVersion.Name == data.VersionID || fmt.Sprintf("%d", latestVersion.ID) == data.VersionID {
				results[i] = core.UpdateCheck{UpdateAvailable: false}
				continue
			}
			// If current version is in the list, make sure latest is actually newer (earlier in list)
			for _, v := range compatibleVersions {
				if v.Name == data.VersionID || fmt.Sprintf("%d", v.ID) == data.VersionID {
					// Current version found - if it's not first, there's an update
					if v.Name != latestVersion.Name && fmt.Sprintf("%d", v.ID) != fmt.Sprintf("%d", latestVersion.ID) {
						// Latest is different, so there's an update
						break
					} else {
						// Current is already latest
						results[i] = core.UpdateCheck{UpdateAvailable: false}
						continue
					}
				}
			}
		}

		// Check if platform download exists (should already be filtered, but double-check)
		platformDownload, ok := latestVersion.Downloads[data.Platform]
		if !ok {
			// No download for configured platform - silently skip (no update available)
			results[i] = core.UpdateCheck{UpdateAvailable: false}
			continue
		}

		if platformDownload.FileInfo.Name == "" {
			results[i] = core.UpdateCheck{Error: errors.New("new version doesn't have file info")}
			continue
		}

		results[i] = core.UpdateCheck{
			UpdateAvailable: true,
			UpdateString:    mod.FileName + " -> " + platformDownload.FileInfo.Name,
			CachedState:     cachedStateStore{data.ProjectID, latestVersion, data.Platform},
		}
	}

	return results, nil
}

func (u hangarUpdater) DoUpdate(mods []*core.Mod, cachedState []interface{}) error {
	for i, mod := range mods {
		modState := cachedState[i].(cachedStateStore)
		version := modState.Version

		platformDownload, ok := version.Downloads[modState.Platform]
		if !ok {
			return fmt.Errorf("version doesn't have download for platform %s", modState.Platform)
		}

		// Prefer downloadUrl over externalUrl
		downloadURL := platformDownload.DownloadURL
		if downloadURL == "" {
			downloadURL = platformDownload.ExternalURL
		}
		if downloadURL == "" {
			return fmt.Errorf("no download URL available for platform %s", modState.Platform)
		}

		mod.FileName = platformDownload.FileInfo.Name
		mod.Download = core.ModDownload{
			URL:        downloadURL,
			HashFormat: "sha256",
			Hash:       platformDownload.FileInfo.SHA256Hash,
		}

		// Update stored version ID
		if mod.Update["hangar"] == nil {
			mod.Update["hangar"] = make(map[string]interface{})
		}
		mod.Update["hangar"]["version-id"] = version.Name
	}

	return nil
}

