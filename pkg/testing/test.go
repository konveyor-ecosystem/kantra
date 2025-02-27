package testing

import (
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	"github.com/konveyor/analyzer-lsp/output/v1/konveyor"
	"github.com/konveyor/analyzer-lsp/provider"
)

const (
	RULESET_TEST_CONFIG_GOLDEN_FILE = "testing-config.yaml"
)

type TestsFile struct {
	// RulesPath is an optional path to respective rules file
	RulesPath string `yaml:"rulesPath,omitempty" json:"rulesPath,omitempty"`
	// Providers is a list of configs with each item containing config specific to a provider
	Providers []ProviderConfig `yaml:"providers,omitempty" json:"providers,omitempty"`
	// Tests is a list of tests with each item defining one or more test cases specific to a rule
	Tests []Test `yaml:"tests,omitempty" json:"tests,omitempty"`
	Path  string `yaml:"-" json:"-"`
}

type ProviderConfig struct {
	// Name is the name of the provider this config applies to
	Name string `yaml:"name" json:"name"`
	// DataPath is a relative path to test data to be used for this provider
	DataPath string `yaml:"dataPath" json:"dataPath"`
}

type Test struct {
	// RuleID is the ID of the rule this test applies to
	RuleID string `yaml:"ruleID" json:"ruleID"`
	// TestCases is a list of distinct test cases for this rule
	TestCases []TestCase `yaml:"testCases" json:"testCases"`
}

type TestCase struct {
	// Name is a unique name for this test case
	Name string `yaml:"name" json:"name"`
	// AnalysisParams is analysis parameters to be used when running this test case
	AnalysisParams `yaml:"analysisParams,omitempty" json:"analysisParams,omitempty"`
	// IsUnmatched passes test case when the rule is not matched
	IsUnmatched bool `yaml:"isUnmatched,omitempty" json:"isUnmatched,omitempty"`
	// HasIncidents defines criteria to pass the test case based on incidents for this rule
	HasIncidents *IncidentVerification `yaml:"hasIncidents,omitempty" json:"hasIncidents,omitempty"`
	// HasInsights defines criteria to pass the test case based on insights for this rule
	HasInsights *InsightVerification `yaml:"hasInsights,omitempty" json:"hasInsights,omitempty"`
	// HasTags passes test case when all of the given tags are generated
	HasTags []string `yaml:"hasTags,omitempty" json:"hasTags,omitempty"`
	RuleID  string   `yaml:"-" json:"-"`
}

type AnalysisParams struct {
	// Mode analysis mode to use when running the test, one of - source-only, full
	Mode provider.AnalysisMode `yaml:"mode,omitempty" json:"mode,omitempty"`
	// DepLabelSelector dependency label selector to use when running the test
	DepLabelSelector string `yaml:"depLabelSelector,omitempty" json:"depLabelSelector,omitempty"`
}

// IncidentVerification defines criterias to pass a test case.
// Only one of CountBased or LocationBased can be defined at a time.
type IncidentVerification struct {
	// CountBased defines a simple test case passing criteria based on count of incidents
	CountBased *CountBasedVerification `yaml:",inline,omitempty" json:",inline,omitempty"`
	// LocationBased defines a detailed test case passing criteria based on each incident
	LocationBased *LocationBasedVerification `yaml:",inline,omitempty" json:",inline,omitempty"`
}

// InsightVerification defines criterias to pass a test case.
// Only one of CountBased or LocationBased can be defined at a time.
type InsightVerification struct {
	// CountBased defines a simple test case passing criteria based on count of insights
	CountBased *CountBasedVerification `yaml:",inline,omitempty" json:",inline,omitempty"`
	// LocationBased defines a detailed test case passing criteria based on each incident
	LocationBased *LocationBasedVerification `yaml:",inline,omitempty" json:",inline,omitempty"`
}

func (i *IncidentVerification) MarshalYAML() (interface{}, error) {
	if i.CountBased != nil {
		return i.CountBased, nil
	}
	return i.LocationBased, nil
}

func (i *InsightVerification) MarshalYAML() (interface{}, error) {
	return i.CountBased, nil
}

// CountBasedVerification defines test case passing criteria based on count of incidents.
// Only one of exactly, atLeast, or atMost can be defined at a time.
type CountBasedVerification struct {
	// Exactly pass test case when there are exactly this many incidents
	Exactly *int `yaml:"exactly,omitempty" json:"exactly,omitempty"`
	// AtLeast pass test case when there are this many or more incidents
	AtLeast *int `yaml:"atLeast,omitempty" json:"atLeast,omitempty"`
	// AtMost pass test case when there are no more than this many incidents
	AtMost *int `yaml:"atMost,omitempty" json:"atMost,omitempty"`
	// MessageMatches pass test case when all incidents contain this message
	MessageMatches *string `yaml:"messageMatches,omitempty" json:"messageMatches,omitempty"`
	// CodeSnipMatches pass test case when all incidents contain this code snip
	CodeSnipMatches *string `yaml:"codeSnipMatches,omitempty" json:"codeSnipMatches,omitempty"`
}

