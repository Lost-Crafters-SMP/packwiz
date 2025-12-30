package maven

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/BurntSushi/toml"
	"github.com/spf13/viper"
)

const (
	mavenCentralURL = "https://repo1.maven.org/maven2"
	defaultPriority = 999
)

// MavenConfig represents the maven.toml configuration file
type MavenConfig struct {
	MavenCentral MavenCentralConfig `toml:"maven-central"`
	Repositories []RepositoryConfig  `toml:"repositories"`
}

// MavenCentralConfig represents the built-in Maven Central configuration
type MavenCentralConfig struct {
	Enabled  bool `toml:"enabled"`
	Priority int  `toml:"priority"`
}

// RepositoryConfig represents a custom repository configuration
type RepositoryConfig struct {
	Name     string `toml:"name"`
	URL      string `toml:"url"`
	Priority int    `toml:"priority"`
}

// Repository represents a repository that can be used for artifact resolution
type Repository struct {
	Name     string
	URL      string
	Priority int
	IsCentral bool
}

// LoadMavenConfig loads and parses maven.toml from the same directory as pack.toml
func LoadMavenConfig() (MavenConfig, error) {
	var config MavenConfig

	packFile := viper.GetString("pack-file")
	packDir := filepath.Dir(packFile)
	mavenTomlPath := filepath.Join(packDir, "maven.toml")

	// Check if maven.toml exists
	if _, err := os.Stat(mavenTomlPath); os.IsNotExist(err) {
		// File doesn't exist - return default config (Maven Central enabled)
		config.MavenCentral.Enabled = true
		config.MavenCentral.Priority = 1
		return config, nil
	}

	// Parse the file
	_, err := toml.DecodeFile(mavenTomlPath, &config)
	if err != nil {
		return config, fmt.Errorf("failed to parse maven.toml: %w", err)
	}

	// Set defaults if not specified
	// We need to detect if the [maven-central] section existed in the file
	// If both Enabled is false and Priority is 0, the section likely didn't exist
	// In that case, we default to enabled=true, priority=1
	// Otherwise, if priority is 0 but enabled might be set, default priority to 1
	sectionExists := config.MavenCentral.Enabled || config.MavenCentral.Priority != 0

	if !sectionExists {
		// Section didn't exist - use defaults
		config.MavenCentral.Enabled = true
		config.MavenCentral.Priority = 1
	} else if config.MavenCentral.Priority == 0 {
		// Section exists but priority not set - default to 1
		config.MavenCentral.Priority = 1
	}

	return config, nil
}

// GetRepositories returns a sorted list of repositories based on priority and tie-breaking rules
func GetRepositories(config MavenConfig) []Repository {
	var repos []Repository

	// Add Maven Central if enabled
	// The enabled flag is already set correctly in LoadMavenConfig
	enabled := config.MavenCentral.Enabled

	if enabled {
		repos = append(repos, Repository{
			Name:       "Maven Central",
			URL:        mavenCentralURL,
			Priority:   config.MavenCentral.Priority,
			IsCentral:  true,
		})
	}

	// Add custom repositories
	for _, repoConfig := range config.Repositories {
		priority := repoConfig.Priority
		if priority == 0 {
			priority = defaultPriority
		}
		repos = append(repos, Repository{
			Name:      repoConfig.Name,
			URL:       repoConfig.URL,
			Priority:  priority,
			IsCentral: false,
		})
	}

	// Sort repositories
	sort.Slice(repos, func(i, j int) bool {
		// Primary sort: by priority
		if repos[i].Priority != repos[j].Priority {
			return repos[i].Priority < repos[j].Priority
		}
		// Tie-breaking: Maven Central always comes first
		if repos[i].IsCentral && !repos[j].IsCentral {
			return true
		}
		if !repos[i].IsCentral && repos[j].IsCentral {
			return false
		}
		// Both are non-central with same priority - maintain original order (stable sort)
		return i < j
	})

	return repos
}

