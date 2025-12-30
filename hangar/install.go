package hangar

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/packwiz/packwiz/cmdshared"
	"github.com/packwiz/packwiz/core"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/dixonwille/wmenu.v4"
)

// installCmd represents the install command
var installCmd = &cobra.Command{
	Use:     "add [URL|slug|search]",
	Short:   "Add a project from a Hangar URL, slug or search",
	Aliases: []string{"install", "get"},
	Args:    cobra.ArbitraryArgs,
	Run: func(cmd *cobra.Command, args []string) {
		pack, err := core.LoadPack()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		index, err := pack.LoadIndex()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		if len(args) == 0 || len(args[0]) == 0 {
			fmt.Println("You must specify a project; by passing a URL, slug or search term directly.")
			os.Exit(1)
		}

		var projectID, versionID string
		var parsedSlug bool
		if len(args) == 1 {
			// Try interpreting the argument as a slug/project ID, or URL
			parsedSlug, err = parseSlugOrUrl(args[0], &projectID, &versionID)
			if err != nil {
				fmt.Printf("Failed to parse URL: %v\n", err)
				os.Exit(1)
			}
		}

		// Use --version flag if provided (overrides URL parsing)
		if versionFlag != "" {
			versionID = versionFlag
		}

		// Got version ID; install using this ID
		if versionID != "" && projectID != "" {
			err = installVersionById(projectID, versionID, pack, &index)
			if err != nil {
				fmt.Printf("Failed to add project: %s\n", err)
				os.Exit(1)
			}
			return
		}

		// Look up project ID
		if projectID != "" {
			project, err := hgDefaultClient.getProject(projectID)
			if err == nil {
				// We found a project with that id/slug
				// If version was specified via flag, install that version, otherwise install latest
				if versionFlag != "" {
					err = installVersionById(projectID, versionID, pack, &index)
				} else {
					err = installProject(project, pack, &index)
				}
				if err != nil {
					fmt.Printf("Failed to add project: %s\n", err)
					os.Exit(1)
				}
				return
			}
		}

		// Arguments weren't a valid slug/project ID, try to search for it instead
		err = installViaSearch(strings.Join(args, " "), !parsedSlug, pack, &index)
		if err != nil {
			fmt.Printf("Failed to add project: %s\n", err)
			os.Exit(1)
		}
	},
}

var urlRegexes = [...]*regexp.Regexp{
	regexp.MustCompile(`^https?://(?:www\.)?hangar\.papermc\.io/(?P<author>[^/]+)/(?P<slug>[^/]+)(?:/versions/(?P<version>[^/]+))?`),
	regexp.MustCompile(`^(?P<author>[^/]+)/(?P<slug>[^/]+)(?:/versions/(?P<version>[^/]+))?$`),
	regexp.MustCompile(`^(?P<slug>[^/]+)$`),
}

func parseSlugOrUrl(input string, projectID *string, versionID *string) (parsedSlug bool, err error) {
	for i, r := range urlRegexes {
		matches := r.FindStringSubmatch(input)
		if matches != nil {
			if i := r.SubexpIndex("author"); i >= 0 && matches[i] != "" {
				if i := r.SubexpIndex("slug"); i >= 0 && matches[i] != "" {
					*projectID = matches[r.SubexpIndex("author")] + "/" + matches[r.SubexpIndex("slug")]
				}
			} else if i := r.SubexpIndex("slug"); i >= 0 {
				*projectID = matches[i]
			}
			if i := r.SubexpIndex("version"); i >= 0 && matches[i] != "" {
				*versionID = matches[i]
			}
			parsedSlug = (i == len(urlRegexes)-1) // Last regex is slug-only
			return
		}
	}
	return
}

func installVersionById(projectID string, versionID string, pack core.Pack, index *core.Index) error {
	version, err := hgDefaultClient.getVersion(projectID, versionID)
	if err != nil {
		return fmt.Errorf("failed to fetch version %s: %v", versionID, err)
	}

	project, err := hgDefaultClient.getProject(projectID)
	if err != nil {
		return fmt.Errorf("failed to fetch project %s: %v", projectID, err)
	}

	return installVersion(project, version, pack, index)
}

