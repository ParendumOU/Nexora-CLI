// Package selfupdate checks the public GitHub releases for a newer NexoraCLI and can
// replace the running binary in place (`nexora update`). It talks only to the GitHub
// REST API + release asset CDN, never to a Nexora instance, so it works regardless of
// which instance (if any) is configured.
package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Repo is the public GitHub repository that ships the release binaries.
const (
	repoOwner  = "ParendumOU"
	repoName   = "Nexora-CLI"
	latestURL  = "https://api.github.com/repos/" + repoOwner + "/" + repoName + "/releases/latest"
	envDisable = "NEXORA_NO_UPDATE_CHECK"
)

// Asset is one downloadable file attached to a release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// Release is the subset of the GitHub release payload we care about.
type Release struct {
	Tag    string  `json:"tag_name"`
	Assets []Asset `json:"assets"`
}

// CheckEnabled reports whether the launch-time update check should run: skip dev builds
// (unversioned) and honor an opt-out env var so scripted/offline use is never nagged.
func CheckEnabled(current string) bool {
	if os.Getenv(envDisable) != "" {
		return false
	}
	return current != "" && current != "dev" && current != "0.0.0"
}

// Latest fetches the newest published release.
func Latest(ctx context.Context) (*Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github releases API returned %s", resp.Status)
	}
	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	if rel.Tag == "" {
		return nil, fmt.Errorf("no tag_name in latest release")
	}
	return &rel, nil
}

// AssetName is the release asset for this host, matching the names the build/CI upload:
// nexora-<tag>-<goos>-<goarch>[.exe] (e.g. nexora-v0.4.1-linux-amd64).
func AssetName(tag string) string {
	name := fmt.Sprintf("nexora-%s-%s-%s", tag, runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// asset returns the download URL for this host's binary in the release, or "".
func (r *Release) asset() (Asset, bool) {
	want := AssetName(r.Tag)
	for _, a := range r.Assets {
		if a.Name == want {
			return a, true
		}
	}
	return Asset{}, false
}

// IsNewer reports whether latest is a strictly higher semver than current. A "dev" or
// unparsable current is treated as 0.0.0 so an update is always offered.
func IsNewer(current, latest string) bool {
	if latest == "" {
		return false
	}
	c := parseSemver(current)
	l := parseSemver(latest)
	for i := 0; i < 3; i++ {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

func parseSemver(v string) [3]int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	// drop any pre-release/build suffix (e.g. 1.2.3-rc1)
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	var out [3]int
	for i, p := range strings.SplitN(v, ".", 3) {
		if i > 2 {
			break
		}
		n, _ := strconv.Atoi(strings.TrimSpace(p))
		out[i] = n
	}
	return out
}

// Apply downloads this host's binary from the release and replaces the running
// executable in place. It returns the path that was updated. On Windows the running
// image cannot be overwritten, so the current file is renamed aside first (removed on
// the next launch via CleanupOld).
func Apply(ctx context.Context, rel *Release) (string, error) {
	a, ok := rel.asset()
	if !ok {
		return "", fmt.Errorf("release %s has no binary for %s/%s (%s)",
			rel.Tag, runtime.GOOS, runtime.GOARCH, AssetName(rel.Tag))
	}

	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil {
		exe = resolved
	}
	dir := filepath.Dir(exe)

	// Download into the target directory so the final rename stays on one filesystem.
	tmp, err := os.CreateTemp(dir, ".nexora-update-*")
	if err != nil {
		return "", fmt.Errorf("cannot write to %s (need write permission to self-update): %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed into place

	n, err := download(ctx, a.URL, tmp)
	tmp.Close()
	if err != nil {
		return "", err
	}
	if a.Size > 0 && n != a.Size {
		return "", fmt.Errorf("downloaded %d bytes, expected %d — aborting", n, a.Size)
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return "", err
	}

	if runtime.GOOS == "windows" {
		old := exe + ".old"
		_ = os.Remove(old)
		if err := os.Rename(exe, old); err != nil {
			return "", fmt.Errorf("cannot move current binary aside: %w", err)
		}
		if err := os.Rename(tmpName, exe); err != nil {
			_ = os.Rename(old, exe) // roll back
			return "", fmt.Errorf("cannot install new binary: %w", err)
		}
		return exe, nil
	}

	if err := os.Rename(tmpName, exe); err != nil {
		return "", fmt.Errorf("cannot install new binary: %w", err)
	}
	return exe, nil
}

// CleanupOld removes a leftover <exe>.old from a previous Windows self-update. Best
// effort; call once at startup.
func CleanupOld() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil {
		exe = resolved
	}
	_ = os.Remove(exe + ".old")
}

func download(ctx context.Context, url string, w io.Writer) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := http.DefaultClient.Do(req) // follows the CDN redirect automatically
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("download failed: %s", resp.Status)
	}
	return io.Copy(w, resp.Body)
}

// DefaultCheckInterval throttles the launch-time check so it hits the network at most
// once per day; the cached result drives the header hint in between.
const DefaultCheckInterval = 24 * time.Hour
