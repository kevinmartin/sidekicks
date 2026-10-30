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
	r := report{Images: map[string]imageRecord{}}
	for i, a := range adapters {
		r.Images[a] = fakeImages()[i]
		_ = os.WriteFile(filepath.Join(p, a+"-trivy.json"), []byte(`{"Results":[{}]}`), 0600)
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
