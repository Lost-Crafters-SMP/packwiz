package maven

import (
	"fmt"
	"strings"
)

// buildMavenURL constructs a Maven repository URL for an artifact
// For release versions: {repository}/{groupPath}/{artifactID}/{version}/{artifactID}-{version}[-{classifier}].{extension}
// For snapshot versions: {repository}/{groupPath}/{artifactID}/{version}/{artifactID}-{timestampedVersion}[-{classifier}].{extension}
func buildMavenURL(repository, groupID, artifactID, version, classifier, extension string) string {
	groupPath := strings.ReplaceAll(groupID, ".", "/")

	// Remove trailing slash from repository if present
	repo := strings.TrimSuffix(repository, "/")

	// Build base path
	basePath := fmt.Sprintf("%s/%s/%s/%s", repo, groupPath, artifactID, version)

	// Build filename
	filename := artifactID + "-" + version
	if classifier != "" {
		filename += "-" + classifier
	}
	filename += "." + extension

	return fmt.Sprintf("%s/%s", basePath, filename)
}

// buildMetadataURL constructs the URL for maven-metadata.xml
func buildMetadataURL(repository, groupID, artifactID string) string {
	groupPath := strings.ReplaceAll(groupID, ".", "/")
	repo := strings.TrimSuffix(repository, "/")
	return fmt.Sprintf("%s/%s/%s/maven-metadata.xml", repo, groupPath, artifactID)
}

// buildSnapshotMetadataURL constructs the URL for snapshot version metadata
func buildSnapshotMetadataURL(repository, groupID, artifactID, version string) string {
	groupPath := strings.ReplaceAll(groupID, ".", "/")
	repo := strings.TrimSuffix(repository, "/")
	return fmt.Sprintf("%s/%s/%s/%s/maven-metadata.xml", repo, groupPath, artifactID, version)
}

// isSnapshotVersion checks if a version string is a snapshot version
func isSnapshotVersion(version string) bool {
	return strings.HasSuffix(version, "-SNAPSHOT")
}