func installViaSearch(query string, autoAcceptFirst bool, pack core.Pack, index *core.Index) error {
	// Get compatible platforms
	platforms, err := getCompatiblePlatforms(pack)
	if err != nil {
		return err
	}
	if len(platforms) == 0 {
		return errors.New("no plugin platforms configured in pack.toml. Add plugin platform(s) to versions section (e.g., versions = { paper = \"1.20.1-205\" })")
	}

	fmt.Println("Searching Hangar...")

	// Search without platform/version filters first to get more results
	// Then we'll post-filter if needed
	// Use prioritizeExactMatch to help with relevance
	results, err := hgDefaultClient.searchProjects(query, "", "", 20, 0)
	if err != nil {
		return err
	}

	if len(results.Result) == 0 {
		return errors.New("no projects found")
	}

	// Post-filter by platform if we have results
	// Prioritize platform-compatible results, but don't remove others completely
	if len(platforms) > 0 && len(results.Result) > 1 {
		var platformCompatible []ProjectResult
		var otherResults []ProjectResult

		for _, result := range results.Result {
			// Check if project supports any of our platforms
			compatible := false
			for _, platform := range platforms {
				if supportedVersions, ok := result.SupportedPlatforms[platform]; ok && len(supportedVersions) > 0 {
					compatible = true
					break
				}
			}

			if compatible {
				platformCompatible = append(platformCompatible, result)
			} else {
				otherResults = append(otherResults, result)
			}
		}

		// Combine: platform-compatible first, then others
		// Limit to top 5 platform-compatible, then add up to 2 more from others
		if len(platformCompatible) > 5 {
			results.Result = platformCompatible[:5]
		} else {
			results.Result = platformCompatible
			// Add a few non-compatible results if we have space
			remaining := 5 - len(results.Result)
			if remaining > 0 && len(otherResults) > 0 {
				if len(otherResults) > remaining {
					results.Result = append(results.Result, otherResults[:remaining]...)
				} else {
					results.Result = append(results.Result, otherResults...)
				}
			}
		}
	} else {
		// Limit to top 5
		if len(results.Result) > 5 {
			results.Result = results.Result[:5]
		}
	}

	if viper.GetBool("non-interactive") || (len(results.Result) == 1 && autoAcceptFirst) {
		// Install the first project found
		projectID := getProjectIdentifier(results.Result[0])
		project, err := hgDefaultClient.getProject(projectID)
		if err != nil {
			return err
		}
		return installProject(project, pack, index)
	}

	// Create menu for the user to choose the correct project
	menu := wmenu.NewMenu("Choose a number:")
	menu.Option("Cancel", nil, false, nil)
	for i, v := range results.Result {
		menu.Option(v.Name+" - "+v.Description, v, i == 0, nil)
	}

	menu.Action(func(menuRes []wmenu.Opt) error {
		if len(menuRes) != 1 || menuRes[0].Value == nil {
			return errors.New("project selection cancelled")
		}

		// Get the selected project
		selectedProject, ok := menuRes[0].Value.(ProjectResult)
		if !ok {
			return errors.New("error converting interface from wmenu")
		}

		// Get full project details
		projectID := getProjectIdentifier(selectedProject)
		project, err := hgDefaultClient.getProject(projectID)
		if err != nil {
			return err
		}

		return installProject(project, pack, index)
	})

	return menu.Run()
}

func installProject(project Project, pack core.Pack, index *core.Index) error {
	latestVersion, err := getLatestVersion(project, pack)
	if err != nil {
		return fmt.Errorf("failed to get latest version: %v", err)
	}
	if latestVersion.ID == 0 {
		return errors.New("plugin not available for the configured Minecraft version(s) or platform")
	}

	return installVersion(project, latestVersion, pack, index)
}

const maxCycles = 20

type depMetadataStore struct {
	projectInfo Project
	versionInfo Version
	platform    string
}

