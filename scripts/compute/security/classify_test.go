package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestClassificationDedupVersionsAndNoSuppression verifies classification dedup versions and no suppression.
func TestClassificationDedupVersionsAndNoSuppression(t *testing.T) {
	inputs := map[string][]byte{
		"shell":        []byte(`{"Results":[{"Target":"debian","Class":"os-pkgs","Type":"debian","Vulnerabilities":[{"VulnerabilityID":"CVE-A","PkgName":"one","InstalledVersion":"1","Severity":"HIGH","Status":"affected","SeveritySource":"nvd","PrimaryURL":"https://vendor.test/A"},{"VulnerabilityID":"CVE-B","PkgName":"two","InstalledVersion":"2","FixedVersion":"3","Severity":"CRITICAL","Status":"fixed"},{"VulnerabilityID":"CVE-LOW","PkgName":"low","Severity":"LOW"}]}]}`),
		"desktop-neko": []byte(`{"Results":[{"Target":"usr/bin/neko","Class":"lang-pkgs","Type":"gobinary","Vulnerabilities":[{"VulnerabilityID":"CVE-A","PkgName":"go/module","InstalledVersion":"v1","FixedVersion":"v2","Severity":"MEDIUM","Status":"fixed"}]}]}`),
	}
	result, err := classifyFindings(inputs)
	if err != nil {
		t.Fatal(err)
	}
	if result.DistinctHighCritical != 2 || result.DistinctCritical != 1 || result.WithFixedVersion != 2 || result.NoFixedVersion != 0 {
		t.Fatalf("incorrect classification %+v", result)
	}
	if result.Findings[0].FixClassification != "MIXED_FIXED_VERSION_REPORTING" || len(result.Findings[0].Occurrences) != 2 {
		t.Fatal("cross-image versions suppressed")
	}
	inputs = map[string][]byte{"shell": []byte(`{"Results":[{"Vulnerabilities":[{"VulnerabilityID":"CVE-A","Severity":"HIGH"}]}]}`)}
	result, err = classifyFindings(inputs)
	if err != nil || result.NoFixedVersion != 1 || result.Findings[0].FixClassification != "NO_FIXED_VERSION_REPORTED" {
		t.Fatal("unfixed distinction lost")
	}
	if _, err = classifyFindings(map[string][]byte{"bad": []byte(`bad`)}); err == nil {
		t.Fatal("invalid raw accepted")
	}
}

// TestClassifyPreservesOriginalAndRefusesOverwrite verifies classify preserves original and refuses overwrite.
func TestClassifyPreservesOriginalAndRefusesOverwrite(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "artifacts/compute/release/fixture")
	_ = os.MkdirAll(p, 0700)
	r := report{Mode: "verify", ImagesUnchanged: true, Database: check{Status: "COMPLETE"}, Images: map[string]imageRecord{}}
	for i, a := range adapters {
		r.Images[a] = fakeImages()[i]
		id := r.Images[a].ID
		r.Scans = append(r.Scans, imageScan{Adapter: a, ImageID: id, ObservedImageID: id, Check: check{Status: "COMPLETE", Artifact: a + "-trivy.json"}, OS: check{Status: "COMPLETE"}, UpstreamNeko: check{Status: "COMPLETE"}})
		_ = os.WriteFile(filepath.Join(p, a+"-trivy.json"), []byte(`{"ArtifactName":"`+id+`","Metadata":{"ImageID":"`+id+`"},"Results":[{}]}`), 0600)
	}
	raw, _ := json.Marshal(r)
	_ = os.WriteFile(filepath.Join(p, "report.json"), raw, 0600)
	if err := classifyDirectory(root, p); err != nil {
		t.Fatal(err)
	}
	if err := classifyDirectory(root, p); err == nil {
		t.Fatal("existing classification overwritten")
	}
	after, _ := os.ReadFile(filepath.Join(p, "report.json"))
	if string(raw) != string(after) {
		t.Fatal("source report changed")
	}
	if err := classifyDirectory(root, filepath.Join(root, "outside")); err == nil {
		t.Fatal("outside directory accepted")
	}
	result, _ := os.ReadFile(filepath.Join(p, "high-critical-classification.json"))
	if !strings.Contains(string(result), "No fixed version") {
		t.Fatal("limits absent")
	}
}

// TestClassifyRejectsMismatchedRawImage verifies raw evidence matches its report identity.
func TestClassifyRejectsMismatchedRawImage(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "artifacts/compute/release/fixture")
	if err := os.MkdirAll(p, 0700); err != nil {
		t.Fatal(err)
	}
	r := report{Mode: "verify", ImagesUnchanged: true, Database: check{Status: "COMPLETE"}, Images: map[string]imageRecord{}}
	for i, adapter := range adapters {
		image := fakeImages()[i]
		r.Images[adapter] = image
		r.Scans = append(r.Scans, imageScan{Adapter: adapter, ImageID: image.ID, ObservedImageID: image.ID, Check: check{Status: "COMPLETE", Artifact: adapter + "-trivy.json"}, OS: check{Status: "COMPLETE"}, UpstreamNeko: check{Status: "COMPLETE"}})
		id := image.ID
		if adapter == "shell" {
			id = fakeImages()[1].ID
		}
		if err := os.WriteFile(filepath.Join(p, adapter+"-trivy.json"), []byte(`{"ArtifactName":"`+id+`","Metadata":{"ImageID":"`+id+`"},"Results":[{}]}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, "report.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := classifyDirectory(root, p); err == nil {
		t.Fatal("accepted a mismatched raw image scan")
	}
}

// TestClassifyRejectsIncompleteEvidenceAndEscapingSymlinks verifies evidence boundaries.
func TestClassifyRejectsIncompleteEvidenceAndEscapingSymlinks(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "artifacts/compute/release")
	if err := os.MkdirAll(parent, 0700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(parent, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := classifyDirectory(root, filepath.Join(parent, "escape")); err == nil {
		t.Fatal("accepted symlink outside release root")
	}
	inside := filepath.Join(parent, "incomplete")
	if err := os.Mkdir(inside, 0700); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(report{Images: map[string]imageRecord{"shell": fakeImages()[0]}})
	if err := os.WriteFile(filepath.Join(inside, "report.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := classifyDirectory(root, inside); err == nil {
		t.Fatal("accepted incomplete report")
	}
}

// TestClassifyRejectsSymlinkedReleaseRoot verifies the release root cannot escape the repository.
func TestClassifyRejectsSymlinkedReleaseRoot(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "artifacts", "compute")
	if err := os.MkdirAll(parent, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(parent, "release")); err != nil {
		t.Fatal(err)
	}
	if err := classifyDirectory(root, filepath.Join(parent, "release", "fixture")); err == nil {
		t.Fatal("accepted a release root outside the repository")
	}
}
