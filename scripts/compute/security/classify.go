package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type classifiedOccurrence struct {
	Adapter          string          `json:"adapter"`
	Target           string          `json:"target"`
	Class            string          `json:"class"`
	PackageType      string          `json:"packageType"`
	Package          string          `json:"package"`
	InstalledVersion string          `json:"installedVersion"`
	FixedVersion     string          `json:"fixedVersion,omitempty"`
	FixAvailability  string          `json:"fixAvailability"`
	Status           string          `json:"vendorStatus"`
	Severity         string          `json:"reportedSeverity"`
	SeveritySource   string          `json:"severitySource,omitempty"`
	DataSource       json.RawMessage `json:"dataSource,omitempty"`
	PrimaryURL       string          `json:"primaryURL,omitempty"`
}
type classifiedFinding struct {
	ID                string                 `json:"id"`
	Title             string                 `json:"title"`
	MaximumSeverity   string                 `json:"maximumReportedSeverity"`
	FixClassification string                 `json:"fixClassification"`
	Occurrences       []classifiedOccurrence `json:"occurrences"`
}
type classification struct {
	Scope                string              `json:"scope"`
	DistinctHighCritical int                 `json:"distinctHighCritical"`
	DistinctCritical     int                 `json:"distinctCritical"`
	WithFixedVersion     int                 `json:"withAnyFixedVersionReported"`
	NoFixedVersion       int                 `json:"withoutFixedVersionReported"`
	Findings             []classifiedFinding `json:"findings"`
	Limits               []string            `json:"limits"`
}