func installVersion(project Project, version Version, pack core.Pack, index *core.Index) error {
	// Get compatible platforms
	platforms, err := getCompatiblePlatforms(pack)
	if err != nil {
		return err
	}
	if len(platforms) == 0 {
		return errors.New("no plugin platforms configured in pack.toml")
	}

	// Select platform - prefer one that matches project's supported platforms
	selectedPlatform := ""
	for _, platform := range platforms {
		if supportedVersions, ok := project.SupportedPlatforms[platform]; ok && len(supportedVersions) > 0 {
			selectedPlatform = platform
			break
		}
	}
	if selectedPlatform == "" {
		// Use first available platform from the version
		for platform := range version.Downloads {
			if slices.Contains(platforms, platform) {
				selectedPlatform = platform
				break
			}
		}
	}
	if selectedPlatform == "" {
		return errors.New("no compatible platform found for this plugin")
	}

	// Check if version has download for selected platform
	platformDownload, ok := version.Downloads[selectedPlatform]
	if !ok {
		return errors.New("version doesn't have a download for the selected platform")
	}

	// Handle dependencies
	if len(version.PluginDependencies) > 0 {
		installedProjects := getInstalledProjectIDs(index)

		var depMetadata []depMetadataStore
		var depProjectIDPendingQueue []string

		// Get dependencies for the selected platform
		deps, ok := version.PluginDependencies[selectedPlatform]
		if ok {
			for _, dep := range deps {
				if dep.Required {
					if dep.ProjectID != nil {
						projectIDStr := fmt.Sprintf("%d", *dep.ProjectID)
						// Check if already installed
						if !slices.Contains(installedProjects, projectIDStr) {
							// Check if already in dependency queue
							found := false
							for _, queuedID := range depProjectIDPendingQueue {
								if queuedID == projectIDStr {
									found = true
									break
								}
							}
							if !found {
								for _, queuedDep := range depMetadata {
									if fmt.Sprintf("%d", queuedDep.projectInfo.ID) == projectIDStr {
										found = true
										break
									}
								}
							}
							if !found {
								depProjectIDPendingQueue = append(depProjectIDPendingQueue, projectIDStr)
							}
						}
					} else if dep.ExternalURL != "" {
						fmt.Printf("Warning: External dependency '%s' cannot be auto-installed. URL: %s\n", dep.Name, dep.ExternalURL)
					}
				}
			}
		}

		if len(depProjectIDPendingQueue) > 0 {
			fmt.Println("Finding dependencies...")

			cycles := 0
			for len(depProjectIDPendingQueue) > 0 && cycles < maxCycles {
				// Remove installed project IDs from dep queue
				i := 0
				for _, id := range depProjectIDPendingQueue {
					contains := slices.Contains(installedProjects, id)
					for _, dep := range depMetadata {
						depIDStr := fmt.Sprintf("%d", dep.projectInfo.ID)
						if dep.projectInfo.Namespace.Owner != "" && dep.projectInfo.Namespace.Slug != "" {
							depIDStr = dep.projectInfo.Namespace.Owner + "/" + dep.projectInfo.Namespace.Slug
						}
						if depIDStr == id {
							contains = true
							break
						}
					}
					if !contains {
						depProjectIDPendingQueue[i] = id
						i++
					}
				}
				depProjectIDPendingQueue = depProjectIDPendingQueue[:i]

				// Clean up duplicates
				slices.Sort(depProjectIDPendingQueue)
				depProjectIDPendingQueue = slices.Compact(depProjectIDPendingQueue)

				if len(depProjectIDPendingQueue) == 0 {
					break
				}

				// Fetch projects
				var depProjects []Project
				for _, projectIDStr := range depProjectIDPendingQueue {
					depProject, err := hgDefaultClient.getProject(projectIDStr)
					if err != nil {
						fmt.Printf("Error retrieving dependency data for %s: %s\n", projectIDStr, err.Error())
						continue
					}
					depProjects = append(depProjects, depProject)
				}
				depProjectIDPendingQueue = depProjectIDPendingQueue[:0]

				for _, depProject := range depProjects {
					// Get latest version for dependency
					depLatestVersion, err := getLatestVersionForPlatform(depProject, selectedPlatform, pack)
					if err != nil {
						fmt.Printf("Failed to get latest version of dependency %v: %v\n", depProject.Name, err)
						continue
					}

					// Get project identifier for comparison
					depProjectIDStr := fmt.Sprintf("%d", depProject.ID)
					if depProject.Namespace.Owner != "" && depProject.Namespace.Slug != "" {
						depProjectIDStr = depProject.Namespace.Owner + "/" + depProject.Namespace.Slug
					}

					// Check for nested dependencies
					if depDeps, ok := depLatestVersion.PluginDependencies[selectedPlatform]; ok {
						for _, dep := range depDeps {
							if dep.Required && dep.ProjectID != nil {
								projectIDStr := fmt.Sprintf("%d", *dep.ProjectID)
								// Also check namespace format
								if !slices.Contains(installedProjects, projectIDStr) && !slices.Contains(installedProjects, depProjectIDStr) {
									found := false
									for _, queuedID := range depProjectIDPendingQueue {
										if queuedID == projectIDStr {
											found = true
											break
										}
									}
									if !found {
										for _, queuedDep := range depMetadata {
											queuedIDStr := fmt.Sprintf("%d", queuedDep.projectInfo.ID)
											if queuedDep.projectInfo.Namespace.Owner != "" && queuedDep.projectInfo.Namespace.Slug != "" {
												queuedIDStr = queuedDep.projectInfo.Namespace.Owner + "/" + queuedDep.projectInfo.Namespace.Slug
											}
											if queuedIDStr == projectIDStr {
												found = true
												break
											}
										}
									}
									if !found {
										depProjectIDPendingQueue = append(depProjectIDPendingQueue, projectIDStr)
									}
								}
							}
						}
					}

					depMetadata = append(depMetadata, depMetadataStore{
						projectInfo: depProject,
						versionInfo: depLatestVersion,
						platform:    selectedPlatform,
					})
				}

				cycles++
			}
			if cycles >= maxCycles {
				return errors.New("dependencies recurse too deeply, try increasing maxCycles")
			}

			if len(depMetadata) > 0 {
				fmt.Println("Dependencies found:")
				for _, v := range depMetadata {
					fmt.Println(v.projectInfo.Name)
				}

				if cmdshared.PromptYesNo("Would you like to add them? [Y/n]: ") {
					for _, v := range depMetadata {
						err := createFileMeta(v.projectInfo, v.versionInfo, v.platform, pack, index)
						if err != nil {
							return err
						}
						platformDownload := v.versionInfo.Downloads[v.platform]
						fmt.Printf("Dependency \"%s\" successfully added! (%s)\n", v.projectInfo.Name, platformDownload.FileInfo.Name)
					}
				}
			} else {
				fmt.Println("All dependencies are already added!")
			}
		}
	}

	// Create the metadata file
	err = createFileMeta(project, version, selectedPlatform, pack, index)
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

	fmt.Printf("Project \"%s\" successfully added! (%s)\n", project.Name, platformDownload.FileInfo.Name)
	return nil
}

