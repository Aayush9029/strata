package projectinfo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

type Info struct {
	AppName     string
	Description string
	BundleID    string
	AppStoreID  string
	Catalogs    []string
	Discover    []string
	SourceRoots []string
	Terms       []string
}

func Infer(root string) Info {
	info := Info{}
	if root == "" {
		root = "."
	}
	root, _ = filepath.Abs(root)
	info.Catalogs = discoverFiles(root, ".xcstrings", skipDir)
	info.Discover = discoverRoots(info.Catalogs, root)
	info.SourceRoots = discoverSourceRoots(root)
	info.AppName = inferAppName(root)
	info.BundleID = inferBundleID(root)
	applyAppStoreLookup(&info)
	info.Terms = []string{"Apple"}
	if info.AppName != "" {
		info.Terms = append([]string{info.AppName}, info.Terms...)
	}
	return info
}

func inferAppName(root string) string {
	for _, project := range discoverFiles(root, ".xcodeproj", skipNestedProjectDir) {
		data, err := os.ReadFile(filepath.Join(project, "project.pbxproj"))
		if err != nil {
			continue
		}
		if name := firstBuildSetting(string(data), "INFOPLIST_KEY_CFBundleDisplayName"); name != "" {
			return name
		}
	}
	for _, plist := range discoverFiles(root, "Info.plist", skipDir) {
		data, err := os.ReadFile(plist)
		if err != nil {
			continue
		}
		if name := plistString(data, "CFBundleDisplayName"); name != "" && !strings.Contains(name, "$(") {
			return name
		}
		if name := plistString(data, "CFBundleName"); name != "" && !strings.Contains(name, "$(") {
			return name
		}
	}
	return ""
}

func inferBundleID(root string) string {
	for _, project := range discoverFiles(root, ".xcodeproj", skipNestedProjectDir) {
		data, err := os.ReadFile(filepath.Join(project, "project.pbxproj"))
		if err != nil {
			continue
		}
		if id := preferredBundleID(string(data)); id != "" {
			return id
		}
	}
	for _, plist := range discoverFiles(root, "Info.plist", skipDir) {
		data, err := os.ReadFile(plist)
		if err != nil {
			continue
		}
		if id := plistString(data, "CFBundleIdentifier"); id != "" && !strings.Contains(id, "$(") {
			return id
		}
	}
	return ""
}

func preferredBundleID(project string) string {
	var fallback string
	for _, block := range strings.Split(project, "buildSettings = {") {
		block = strings.SplitN(block, "};", 2)[0]
		id := firstBuildSetting(block, "PRODUCT_BUNDLE_IDENTIFIER")
		if id == "" || strings.Contains(strings.ToLower(id), "test") {
			continue
		}
		if fallback == "" {
			fallback = id
		}
		displayName := firstBuildSetting(block, "INFOPLIST_KEY_CFBundleDisplayName")
		skipInstall := firstBuildSetting(block, "SKIP_INSTALL")
		if displayName != "" && !strings.EqualFold(skipInstall, "YES") {
			return id
		}
	}
	return fallback
}

func applyAppStoreLookup(info *Info) {
	if info.BundleID == "" {
		return
	}
	client := &http.Client{Timeout: 2 * time.Second}
	url := fmt.Sprintf("https://itunes.apple.com/lookup?bundleId=%s&entity=software", info.BundleID)
	response, err := client.Get(url)
	if err != nil {
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return
	}
	var lookup struct {
		Results []struct {
			TrackID     int    `json:"trackId"`
			TrackName   string `json:"trackName"`
			Description string `json:"description"`
		} `json:"results"`
	}
	if json.NewDecoder(response.Body).Decode(&lookup) != nil || len(lookup.Results) == 0 {
		return
	}
	result := lookup.Results[0]
	if info.AppName == "" {
		info.AppName = strings.TrimSpace(result.TrackName)
	}
	if info.Description == "" {
		info.Description = strings.TrimSpace(result.Description)
	}
	if result.TrackID != 0 {
		info.AppStoreID = fmt.Sprint(result.TrackID)
	}
}

func firstBuildSetting(project, key string) string {
	pattern := regexp.MustCompile(regexp.QuoteMeta(key) + `\s*=\s*("[^"]+"|[^";]+);`)
	match := pattern.FindStringSubmatch(project)
	if len(match) < 2 {
		return ""
	}
	return strings.Trim(strings.TrimSpace(match[1]), `"`)
}

func plistString(data []byte, key string) string {
	var object map[string]any
	if json.Unmarshal(data, &object) == nil {
		if value, ok := object[key].(string); ok {
			return strings.TrimSpace(value)
		}
	}
	text := string(data)
	pattern := regexp.MustCompile(`<key>` + regexp.QuoteMeta(key) + `</key>\s*<string>([^<]+)</string>`)
	match := pattern.FindStringSubmatch(text)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func discoverRoots(paths []string, root string) []string {
	seen := map[string]bool{}
	var roots []string
	for _, path := range paths {
		dir := filepath.Dir(path)
		rel, err := filepath.Rel(root, dir)
		if err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			dir = rel
		}
		if !seen[dir] {
			seen[dir] = true
			roots = append(roots, dir)
		}
	}
	slices.Sort(roots)
	return roots
}

func discoverSourceRoots(root string) []string {
	candidates := []string{}
	entries, err := os.ReadDir(root)
	if err != nil {
		return candidates
	}
	for _, entry := range entries {
		if !entry.IsDir() || skipDir(entry.Name()) {
			continue
		}
		name := entry.Name()
		if name == "Sources" || strings.HasSuffix(name, "Kit") || strings.HasSuffix(name, "Scan") || strings.HasSuffix(name, "Controls") {
			candidates = append(candidates, name)
			continue
		}
		sourcePath := filepath.Join(root, name, "Sources")
		if stat, err := os.Stat(sourcePath); err == nil && stat.IsDir() {
			candidates = append(candidates, filepath.Join(name, "Sources"))
		}
	}
	slices.Sort(candidates)
	return candidates
}

func discoverFiles(root, suffix string, skip func(string) bool) []string {
	var paths []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && skip(name) {
				return filepath.SkipDir
			}
			if strings.HasSuffix(name, suffix) {
				paths = append(paths, path)
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, suffix) {
			rel, err := filepath.Rel(root, path)
			if err == nil && !strings.HasPrefix(rel, "..") {
				paths = append(paths, rel)
			} else {
				paths = append(paths, path)
			}
		}
		return nil
	})
	slices.Sort(paths)
	return paths
}

func skipNestedProjectDir(name string) bool {
	return skipDir(name) && !strings.HasSuffix(name, ".xcodeproj")
}

func skipDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "build", "DerivedData", "node_modules", "Pods", "Carthage", ".build", ".derived", ".macro-build":
		return true
	default:
		return false
	}
}