type LocationBasedVerification struct {
	// Locations defines detailed conditions for each incident
	Locations []LocationVerification `yaml:"locations" json:"locations"`
}

// LocationVerification defines test case passing criteria based on detailed information in an incident.
// FileURI and LineNumber are required.
type LocationVerification struct {
	// FileURI is the file in which incident is supposed to be found
	FileURI *string `yaml:"fileURI,omitempty" json:"fileURI,omitempty"`
	// LineNumber is the line number where incident is supposed to be found
	LineNumber *int `yaml:"lineNumber,omitempty" json:"lineNumber,omitempty"`
	// MessageMatches is the message that's supposed to be contained within the message of this incident
	MessageMatches *string `yaml:"messageMatches,omitempty" json:"messageMatches,omitempty"`
	// CodeSnipMatches is the code snippet which is supposed to be present within the codeSnip of this incident
	CodeSnipMatches *string `yaml:"codeSnipMatches,omitempty" json:"codeSnipMatches,omitempty"`
}

func (t TestsFile) Validate() error {
	for idx, prov := range t.Providers {
		if err := prov.Validate(); err != nil {
			return fmt.Errorf("providers[%d] - %s", idx, err.Error())
		}
	}
	for _, test := range t.Tests {
		if err := test.Validate(); err != nil {
			return fmt.Errorf("%s#%s", test.RuleID, err.Error())
		}
	}
	return nil
}

func (p ProviderConfig) Validate() error {
	if p.Name == "" {
		return fmt.Errorf("'name' cannot be empty")
	}
	if p.DataPath == "" {
		return fmt.Errorf("'dataPath' cannot be empty")
	}
	return nil
}

func (t Test) Validate() error {
	if t.RuleID == "" {
		return fmt.Errorf("'ruleID' cannot be empty")
	}
	for _, tc := range t.TestCases {
		if err := tc.Validate(); err != nil {
			return fmt.Errorf("%s - %s", tc.Name, err.Error())
		}
	}
	return nil
}

func (t TestCase) Validate() error {
	if t.HasIncidents != nil {
		if err := t.HasIncidents.Validate(); err != nil {
			return err
		}
	}
	if t.Name == "" {
		return fmt.Errorf("'name' cannot be empty")
	}
	return nil
}

func (a AnalysisParams) Validate() error {
	if a.Mode != "" && a.Mode != provider.FullAnalysisMode &&
		a.Mode != provider.SourceOnlyAnalysisMode {
		return fmt.Errorf("mode must be either %s or %s",
			provider.FullAnalysisMode, provider.SourceOnlyAnalysisMode)
	}
	return nil
}

func (t IncidentVerification) Validate() error {
	if t.CountBased == nil && t.LocationBased == nil {
		return fmt.Errorf(
			"exactly one of the following properties of hasIncidents must be defined - 'exactly', 'atLeast', 'atMost' or 'locations'")
	}
	if t.CountBased != nil && t.LocationBased != nil {
		return fmt.Errorf(
			"properties 'exactly', 'atLeast', 'atMost' and 'locations' are mutually exclusive")
	}
	if t.LocationBased != nil {
		if t.LocationBased.Locations == nil {
			return fmt.Errorf(
				"at least one location must be defined under 'hasIncidents.locations'")
		} else {
			for idx, loc := range t.LocationBased.Locations {
				err := loc.Validate()
				if err != nil {
					return fmt.Errorf("locations[%d] - %s", idx, err.Error())
				}
			}
		}

	}
	if t.CountBased != nil {
		total := 0
		if t.CountBased.AtLeast != nil {
			total += 1
		}
		if t.CountBased.AtMost != nil {
			total += 1
		}
		if t.CountBased.Exactly != nil {
			total += 1
		}
		if total > 1 {
			return fmt.Errorf("properties 'exactly', 'atMost', 'atLeast' are mutually exclusive")
		}
	}
	return nil
}

func (t InsightVerification) Validate() error {
	if t.CountBased == nil {
		return fmt.Errorf(
			"exactly one of the following properties of hasIncidents must be defined - 'exactly', 'atLeast', 'atMost' or 'locations'")
	}
	if t.CountBased != nil {
		total := 0
		if t.CountBased.AtLeast != nil {
			total += 1
		}
		if t.CountBased.AtMost != nil {
			total += 1
		}
		if t.CountBased.Exactly != nil {
			total += 1
		}
		if total > 1 {
			return fmt.Errorf("properties 'exactly', 'atMost', 'atLeast' are mutually exclusive")
		}
	}
	return nil
}