func createFileMeta(project Project, version Version, platform string, pack core.Pack, index *core.Index) error {
	updateMap := make(map[string]map[string]interface{})

	platformDownload, ok := version.Downloads[platform]
	if !ok {
		return errors.New("version doesn't have a download for the selected platform")
	}

	// Prefer downloadUrl over externalUrl
	downloadURL := platformDownload.DownloadURL
	if downloadURL == "" {
		downloadURL = platformDownload.ExternalURL
	}
	if downloadURL == "" {
		return errors.New("no download URL available")
	}

	// Store only the slug in the TOML
	projectID := project.Namespace.Slug
	if projectID == "" {
		// Fallback to numeric ID if slug is empty
		projectID = fmt.Sprintf("%d", project.ID)
	}

	var err error
	updateMap["hangar"], err = hangarUpdateData{
		ProjectID: projectID,
		VersionID: version.Name,
		Platform:  platform,
	}.ToMap()
	if err != nil {
		return err
	}

	modMeta := core.Mod{
		Name:     project.Name,
		FileName: platformDownload.FileInfo.Name,
		Side:     core.UniversalSide,
		Download: core.ModDownload{
			URL:        downloadURL,
			HashFormat: "sha256",
			Hash:       platformDownload.FileInfo.SHA256Hash,
		},
		Update: updateMap,
	}

	var path string
	folder := viper.GetString("meta-folder")
	if folder == "" {
		folder = "plugins"
	}

	// Use project slug or slugify name
	slug := project.Namespace.Slug
	if slug == "" {
		slug = core.SlugifyName(project.Name)
	}
	path = modMeta.SetMetaPath(filepath.Join(viper.GetString("meta-folder-base"), folder, slug+core.MetaExtension))

	format, hash, err := modMeta.Write()
	if err != nil {
		return err
	}
	return index.RefreshFileWithHash(path, format, hash, true)
}

func getCompatiblePlatforms(pack core.Pack) ([]string, error) {
	var platforms []string

	// Check for plugin platforms in pack.Versions
	if _, hasPaper := pack.Versions["paper"]; hasPaper {
		platforms = append(platforms, "PAPER")
	}
	if _, hasVelocity := pack.Versions["velocity"]; hasVelocity {
		platforms = append(platforms, "VELOCITY")
	}
	if _, hasWaterfall := pack.Versions["waterfall"]; hasWaterfall {
		platforms = append(platforms, "WATERFALL")
	}

	return platforms, nil
}

