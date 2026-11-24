package misc

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func overrideAntigravityVersionURLsForTest(t *testing.T, hubManifestURL string) func() {
	t.Helper()

	oldHubManifest := antigravityHubLatestManifestURL
	antigravityHubLatestManifestURL = hubManifestURL

	return func() {
		antigravityHubLatestManifestURL = oldHubManifest
	}
}

func overrideAntigravityVersionCacheForTest(t *testing.T, version string, expiry time.Time) func() {
	t.Helper()

	antigravityVersionMu.Lock()
	oldVersion := cachedAntigravityVersion
	oldExpiry := antigravityVersionExpiry
	cachedAntigravityVersion = version
	antigravityVersionExpiry = expiry
	antigravityVersionMu.Unlock()

	return func() {
		antigravityVersionMu.Lock()
		cachedAntigravityVersion = oldVersion
		antigravityVersionExpiry = oldExpiry
		antigravityVersionMu.Unlock()
	}
}

func TestAntigravityLatestVersionUsesCurrentHubFallback(t *testing.T) {
	restore := overrideAntigravityVersionCacheForTest(t, "", time.Time{})
	defer restore()

	version := AntigravityLatestVersion()
	if version != antigravityFallbackVersion {
		t.Fatalf("AntigravityLatestVersion() = %q, want %q", version, antigravityFallbackVersion)
	}
}

// Cloud Code resolves newer models only for clients reporting at least 2.23.0;
// older versions get 404 Requested entity was not found.
func TestAntigravityFallbackVersionMeetsBackendFloor(t *testing.T) {
	const floorMajor, floorMinor = 1, 5

	var major, minor, patch int
	if _, err := fmt.Sscanf(antigravityFallbackVersion, "%d.%d.%d", &major, &minor, &patch); err != nil {
		t.Fatalf("antigravityFallbackVersion = %q is not a dotted version: %v", antigravityFallbackVersion, err)
	}
	if major < floorMajor || (major == floorMajor && minor < floorMinor) {
		t.Fatalf("antigravityFallbackVersion = %q, want at least %d.%d.0", antigravityFallbackVersion, floorMajor, floorMinor)
	}
}

