package maven

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/packwiz/packwiz/core"
)

// MavenMetadata represents the standard maven-metadata.xml structure for releases
type MavenMetadata struct {
	XMLName    xml.Name `xml:"metadata"`
	GroupID    string   `xml:"groupId"`
	ArtifactID string   `xml:"artifactId"`
	Versioning struct {
		Release  string `xml:"release"`
		Latest   string `xml:"latest"`
		Versions struct {
			Version []string `xml:"version"`
		} `xml:"versions"`
		LastUpdated string `xml:"lastUpdated"`
	} `xml:"versioning"`
}

// SnapshotMetadata represents the maven-metadata.xml structure for snapshot versions
type SnapshotMetadata struct {
	XMLName    xml.Name `xml:"metadata"`
	GroupID    string   `xml:"groupId"`
	ArtifactID string   `xml:"artifactId"`
	Version    string   `xml:"version"`
	Versioning struct {
		Snapshot struct {
			Timestamp   string `xml:"timestamp"`
			BuildNumber int    `xml:"buildNumber"`
		} `xml:"snapshot"`
		LastUpdated string `xml:"lastUpdated"`
	} `xml:"versioning"`
}

// fetchMavenMetadata fetches and parses maven-metadata.xml for release versions
func fetchMavenMetadata(repository, groupID, artifactID string) (*MavenMetadata, error) {
	url := buildMetadataURL(repository, groupID, artifactID)

	resp, err := core.GetWithUA(url, "application/xml")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch metadata: status code %d", resp.StatusCode)
	}

	dec := xml.NewDecoder(resp.Body)
	var metadata MavenMetadata
	err = dec.Decode(&metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	return &metadata, nil
}

// fetchSnapshotMetadata fetches and parses snapshot version metadata
func fetchSnapshotMetadata(repository, groupID, artifactID, version string) (*SnapshotMetadata, error) {
	url := buildSnapshotMetadataURL(repository, groupID, artifactID, version)

	resp, err := core.GetWithUA(url, "application/xml")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch snapshot metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("snapshot version %s may have been cleaned up by the repository", version)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch snapshot metadata: status code %d", resp.StatusCode)
	}

	dec := xml.NewDecoder(resp.Body)
	var metadata SnapshotMetadata
	err = dec.Decode(&metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to parse snapshot metadata: %w", err)
	}

	return &metadata, nil
}

// resolveSnapshotVersion resolves a SNAPSHOT version to its timestamped version
func resolveSnapshotVersion(repository, groupID, artifactID, snapshotVersion string) (string, error) {
	metadata, err := fetchSnapshotMetadata(repository, groupID, artifactID, snapshotVersion)
	if err != nil {
		return "", err
	}

	// Construct timestamped version: baseVersion-timestamp-buildNumber
	baseVersion := strings.TrimSuffix(snapshotVersion, "-SNAPSHOT")
	timestampedVersion := fmt.Sprintf("%s-%s-%d", baseVersion, metadata.Versioning.Snapshot.Timestamp, metadata.Versioning.Snapshot.BuildNumber)

	return timestampedVersion, nil
}

// downloadArtifact downloads an artifact from a URL and returns its content
func downloadArtifact(url string) (io.ReadCloser, error) {
	resp, err := core.GetWithUA(url, "application/octet-stream")
	if err != nil {
		return nil, fmt.Errorf("failed to download artifact: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, errors.New("artifact not found (404) - it may have been cleaned up if it's a snapshot version")
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("failed to download artifact: status code %d", resp.StatusCode)
	}

	return resp.Body, nil
}

// getArtifactHash downloads an artifact and computes its hash
func getArtifactHash(url string, format string) (string, error) {
	body, err := downloadArtifact(url)
	if err != nil {
		return "", err
	}
	defer body.Close()

	hasher, err := core.GetHashImpl(format)
	if err != nil {
		return "", fmt.Errorf("unsupported hash format: %s", format)
	}

	_, err = io.Copy(hasher, body)
	if err != nil {
		return "", fmt.Errorf("failed to read artifact: %w", err)
	}

	hash := hasher.Sum(nil)
	return hasher.HashToString(hash), nil
}

