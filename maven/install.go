package maven

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/packwiz/packwiz/core"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/unascribed/FlexVer/go/flexver"
	"gopkg.in/dixonwille/wmenu.v4"
)

var installCmd = &cobra.Command{
	Use:     "add [coordinates]",
	Short:   "Add an artifact from a Maven repository",
	Aliases: []string{"install", "get"},
	Args:    cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		pack, err := core.LoadPack()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		coordinates := args[0]
		repository := repositoryFlag
		classifier := classifierFlag
		extension := extensionFlag
		version := versionFlag
		if extension == "" {
			extension = "jar"
		}

		// Validate flags
		if typeFlag != "" && typeFlag != "mods" && typeFlag != "plugins" && typeFlag != "resourcepacks" && typeFlag != "shaderpacks" {
			// Allow custom folder names, but warn if it looks invalid
			if strings.Contains(typeFlag, "/") || strings.Contains(typeFlag, "\\") {
				fmt.Printf("Error: --type should be a folder name, not a path\n")
				os.Exit(1)
			}
		}
		if sideFlag != "" && sideFlag != "server" && sideFlag != "client" && sideFlag != "both" {
			fmt.Printf("Error: --side must be one of: server, client, both\n")
			os.Exit(1)
		}

		// If --repository flag is provided, use it directly (override config)
		if repository != "" {
			err = installArtifact(coordinates, repository, classifier, extension, version, typeFlag, sideFlag, pack)
			if err != nil {
				fmt.Printf("Failed to add artifact: %s\n", err)
				os.Exit(1)
			}
			return
		}

		// Otherwise, use multi-repository resolution
		err = installArtifactFromRepos(coordinates, classifier, extension, version, typeFlag, sideFlag, pack)
		if err != nil {
			fmt.Printf("Failed to add artifact: %s\n", err)
			os.Exit(1)
		}
	},
}

// ArtifactResult represents a found artifact in a repository
type ArtifactResult struct {
	Repository Repository
	URL        string
	Exists     bool
	Error      error
}

// inferArtifactType attempts to infer the artifact type (folder) from groupID and artifactID
// Returns the folder name (e.g., "mods", "plugins", "resourcepacks", "shaderpacks") or an error if cannot infer
func inferArtifactType(groupID, artifactID string) (string, error) {
	// Convert to lowercase for case-insensitive matching
	groupLower := strings.ToLower(groupID)
	artifactLower := strings.ToLower(artifactID)
	combined := groupLower + ":" + artifactLower

	// Check for mod loader keywords
	modKeywords := []string{"fabric", "quilt", "forge", "neoforge", "liteloader", "modloader", "rift"}
	for _, keyword := range modKeywords {
		if strings.Contains(combined, keyword) {
			return "mods", nil
		}
	}

	// Check for plugin loader keywords
	pluginKeywords := []string{"bukkit", "spigot", "paper", "purpur", "sponge", "bungeecord", "waterfall", "velocity"}
	for _, keyword := range pluginKeywords {
		if strings.Contains(combined, keyword) {
			return "plugins", nil
		}
	}

	// Check for resource pack keywords
	resourcePackKeywords := []string{"resourcepack", "resource-pack", "resource_pack"}
	for _, keyword := range resourcePackKeywords {
		if strings.Contains(combined, keyword) {
			return "resourcepacks", nil
		}
	}

	// Check for shader pack keywords
	shaderPackKeywords := []string{"shaderpack", "shader-pack", "shader_pack", "shader"}
	for _, keyword := range shaderPackKeywords {
		if strings.Contains(combined, keyword) {
			return "shaderpacks", nil
		}
	}

	// Check for specific shader/resource pack loaders
	// Note: canvas can be either resourcepacks or shaderpacks depending on context
	// We'll check for shader-specific context first
	if strings.Contains(combined, "iris") || strings.Contains(combined, "optifine") {
		return "shaderpacks", nil
	}
	if strings.Contains(combined, "canvas") {
		// Canvas can be either, but if we see shader-related terms, prefer shaderpacks
		// Otherwise default to resourcepacks (canvas is more commonly used for resource packs)
		if strings.Contains(combined, "shader") {
			return "shaderpacks", nil
		}
		return "resourcepacks", nil
	}

	return "", errors.New("could not infer artifact type from coordinates")
}

