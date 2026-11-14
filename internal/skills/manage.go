package skills

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/swadhinbiswas/ghost/internal/config"
	"github.com/swadhinbiswas/ghost/internal/home"
)

const maxSkillBundleSize = 12 << 20

// MarketplaceSkill is one searchable skills.sh result.
type MarketplaceSkill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"summary"`
	Source      string `json:"source"`
	Slug        string `json:"slug"`
	InstallURL  string `json:"installUrl"`
	URL         string `json:"url"`
	Installs    int    `json:"installs"`
	Catalog     string `json:"catalog"`
}

type githubRepo struct {
	DefaultBranch string `json:"default_branch"`
}

type githubTree struct {
	Truncated bool `json:"truncated"`
	Tree      []struct {
		Path string `json:"path"`
		Mode string `json:"mode"`
		Type string `json:"type"`
	} `json:"tree"`
}

// AgentSkill pairs a local skill with its source agent.
type AgentSkill struct {
	Skill     *Skill
	Agent     string
	Installed bool
}

// ManagedDir returns Ghost's default skill installation directory.
func ManagedDir() string {
	return config.GlobalSkillsDirs()[0]
}

// ValidName reports whether a skill name can be used as a directory name.
func ValidName(name string) bool {
	return len(name) <= MaxNameLength && namePattern.MatchString(name)
}

// SearchMarketplace searches the skills.sh catalog.
func SearchMarketplace(ctx context.Context, query string) ([]MarketplaceSkill, error) {
	query = strings.TrimSpace(query)
	if len([]rune(query)) < 2 {
		return nil, nil
	}

	endpoint := "https://skills.sh/api/search?q=" + url.QueryEscape(query)
	var result struct {
		Skills []MarketplaceSkill `json:"skills"`
	}
	if err := getJSON(ctx, endpoint, &result); err != nil {
		return nil, err
	}
	for i := range result.Skills {
		result.Skills[i].Catalog = "skills.sh"
	}
	return result.Skills, nil
}

// SearchClawHub searches the public ClawHub skill catalog.
func SearchClawHub(ctx context.Context, query string) ([]MarketplaceSkill, error) {
	query = strings.TrimSpace(query)
	if len([]rune(query)) < 2 {
		return nil, nil
	}
	endpoint := "https://clawhub.ai/api/v1/search?q=" + url.QueryEscape(query) + "&limit=40&nonSuspiciousOnly=true"
	var result struct {
		Results []struct {
			Slug        string `json:"slug"`
			Name        string `json:"displayName"`
			Description string `json:"summary"`
			Owner       string `json:"ownerHandle"`
			Installs    int    `json:"installs"`
		} `json:"results"`
	}
	if err := getJSON(ctx, endpoint, &result); err != nil {
		return nil, err
	}
	items := make([]MarketplaceSkill, 0, len(result.Results))
	for _, found := range result.Results {
		id := found.Slug
		if found.Owner != "" {
			id = found.Owner + "/" + found.Slug
		}
		items = append(items, MarketplaceSkill{
			ID:          id,
			Name:        found.Name,
			Description: found.Description,
			Source:      found.Owner,
			Slug:        found.Slug,
			Installs:    found.Installs,
			Catalog:     "clawhub",
		})
	}
	return items, nil
}

// InstallMarketplace installs a skills.sh result from its public GitHub source.
func InstallMarketplace(ctx context.Context, root, id string) (*Skill, error) {
	if strings.HasPrefix(id, "official/") {
		return InstallGitHubSkill(ctx, root, "hermes-agent-org/hermes/optional-skills/"+strings.TrimPrefix(id, "official/"))
	}
	if strings.HasPrefix(id, "skills-sh/") {
		id = strings.TrimPrefix(id, "skills-sh/")
	}
	parts := strings.Split(id, "/")
	if len(parts) < 3 {
		return nil, errors.New("invalid marketplace skill ID")
	}
	for _, part := range parts[:2] {
		if !validGitHubPart(part) {
			return nil, errors.New("invalid marketplace skill source")
		}
	}
	for _, part := range parts[2:] {
		if part == "" || part == "." || part == ".." {
			return nil, errors.New("invalid marketplace skill ID")
		}
	}
	return InstallGitHubSkill(ctx, root, strings.Join(parts, "/"))
}

// InstallClawHub installs a public skill archive or its GitHub source.
func InstallClawHub(ctx context.Context, root, slug string) (*Skill, error) {
	owner := ""
	if parts := strings.SplitN(slug, "/", 2); len(parts) == 2 {
		owner, slug = parts[0], parts[1]
	}
	if slug == "" || strings.ContainsAny(slug, "/?#\\") || slug == "." || slug == ".." || (owner != "" && !validGitHubPart(owner)) {
		return nil, errors.New("invalid ClawHub skill slug")
	}
	query := url.Values{"slug": {slug}}
	if owner != "" {
		query.Set("ownerHandle", owner)
	}
	endpoint := "https://clawhub.ai/api/v1/download?" + query.Encode()
	data, contentType, err := getFile(ctx, endpoint, maxSkillBundleSize)
	if err != nil {
		return nil, err
	}
	if strings.Contains(contentType, "json") {
		var source struct {
			SourceRef string `json:"sourceRef"`
			Repo      string `json:"repo"`
			Commit    string `json:"commit"`
			Path      string `json:"path"`
		}
		if err := json.Unmarshal(data, &source); err != nil {
			return nil, fmt.Errorf("reading ClawHub source: %w", err)
		}
		if source.SourceRef != "public-github" || source.Commit == "" {
			return nil, errors.New("ClawHub returned no installable skill archive")
		}
		return InstallGitHubSkill(ctx, root, "https://github.com/"+source.Repo+"/tree/"+url.PathEscape(source.Commit)+"/"+escapeGitHubPath(source.Path))
	}
	if !strings.Contains(contentType, "zip") && !bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		return nil, errors.New("ClawHub returned an unsupported skill archive")
	}
	return installZip(root, data)
}

// InstallGitHubSkill installs a skill folder from a public GitHub repository.
func InstallGitHubSkill(ctx context.Context, root, source string) (*Skill, error) {
	owner, repo, ref, dir, err := parseGitHubSkillSource(source)
	if err != nil {
		return nil, err
	}
	base := "https://api.github.com/repos/" + owner + "/" + repo
	if ref == "" {
		var repoInfo githubRepo
		if err := getJSON(ctx, base, &repoInfo); err != nil {
			return nil, err
		}
		ref = repoInfo.DefaultBranch
	}
	if ref == "" {
		return nil, errors.New("GitHub repository has no default branch")
	}

	var tree githubTree
	if err := getJSON(ctx, base+"/git/trees/"+url.PathEscape(ref)+"?recursive=1", &tree); err != nil {
		return nil, err
	}
	if tree.Truncated {
		return nil, errors.New("GitHub repository tree is too large to search")
	}
	var dirs []string
	for _, entry := range tree.Tree {
		if entry.Type != "blob" || entry.Mode == "120000" || filepath.Base(entry.Path) != SkillFileName {
			continue
		}
		skillDir := path.Dir(entry.Path)
		if skillDir == "." {
			skillDir = ""
		}
		if dir == "" || dir == skillDir || dir == entry.Path || path.Base(dir) == path.Base(skillDir) {
			dirs = append(dirs, skillDir)
		}
	}
	dirs = slices.Compact(dirs)
	if len(dirs) == 0 {
		return nil, errors.New("no SKILL.md found at that GitHub path")
	}
	if len(dirs) > 1 {
		return nil, errors.New("repository has multiple skills; enter the path to one skill folder")
	}
	skillDir := dirs[0]
	files := make([]marketplaceFile, 0)
	total := 0
	for _, entry := range tree.Tree {
		if entry.Type != "blob" || entry.Mode == "120000" {
			continue
		}
		if skillDir == "" {
			if strings.Contains(entry.Path, "/") {
				continue
			}
		} else if !strings.HasPrefix(entry.Path, skillDir+"/") {
			continue
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(entry.Path, skillDir), "/")
		if !filepath.IsLocal(filepath.FromSlash(rel)) || strings.Contains(rel, "\\") {
			return nil, fmt.Errorf("invalid GitHub skill file path %q", entry.Path)
		}
		if len(files) >= 500 {
			return nil, errors.New("skill bundle has too many files")
		}
		raw := "https://raw.githubusercontent.com/" + owner + "/" + repo + "/" + escapeGitHubPath(ref)
		if skillDir != "" {
			raw += "/" + escapeGitHubPath(skillDir)
		}
		raw += "/" + escapeGitHubPath(rel)
		contents, err := getBytes(ctx, raw, maxSkillBundleSize)
		if err != nil {
			return nil, err
		}
		total += len(contents)
		if total > maxSkillBundleSize {
			return nil, errors.New("skill bundle is too large")
		}
		files = append(files, marketplaceFile{Path: rel, Contents: string(contents)})
	}
	return installFiles(root, files)
}

// DiscoverAgentSkills finds skills in common coding-agent directories.
func DiscoverAgentSkills(workingDir, managedRoot string) []AgentSkill {
	homeDir := home.Dir()
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(homeDir, ".config")
	}
	roots := []struct {
		path  string
		agent string
	}{
		{filepath.Join(homeDir, ".agents", "skills"), "Codex / shared agents"},
		{filepath.Join(configHome, "agents", "skills"), "Shared agents"},
		{filepath.Join(homeDir, ".claude", "skills"), "Claude Code"},
		{filepath.Join(homeDir, ".codex", "skills"), "Codex"},
		{filepath.Join(homeDir, ".hermes", "skills"), "Hermes"},
		{filepath.Join(homeDir, ".openclaw", "skills"), "OpenClaw"},
		{filepath.Join(homeDir, ".opencode", "skills"), "OpenCode"},
		{filepath.Join(configHome, "opencode", "skills"), "OpenCode"},
		{filepath.Join(configHome, "goose", "skills"), "Goose"},
		{filepath.Join(homeDir, ".amp", "skills"), "Amp"},
		{filepath.Join(homeDir, ".kilocode", "skills"), "Kilo Code"},
		{filepath.Join(homeDir, ".copilot", "skills"), "GitHub Copilot"},
		{filepath.Join(homeDir, ".qwen", "skills"), "Qwen Code"},
		{filepath.Join(homeDir, ".continue", "skills"), "Continue"},
		{filepath.Join(homeDir, ".cursor", "skills"), "Cursor"},
		{filepath.Join(homeDir, ".gemini", "skills"), "Gemini CLI"},
		{filepath.Join(workingDir, ".openclaw", "skills"), "Project OpenClaw"},
		{filepath.Join(workingDir, ".agents", "skills"), "Project agents"},
		{filepath.Join(workingDir, ".claude", "skills"), "Project Claude Code"},
		{filepath.Join(workingDir, ".codex", "skills"), "Project Codex"},
		{filepath.Join(workingDir, ".hermes", "skills"), "Project Hermes"},
		{filepath.Join(workingDir, ".opencode", "skills"), "Project OpenCode"},
		{filepath.Join(workingDir, ".kilocode", "skills"), "Project Kilo Code"},
		{filepath.Join(workingDir, ".roo", "skills"), "Project Roo Code"},
		{filepath.Join(workingDir, ".windsurf", "skills"), "Project Windsurf"},
		{filepath.Join(workingDir, ".cline", "skills"), "Project Cline"},
		{filepath.Join(workingDir, ".copilot", "skills"), "Project GitHub Copilot"},
		{filepath.Join(workingDir, ".qwen", "skills"), "Project Qwen Code"},
		{filepath.Join(workingDir, ".continue", "skills"), "Project Continue"},
		{filepath.Join(workingDir, ".cursor", "skills"), "Project Cursor"},
		{filepath.Join(workingDir, ".gemini", "skills"), "Project Gemini CLI"},
	}
	if runtime.GOOS == "windows" {
		appData := os.Getenv("LOCALAPPDATA")
		if appData != "" {
			roots = append(roots, struct {
				path  string
				agent string
			}{filepath.Join(appData, "agents", "skills"), "Shared agents"})
		}
	}

	managed, _ := filepath.Abs(managedRoot)
	seen := make(map[string]struct{})
	found := make([]AgentSkill, 0)
	for _, root := range roots {
		rootPath, err := filepath.Abs(root.path)
		if err != nil || rootPath == managed {
			continue
		}
		for _, skill := range Discover([]string{root.path}) {
			path, err := filepath.EvalSymlinks(skill.SkillFilePath)
			if err != nil {
				continue
			}
			path, _ = filepath.Abs(path)
			if _, ok := seen[path]; ok {
				continue
			}
			seen[path] = struct{}{}
			target := filepath.Join(managedRoot, skill.Name)
			info, statErr := os.Lstat(target)
			installed := statErr == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0
			found = append(found, AgentSkill{Skill: skill, Agent: root.agent, Installed: installed})
		}
	}

	slices.SortFunc(found, func(a, b AgentSkill) int {
		if order := strings.Compare(strings.ToLower(a.Skill.Name), strings.ToLower(b.Skill.Name)); order != 0 {
			return order
		}
		return strings.Compare(a.Agent, b.Agent)
	})
	return found
}

// CopySkill imports one local skill directory.
func CopySkill(root, source string) (*Skill, error) {
	skill, err := Parse(filepath.Join(source, SkillFileName))
	if err != nil {
		return nil, err
	}
	if err := skill.Validate(); err != nil {
		return nil, err
	}
	if err := copyDir(root, skill.Path, skill.Name); err != nil {
		return nil, err
	}
	return skill, nil
}

// Uninstall removes a skill from Ghost's managed directory.
func Uninstall(root, name string) error {
	if !ValidName(name) {
		return errors.New("invalid skill name")
	}
	rootPath, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	target := filepath.Join(rootPath, name)
	info, err := os.Lstat(target)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("skill install path is not a directory")
	}
	return os.RemoveAll(target)
}

func getJSON(ctx context.Context, endpoint string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Ghost")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("skill source request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("skill source returned %s", resp.Status)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxSkillBundleSize)).Decode(target); err != nil {
		return fmt.Errorf("reading skill source response: %w", err)
	}
	return nil
}

func getBytes(ctx context.Context, endpoint string, limit int64) ([]byte, error) {
	data, _, err := getFile(ctx, endpoint, limit)
	return data, err
}

func getFile(ctx context.Context, endpoint string, limit int64) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "Ghost")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("skill download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, "", fmt.Errorf("skill source returned %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(data)) > limit {
		return nil, "", errors.New("skill bundle is too large")
	}
	return data, resp.Header.Get("Content-Type"), nil
}

func installZip(root string, data []byte) (*Skill, error) {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("reading ClawHub archive: %w", err)
	}
	if len(archive.File) > 500 {
		return nil, errors.New("skill archive has too many files")
	}
	var skillDirs []string
	for _, file := range archive.File {
		if filepath.Base(file.Name) != SkillFileName || file.Mode()&os.ModeSymlink != 0 {
			continue
		}
		dir := path.Dir(file.Name)
		if dir == "." {
			dir = ""
		}
		skillDirs = append(skillDirs, dir)
	}
	skillDirs = slices.Compact(skillDirs)
	if len(skillDirs) != 1 {
		return nil, errors.New("ClawHub archive must contain one skill folder")
	}
	skillDir := skillDirs[0]
	files := make([]marketplaceFile, 0, len(archive.File))
	var total uint64
	for _, file := range archive.File {
		if file.FileInfo().IsDir() || file.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if skillDir == "" {
			if strings.Contains(file.Name, "/") {
				continue
			}
		} else if !strings.HasPrefix(file.Name, skillDir+"/") {
			continue
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(file.Name, skillDir), "/")
		if !filepath.IsLocal(filepath.FromSlash(rel)) || strings.Contains(rel, "\\") {
			return nil, fmt.Errorf("invalid skill archive path %q", file.Name)
		}
		total += file.UncompressedSize64
		if total > maxSkillBundleSize {
			return nil, errors.New("skill archive is too large")
		}
		reader, err := file.Open()
		if err != nil {
			return nil, err
		}
		contents, readErr := io.ReadAll(io.LimitReader(reader, maxSkillBundleSize+1))
		closeErr := reader.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		files = append(files, marketplaceFile{Path: rel, Contents: string(contents)})
	}
	return installFiles(root, files)
}

func parseGitHubSkillSource(source string) (owner, repo, ref, dir string, err error) {
	source = strings.TrimSpace(strings.TrimPrefix(source, "github:"))
	if strings.HasPrefix(source, "https://github.com/") {
		parsed, parseErr := url.Parse(source)
		if parseErr != nil {
			return "", "", "", "", parseErr
		}
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(parts) < 2 {
			return "", "", "", "", errors.New("use a GitHub owner/repo/path")
		}
		owner, repo = parts[0], strings.TrimSuffix(parts[1], ".git")
		if len(parts) > 2 {
			switch parts[2] {
			case "tree", "blob":
				if len(parts) < 4 {
					return "", "", "", "", errors.New("GitHub URL needs a branch name")
				}
				ref = parts[3]
				dir = strings.Join(parts[4:], "/")
			default:
				dir = strings.Join(parts[2:], "/")
			}
		}
	} else {
		parts := strings.Split(strings.Trim(source, "/"), "/")
		if len(parts) < 2 {
			return "", "", "", "", errors.New("use owner/repo or owner/repo/path")
		}
		owner, repo = parts[0], strings.TrimSuffix(parts[1], ".git")
		dir = strings.Join(parts[2:], "/")
	}
	for _, part := range []string{owner, repo} {
		if part == "" || part == "." || part == ".." || !validGitHubPart(part) {
			return "", "", "", "", errors.New("invalid GitHub repository path")
		}
	}
	if dir != "" && !filepath.IsLocal(filepath.FromSlash(dir)) {
		return "", "", "", "", errors.New("invalid GitHub skill folder path")
	}
	return owner, repo, ref, dir, nil
}

func validGitHubPart(part string) bool {
	for _, char := range part {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '-' || char == '_' || char == '.' {
			continue
		}
		return false
	}
	return true
}

func escapeGitHubPath(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

type marketplaceFile struct {
	Path     string
	Contents string
}

func installFiles(root string, files []marketplaceFile) (*Skill, error) {
	if len(files) > 500 {
		return nil, errors.New("skill bundle has too many files")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp(root, ".skill-install-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)

	var total int
	for _, file := range files {
		if !filepath.IsLocal(filepath.FromSlash(file.Path)) {
			return nil, fmt.Errorf("invalid skill file path %q", file.Path)
		}
		if file.Path == "" || strings.Contains(file.Path, "\\") {
			return nil, fmt.Errorf("invalid skill file path %q", file.Path)
		}
		total += len(file.Contents)
		if total > maxSkillBundleSize {
			return nil, errors.New("skill bundle is too large")
		}
		path := filepath.Join(stage, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(file.Contents), 0o644); err != nil {
			return nil, err
		}
	}

	skill, err := Parse(filepath.Join(stage, SkillFileName))
	if err != nil {
		return nil, err
	}
	skill.Path = filepath.Join(root, skill.Name)
	if err := skill.Validate(); err != nil {
		return nil, err
	}
	target := filepath.Join(root, skill.Name)
	if _, err := os.Lstat(target); err == nil {
		return nil, fmt.Errorf("skill %q is already installed", skill.Name)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if err := os.Rename(stage, target); err != nil {
		return nil, err
	}
	skill.Path = target
	skill.SkillFilePath = filepath.Join(target, SkillFileName)
	return skill, nil
}

func copyDir(root, source, name string) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	target := filepath.Join(root, name)
	if _, err := os.Lstat(target); err == nil {
		return fmt.Errorf("skill %q is already installed", name)
	} else if !os.IsNotExist(err) {
		return err
	}
	stage, err := os.MkdirTemp(root, ".skill-import-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	var total int64
	err = filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("skill contains a symbolic link: %s", path)
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		dest := filepath.Join(stage, rel)
		if entry.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		total += info.Size()
		if total > maxSkillBundleSize {
			return errors.New("skill folder is too large")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		mode := info.Mode().Perm() & 0o755
		if mode == 0 {
			mode = 0o644
		}
		return os.WriteFile(dest, data, mode)
	})
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(stage, SkillFileName)); err != nil {
		return err
	}
	return os.Rename(stage, target)
}
