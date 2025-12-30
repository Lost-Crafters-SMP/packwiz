package maven

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mitchellh/mapstructure"
	"github.com/packwiz/packwiz/core"
	"github.com/unascribed/FlexVer/go/flexver"
)

type mavenUpdateData struct {
	Repository string `mapstructure:"repository"`
	GroupID    string `mapstructure:"group-id"`
	ArtifactID string `mapstructure:"artifact-id"`
	Version    string `mapstructure:"version"`
	Classifier string `mapstructure:"classifier"`
	Extension  string `mapstructure:"extension"`
}

func (u mavenUpdateData) ToMap() (map[string]interface{}, error) {
	newMap := make(map[string]interface{})
	err := mapstructure.Decode(u, &newMap)
	if err != nil {
		return nil, err
	}

	// Omit empty optional fields for cleaner TOML
	if classifier, ok := newMap["classifier"].(string); ok && classifier == "" {
		delete(newMap, "classifier")
	}
	if extension, ok := newMap["extension"].(string); ok && extension == "jar" {
		// Omit extension if it's the default "jar"
		delete(newMap, "extension")
	}

	return newMap, nil
}

type mavenUpdater struct{}

func (u mavenUpdater) ParseUpdate(updateUnparsed map[string]interface{}) (interface{}, error) {
	var updateData mavenUpdateData
	err := mapstructure.Decode(updateUnparsed, &updateData)
	return updateData, err
}

type cachedStateStore struct {
	Repository string
	GroupID    string
	ArtifactID string
	Version    string
	Classifier string
	Extension  string
	Filename   string
	URL        string
	Hash       string
}

func (u mavenUpdater) CheckUpdate(mods []*core.Mod, pack core.Pack) ([]core.UpdateCheck, error) {
	results := make([]core.UpdateCheck, len(mods))

	for i, mod := range mods {
		rawData, ok := mod.GetParsedUpdateData("maven")
		if !ok {
			results[i] = core.UpdateCheck{Error: errors.New("failed to parse update metadata")}
			continue
		}

		data := rawData.(mavenUpdateData)

		// Check if it's a snapshot version
		if isSnapshotVersion(data.Version) {
			// Handle snapshot updates
			updateAvailable, newVersion, err := checkSnapshotUpdate(data)
			if err != nil {
				// Check if it's a cleanup error
				if strings.Contains(err.Error(), "cleaned up") {
					results[i] = core.UpdateCheck{
						Error: fmt.Errorf("snapshot version %s appears to have been cleaned up by the repository. Consider using a release version or a specific timestamped version", data.Version),
					}
				} else {
					results[i] = core.UpdateCheck{Error: fmt.Errorf("failed to check snapshot update: %v", err)}
				}
				continue
			}

			if !updateAvailable {
				results[i] = core.UpdateCheck{UpdateAvailable: false}
				continue
			}

			// Build new filename and URL
			extension := data.Extension
			if extension == "" {
				extension = "jar"
			}

			filename := data.ArtifactID + "-" + newVersion
			if data.Classifier != "" {
				filename += "-" + data.Classifier
			}
			filename += "." + extension

			// For snapshots, the path uses SNAPSHOT but filename uses timestamped version
			groupPath := strings.ReplaceAll(data.GroupID, ".", "/")
			repo := strings.TrimSuffix(data.Repository, "/")
			basePath := fmt.Sprintf("%s/%s/%s/%s", repo, groupPath, data.ArtifactID, data.Version)
			artifactURL := fmt.Sprintf("%s/%s", basePath, filename)

			// Get hash for new version
			hash, err := getArtifactHash(artifactURL, "sha256")
			if err != nil {
				results[i] = core.UpdateCheck{Error: fmt.Errorf("failed to get hash for new version: %v", err)}
				continue
			}

			results[i] = core.UpdateCheck{
				UpdateAvailable: true,
				UpdateString:    mod.FileName + " -> " + filename,
				CachedState: cachedStateStore{
					Repository: data.Repository,
					GroupID:    data.GroupID,
					ArtifactID: data.ArtifactID,
					Version:    data.Version, // Keep original snapshot version
					Classifier: data.Classifier,
					Extension:  data.Extension,
					Filename:   filename,
					URL:        artifactURL,
					Hash:       hash,
				},
			}
		} else {
			// Handle release version updates
			// Default extension to "jar" if not specified
			extension := data.Extension
			if extension == "" {
				extension = "jar"
			}

			metadata, err := fetchMavenMetadata(data.Repository, data.GroupID, data.ArtifactID)
			if err != nil {
				results[i] = core.UpdateCheck{Error: fmt.Errorf("failed to fetch metadata: %v", err)}
				continue
			}

			// Find latest version
			latestVersion := data.Version
			if metadata.Versioning.Release != "" {
				latestVersion = metadata.Versioning.Release
			} else if metadata.Versioning.Latest != "" {
				latestVersion = metadata.Versioning.Latest
			} else if len(metadata.Versioning.Versions.Version) > 0 {
				// Sort versions and pick the latest
				versions := make([]string, len(metadata.Versioning.Versions.Version))
				copy(versions, metadata.Versioning.Versions.Version)
				flexver.VersionSlice(versions).Sort()
				latestVersion = versions[len(versions)-1]
			}

			// Compare versions using FlexVer
			compare := flexver.Compare(latestVersion, data.Version)
			if compare <= 0 {
				// Current version is up to date or newer
				results[i] = core.UpdateCheck{UpdateAvailable: false}
				continue
			}

			// Build new filename and URL
			filename := data.ArtifactID + "-" + latestVersion
			if data.Classifier != "" {
				filename += "-" + data.Classifier
			}
			filename += "." + extension

			artifactURL := buildMavenURL(data.Repository, data.GroupID, data.ArtifactID, latestVersion, data.Classifier, extension)

			// Get hash for new version
			hash, err := getArtifactHash(artifactURL, "sha256")
			if err != nil {
				results[i] = core.UpdateCheck{Error: fmt.Errorf("failed to get hash for new version: %v", err)}
				continue
			}

			results[i] = core.UpdateCheck{
				UpdateAvailable: true,
				UpdateString:    mod.FileName + " -> " + filename,
				CachedState: cachedStateStore{
					Repository: data.Repository,
					GroupID:    data.GroupID,
					ArtifactID: data.ArtifactID,
					Version:    latestVersion,
					Classifier: data.Classifier,
					Extension:  extension,
					Filename:   filename,
					URL:        artifactURL,
					Hash:       hash,
				},
			}
		}
	}

	return results, nil
}