// resolveLatestVersion fetches Maven metadata and determines the latest version
func resolveLatestVersion(repository, groupID, artifactID string) (string, error) {
	fmt.Printf("Fetching latest version for %s:%s...\n", groupID, artifactID)
	metadata, err := fetchMavenMetadata(repository, groupID, artifactID)
	if err != nil {
		return "", fmt.Errorf("failed to fetch latest version: %w", err)
	}

	// Find latest version: Release > Latest > sorted Versions
	if metadata.Versioning.Release != "" {
		version := metadata.Versioning.Release
		fmt.Printf("Latest version: %s\n", version)
		return version, nil
	}
	if metadata.Versioning.Latest != "" {
		version := metadata.Versioning.Latest
		fmt.Printf("Latest version: %s\n", version)
		return version, nil
	}
	if len(metadata.Versioning.Versions.Version) > 0 {
		// Sort versions and pick the latest
		versions := make([]string, len(metadata.Versioning.Versions.Version))
		copy(versions, metadata.Versioning.Versions.Version)
		flexver.VersionSlice(versions).Sort()
		version := versions[len(versions)-1]
		fmt.Printf("Latest version: %s\n", version)
		return version, nil
	}
	return "", errors.New("no versions found in repository metadata")
}

// parseAndApplyFlags parses coordinates and applies flag overrides
func parseAndApplyFlags(coords, classifierFlag, extensionFlag, versionFlag string) (groupID, artifactID, version, classifier, extension string, err error) {
	groupID, artifactID, version, classifier, extension, err = parseCoordinates(coords)
	if err != nil {
		return "", "", "", "", "", err
	}

	// Apply flag overrides
	if versionFlag != "" {
		version = versionFlag
	}
	if classifierFlag != "" {
		classifier = classifierFlag
	}
	if extensionFlag != "" {
		extension = extensionFlag
	}
	if extension == "" {
		extension = "jar"
	}

	return groupID, artifactID, version, classifier, extension, nil
}

// parseCoordinates parses Maven coordinates in the format groupId:artifactId[:version[:classifier[:extension]]]
// Version is optional - if not provided, latest will be fetched from metadata
func parseCoordinates(coords string) (groupID, artifactID, version, classifier, extension string, err error) {
	parts := strings.Split(coords, ":")
	if len(parts) < 2 {
		return "", "", "", "", "", errors.New("invalid coordinates format: expected groupId:artifactId[:version[:classifier[:extension]]]")
	}

	groupID = parts[0]
	artifactID = parts[1]

	// Version is optional (parts[2])
	if len(parts) >= 3 {
		version = parts[2]
	}

	if len(parts) >= 4 {
		classifier = parts[3]
	}
	if len(parts) >= 5 {
		extension = parts[4]
	}

	return groupID, artifactID, version, classifier, extension, nil
}

// tryRepositoriesForArtifact tries to resolve an artifact across multiple repositories
func tryRepositoriesForArtifact(groupID, artifactID, version, classifier, extension string, repos []Repository) []ArtifactResult {
	results := make([]ArtifactResult, len(repos))

	for i, repo := range repos {
		artifactURL := buildMavenURL(repo.URL, groupID, artifactID, version, classifier, extension)

		// Check if artifact exists by trying to fetch it (HEAD request would be better, but some repos don't support it)
		// For now, we'll try to fetch metadata or check the artifact URL
		resp, err := core.GetWithUA(artifactURL, "application/octet-stream")
		if err != nil {
			results[i] = ArtifactResult{
				Repository: repo,
				URL:        artifactURL,
				Exists:     false,
				Error:      err,
			}
			continue
		}
		resp.Body.Close()

		exists := resp.StatusCode == http.StatusOK
		results[i] = ArtifactResult{
			Repository: repo,
			URL:        artifactURL,
			Exists:     exists,
			Error:      nil,
		}
	}

	return results
}