func (l LocationVerification) Validate() error {
	if l.FileURI == nil {
		return fmt.Errorf("'hasIncidents.fileURI' must be defined")
	}
	if l.LineNumber == nil {
		return fmt.Errorf("'lineNumber' must be defined")
	}
	return nil
}

type Violation interface {
	GetCountBased() *CountBasedVerification
	GetLocationBased() *LocationBasedVerification
}

func (t TestCase) Verify(output konveyor.RuleSet) []string {
	failures := []string{}
	violation, violationExists := output.Violations[t.RuleID]
	insight, insightExists := output.Insights[t.RuleID]

	// violations and insights are essentially the same: a container for incidents
	var incidents []konveyor.Incident
	if violationExists {
		incidents = violation.Incidents
	} else if insightExists {
		incidents = insight.Incidents
	}

	existsInUnmatched := false
	for _, unmatchd := range output.Unmatched {
		if unmatchd == t.RuleID {
			existsInUnmatched = true
		}
	}
	if t.IsUnmatched && (violationExists || insightExists || !existsInUnmatched) {
		failures = append(failures, "expected rule to not match but matched")
		return failures
	}
	if !t.IsUnmatched && existsInUnmatched {
		failures = append(failures, "expected rule to match but unmatched")
		return failures
	}
	for _, expectedTag := range t.HasTags {
		found := false
		for _, foundTag := range output.Tags {
			if foundTag == expectedTag {
				found = true
				break
			}
			if r, err := regexp.Compile(expectedTag); err == nil && r.MatchString(foundTag) {
				found = true
				break
			}
		}
		if !found {
			failures = append(failures, fmt.Sprintf("expected tag %s not found", expectedTag))
		}
	}

	compareMessageOrCodeSnip := func(with string, pattern string) bool {
		if r, err := regexp.Compile(pattern); err == nil &&
			!r.MatchString(with) {
			return false
		}
		if !strings.Contains(with, pattern) {
			return false
		}
		return true
	}

	if t.HasIncidents != nil || t.HasInsights != nil {
		var countBased *CountBasedVerification
		var locationBased *LocationBasedVerification
		if t.HasIncidents != nil {
			countBased = t.HasIncidents.CountBased
			locationBased = t.HasIncidents.LocationBased
		} else if t.HasInsights != nil {
			countBased = t.HasInsights.CountBased
			locationBased = t.HasInsights.LocationBased
		}

		if locationBased != nil {
			for _, loc := range locationBased.Locations {
				foundIncidentsInFile := []konveyor.Incident{}
				for idx := range incidents {
					incident := &incidents[idx]
					if strings.HasSuffix(string(incident.URI), filepath.Clean(*loc.FileURI)) {
						foundIncidentsInFile = append(foundIncidentsInFile, *incident)
					}
				}
				if len(foundIncidentsInFile) == 0 {
					failures = append(failures, fmt.Sprintf("expected incident in file %s not found", filepath.Clean(*loc.FileURI)))
					continue
				}
				foundIncident := konveyor.Incident{}
				lineNumberFound := false
				for _, inc := range foundIncidentsInFile {
					if reflect.DeepEqual(inc.LineNumber, loc.LineNumber) {
						lineNumberFound = true
						foundIncident = inc
						break
					}
				}
				if !lineNumberFound {
					failures = append(failures,
						fmt.Sprintf("expected incident in %s on line number %d not found",
							*loc.FileURI, *loc.LineNumber))
					continue
				}
				if loc.CodeSnipMatches != nil {
					if !compareMessageOrCodeSnip(foundIncident.CodeSnip, *loc.CodeSnipMatches) {
						failures = append(failures, fmt.Sprintf(
							"expected code snip to match pattern `%s`, got `%s`",
							*loc.CodeSnipMatches, foundIncident.CodeSnip))
						continue
					}
				}
				if loc.MessageMatches != nil {
					if !compareMessageOrCodeSnip(foundIncident.Message, *loc.MessageMatches) {
						failures = append(failures, fmt.Sprintf(
							"expected code snip to match pattern `%s`, got `%s`",
							*loc.MessageMatches, foundIncident.Message))
						continue
					}
				}
			}
		}
		if countBased != nil {
			if countBased.Exactly != nil && *countBased.Exactly != len(incidents) {
				return append(failures,
					fmt.Sprintf("expected exactly %d incidents, got %d",
						*countBased.Exactly, len(incidents)))
			}
			if countBased.AtLeast != nil && *countBased.AtLeast > len(incidents) {
				return append(failures,
					fmt.Sprintf("expected at least %d incidents, got %d",
						*countBased.AtLeast, len(incidents)))
			}
			if countBased.AtMost != nil && *countBased.AtMost < len(incidents) {
				return append(failures,
					fmt.Sprintf("expected at most %d incidents, got %d",
						*countBased.AtMost, len(incidents)))
			}
		}
	}
	return failures
}