// checkSnapshotUpdate checks if there's a newer snapshot build available
func checkSnapshotUpdate(data mavenUpdateData) (bool, string, error) {
	metadata, err := fetchSnapshotMetadata(data.Repository, data.GroupID, data.ArtifactID, data.Version)
	if err != nil {
		return false, "", err
	}

	// Construct current timestamped version from stored snapshot version
	// We need to check if there's a newer timestamp
	// For simplicity, we'll always consider a new snapshot metadata fetch as potentially newer
	// The actual comparison would require storing the timestamp, but for now we'll update if metadata exists

	baseVersion := strings.TrimSuffix(data.Version, "-SNAPSHOT")
	newTimestampedVersion := fmt.Sprintf("%s-%s-%d", baseVersion, metadata.Versioning.Snapshot.Timestamp, metadata.Versioning.Snapshot.BuildNumber)

	// Try to resolve the current version to see if it's different
	// Since we don't store the timestamp, we'll assume an update is available if metadata exists
	// In practice, this means we'll update to the latest snapshot build
	return true, newTimestampedVersion, nil
}

func (u mavenUpdater) DoUpdate(mods []*core.Mod, cachedState []interface{}) error {
	for i, mod := range mods {
		modState := cachedState[i].(cachedStateStore)

		mod.FileName = modState.Filename
		mod.Download = core.ModDownload{
			URL:        modState.URL,
			HashFormat: "sha256",
			Hash:       modState.Hash,
		}

		// Update the version in the update metadata
		// For snapshots, keep the -SNAPSHOT suffix; for releases, update to new version
		if mod.Update["maven"] != nil {
			mod.Update["maven"]["version"] = modState.Version
		}
	}

	return nil
}