// installArtifactFromRepos tries to install an artifact from configured repositories
func installArtifactFromRepos(coords, classifier, extension, versionFlag, typeFlag, sideFlag string, pack core.Pack) error {
	// Parse coordinates and apply flags
	groupID, artifactID, version, parsedClassifier, parsedExtension, err := parseAndApplyFlags(coords, classifier, extension, versionFlag)
	if err != nil {
		return err
	}

	// Load repository configuration once (will be reused for version resolution and artifact search)
	config, err := LoadMavenConfig()
	if err != nil {
		return fmt.Errorf("failed to load maven.toml: %w", err)
	}

	repos := GetRepositories(config)
	if len(repos) == 0 {
		return errors.New("no repositories configured. Enable Maven Central in maven.toml or use --repository flag")
	}

	// If no version specified, fetch latest from metadata
	if version == "" {
		// Try to get latest version from first repository (they should all have the same versions)
		version, err = resolveLatestVersion(repos[0].URL, groupID, artifactID)
		if err != nil {
			return err
		}
	}

	// Try all repositories
	fmt.Printf("Searching for %s:%s:%s in %d repository(ies)...\n", groupID, artifactID, version, len(repos))
	results := tryRepositoriesForArtifact(groupID, artifactID, version, parsedClassifier, parsedExtension, repos)

	// Filter to only successful results
	var foundResults []ArtifactResult
	for _, result := range results {
		if result.Exists {
			foundResults = append(foundResults, result)
		}
	}

	if len(foundResults) == 0 {
		return fmt.Errorf("artifact not found in any configured repository. Consider adding more repositories to maven.toml")
	}

	// If only one repository has it, use it automatically
	if len(foundResults) == 1 {
		fmt.Printf("Found in: %s\n", foundResults[0].Repository.Name)
		return installArtifactFromResult(foundResults[0], groupID, artifactID, version, parsedClassifier, parsedExtension, typeFlag, sideFlag, pack)
	}

	// Multiple repositories have it - show menu
	if viper.GetBool("non-interactive") {
		// Auto-select first one in non-interactive mode
		fmt.Printf("Found in multiple repositories, selecting: %s (non-interactive mode)\n", foundResults[0].Repository.Name)
		return installArtifactFromResult(foundResults[0], groupID, artifactID, version, parsedClassifier, parsedExtension, typeFlag, sideFlag, pack)
	}

	fmt.Println("Found in multiple repositories:")
	menu := wmenu.NewMenu("Choose a repository:")
	menu.Option("Cancel", nil, false, nil)
	for i, result := range foundResults {
		menu.Option(fmt.Sprintf("%s (%s)", result.Repository.Name, result.Repository.URL), result, i == 0, nil)
	}

	var selectedResult ArtifactResult
	var cancelled bool
	menu.Action(func(menuRes []wmenu.Opt) error {
		if len(menuRes) != 1 || menuRes[0].Value == nil {
			cancelled = true
			return nil
		}

		var ok bool
		selectedResult, ok = menuRes[0].Value.(ArtifactResult)
		if !ok {
			return errors.New("error converting interface from wmenu")
		}
		return nil
	})

	err = menu.Run()
	if err != nil {
		return err
	}

	if cancelled {
		return errors.New("installation cancelled")
	}

	return installArtifactFromResult(selectedResult, groupID, artifactID, version, parsedClassifier, parsedExtension, typeFlag, sideFlag, pack)
}

// installArtifactFromResult installs an artifact from a specific repository result
func installArtifactFromResult(result ArtifactResult, groupID, artifactID, version, classifier, extension, typeFlag, sideFlag string, pack core.Pack) error {
	return installArtifactWithRepo(groupID, artifactID, version, classifier, extension, result.Repository.URL, result.Repository.Name, typeFlag, sideFlag, pack)
}

// installArtifact installs an artifact from a specific repository (legacy function, kept for --repository flag)
func installArtifact(coords, repository, classifier, extension, versionFlag, typeFlag, sideFlag string, pack core.Pack) error {
	// Parse coordinates and apply flags
	groupID, artifactID, version, parsedClassifier, parsedExtension, err := parseAndApplyFlags(coords, classifier, extension, versionFlag)
	if err != nil {
		return err
	}

	// If no version specified, fetch latest from metadata
	if version == "" {
		version, err = resolveLatestVersion(repository, groupID, artifactID)
		if err != nil {
			return err
		}
	}

	return installArtifactWithRepo(groupID, artifactID, version, parsedClassifier, parsedExtension, repository, repository, typeFlag, sideFlag, pack)
}

