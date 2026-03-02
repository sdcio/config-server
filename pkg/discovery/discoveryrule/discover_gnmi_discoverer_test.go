/*
Copyright 2024 Nokia.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package discoveryrule

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/openconfig/gnmi/proto/gnmi"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/testhelper"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	DiscoveryVendorProfilePath = "../../../example/discoveryvendor-profile"
)

func TestGetDiscovererGNMI(t *testing.T) {
	cases := map[string]struct {
		capabilityFile   string
		expectedProvider string
		expectError      bool
	}{
		"NokiaSROS": {
			capabilityFile:   "data/nokia-sros-capabilities.json",
			expectedProvider: "sros.nokia.sdcio.dev",
		},
		"NokiaSRL": {
			capabilityFile:   "data/nokia-srl-capabilities.json",
			expectedProvider: "srl.nokia.sdcio.dev",
		},
		"Arista": {
			capabilityFile:   "data/arista-capabilities.json",
			expectedProvider: "eos.arista.sdcio.dev",
		},
		"Unknown": {
			capabilityFile:   "data/unknown-capabilities.json",
			expectedProvider: "eos.arista.sdcio.dev",
			expectError:      true,
		},
	}

	profiles, err := LoadDiscoveryProfiles(DiscoveryVendorProfilePath)
	if err != nil {
		t.Fatalf("Failed to load discovery profiles: %v", err)
	}
	dr := &dr{
		gnmiDiscoveryProfiles: profiles,
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// Load gNMI capability response
			capabilityResponse, err := getCapbilityResponse(tc.capabilityFile)
			if err != nil {
				t.Fatalf("Failed to load capability response: %v", err)
			}

			// Get discoverer
			discoverer, err := dr.getDiscovererGNMI(capabilityResponse)
			if err != nil {
				if tc.expectError {
					t.Logf("Expected error occurred: %v", err)
					return // Test passes since error was expected
				}
				t.Fatalf("getDiscovererGNMI failed: %v", err)
			}

			// If an error was expected but didn't occur
			if tc.expectError {
				t.Fatalf("Expected an error but got none")
			}

			// Check provider match
			if discoverer.Provider != tc.expectedProvider {
				t.Errorf("Expected provider %s, got %s", tc.expectedProvider, discoverer.Provider)
			}
		})
	}
}

func LoadDiscoveryProfiles(path string) (map[string]invv1alpha1.GnmiDiscoveryVendorProfileParameters, error) {

	discoveryProfiles := map[string]invv1alpha1.GnmiDiscoveryVendorProfileParameters{}

	addToScheme := invv1alpha1.AddToScheme

	gvk := invv1alpha1.SchemeGroupVersion.WithKind(invv1alpha1.DiscoveryVendorProfileKind)
	// build object from file

	// Walk through the directory
	err := filepath.WalkDir(path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Process only YAML files
		if d.IsDir() || (filepath.Ext(path) != ".yaml" && filepath.Ext(path) != ".yml") {
			return nil
		}

		obj := &invv1alpha1.DiscoveryVendorProfile{}
		if err := testhelper.GetKRMResource(path, obj, gvk, addToScheme); err != nil {
			return err
		}

		// Populate the map using the profile's metadata name as the key
		discoveryProfiles[obj.Name] = obj.Spec.Gnmi
		fmt.Printf("Loaded discovery profile: %s\n", obj.Name)

		return nil
	})

	return discoveryProfiles, err
}

func getCapbilityResponse(path string) (*gnmi.CapabilityResponse, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var capResponse gnmi.CapabilityResponse
	if err := protojson.Unmarshal(b, &capResponse); err != nil {
		return nil, err
	}
	return &capResponse, nil
}