func TestAntigravityUserAgentUsesHubFamily(t *testing.T) {
	for _, tc := range []struct {
		version string
		want    string
	}{
		{"1.5.14", "antigravity/cli/1.5.14 (aidev_client; " + antigravityHubPlatform + "; cl=1023160026; auth_method=consumer)"},
		{"1.5.15", "antigravity/cli/1.5.15 (aidev_client; " + antigravityHubPlatform + "; cl=1027820220; auth_method=consumer)"},
		{"1.5.16", "antigravity/cli/1.5.16 (aidev_client; " + antigravityHubPlatform + "; cl=1027820220; auth_method=consumer)"},
		{"1.5.17", "antigravity/cli/1.5.16 (aidev_client; " + antigravityHubPlatform + "; cl=1027820220; auth_method=consumer)"},
	} {
		t.Run(tc.version, func(t *testing.T) {
			restore := overrideAntigravityVersionCacheForTest(t, tc.version, time.Now().Add(time.Hour))
			defer restore()
			if got := AntigravityUserAgent(); got != tc.want {
				t.Fatalf("AntigravityUserAgent() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAntigravityUserAgentPinsUnmappedVersionUsingNumericSemverOrder(t *testing.T) {
	antigravityHubChangelists["2.100.0"] = "1099999999"
	defer delete(antigravityHubChangelists, "2.100.0")

	restore := overrideAntigravityVersionCacheForTest(t, "2.101.0", time.Now().Add(time.Hour))
	defer restore()

	want := "antigravity/cli/2.100.0 (aidev_client; " + antigravityHubPlatform + "; cl=1099999999; auth_method=consumer)"
	if got := AntigravityUserAgent(); got != want {
		t.Fatalf("AntigravityUserAgent() = %q, want %q", got, want)
	}
}

func TestAntigravityFallbackVersionHasMatchingChangelist(t *testing.T) {
	if antigravityFallbackVersion != "1.5.15" {
		t.Fatalf("antigravityFallbackVersion = %q, want %q", antigravityFallbackVersion, "1.5.15")
	}
	if antigravityHubChangelists[antigravityFallbackVersion] == "" {
		t.Fatal("fallback version must be updated together with its verified changelist")
	}
}

func TestAntigravityVersionFromUserAgentParsesHubFamily(t *testing.T) {
	for _, userAgent := range []string{
		"antigravity/cli/1.5.15 linux/amd64",
		"antigravity/cli/1.5.15 (aidev_client; os_type=linux; arch=amd64; cl=1027820220)",
		"antigravity/cli/1.5.15 (aidev_client; os_type=linux; arch=amd64; cl=1027820220) google-api-nodejs-client/10.3.0",
	} {
		if got := AntigravityVersionFromUserAgent(userAgent); got != "1.5.15" {
			t.Fatalf("AntigravityVersionFromUserAgent(%q) = %q, want %q", userAgent, got, "1.5.15")
		}
	}
}

func TestAntigravityVersionFromUserAgentParsesLegacyFamily(t *testing.T) {
	if got := AntigravityVersionFromUserAgent("antigravity/1.23.2 windows/amd64"); got != "1.23.2" {
		t.Fatalf("AntigravityVersionFromUserAgent() = %q, want %q", got, "1.23.2")
	}
}

func TestAntigravityVersionFromUserAgentFallbackMatchesPinnedHubVersion(t *testing.T) {
	restore := overrideAntigravityVersionCacheForTest(t, "2.35.0", time.Now().Add(time.Hour))
	defer restore()

	for _, ua := range []string{"", "custom-client/1.0", "antigravity/cli/ "} {
		if got := AntigravityVersionFromUserAgent(ua); got != "1.5.16" {
			t.Fatalf("AntigravityVersionFromUserAgent(%q) = %q, want %q", ua, got, "1.5.16")
		}
	}
}

func TestAntigravityLoadCodeAssistUserAgentUsesShortUA(t *testing.T) {
	restore := overrideAntigravityVersionCacheForTest(t, "1.5.16", time.Now().Add(time.Hour))
	defer restore()

	want := "antigravity/cli/1.5.16 (aidev_client; " + antigravityHubPlatform + "; cl=1027820220; auth_method=consumer)"
	if got := AntigravityLoadCodeAssistUserAgent(""); got != want {
		t.Fatalf("AntigravityLoadCodeAssistUserAgent() = %q, want %q", got, want)
	}
	if got := AntigravityLoadCodeAssistUserAgent(want); got != want {
		t.Fatalf("AntigravityLoadCodeAssistUserAgent(configured) = %q, want %q", got, want)
	}
}

func TestAntigravityOnboardUserUserAgentUsesLongUA(t *testing.T) {
	restore := overrideAntigravityVersionCacheForTest(t, "1.5.16", time.Now().Add(time.Hour))
	defer restore()

	want := "antigravity/cli/1.5.16 (aidev_client; " + antigravityHubPlatform + "; cl=1027820220; auth_method=consumer) google-api-nodejs-client/10.3.0"
	if got := AntigravityOnboardUserUserAgent(""); got != want {
		t.Fatalf("AntigravityOnboardUserUserAgent() = %q, want %q", got, want)
	}
}

func TestFetchAntigravityLatestVersionUsesHubManifest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hub/latest-arm64-mac.yml":
			if got := r.Header.Get("User-Agent"); got != "electron-builder" {
				t.Errorf("hub manifest User-Agent = %q, want %q", got, "electron-builder")
			}
			if got := r.Header.Get("Cache-Control"); got != "no-cache" {
				t.Errorf("hub manifest Cache-Control = %q, want %q", got, "no-cache")
			}
			w.Header().Set("Content-Type", "application/yaml")
			_, _ = w.Write([]byte("version: 2.2.1\npath: Antigravity-arm64-mac.zip\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	restore := overrideAntigravityVersionURLsForTest(t, server.URL+"/hub/latest-arm64-mac.yml")
	defer restore()

	version, errFetch := fetchAntigravityLatestVersion(context.Background())
	if errFetch != nil {
		t.Fatalf("fetchAntigravityLatestVersion() error = %v", errFetch)
	}
	if version != "2.2.1" {
		t.Fatalf("fetchAntigravityLatestVersion() = %q, want %q", version, "2.2.1")
	}
}

func TestFetchAntigravityLatestVersionReturnsHubManifestError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "temporary outage", http.StatusInternalServerError)
	}))
	defer server.Close()

	restore := overrideAntigravityVersionURLsForTest(t, server.URL+"/hub/latest-arm64-mac.yml")
	defer restore()

	_, errFetch := fetchAntigravityLatestVersion(context.Background())
	if errFetch == nil {
		t.Fatal("fetchAntigravityLatestVersion() error = nil, want error")
	}
}