// installArtifactWithRepo is the main installation logic
func installArtifactWithRepo(groupID, artifactID, version, classifier, extension, repository, repoName, typeFlag, sideFlag string, pack core.Pack) error {
	// Check if it's a snapshot version
	isSnapshot := isSnapshotVersion(version)
	var artifactURL string
	var actualVersion string

	if isSnapshot {
		fmt.Printf("Warning: Snapshot versions are mutable and may be cleaned up by the repository.\n")
		fmt.Printf("Consider using a specific timestamped version for stability.\n")

		// Resolve snapshot to timestamped version
		timestampedVersion, err := resolveSnapshotVersion(repository, groupID, artifactID, version)
		if err != nil {
			return fmt.Errorf("failed to resolve snapshot version: %w", err)
		}
		actualVersion = timestampedVersion

		// For snapshots, the path uses SNAPSHOT but filename uses timestamped version
		groupPath := strings.ReplaceAll(groupID, ".", "/")
		repo := strings.TrimSuffix(repository, "/")
		basePath := fmt.Sprintf("%s/%s/%s/%s", repo, groupPath, artifactID, version)

		// Build filename with timestamped version
		filename := artifactID + "-" + timestampedVersion
		if classifier != "" {
			filename += "-" + classifier
		}
		filename += "." + extension

		artifactURL = fmt.Sprintf("%s/%s", basePath, filename)
	} else {
		actualVersion = version
		artifactURL = buildMavenURL(repository, groupID, artifactID, version, classifier, extension)
	}

	// Download and compute hash
	fmt.Printf("Downloading %s:%s:%s from %s...\n", groupID, artifactID, version, repoName)
	hash, err := getArtifactHash(artifactURL, "sha256")
	if err != nil {
		return fmt.Errorf("failed to get artifact hash: %w", err)
	}

	// Determine filename
	filename := artifactID + "-" + actualVersion
	if classifier != "" {
		filename += "-" + classifier
	}
	filename += "." + extension

	// Load index
	index, err := pack.LoadIndex()
	if err != nil {
		return err
	}

	// Create update metadata
	updateMap := make(map[string]map[string]interface{})
	updateMap["maven"], err = mavenUpdateData{
		Repository: repository,
		GroupID:    groupID,
		ArtifactID: artifactID,
		Version:    version, // Store the original version (including -SNAPSHOT if applicable)
		Classifier: classifier,
		Extension:  extension,
	}.ToMap()
	if err != nil {
		return err
	}

	// Determine folder type
	// Priority: --type flag > meta-folder config > inference
	var folder string
	if typeFlag != "" {
		// --type flag explicitly provided, use it
		folder = typeFlag
	} else {
		// Check if meta-folder is set in config
		folder = viper.GetString("meta-folder")
		if folder == "" {
			// Try to infer from coordinates
			inferredType, err := inferArtifactType(groupID, artifactID)
			if err != nil {
				return fmt.Errorf("could not infer artifact type from coordinates. Please specify --type (e.g., --type mods, --type plugins)")
			}
			folder = inferredType
		}
	}

	// Determine side (defaults to "both" via core.UniversalSide)
	side := core.UniversalSide
	if sideFlag != "" {
		switch sideFlag {
		case "server":
			side = core.ServerSide
		case "client":
			side = core.ClientSide
		// "both" and empty string already handled by default value
		}
	}

	// Create mod metadata
	modMeta := core.Mod{
		Name:     artifactID,
		FileName: filename,
		Side:     side,
		Download: core.ModDownload{
			URL:        artifactURL,
			HashFormat: "sha256",
			Hash:       hash,
		},
		Update: updateMap,
	}

	// Set metadata file path
	path := modMeta.SetMetaPath(filepath.Join(viper.GetString("meta-folder-base"), folder, core.SlugifyName(artifactID)+core.MetaExtension))

	// Write metadata file
	format, hash, err := modMeta.Write()
	if err != nil {
		return err
	}

	// Update index
	err = index.RefreshFileWithHash(path, format, hash, true)
	if err != nil {
		return err
	}
	err = index.Write()
	if err != nil {
		return err
	}
	err = pack.UpdateIndexHash()
	if err != nil {
		return err
	}
	err = pack.Write()
	if err != nil {
		return err
	}

	fmt.Printf("Artifact \"%s\" successfully added! (%s)\n", artifactID, filename)
	return nil
}

var repositoryFlag string
var classifierFlag string
var extensionFlag string
var versionFlag string
var typeFlag string
var sideFlag string

func init() {
	mavenCmd.AddCommand(installCmd)

	installCmd.Flags().StringVar(&repositoryFlag, "repository", "", "Maven repository base URL (overrides maven.toml config for this install)")
	installCmd.Flags().StringVar(&classifierFlag, "classifier", "", "Artifact classifier")
	installCmd.Flags().StringVar(&extensionFlag, "extension", "", "Artifact extension (default: jar)")
	installCmd.Flags().StringVar(&versionFlag, "version", "", "Artifact version (if not specified in coordinates, latest will be fetched)")
	installCmd.Flags().StringVar(&typeFlag, "type", "", "Folder type for the artifact (mods, plugins, resourcepacks, shaderpacks, or custom). If not specified, will attempt to infer from coordinates.")
	installCmd.Flags().StringVar(&sideFlag, "side", "", "Side for the artifact (server, client, or both). Defaults to both if not specified.")
}