func classifyDirectory(root, name string) error {
	abs, err := filepath.Abs(name)
	if err != nil {
		return err
	}
	parent := filepath.Join(root, "artifacts/compute/release")
	rel, err := filepath.Rel(parent, abs)
	if name == "" || err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("classify requires existing evidence directory beneath artifacts/compute/release")
	}
	repositoryRoot, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer repositoryRoot.Close()
	releaseRoot, err := repositoryRoot.OpenRoot("artifacts/compute/release")
	if err != nil {
		return errors.New("invalid release evidence root")
	}
	defer releaseRoot.Close()
	evidence, err := releaseRoot.OpenRoot(rel)
	if err != nil {
		return errors.New("invalid release evidence directory")
	}
	defer evidence.Close()
	raw, err := evidence.ReadFile("report.json")
	if err != nil {
		return err
	}
	var source report
	if json.Unmarshal(raw, &source) != nil || source.Mode != "verify" || !source.ImagesUnchanged || source.Database.Status != "COMPLETE" || len(source.Images) != len(adapters) || len(source.Scans) != len(adapters) {
		return errors.New("invalid original release report")
	}
	seen := make(map[string]bool, len(adapters))
	seenIDs := make(map[string]bool, len(adapters))
	for _, scan := range source.Scans {
		image, ok := source.Images[scan.Adapter]
		if !ok || seen[scan.Adapter] || seenIDs[image.ID] || !imagePattern.MatchString(image.ID) || scan.ImageID != image.ID || scan.ObservedImageID != image.ID || scan.Check.Status != "COMPLETE" || scan.OS.Status != "COMPLETE" || scan.Check.Artifact != scan.Adapter+"-trivy.json" || (scan.Adapter == "desktop-neko" && scan.UpstreamNeko.Status != "COMPLETE") {
			return errors.New("incomplete or mismatched image scan evidence")
		}
		seen[scan.Adapter] = true
		seenIDs[image.ID] = true
	}
	inputs := map[string][]byte{}
	for _, a := range adapters {
		if !seen[a] {
			return errors.New("required image scan is missing")
		}
		b, e := evidence.ReadFile(a + "-trivy.json")
		if e != nil {
			return e
		}
		if len(b) > 32<<20 {
			return errors.New("oversized scanner evidence")
		}
		var identity struct {
			ArtifactName string
			Metadata     struct{ ImageID string }
		}
		if json.Unmarshal(b, &identity) != nil || identity.ArtifactName != source.Images[a].ID || identity.Metadata.ImageID != source.Images[a].ID {
			return errors.New("raw scanner evidence does not match selected image")
		}
		inputs[a] = b
	}
	result, err := classifyFindings(inputs)
	if err != nil {
		return err
	}
	output, err := evidence.OpenFile("high-critical-classification.json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("refusing to overwrite existing classification")
	}
	defer output.Close()
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}
func classifyFindings(inputs map[string][]byte) (classification, error) {
	result := classification{Scope: "Distinct IDs with at least one HIGH/CRITICAL package-advisory record; all observed occurrences retained", Limits: []string{
		"Installed versions are the scanned versions, not a complete affected-version range. Consult linked vendor records for ranges/applicability.",
		"No fixed version reported does not establish that a vulnerability is unfixable, exploitable, or ignored by upstream; it means this advisory snapshot supplied no fixed package version.",
		"Version-matched package findings are not reachable-code or exploit validation. Severity sources and vendor statuses remain explicit.",
		"Raw scan artifacts are unchanged; no upgrades, exceptions, suppressions or image mutations were performed by classification.",
	}}
	type vulnerability struct {
		ID                                                                                           string `json:"VulnerabilityID"`
		Title, PkgName, InstalledVersion, FixedVersion, Status, Severity, SeveritySource, PrimaryURL string
		DataSource                                                                                   json.RawMessage
	}
	type document struct {
		Results []struct {
			Target, Class, Type string
			Vulnerabilities     []vulnerability
		}
	}
	docs := map[string]document{}
	selected := map[string]bool{}
	for adapter, raw := range inputs {
		var doc document
		if json.Unmarshal(raw, &doc) != nil || len(doc.Results) == 0 {
			return result, errors.New("invalid raw scan evidence")
		}
		docs[adapter] = doc
		for _, r := range doc.Results {
			for _, v := range r.Vulnerabilities {
				if v.Severity == "HIGH" || v.Severity == "CRITICAL" {
					selected[v.ID] = true
				}
			}
		}
	}
	groups := map[string]*classifiedFinding{}
	for adapter, doc := range docs {
		for _, r := range doc.Results {
			for _, v := range r.Vulnerabilities {
				if !selected[v.ID] {
					continue
				}
				finding := groups[v.ID]
				if finding == nil {
					finding = &classifiedFinding{ID: v.ID, Title: v.Title, MaximumSeverity: "HIGH"}
					groups[v.ID] = finding
				}
				if v.Severity == "CRITICAL" {
					finding.MaximumSeverity = "CRITICAL"
				}
				fix := "NO_FIXED_VERSION_REPORTED"
				if v.FixedVersion != "" {
					fix = "FIX_VERSION_REPORTED"
				}
				finding.Occurrences = append(finding.Occurrences, classifiedOccurrence{Adapter: adapter, Target: r.Target, Class: r.Class, PackageType: r.Type, Package: v.PkgName, InstalledVersion: v.InstalledVersion, FixedVersion: v.FixedVersion, FixAvailability: fix, Status: v.Status, Severity: v.Severity, SeveritySource: v.SeveritySource, DataSource: v.DataSource, PrimaryURL: v.PrimaryURL})
			}
		}
	}
	ids := make([]string, 0, len(groups))
	for id := range groups {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		finding := groups[id]
		available, missing := false, false
		sort.Slice(finding.Occurrences, func(i, j int) bool {
			a, b := finding.Occurrences[i], finding.Occurrences[j]
			x, _ := json.Marshal(a)
			y, _ := json.Marshal(b)
			return string(x) < string(y)
		})
		for _, o := range finding.Occurrences {
			if o.FixedVersion != "" {
				available = true
			} else {
				missing = true
			}
		}
		switch {
		case available && missing:
			finding.FixClassification = "MIXED_FIXED_VERSION_REPORTING"
		case available:
			finding.FixClassification = "FIXED_VERSION_REPORTED"
		default:
			finding.FixClassification = "NO_FIXED_VERSION_REPORTED"
		}
		if available {
			result.WithFixedVersion++
		} else {
			result.NoFixedVersion++
		}
		if finding.MaximumSeverity == "CRITICAL" {
			result.DistinctCritical++
		}
		result.Findings = append(result.Findings, *finding)
	}
	result.DistinctHighCritical = len(result.Findings)
	return result, nil
}