func getLatestVersion(project Project, pack core.Pack) (Version, error) {
	platforms, err := getCompatiblePlatforms(pack)
	if err != nil {
		return Version{}, err
	}
	if len(platforms) == 0 {
		return Version{}, errors.New("no plugin platforms configured")
	}

	// Try each platform until we find a compatible version
	for _, platform := range platforms {
		version, err := getLatestVersionForPlatform(project, platform, pack)
		if err == nil && version.ID != 0 {
			return version, nil
		}
	}

	return Version{}, errors.New("no compatible version found for any platform")
}

func getLatestVersionForPlatform(project Project, platform string, pack core.Pack) (Version, error) {
	mcVersions, err := pack.GetSupportedMCVersions()
	if err != nil {
		return Version{}, err
	}

	// Use slug for project ID (unique and what /latest endpoint requires)
	projectID := project.Namespace.Slug
	if projectID == "" {
		// Fallback to numeric ID if slug is not available
		projectID = fmt.Sprintf("%d", project.ID)
	}

	// Get all versions
	versionsResult, err := hgDefaultClient.getVersions(projectID, 25, 0)
	if err != nil {
		return Version{}, fmt.Errorf("failed to fetch versions: %w", err)
	}

	if len(versionsResult.Result) == 0 {
		return Version{}, errors.New("project has no versions")
	}

	// Filter versions by platform and game version
	var compatibleVersions []Version
	for _, v := range versionsResult.Result {
		// Check if version has download for this platform
		if _, ok := v.Downloads[platform]; !ok {
			continue
		}

		// Check platform dependencies if they exist
		// If platform dependencies are specified, at least one should match
		// If no platform dependencies are specified, assume compatibility (version has download for platform)
		if platformDeps, ok := v.PlatformDependencies[platform]; ok && len(platformDeps) > 0 {
			// Check if any of the platform dependencies match our game versions
			matches := false
			for _, depVersion := range platformDeps {
				for _, mcVersion := range mcVersions {
					// Try exact match first
					if depVersion == mcVersion {
						matches = true
						break
					}
					// Try prefix match (e.g., "1.21" matches "1.21.10")
					if strings.HasPrefix(mcVersion, depVersion+".") || strings.HasPrefix(mcVersion, depVersion+"-") {
						matches = true
						break
					}
					// Try reverse prefix match (e.g., "1.21.10" matches "1.21")
					if strings.HasPrefix(depVersion, mcVersion+".") || strings.HasPrefix(depVersion, mcVersion+"-") {
						matches = true
						break
					}
					// Try major.minor match (e.g., "1.21" matches "1.21.10")
					depParts := strings.Split(depVersion, ".")
					mcParts := strings.Split(mcVersion, ".")
					if len(depParts) >= 2 && len(mcParts) >= 2 {
						if depParts[0] == mcParts[0] && depParts[1] == mcParts[1] {
							matches = true
							break
						}
					}
				}
				if matches {
					break
				}
			}
			// If platform dependencies are specified but don't match, still include as fallback
			// (The version has a download for the platform, so it might still work)
			if !matches {
				// Don't skip - include it anyway since it has a download
			}
		}
		// Include this version (has download for platform)
		compatibleVersions = append(compatibleVersions, v)
	}

	if len(compatibleVersions) == 0 {
		return Version{}, fmt.Errorf("no compatible versions found for platform %s and Minecraft version(s) %v", platform, mcVersions)
	}

	// Return the first one (they should be ordered by newest first)
	return compatibleVersions[0], nil
}

func getProjectIdentifier(project ProjectResult) string {
	// Use slug (unique and what /latest endpoint requires)
	if project.Namespace.Slug != "" {
		return project.Namespace.Slug
	}
	// Fallback to numeric ID if slug is not available
	return fmt.Sprintf("%d", project.ID)
}

func getInstalledProjectIDs(index *core.Index) []string {
	var installedProjects []string
	mods, err := index.LoadAllMods()
	if err != nil {
		fmt.Printf("Failed to determine existing projects: %v\n", err)
	} else {
		for _, mod := range mods {
			data, ok := mod.GetParsedUpdateData("hangar")
			if ok {
				updateData, ok := data.(hangarUpdateData)
				if ok {
					if len(updateData.ProjectID) > 0 {
						installedProjects = append(installedProjects, updateData.ProjectID)
					}
				}
			}
		}
	}
	return installedProjects
}

var versionFlag string

func init() {
	hangarCmd.AddCommand(installCmd)

	installCmd.Flags().StringVar(&versionFlag, "version", "", "The version to install (e.g., bukkit-2.6.5)")
}
